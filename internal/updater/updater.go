// Package updater implements opt-in self-update for the Hopper binaries. It polls
// the project's GitHub Releases, and when a newer semver tag is published it
// downloads the matching asset for this OS/arch, verifies its SHA-256 against the
// release's checksums.txt, atomically replaces the running executable, and
// re-execs into the new version.
//
// The trust model is: releases are published by the repository's CI to GitHub,
// fetched over HTTPS, and integrity-checked against the signed-by-TLS
// checksums.txt. For stronger guarantees, add minisign/cosign verification.
package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Updater self-updates a single binary from GitHub releases.
type Updater struct {
	Repo       string // "owner/repo", e.g. "mcpeixoto/hopper"
	BinaryName string // "hopperd" or "hopper-agent"
	Current    string // current version, e.g. "v0.1.0" or "dev"
	HTTP       *http.Client
}

// New returns an Updater for the given binary.
func New(repo, binaryName, current string) *Updater {
	return &Updater{
		Repo:       repo,
		BinaryName: binaryName,
		Current:    current,
		HTTP:       &http.Client{Timeout: 60 * time.Second},
	}
}

// assetName is the release asset filename for this binary on this platform,
// matching the Makefile `dist` target, e.g. "hopperd_linux_amd64".
func (u *Updater) assetName() string {
	return fmt.Sprintf("%s_%s_%s", u.BinaryName, runtime.GOOS, runtime.GOARCH)
}

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// latest fetches the latest release metadata.
func (u *Updater) latest(ctx context.Context) (release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", u.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("github releases: %s", resp.Status)
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return release{}, err
	}
	return rel, nil
}

// CheckOnce checks for a newer release and updates+re-execs if found and the
// current version is a real (non-"dev") build. Returns (updated, latestTag, err).
// On a successful update this function does not return (the process re-execs);
// the bool is for the no-update path and tests.
func (u *Updater) CheckOnce(ctx context.Context) (bool, string, error) {
	rel, err := u.latest(ctx)
	if err != nil {
		return false, "", err
	}
	if !IsNewer(rel.TagName, u.Current) {
		return false, rel.TagName, nil
	}
	log.Printf("updater: %s -> %s available, updating %s", u.Current, rel.TagName, u.BinaryName)

	binURL, sumURL := "", ""
	want := u.assetName()
	for _, a := range rel.Assets {
		switch a.Name {
		case want:
			binURL = a.URL
		case "checksums.txt":
			sumURL = a.URL
		}
	}
	if binURL == "" {
		return false, rel.TagName, fmt.Errorf("no release asset %q for this platform", want)
	}

	data, err := u.download(ctx, binURL)
	if err != nil {
		return false, rel.TagName, err
	}
	if sumURL != "" {
		sums, err := u.download(ctx, sumURL)
		if err != nil {
			return false, rel.TagName, err
		}
		want, ok := ParseChecksum(string(sums), u.assetName())
		if !ok {
			return false, rel.TagName, fmt.Errorf("checksum for %q missing", u.assetName())
		}
		got := sha256.Sum256(data)
		if hex.EncodeToString(got[:]) != want {
			return false, rel.TagName, fmt.Errorf("checksum mismatch for %q", u.assetName())
		}
	}

	if err := ReplaceExecutable(data); err != nil {
		return false, rel.TagName, err
	}
	log.Printf("updater: installed %s, restarting", rel.TagName)
	return true, rel.TagName, Restart()
}

// Run periodically checks for updates until ctx is cancelled.
func (u *Updater) Run(ctx context.Context, interval time.Duration) {
	if interval < time.Minute {
		interval = time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, _, err := u.CheckOnce(ctx); err != nil {
				log.Printf("updater: %v", err)
			}
		}
	}
}

func (u *Updater) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// IsNewer reports whether latest is a strictly higher semver than current.
// A "dev" or unparseable current is treated as NOT older, so local/dev builds are
// never clobbered by auto-update.
func IsNewer(latest, current string) bool {
	lm, lok := parseSemver(latest)
	cm, cok := parseSemver(current)
	if !lok || !cok {
		return false
	}
	for i := 0; i < 3; i++ {
		if lm[i] != cm[i] {
			return lm[i] > cm[i]
		}
	}
	return false
}

// parseSemver parses "vX.Y.Z" (optionally with a -suffix) into [3]int.
func parseSemver(s string) ([3]int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// ParseChecksum extracts the hex sha256 for filename from a checksums.txt body
// (lines of "<hex>  <filename>", as produced by sha256sum).
func ParseChecksum(body, filename string) (string, bool) {
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && fields[1] == filename {
			return fields[0], true
		}
	}
	return "", false
}

// ReplaceExecutable atomically replaces the currently running executable with the
// given bytes. On Linux/macOS a rename over the running binary's path is safe.
func ReplaceExecutable(data []byte) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return err
	}
	dir := filepath.Dir(self)
	tmp, err := os.CreateTemp(dir, ".hopper-update-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	return os.Rename(tmpName, self)
}

// Restart re-execs the current process with the same args and environment, so the
// freshly installed binary takes over in place.
func Restart() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(self, os.Args, os.Environ())
}
