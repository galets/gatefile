package store

import (
	"crypto/md5"
	"fmt"
	"os"
	"sync"
)

var (
	ErrConflict = fmt.Errorf("conflict")
)

func Hash(content []byte) string {
	sum := md5.Sum(content)
	return fmt.Sprintf("%x", sum)
}

type DocumentStore struct {
	mu      sync.RWMutex
	path    string
	content []byte
	etag    string
}

func New(path string) (*DocumentStore, error) {
	s := &DocumentStore{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *DocumentStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			data = []byte{}
		} else {
			return err
		}
	}
	s.content = data
	s.etag = Hash(data)
	return nil
}

func (s *DocumentStore) Current() ([]byte, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]byte, len(s.content))
	copy(out, s.content)
	return out, s.etag
}

func (s *DocumentStore) Update(newContent []byte, expectedEtag string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedEtag != s.etag {
		return s.etag, ErrConflict
	}
	content := make([]byte, len(newContent))
	copy(content, newContent)
	if err := os.WriteFile(s.path, content, 0644); err != nil {
		return s.etag, err
	}
	s.content = content
	s.etag = Hash(content)
	return s.etag, nil
}
