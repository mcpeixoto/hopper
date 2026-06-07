package livelog

import (
	"io"
	"testing"
)

func TestAppendOpenRemove(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s.Exists("job_1") {
		t.Fatal("should not exist yet")
	}
	if err := s.Append("job_1", []byte("hello ")); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("job_1", []byte("world")); err != nil {
		t.Fatal(err)
	}
	if !s.Exists("job_1") {
		t.Fatal("should exist after append")
	}
	rc, err := s.Open("job_1")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "hello world" {
		t.Fatalf("got %q", b)
	}
	if err := s.Remove("job_1"); err != nil {
		t.Fatal(err)
	}
	if s.Exists("job_1") {
		t.Fatal("should be gone after remove")
	}
	// Removing again is not an error.
	if err := s.Remove("job_1"); err != nil {
		t.Fatalf("remove absent: %v", err)
	}
}

func TestRejectsBadID(t *testing.T) {
	s, _ := New(t.TempDir())
	if err := s.Append("../escape", []byte("x")); err == nil {
		t.Fatal("expected invalid id rejection")
	}
	if _, err := s.Open("a/b"); err == nil {
		t.Fatal("expected invalid id rejection on open")
	}
}
