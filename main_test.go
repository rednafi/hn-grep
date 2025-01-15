package main

import (
	"bytes"
	"flag"
	"html/template"
	"log"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// FakeHackerNewsClient is a mock implementation of hackerNewsClient.
type FakeHackerNewsClient struct {
	TopStories []int
	Stories    map[int]story
}

// getTopStories simulates fetching top story IDs.
func (f *FakeHackerNewsClient) getTopStories() ([]int, error) {
	return f.TopStories, nil
}

// getStory simulates fetching a story by ID.
func (f *FakeHackerNewsClient) getStory(id int) (*story, error) {
	st, ok := f.Stories[id]
	if !ok {
		// Simulate a story not found (nil, nil).
		return nil, nil
	}
	return &st, nil
}

func TestParseFlags(t *testing.T) {
	t.Parallel()
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	tests := []struct {
		name        string
		args        []string
		want        *cliFlags
		expectError string
	}{
		{
			name: "Valid flags",
			args: []string{
				"cmd", "-max-stories=10", "-keywords=go,rust", "-domain=example.com", "-html-file=test.html", "-delay=200ms",
			},
			want: &cliFlags{
				maxStories: 10,
				keywords:   []string{"go", "rust"},
				domain:     "example.com",
				htmlFile:   "test.html",
				delay:      200 * time.Millisecond,
			},
		},
		{
			name:        "Missing keywords",
			args:        []string{"cmd", "-max-stories=10", "-keywords="},
			expectError: "keywords must be provided",
		},
		{
			name:        "Negative max-stories",
			args:        []string{"cmd", "-max-stories=-5", "-keywords=go"},
			expectError: "max-stories must be a positive integer",
		},
		{
			name:        "Delay less than 100ms",
			args:        []string{"cmd", "-max-stories=10", "-keywords=go", "-delay=50ms"},
			expectError: "delay must be greater than or equal to 100ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Args = tt.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

			got, err := parseFlags()
			if tt.expectError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectError) {
					t.Errorf("Expected error containing %q, got %v", tt.expectError, err)
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("Expected %+v, got %+v", tt.want, got)
				}
			}
		})
	}
}

func TestCompilePattern(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		keywords []string
		input    string
		want     bool
	}{
		{
			name:     "Single keyword, partial match",
			keywords: []string{"go"},
			input:    "I love golang", // Should NOT match
			want:     false,
		},
		{
			name:     "Single keyword, full word match",
			keywords: []string{"go"},
			input:    "Let's learn go today", // Should match
			want:     true,
		},
		{
			name:     "Multiple keywords, one matches",
			keywords: []string{"java", "rust", "python"},
			input:    "Rust is fast", // Should match "rust"
			want:     true,
		},
		{
			name:     "Case-insensitivity",
			keywords: []string{"gO"},
			input:    "Let's learn Go today", // "gO" => should match
			want:     true,
		},
		{
			name:     "No match",
			keywords: []string{"go"},
			input:    "Rust is also cool", // Doesn't contain "go"
			want:     false,
		},
		{
			name:     "Special regex characters, should be escaped",
			keywords: []string{"c++", "c#"},
			input:    "I like c++ and c# a lot", // Should match both
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := compilePattern(tt.keywords)
			re := regexp.MustCompile(pattern)

			got := re.MatchString(tt.input)
			if got != tt.want {
				t.Errorf("For keywords=%v and input=%q, pattern=%q => got %v, want %v",
					tt.keywords, tt.input, pattern, got, tt.want)
			}
		})
	}
}

func TestMatches(t *testing.T) {
	t.Parallel()
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
			want:     true,
		},
		{
			name:     "Keyword match only",
			s:        story{Title: "Go is awesome", URL: "https://otherdomain.com"},
			keywords: []string{"go"},
			domain:   "",
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
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matches(&tt.s, tt.keywords, tt.domain)
			if got != tt.want {
				t.Errorf("matches(%+v, %+v, %q) = %v, want %v",
					tt.s, tt.keywords, tt.domain, got, tt.want)
			}
		})
	}
}

func TestWriteHTML(t *testing.T) {
	t.Parallel()
	// 1. Arrange
	tmpl, err := template.New("test").Parse(`<h1>Stories</h1>
{{range .Stories}}<p>{{.Title}} - {{.URL}}</p>{{end}}`)
	if err != nil {
		t.Fatalf("Failed to parse inline template: %v", err)
	}

	data := HTMLData{
		Keywords: "go, rust",
		Domain:   "example.com",
		Stories: []story{
			{Title: "Story 1", URL: "https://example.com/1", StoryURL: "https://news.ycombinator.com/item?id=1"},
			{Title: "Story 2", URL: "https://example.com/2", StoryURL: "https://news.ycombinator.com/item?id=2"},
		},
	}

	outFile := "test_write.html"
	_ = os.Remove(outFile) // Clean up old files if they exist

	// 2. Act
	err = writeHTML(outFile, tmpl, data)
	if err != nil {
		t.Fatalf("writeHTML returned error: %v", err)
	}

	// 3. Assert
	contents, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed to read output file %q: %v", outFile, err)
	}
	htmlOutput := string(contents)

	if !strings.Contains(htmlOutput, "Story 1") ||
		!strings.Contains(htmlOutput, "Story 2") {
		t.Errorf("Output HTML does not contain expected story titles.\nOutput:\n%s", htmlOutput)
	}

	if !strings.Contains(htmlOutput, "https://example.com/1") ||
		!strings.Contains(htmlOutput, "https://example.com/2") {
		t.Errorf("Output HTML does not contain expected story URLs.\nOutput:\n%s", htmlOutput)
	}

	// Cleanup
	_ = os.Remove(outFile)
}

func TestRun(t *testing.T) {
	t.Parallel()
	// 1. Arrange (setup)
	fakeClient := &FakeHackerNewsClient{
		TopStories: []int{101, 202, 303},
		Stories: map[int]story{
			101: {ID: 101, Title: "Go is cool", URL: "https://golang.org"},
			202: {ID: 202, Title: "Random article", URL: "https://example.com/abc"},
			303: {ID: 303, Title: "Rust is also cool", URL: "https://rust-lang.org"},
		},
	}

	// We'll match stories that have "go" in the title OR that have domain "example.com".
	cfg := &cliFlags{
		maxStories: 3,
		keywords:   []string{"go"},
		domain:     "example.com",
		htmlFile:   "test_output.html",
		delay:      0, // zero for no actual sleeping in tests
	}

	var logBuf bytes.Buffer
	stdoutLogger := log.New(&logBuf, "", 0)

	tmpl, err := template.New("test").Parse(`
<!DOCTYPE html>
<html>
<body>
	<h2>Keywords: {{.Keywords}}</h2>
	<h2>Domain: {{.Domain}}</h2>
	<ul>
	{{range .Stories}}
		<li><a href="{{.StoryURL}}">{{.Title}}</a> - {{.URL}}</li>
	{{end}}
	</ul>
</body>
</html>`)
	if err != nil {
		t.Fatalf("Failed to parse inline template: %v", err)
	}

	_ = os.Remove(cfg.htmlFile) // Clean old file if present

	// 2. Act
	err = run(cfg, stdoutLogger, fakeClient, tmpl)
	if err != nil {
		t.Fatalf("run(...) returned error: %v", err)
	}

	// 3. Assert
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "[1] Title: Go is cool") ||
		!strings.Contains(logOutput, "MATCHED!") {
		t.Errorf("Expected story 101 to be matched; not found in log:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "[2] Title: Random article") ||
		!strings.Contains(logOutput, "MATCHED!") {
		t.Errorf("Expected story 202 to be matched (domain match); not found in log:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "[3] Title: Rust is also cool") ||
		!strings.Contains(logOutput, "NOT MATCHED") {
		t.Errorf("Expected story 303 to not match; not found in log:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "Matched 2 stories.") {
		t.Errorf("Expected final log to indicate 2 matches, got:\n%s", logOutput)
	}

	fileBytes, err := os.ReadFile(cfg.htmlFile)
	if err != nil {
		t.Fatalf("Failed to read output HTML file %q: %v", cfg.htmlFile, err)
	}
	outputHTML := string(fileBytes)

	if !strings.Contains(outputHTML, "Go is cool") ||
		!strings.Contains(outputHTML, "Random article") ||
		strings.Contains(outputHTML, "Rust is also cool") {
		t.Errorf("HTML output does not contain the expected matched stories.\nGot:\n%s", outputHTML)
	}

	_ = os.Remove(cfg.htmlFile)
}
