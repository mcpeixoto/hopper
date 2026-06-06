package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestTarGzRoundTrip(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := TarGz(src, &buf); err != nil {
		t.Fatalf("tar: %v", err)
	}

	dest := t.TempDir()
	if err := UntarGz(&buf, dest); err != nil {
		t.Fatalf("untar: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(got) != "alpha" {
		t.Fatalf("a.txt = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "sub", "b.txt")); string(got) != "beta" {
		t.Fatalf("sub/b.txt = %q", got)
	}
}

func TestUntarRejectsTraversal(t *testing.T) {
	// Craft a malicious tarball with a ../ path.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("pwned")
	_ = tw.WriteHeader(&tar.Header{Name: "../escape.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(body)
	tw.Close()
	gz.Close()

	dest := t.TempDir()
	if err := UntarGz(&buf, dest); err == nil {
		t.Fatal("expected path-traversal rejection")
	}
}

func TestIsEmptyDir(t *testing.T) {
	d := t.TempDir()
	if !IsEmptyDir(d) {
		t.Fatal("fresh temp dir should be empty")
	}
	os.WriteFile(filepath.Join(d, "x"), []byte("y"), 0o644)
	if IsEmptyDir(d) {
		t.Fatal("dir with a file should not be empty")
	}
}
