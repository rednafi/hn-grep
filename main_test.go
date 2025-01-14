package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestParseFlags tests parseFlags' ability to parse command-line flags correctly
// and handle error conditions: no keywords, invalid maxStories, or delay < 100ms.
func TestParseFlags(t *testing.T) {
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// -----------------------------------
	// 1) Valid flags
	// -----------------------------------
	os.Args = []string{
		"cmd",
		"-max-stories=10",
		"-keywords=go,rust",
		"-domain=example.com",
		"-log-file=test.log",
		"-delay=200ms",
	}
	// We must reset the flag package between tests.
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	cfg, err := parseFlags()
	if err != nil {
		t.Fatalf("parseFlags() returned an unexpected error: %v", err)
	}
	if cfg.maxStories != 10 {
		t.Errorf("Expected maxStories=10; got %d", cfg.maxStories)
	}
	if !slices.Equal(cfg.keywords, []string{"go", "rust"}) {
		t.Errorf("Expected keywords = [go rust]; got %#v", cfg.keywords)
	}
	if cfg.domain != "example.com" {
		t.Errorf("Expected domain=example.com; got %s", cfg.domain)
	}
	if cfg.logFile != "test.log" {
		t.Errorf("Expected logFile=test.log; got %s", cfg.logFile)
	}
	if cfg.delay != 200*time.Millisecond {
		t.Errorf("Expected delay=200ms; got %v", cfg.delay)
	}

	// -----------------------------------
	// 2) Missing keywords (should fail)
	// -----------------------------------
	os.Args = []string{"cmd",
		"-max-stories=10",
		"-keywords=",
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	_, err = parseFlags()
	if err == nil || !strings.Contains(err.Error(), "keywords must be provided") {
		t.Errorf("Expected error about missing keywords; got %v", err)
	}

	// -----------------------------------
	// 3) Invalid max-stories
	// -----------------------------------
	os.Args = []string{"cmd",
		"-max-stories=-5",
		"-keywords=go",
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	_, err = parseFlags()
	if err == nil || !strings.Contains(err.Error(), "max-stories must be a positive integer") {
		t.Errorf("Expected error about max-stories > 0; got %v", err)
	}

	// -----------------------------------
	// 4) Delay < 100ms
	// -----------------------------------
	os.Args = []string{"cmd",
		"-max-stories=10",
		"-keywords=go",
		"-delay=50ms",
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	_, err = parseFlags()
	if err == nil || !strings.Contains(err.Error(), "delay must be greater than or equal to 100ms") {
		t.Errorf("Expected error about delay >= 100ms; got %v", err)
	}
}

// TestMatches checks the domain-first logic and the fallback to keyword checking.
func TestMatches(t *testing.T) {
	tests := []struct {
		name     string
		s        story
		keywords []string
		domain   string
		want     bool
	}{
		{
			name:     "Domain match only",
			s:        story{Title: "Random Title", URL: "https://example.com/path"},
			keywords: []string{"go"},
			domain:   "example.com",
			want:     true, // domain is present, so match is true immediately
		},
		{
			name:     "Keyword match only",
			s:        story{Title: "Go is awesome", URL: "https://otherdomain.com"},
			keywords: []string{"go"},
			domain:   "", // domain not provided
			want:     true,
		},
		{
			name:     "Neither domain nor keyword match",
			s:        story{Title: "Rust tips", URL: "https://otherdomain.com"},
			keywords: []string{"go"},
			domain:   "example.com",
			want:     false,
		},
		{
			name:     "Multiple keywords, domain mismatch",
			s:        story{Title: "Python concurrency", URL: "https://xyz.com/python"},
			keywords: []string{"go", "rust", "python"},
			domain:   "example.com",
			want:     true, // domain not matched, so check keywords: "Python", which is a match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matches(&tt.s, tt.keywords, tt.domain)
			if got != tt.want {
				t.Errorf("matches(%#v, %v, %q) = %v; want %v",
					tt.s, tt.keywords, tt.domain, got, tt.want)
			}
		})
	}
}

type fakeHNClient struct {
	topStories []int
	stories    map[int]*story
	errTop     error
	errStory   error
}

// Ensure fakeHNClient also implements HackerNewsClient
var _ hackerNewsClient = (*fakeHNClient)(nil)

func (f *fakeHNClient) getTopStories() ([]int, error) {
	return f.topStories, f.errTop
}

func (f *fakeHNClient) getStory(id int) (*story, error) {
	if f.errStory != nil {
		return nil, f.errStory
	}
	s, ok := f.stories[id]
	if !ok {
		return nil, fmt.Errorf("story not found: %d", id)
	}
	return s, nil
}

func TestRunWithFakeClient(t *testing.T) {
	// 1) Create a fake client with data
	fc := &fakeHNClient{
		topStories: []int{101, 202, 303},
		stories: map[int]*story{
			101: {ID: 101, Title: "Go is great", URL: "https://golang.org"},
			202: {ID: 202, Title: "Random", URL: "https://example.com/something"},
			303: {ID: 303, Title: "Rust is memory safe", URL: "https://rust-lang.org"},
		},
	}

	// 2) Create a test cfg
	cfg := &cliFlags{
		maxStories: 3,
		keywords:   []string{"go", "rust"},
		domain:     "example.com",
		logFile:    "unused.log",
		delay:      0, // no delay in tests
	}

	// 3) Capture logs in memory
	var stdoutBuf, fileBuf bytes.Buffer
	stdoutLogger := log.New(&stdoutBuf, "", 0)
	fileLogger := log.New(&fileBuf, "", 0)

	// 4) Call the REAL run function with our fake client
	err := run(cfg, stdoutLogger, fileLogger, fc)
	if err != nil {
		t.Fatalf("run returned an unexpected error: %v", err)
	}

	// 5) Inspect logs
	stdoutOut := stdoutBuf.String()
	fileOut := fileBuf.String()

	// Because domain=example.com and keywords=[go, rust], let's see how it matches:
	//   ID=101 => "Go is great" => matches "go"
	//   ID=202 => "Random" => URL has "example.com", so domain match => yes
	//   ID=303 => "Rust is memory safe" => matches "rust"
	// So all 3 should be [MATCH: Yes].
	if !strings.Contains(stdoutOut, "Go is great") ||
		!strings.Contains(stdoutOut, "[MATCH: Yes]") {
		t.Errorf("Expected 'Go is great' with [MATCH: Yes] in stdout:\n%s", stdoutOut)
	}
	if !strings.Contains(stdoutOut, "Random") ||
		!strings.Contains(stdoutOut, "[MATCH: Yes]") {
		t.Errorf("Expected 'Random' with [MATCH: Yes] in stdout:\n%s", stdoutOut)
	}
	if !strings.Contains(stdoutOut, "Rust is memory safe") ||
		!strings.Contains(stdoutOut, "[MATCH: Yes]") {
		t.Errorf("Expected 'Rust is memory safe' with [MATCH: Yes] in stdout:\n%s", stdoutOut)
	}

	// All matches should appear in file logs
	if !strings.Contains(fileOut, "[MATCH] Title: \"Go is great\"") {
		t.Errorf("Expected file logs to contain 'Go is great' match:\n%s", fileOut)
	}
	if !strings.Contains(fileOut, "[MATCH] Title: \"Random\"") {
		t.Errorf("Expected file logs to contain 'Random' match:\n%s", fileOut)
	}
	if !strings.Contains(fileOut, "[MATCH] Title: \"Rust is memory safe\"") {
		t.Errorf("Expected file logs to contain 'Rust is memory safe' match:\n%s", fileOut)
	}
}
