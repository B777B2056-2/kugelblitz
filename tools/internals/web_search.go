package internals

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/tools"
)

const searchTimeout = 10 * time.Second

// SearchBackend performs a web search and returns structured results.
type SearchBackend interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// SearchResult represents a single search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// WebSearchConfig holds configuration for the web_search tool.
type WebSearchConfig struct {
	Backend SearchBackend // custom backend; nil uses DuckDuckGo
}

// WebSearch searches the web using the configured backend.
type WebSearch struct {
	backend SearchBackend
}

func newWebSearch(cfg *WebSearchConfig) *WebSearch {
	ws := &WebSearch{}
	if cfg != nil && cfg.Backend != nil {
		ws.backend = cfg.Backend
	} else {
		ws.backend = &ddgBackend{client: &http.Client{Timeout: searchTimeout}}
	}
	return ws
}

func (t *WebSearch) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "web_search",
		Description: "Search the web and return a list of results with titles, snippets, and URLs.",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results (default 5, max 10)",
				},
			},
			"required": []string{"query"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":   map[string]any{"type": "string", "description": "The search query that was executed."},
				"results": map[string]any{"type": "array", "description": "List of results, each with {title, url, snippet}."},
			},
		},
	}
}

func (t *WebSearch) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	query, err := tools.Arg(detail, "query")
	if err != nil {
		return tools.ErrorResult(detail.ID, "web_search", err)
	}

	limit, err2 := tools.OptionalInt(detail, "limit", 5)
	if err2 != nil {
		return tools.ErrorResult(detail.ID, "web_search", err2)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 10 {
		limit = 10
	}

	results, err := t.backend.Search(ctx, query, limit)
	if err != nil {
		return tools.ErrorResult(detail.ID, "web_search", err)
	}

	// Convert to output format
	var items []map[string]any
	for _, r := range results {
		items = append(items, map[string]any{
			"title":   r.Title,
			"url":     r.URL,
			"snippet": r.Snippet,
		})
	}

	return tools.SuccessResult(detail.ID, "web_search", map[string]any{
		"query":   query,
		"results": items,
	})
}

// ---- DuckDuckGo backend ----

type ddgBackend struct {
	client *http.Client
}

func (d *ddgBackend) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("format", "json")

	reqURL := "https://lite.duckduckgo.com/lite/?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "kugelblitz/1.0")
	req.Header.Set("Accept", "text/html")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ddg search: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ddg search: http %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	results := parseDDGHTML(string(body), limit)
	if len(results) == 0 {
		// Try DDG HTML API fallback
		results = ddgHTMLFallback(ctx, d.client, query, limit)
	}
	return results, nil
}

func parseDDGHTML(html string, limit int) []SearchResult {
	var results []SearchResult

	// DuckDuckGo Lite returns results in <tr> rows with:
	// <td><a rel="nofollow" href="URL">result text</a></td>
	// We parse the simplified Lite HTML

	i := 0
	for {
		linkStart := findAfter(html, `<a rel="nofollow" href="`, i)
		if linkStart < 0 {
			break
		}
		linkStart += len(`<a rel="nofollow" href="`)
		linkEnd := findAfter(html, `"`, linkStart)
		if linkEnd < 0 {
			break
		}
		href := html[linkStart:linkEnd]

		textStart := findAfter(html, `>`, linkEnd)
		if textStart < 0 {
			break
		}
		textStart++ // skip '>'
		textEnd := findAfter(html, `</a>`, textStart)
		if textEnd < 0 {
			break
		}
		text := stripTags(html[textStart:textEnd])

		// Find snippet from the next <td class="result-snippet">
		snippet := ""
		snipStart := findAfter(html, `<td class="result-snippet">`, textEnd)
		if snipStart >= 0 {
			snipStart += len(`<td class="result-snippet">`)
			snipEnd := findAfter(html, `</td>`, snipStart)
			if snipEnd >= 0 {
				snippet = stripTags(html[snipStart:snipEnd])
			}
		}

		if href != "" && text != "" {
			results = append(results, SearchResult{
				Title:   text,
				URL:     href,
				Snippet: snippet,
			})
		}

		i = textEnd
		if len(results) >= limit {
			break
		}
	}

	return results
}

func ddgHTMLFallback(ctx context.Context, client *http.Client, query string, limit int) []SearchResult {
	// Fallback: use DuckDuckGo HTML search page
	params := url.Values{}
	params.Set("q", query)

	reqURL := "https://html.duckduckgo.com/html/?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "kugelblitz/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	var results []SearchResult
	i := 0
	for {
		linkStart := findAfter(html, `<a rel="nofollow" class="result__a" href="`, i)
		if linkStart < 0 {
			linkStart = findAfter(html, `class="result__a" href="`, i)
		}
		if linkStart < 0 {
			break
		}
		// Find href value
		hrefStart := findAfter(html, `href="`, linkStart-5)
		if hrefStart < 0 {
			hrefStart = linkStart
		} else {
			hrefStart += len(`href="`)
		}
		hrefEnd := findAfter(html, `"`, hrefStart)
		if hrefEnd < 0 {
			break
		}
		href := html[hrefStart:hrefEnd]

		textStart := findAfter(html, `>`, hrefEnd)
		if textStart < 0 {
			break
		}
		textStart++
		textEnd := findAfter(html, `</a>`, textStart)
		if textEnd < 0 {
			break
		}
		text := stripTags(html[textStart:textEnd])

		if href != "" && text != "" {
			results = append(results, SearchResult{Title: text, URL: href})
		}
		i = textEnd
		if len(results) >= limit {
			break
		}
	}
	return results
}

func findAfter(s, substr string, start int) int {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func stripTags(s string) string {
	var sb []byte
	inTag := false
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			inTag = true
			continue
		}
		if s[i] == '>' {
			inTag = false
			continue
		}
		if !inTag {
			sb = append(sb, s[i])
		}
	}
	// Decode common HTML entities
	str := string(sb)
	str = decodeEntities(str)
	return str
}

func decodeEntities(s string) string {
	repl := map[string]string{
		"&amp;":  "&",
		"&lt;":   "<",
		"&gt;":   ">",
		"&quot;": `"`,
		"&#x27;": "'",
		"&apos;": "'",
	}
	for k, v := range repl {
		s = replaceAll(s, k, v)
	}
	return s
}

func replaceAll(s, old, new string) string {
	result := ""
	for i := 0; i < len(s); {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			result += new
			i += len(old)
		} else {
			result += string(s[i])
			i++
		}
	}
	return result
}

// Ensure unused imports are valid
var _ = json.Marshal
