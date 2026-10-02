package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmptyDocumentEtag(t *testing.T) {
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	content, etag := s.Current()
	if len(content) != 0 {
		t.Fatalf("expected empty content, got %q", content)
	}
	if etag != Hash([]byte{}) {
		t.Fatalf("expected md5(empty), got %q", etag)
	}
	if etag != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Fatalf("unexpected etag %q", etag)
	}
}

func TestUpdateAndPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	_, etag := s.Current()
	newEtag, err := s.Update([]byte("hello"), etag)
	if err != nil {
		t.Fatal(err)
	}
	if newEtag != Hash([]byte("hello")) {
		t.Fatalf("unexpected etag %q", newEtag)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "hello" {
		t.Fatalf("not persisted: %q", onDisk)
	}
}

func TestConflict(t *testing.T) {
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update([]byte("x"), "wrong"); err != ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestReloadDetectsExternalChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	// External write bypassing Update.
	if err := os.WriteFile(path, []byte("v2-longer"), 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := s.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	content, etag := s.Current()
	if string(content) != "v2-longer" {
		t.Fatalf("unexpected content %q", content)
	}
	if etag != Hash([]byte("v2-longer")) {
		t.Fatalf("unexpected etag %q", etag)
	}
}

func TestReloadNoChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := s.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected changed=false")
	}
}
