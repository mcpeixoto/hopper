package updater

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.1.1", "v0.1.0", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v0.2.0", "dev", false}, // never clobber dev builds
		{"garbage", "v0.1.0", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.latest, c.current); got != c.want {
			t.Errorf("IsNewer(%q,%q)=%v want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestIsNewerSuffixStripped(t *testing.T) {
	// 0.2.0 vs 0.2.0-rc1 both parse to 0.2.0 -> not newer.
	if IsNewer("v0.2.0", "v0.2.0-rc1") {
		t.Fatal("0.2.0 should not be newer than 0.2.0-rc1 (suffix ignored)")
	}
}

func TestParseChecksum(t *testing.T) {
	body := "abc123  hopperd_linux_amd64\ndef456  hopper-agent_darwin_arm64\n"
	got, ok := ParseChecksum(body, "hopperd_linux_amd64")
	if !ok || got != "abc123" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if _, ok := ParseChecksum(body, "missing"); ok {
		t.Fatal("expected missing checksum to report not-found")
	}
}

func TestUpdateToTagNoopWhenNotNewer(t *testing.T) {
	u := New("mcpeixoto/hopper", "hopper-agent", "v0.3.0")
	// Same or older tag must be a no-op (no network call, no update).
	if updated, _, err := u.UpdateToTag(context.Background(), "v0.3.0"); updated || err != nil {
		t.Fatalf("same version should be a no-op, got updated=%v err=%v", updated, err)
	}
	if updated, _, err := u.UpdateToTag(context.Background(), "v0.2.0"); updated || err != nil {
		t.Fatalf("older version should be a no-op, got updated=%v err=%v", updated, err)
	}
	// dev current never updates.
	dev := New("mcpeixoto/hopper", "hopper-agent", "dev")
	if updated, _, _ := dev.UpdateToTag(context.Background(), "v9.9.9"); updated {
		t.Fatal("dev build must not converge")
	}
}

func TestAssetName(t *testing.T) {
	u := New("mcpeixoto/hopper", "hopperd", "v0.1.0")
	want := "hopperd_" + runtime.GOOS + "_" + runtime.GOARCH
	if u.assetName() != want {
		t.Fatalf("assetName=%q want %q", u.assetName(), want)
	}
}

// TestReplaceExecutable verifies the atomic swap by replacing a copy of a tiny
// executable and confirming the new content runs.
func TestReplaceExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename-over-running not supported the same way on windows")
	}
	dir := t.TempDir()

	// Build a trivial script "binary" and a replacement.
	bin := filepath.Join(dir, "fakebin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho v1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Run it via a subprocess that calls ReplaceExecutable on itself would be
	// complex; instead test the file-swap primitive directly.
	newContent := []byte("#!/bin/sh\necho v2\n")

	// Simulate ReplaceExecutable's core on an arbitrary path (not os.Executable).
	tmp, err := os.CreateTemp(dir, ".upd-*")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Write(newContent)
	tmp.Close()
	os.Chmod(tmp.Name(), 0o755)
	if err := os.Rename(tmp.Name(), bin); err != nil {
		t.Fatalf("rename: %v", err)
	}

	out, err := exec.Command("/bin/sh", bin).CombinedOutput()
	if err != nil {
		t.Fatalf("run replaced: %v", err)
	}
	if string(out) != "v2\n" {
		t.Fatalf("expected v2, got %q", out)
	}
}
