package main

import (
	"flag"
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
func TestSortLogFile(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectedErr bool
	}{
		{
			name: "basic sorting",
			input: `2025/01/15 00:09:17 [MATCH] Title: "Ask HN: Is maintaining a personal blog still worth it?" | URL:
2025/01/15 00:08:59 [MATCH] Title: "The Missing Nvidia GPU Glossary" | URL: https://modal.com/gpu-glossary/readme
2025/01/15 00:09:35 [MATCH] Title: "Link Blog in a Static Site" | URL: http://rednafi.com/misc/link_blog/
`,
			expected: `2025/01/15 00:09:35 [MATCH] Title: "Link Blog in a Static Site" | URL: http://rednafi.com/misc/link_blog/
2025/01/15 00:09:17 [MATCH] Title: "Ask HN: Is maintaining a personal blog still worth it?" | URL:
2025/01/15 00:08:59 [MATCH] Title: "The Missing Nvidia GPU Glossary" | URL: https://modal.com/gpu-glossary/readme
`,
			expectedErr: false,
		},
		{
			name:        "empty file",
			input:       ``,
			expected:    ``,
			expectedErr: false,
		},
		{
			name: "no [MATCH] entries",
			input: `2025/01/15 00:08:59 Title: "The Missing Nvidia GPU Glossary" | URL: https://modal.com/gpu-glossary/readme
2025/01/15 00:09:35 Title: "Link Blog in a Static Site" | URL: http://rednafi.com/misc/link_blog/
`,
			expected:    ``,
			expectedErr: false,
		},
		{
			name: "mixed entries",
			input: `2025/01/15 00:09:17 [MATCH] Title: "Ask HN: Is maintaining a personal blog still worth it?" | URL:
2025/01/15 00:08:59 Title: "The Missing Nvidia GPU Glossary" | URL: https://modal.com/gpu-glossary/readme
2025/01/15 00:09:35 [MATCH] Title: "Link Blog in a Static Site" | URL: http://rednafi.com/misc/link_blog/
`,
			expected: `2025/01/15 00:09:35 [MATCH] Title: "Link Blog in a Static Site" | URL: http://rednafi.com/misc/link_blog/
2025/01/15 00:09:17 [MATCH] Title: "Ask HN: Is maintaining a personal blog still worth it?" | URL:
`,
			expectedErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary file
			tempFile, err := os.CreateTemp("", "test_log_*.log")
			if err != nil {
				t.Fatalf("failed to create temporary file: %v", err)
			}
			defer os.Remove(tempFile.Name())

			// Write input data to the file
			if _, err := tempFile.WriteString(tt.input); err != nil {
				t.Fatalf("failed to write to temporary file: %v", err)
			}

			// Close the file so it can be read by sortLogFile
			if err := tempFile.Close(); err != nil {
				t.Fatalf("failed to close temporary file: %v", err)
			}

			// Open the file for reading and writing
			file, err := os.OpenFile(tempFile.Name(), os.O_RDWR, 0644)
			if err != nil {
				t.Fatalf("failed to open temporary file: %v", err)
			}
			defer file.Close()

			// Call the function to test
			err = sortLogFile(file)

			// Check if an error was expected
			if (err != nil) != tt.expectedErr {
				t.Fatalf("expected error: %v, got: %v", tt.expectedErr, err)
			}

			// Read the output
			output, err := os.ReadFile(tempFile.Name())
			if err != nil {
				t.Fatalf("failed to read temporary file: %v", err)
			}

			// Compare the output to the expected value
			if strings.TrimSpace(string(output)) != strings.TrimSpace(tt.expected) {
				t.Errorf("expected output:\n%q\ngot:\n%q", tt.expected, output)
			}
		})
	}
}
