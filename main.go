package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"time"
)

// story must have exported fields so the JSON package can unmarshal them.
type story struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// cliFlags holds all command-line flag values.
type cliFlags struct {
	maxStories int
	keywords   []string
	domain     string
	logFile    string
	delay      time.Duration
}

// parseFlags is unexported because only `main` uses it.
func parseFlags() (*cliFlags, error) {
	maxStories := flag.Int("max-stories", 500, "Maximum number of stories to fetch")
	keywords := flag.String("keywords", "", "Comma-separated list of keywords to filter stories")
	domain := flag.String("domain", "", "Domain to filter stories by URL")
	logFile := flag.String("log-file", "hn-alert.log", "File to write log output")
	delay := flag.Duration("delay", 100*time.Millisecond, "Delay between requests (e.g., 100ms)")
	flag.Parse()

	if *maxStories <= 0 {
		return nil, fmt.Errorf("max-stories must be a positive integer")
	}
	if *keywords == "" {
		return nil, fmt.Errorf("keywords must be provided")
	}
	if *delay < 100*time.Millisecond {
		return nil, fmt.Errorf("delay must be greater than or equal to 100ms")
	}

	keywordsList := strings.Split(*keywords, ",")

	return &cliFlags{
		maxStories: *maxStories,
		keywords:   keywordsList,
		domain:     *domain,
		logFile:    *logFile,
		delay:      *delay,
	}, nil
}

// hackerNewsClient is an interface for fetching top stories and story details.
// This makes run() more testable: we can provide a mock client in tests.
type hackerNewsClient interface {
	getTopStories() ([]int, error)
	getStory(id int) (*story, error)
}

type hnClient struct {
	topStoriesURL   string
	itemURLTemplate string
	maxStories      int
}

// Compile-time check that hnClient implements hackerNewsClient
var _ hackerNewsClient = (*hnClient)(nil)

// getTopStories fetches the IDs of the top stories from Hacker News.
func (c *hnClient) getTopStories() ([]int, error) {
	resp, err := http.Get(c.topStoriesURL)
	if err != nil {
		return nil, fmt.Errorf("error fetching top stories: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading top stories body: %w", err)
	}

	var ids []int
	if err := json.Unmarshal(body, &ids); err != nil {
		return nil, fmt.Errorf("error unmarshalling top story IDs: %w", err)
	}
	return ids, nil
}

// getStory fetches the details of a single story by ID.
func (c *hnClient) getStory(id int) (*story, error) {
	url := fmt.Sprintf(c.itemURLTemplate, id)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("error fetching story %d: %w", id, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading story body: %w", err)
	}

	var s story
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("error unmarshalling story %d: %w", id, err)
	}
	return &s, nil
}

// matches returns true if the story Title contains any of the keywords
// (case-insensitive), or if a domain is given and the story's URL contains that domain.
func matches(s *story, keywords []string, domain string) bool {
	// Domain match is highest priority
	if domain != "" && strings.Contains(s.URL, domain) {
		return true
	}

	// Otherwise, check if title contains any of the keywords
	return slices.ContainsFunc(keywords, func(keyword string) bool {
		return strings.Contains(strings.ToLower(s.Title), strings.ToLower(keyword))
	})
}

// logStory prints story info to stdout. If matched, it also logs to the file logger.
func logStory(stdoutLogger, fileLogger *log.Logger, s *story, index int, matched bool) {
	stdoutLogger.Printf("%d. Title: %s", index, s.Title)
	stdoutLogger.Printf("   URL:   %s", s.URL)

	if matched {
		stdoutLogger.Println("   [MATCH: Yes]")
		fileLogger.Printf("[MATCH] Title: %q | URL: %s", s.Title, s.URL)
	} else {
		stdoutLogger.Println("   [MATCH: No]")
	}
	stdoutLogger.Println(strings.Repeat("-", 80))
}

// Sort the log messages in the log file by date and time.
func sortLogFile(file *os.File) error {
	// Ensure file is readable and writable
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	// If the file is empty, there's nothing to sort
	if stat.Size() == 0 {
		return nil
	}

	// Read the entire file
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek to start of file: %w", err)
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read file contents: %w", err)
	}

	// Parse lines and filter for `[MATCH]` entries
	var entries []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.Contains(line, "[MATCH]") {
			entries = append(entries, line)
		}
	}

	// Sort the log entries by their timestamps
	sort.Slice(entries, func(i, j int) bool {
		return entries[i] > entries[j] // Lexicographic sorting works for ISO-like dates
	})

	// Rewrite the file with sorted log entries
	err = file.Truncate(0)
	if err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek to start of file: %w", err)
	}

	_, err = file.WriteString(strings.Join(entries, "\n") + "\n")
	if err != nil {
		return fmt.Errorf("failed to write sorted data to file: %w", err)
	}

	return nil
}

// run orchestrates the main application logic using a Hacker News client.
func run(
	cfg *cliFlags,
	stdoutLogger, fileLogger *log.Logger,
	client hackerNewsClient,
) error {
	ids, err := client.getTopStories()
	if err != nil {
		return fmt.Errorf("failed to get top stories: %w", err)
	}

	stdoutLogger.Printf("Fetched %d stories. Displaying first %d...",
		len(ids), cfg.maxStories)
	stdoutLogger.Println(strings.Repeat("=", 80))

	storyCount := 0
	for i, id := range ids {
		if i >= cfg.maxStories {
			break
		}

		st, err := client.getStory(id)
		if err != nil {
			stdoutLogger.Printf("Failed to fetch story %d: %v", id, err)
			continue
		}

		isMatch := matches(st, cfg.keywords, cfg.domain)
		logStory(stdoutLogger, fileLogger, st, i+1, isMatch)
		storyCount++

		// Respect the delay between requests
		time.Sleep(cfg.delay)
	}

	stdoutLogger.Printf("\nProcessed %d stories.\n", storyCount)
	return nil
}

func main() {
	cfg, err := parseFlags()
	if err != nil {
		log.Fatalf("Failed to parse CLI flags: %v", err)
	}

	file, err := os.OpenFile(cfg.logFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file %q: %v", cfg.logFile, err)
	}
	defer file.Close()

	stdoutLogger := log.New(os.Stdout, "", log.LstdFlags)
	fileLogger := log.New(file, "", log.LstdFlags)

	// Create the real Hacker News client
	client := &hnClient{
		topStoriesURL:   "https://hacker-news.firebaseio.com/v0/topstories.json",
		itemURLTemplate: "https://hacker-news.firebaseio.com/v0/item/%d.json",
		maxStories:      cfg.maxStories,
	}

	if err := run(cfg, stdoutLogger, fileLogger, client); err != nil {
		log.Fatalf("Application error: %v", err)
	}

	if err := sortLogFile(file); err != nil {
		log.Fatalf("Failed to sort log file: %v", err)
	}
}
