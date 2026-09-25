package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	BaseURL      string
	DocumentPath string
	APIKey       string
	Addr         string
	Hook         string
}

func Load() (*Config, error) {
	base := os.Getenv("BASE_URL")
	if base == "" {
		base = "/gatefile/file"
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	doc := os.Getenv("DOCUMENT_PATH")
	if doc == "" {
		return nil, fmt.Errorf("DOCUMENT_PATH is required")
	}
	key := os.Getenv("API_KEY")
	if key == "" {
		return nil, fmt.Errorf("API_KEY is required")
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = "127.0.0.1:8654"
	}
	return &Config{BaseURL: base, DocumentPath: doc, APIKey: key, Addr: addr, Hook: os.Getenv("GATEFILE_HOOK")}, nil
}
