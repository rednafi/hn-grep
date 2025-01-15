package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// story represents a Hacker News story.
// Fields must be exported so the JSON package can unmarshal them.
type story struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	StoryURL string // Not in JSON; we'll populate it manually.
}

// cliFlags holds all command-line flag values.
type cliFlags struct {
	maxStories int
	keywords   []string
	domain     string
	htmlFile   string
	delay      time.Duration
}

// HTMLData represents the data passed to the HTML template.
type HTMLData struct {
	Keywords string
	Domain   string
	Stories  []story
}

// parseFlags parses and validates command-line flags, returning a fully populated *cliFlags.
func parseFlags() (*cliFlags, error) {
	maxStories := flag.Int("max-stories", 250, "Maximum number of stories to fetch")
	keywords := flag.String("keywords", "", "Comma-separated list of keywords to filter stories")
	domain := flag.String("domain", "", "Domain to filter stories by URL (optional)")
	htmlFile := flag.String("html-file", "grep.html", "Output HTML file for matched stories")
	delay := flag.Duration("delay", 100*time.Millisecond, "Delay between requests (e.g. 100ms)")

	flag.Parse()

	if *maxStories <= 0 {
		return nil, fmt.Errorf("max-stories must be a positive integer")
	}
	if strings.TrimSpace(*keywords) == "" {
		return nil, fmt.Errorf("keywords must be provided")
	}
	if *delay < 100*time.Millisecond {
		return nil, fmt.Errorf("delay must be greater than or equal to 100ms")
	}

	rawKeywords := strings.Split(*keywords, ",")
	cleanedKeywords := make([]string, 0, len(rawKeywords))
	for _, kw := range rawKeywords {
		kw = strings.TrimSpace(kw)
		if kw != "" {
			cleanedKeywords = append(cleanedKeywords, kw)
		}
	}

	return &cliFlags{
		maxStories: *maxStories,
		keywords:   cleanedKeywords,
		domain:     *domain,
		htmlFile:   *htmlFile,
		delay:      *delay,
	}, nil
}

// hackerNewsClient defines an interface for fetching top stories and individual story details.
type hackerNewsClient interface {
	getTopStories() ([]int, error)
	getStory(id int) (*story, error)
}

// hnClient implements hackerNewsClient, fetching data from the live Hacker News API.
type hnClient struct {
	topStoriesURL   string
	itemURLTemplate string
	maxStories      int
}

// Compile-time check that hnClient implements hackerNewsClient.
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

// getStory fetches the details of a single story by ID from Hacker News.
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

	s.StoryURL = fmt.Sprintf("https://news.ycombinator.com/item?id=%d", id)
	return &s, nil
}

// compilePattern compiles a regex pattern that matches any of the provided keywords as full words.
func compilePattern(keywords []string) string {
	escapedKeywords := make([]string, len(keywords))
	for i, kw := range keywords {
		// Escape regex metacharacters and lowercase each keyword
		escapedKeywords[i] = regexp.QuoteMeta(strings.ToLower(kw))
	}

	// Simulate word boundaries: (^|[^A-Za-z0-9_]) for the start and ($|[^A-Za-z0-9_]) for the end
	// Example: (?i)(^|[^A-Za-z0-9_])(go|rust|python)($|[^A-Za-z0-9_])
	return `(?i)(^|[^A-Za-z0-9_])(` + strings.Join(escapedKeywords, "|") + `)($|[^A-Za-z0-9_])`
}

// matches checks whether the given story's title or domain (URL) matches any
// of the specified keywords or the provided domain filter.
func matches(s *story, keywords []string, domain string) bool {
	// If domain is non-empty, check if the story's URL contains it (case-insensitive).
	if domain != "" && strings.Contains(strings.ToLower(s.URL), strings.ToLower(domain)) {
		return true
	}

	// Compile a single regex pattern for all keywords
	pattern := compilePattern(keywords)
	re := regexp.MustCompile(pattern)

	// Check if the story's title matches any keyword
	return re.MatchString(strings.ToLower(s.Title))
}

// writeHTML applies tmpl to data and writes the resulting HTML to htmlFilePath.
func writeHTML(htmlFilePath string, tmpl *template.Template, data HTMLData) error {
	file, err := os.OpenFile(htmlFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open HTML file %q: %w", htmlFilePath, err)
	}
	defer file.Close()

	if err := tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}
	return nil
}

// run orchestrates the high-level application logic: fetching top stories,
// filtering them, logging matches, and writing the matched stories to an HTML file.
func run(cfg *cliFlags, logger *log.Logger, client hackerNewsClient, tmpl *template.Template) error {
	ids, err := client.getTopStories()
	if err != nil {
		return fmt.Errorf("failed to get top stories: %w", err)
	}

	logger.Printf("Fetched %d stories. Displaying first %d...", len(ids), cfg.maxStories)
	logger.Println(strings.Repeat("=", 80))

	var matchedStories []story

	for i, id := range ids {
		if i >= cfg.maxStories {
			break
		}

		storyData, err := client.getStory(id)
		if err != nil {
			logger.Printf("Failed to fetch story %d: %v", id, err)
			continue
		}
		if storyData == nil {
			logger.Printf("Story %d not found (nil).", id)
			continue
		}

		// Log the story title to stdout
		logger.Printf("[%d] Title: %s", i+1, storyData.Title)

		// Check if this story matches the keywords or domain
		if matches(storyData, cfg.keywords, cfg.domain) {
			logger.Println("   MATCHED!")
			matchedStories = append(matchedStories, *storyData)
		} else {
			logger.Println("   NOT MATCHED.")
		}

		logger.Println(strings.Repeat("-", 80))
		time.Sleep(cfg.delay)
	}

	logger.Printf("\nMatched %d stories.\n", len(matchedStories))

	data := HTMLData{
		Keywords: strings.Join(cfg.keywords, ", "),
		Domain:   cfg.domain,
		Stories:  matchedStories,
	}

	if err := writeHTML(cfg.htmlFile, tmpl, data); err != nil {
		return fmt.Errorf("failed to write HTML file: %w", err)
	}

	return nil
}

// main is the entry point of the program, orchestrating flag parsing and the run sequence.
func main() {
	cfg, err := parseFlags()
	if err != nil {
		log.Fatalf("Failed to parse CLI flags: %v", err)
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)

	tmpl, err := template.ParseFiles("template.html")
	if err != nil {
		log.Fatalf("Failed to load HTML template: %v", err)
	}

	client := &hnClient{
		topStoriesURL:   "https://hacker-news.firebaseio.com/v0/topstories.json",
		itemURLTemplate: "https://hacker-news.firebaseio.com/v0/item/%d.json",
		maxStories:      cfg.maxStories,
	}

	if err := run(cfg, logger, client, tmpl); err != nil {
		log.Fatalf("Application error: %v", err)
	}
}
