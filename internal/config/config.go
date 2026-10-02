package config

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/galets/gatefile/internal/version"
)

const (
	DefaultPollMs     = 1000
	DefaultPollSettle = 2
)

type Config struct {
	BaseURL      string
	DocumentPath string
	APIKey       string
	Addr         string
	Hook         string
	PollInterval time.Duration
	PollSettle   int
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
	pollMs, err := pollMs()
	if err != nil {
		return nil, err
	}
	settle, err := pollSettle()
	if err != nil {
		return nil, err
	}
	return &Config{BaseURL: base, DocumentPath: doc, APIKey: key, Addr: addr, Hook: os.Getenv("GATEFILE_HOOK"), PollInterval: pollMs, PollSettle: settle}, nil
}

// pollMs parses GATEFILE_POLL_MS. Empty selects default.
// Zero disables the poller.
func pollMs() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv("GATEFILE_POLL_MS"))
	if raw == "" {
		return DefaultPollMs * time.Millisecond, nil
	}
	ms, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("GATEFILE_POLL_MS: %v", err)
	}
	if ms < 0 {
		return 0, fmt.Errorf("GATEFILE_POLL_MS: negative")
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// pollSettle parses GATEFILE_POLL_SETTLE_TICKS. Empty selects default.
func pollSettle() (int, error) {
	raw := strings.TrimSpace(os.Getenv("GATEFILE_POLL_SETTLE_TICKS"))
	if raw == "" {
		return DefaultPollSettle, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("GATEFILE_POLL_SETTLE_TICKS: %v", err)
	}
	if n <= 0 {
		return DefaultPollSettle, nil
	}
	return n, nil
}

func Usage() string {
	return fmt.Sprintf(`Usage: gatefile [OPTIONS]

Gatefile %s is a stateless single-document synchronization server.

Options:
  -h, --help       Show this help message and exit
  -V, --version    Show version and exit

Configuration is environment-only:

  BASE_URL       API endpoint path (default: /gatefile/file)
  DOCUMENT_PATH  Path to the persistent document file (required)
  API_KEY        Shared secret, sent as "Bearer <key>" (required)
  ADDR           Listen address (default: 127.0.0.1:8654)
  GATEFILE_HOOK  Path to executable run on each update (optional, disabled if empty)
  GATEFILE_POLL_MS  Poll interval for external change detection in ms (default: 1000, 0 disables)
  GATEFILE_POLL_SETTLE_TICKS  Stable ticks before a change is committed (default: 2)

Examples:
  DOCUMENT_PATH=$PWD/tmp/file.txt API_KEY=secret gatefile
  ADDR=127.0.0.1:8654 BASE_URL=/gatefile/file DOCUMENT_PATH=$PWD/tmp/file.txt API_KEY=secret gatefile
`, version.Version())
}

func PrintUsage(w io.Writer) {
	fmt.Fprint(w, Usage())
}
