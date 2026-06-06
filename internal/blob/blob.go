// Package blob is a content-addressed blob store on the local filesystem. It
// holds job inputs, outputs, and logs for the control plane. Files are named by
// their SHA-256, so identical content is stored once (free dedup) and integrity
// is verifiable by the name.
package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Store writes and reads blobs under a directory.
type Store struct {
	dir string
}

// New creates (if needed) the blob directory and returns a Store.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("blob: empty directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Save streams r to a temp file while hashing it, then renames it to its
// content address. It returns the storage path (the hash), the hex SHA-256, and
// the byte size.
func (s *Store) Save(r io.Reader) (path, hash string, size int64, err error) {
	tmp, err := os.CreateTemp(s.dir, ".blob-*")
	if err != nil {
		return "", "", 0, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		tmp.Close()
		return "", "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", "", 0, err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	dest := filepath.Join(s.dir, sum)
	if err := os.Rename(tmpName, dest); err != nil {
		return "", "", 0, err
	}
	return sum, sum, size, nil
}

// Open opens a blob by its storage path (the hash) for reading.
func (s *Store) Open(path string) (io.ReadCloser, error) {
	if err := validPath(path); err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(s.dir, path))
}

// Delete removes a blob by its storage path. Missing blobs are not an error.
func (s *Store) Delete(path string) error {
	if err := validPath(path); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(s.dir, path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func validPath(path string) error {
	if path == "" || filepath.Base(path) != path {
		return errors.New("blob: invalid path")
	}
	return nil
}
