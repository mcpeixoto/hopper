package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

func TestSaveAndOpen(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := "hello hopper"
	path, hash, size, err := s.Save(strings.NewReader(content))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if size != int64(len(content)) {
		t.Fatalf("size=%d want %d", size, len(content))
	}
	want := sha256.Sum256([]byte(content))
	if hash != hex.EncodeToString(want[:]) {
		t.Fatalf("hash mismatch")
	}

	rc, err := s.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != content {
		t.Fatalf("round-trip mismatch: %q", got)
	}
}

func TestSaveDedup(t *testing.T) {
	s, _ := New(t.TempDir())
	p1, _, _, _ := s.Save(strings.NewReader("same"))
	p2, _, _, _ := s.Save(strings.NewReader("same"))
	if p1 != p2 {
		t.Fatalf("identical content should dedup: %s != %s", p1, p2)
	}
}

func TestOpenRejectsTraversal(t *testing.T) {
	s, _ := New(t.TempDir())
	if _, err := s.Open("../etc/passwd"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}
