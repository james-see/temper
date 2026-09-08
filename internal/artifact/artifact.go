package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

type Store struct {
	root string
}

func Open(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

func (s *Store) Put(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	dir := filepath.Join(s.root, id[:2])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, id)
	if _, err := os.Stat(path); err == nil {
		return id, nil
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) Get(id string) ([]byte, error) {
	if len(id) < 2 {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(filepath.Join(s.root, id[:2], id))
}

func (s *Store) Path(id string) string {
	if len(id) < 2 {
		return ""
	}
	return filepath.Join(s.root, id[:2], id)
}
