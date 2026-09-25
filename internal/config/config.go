package config

import (
	"fmt"
	"io"
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

func Usage() string {
	return `Usage: gatefile [OPTIONS]

Gatefile is a stateless single-document synchronization server.

Options:
  -h, --help    Show this help message and exit

Configuration is environment-only:

  BASE_URL       API endpoint path (default: /gatefile/file)
  DOCUMENT_PATH  Path to the persistent document file (required)
  API_KEY        Shared secret, sent as "Bearer <key>" (required)
  ADDR           Listen address (default: 127.0.0.1:8654)
  GATEFILE_HOOK  Path to executable run on each update (optional, disabled if empty)

Examples:
  DOCUMENT_PATH=$PWD/tmp/file.txt API_KEY=secret gatefile
  ADDR=127.0.0.1:8654 BASE_URL=/gatefile/file DOCUMENT_PATH=$PWD/tmp/file.txt API_KEY=secret gatefile
`
}

func PrintUsage(w io.Writer) {
	fmt.Fprint(w, Usage())
}
