package internals

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/tools"

	md "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
)

const (
	fetchTimeout  = 15 * time.Second
	renderTimeout = 30 * time.Second
	maxMDLen      = 16000
)

// WebFetch fetches a URL and returns its content as Markdown.
// Set render_js: true to use a headless browser for JavaScript-heavy pages.
type WebFetch struct{}

func (t *WebFetch) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "web_fetch",
		Description: "Fetch a web page and convert it to Markdown. Use render_js: true for JavaScript-rendered pages (requires Chrome/Chromium installed).",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to fetch (must start with http:// or https://)",
				},
				"render_js": map[string]any{
					"type":        "boolean",
					"description": "If true, render the page in a headless browser before extracting content. Use for SPAs and JS-heavy pages.",
				},
			},
			"required": []string{"url"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":      map[string]any{"type": "string", "description": "The fetched URL."},
				"title":    map[string]any{"type": "string", "description": "Page title from <title> tag."},
				"markdown": map[string]any{"type": "string", "description": "Page content in Markdown (max 16000 chars)."},
			},
		},
	}
}

func (t *WebFetch) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	urlStr, err := tools.Arg(detail, "url")
	if err != nil {
		return tools.ErrorResult(detail.ID, "web_fetch", err)
	}

	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		return tools.ErrorResult(detail.ID, "web_fetch", fmt.Errorf("url must start with http:// or https://"))
	}

	renderJS := false
	if v, ok := detail.Args["render_js"]; ok {
		if b, ok := v.(bool); ok {
			renderJS = b
		}
	}

	var htmlStr string
	if renderJS {
		htmlStr, err = fetchDynamic(ctx, urlStr)
		if err != nil {
			return tools.ErrorResult(detail.ID, "web_fetch", fmt.Errorf("dynamic fetch: %w", err))
		}
	} else {
		title, rawHTML, err := fetchStatic(ctx, urlStr)
		if err != nil {
			return tools.ErrorResult(detail.ID, "web_fetch", err)
		}
		// For static fetch, we already have the HTML; extract title separately
		htmlStr = rawHTML
		_ = title
	}

	title := extractTitle(htmlStr)
	markdown, err := htmlToMarkdown(htmlStr)
	if err != nil {
		return tools.ErrorResult(detail.ID, "web_fetch", fmt.Errorf("convert to markdown: %w", err))
	}

	if len(markdown) > maxMDLen {
		markdown = markdown[:maxMDLen] + "\n\n... (truncated)"
	}

	return tools.SuccessResult(detail.ID, "web_fetch", map[string]any{
		"url":      urlStr,
		"title":    title,
		"markdown": markdown,
	})
}

// ---- Static fetch ----

func fetchStatic(ctx context.Context, urlStr string) (title, html string, err error) {
	client := &http.Client{Timeout: fetchTimeout}
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "kugelblitz/1.0")
	req.Header.Set("Accept", "text/html, text/plain, */*")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("http status %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // 5MB max
	if err != nil {
		return "", "", err
	}

	return extractTitle(string(raw)), string(raw), nil
}

// ---- Dynamic fetch (headless Chrome) ----

func fetchDynamic(ctx context.Context, urlStr string) (string, error) {
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx,
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	defer allocCancel()

	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	taskCtx, taskCancel = context.WithTimeout(taskCtx, renderTimeout)
	defer taskCancel()

	var htmlStr string
	err := chromedp.Run(taskCtx,
		chromedp.Navigate(urlStr),
		chromedp.WaitReady("body"),
		chromedp.OuterHTML("html", &htmlStr),
	)
	if err != nil {
		return "", fmt.Errorf("chromedp: %w", err)
	}
	return htmlStr, nil
}

// ---- HTML → Markdown ----

func htmlToMarkdown(htmlStr string) (string, error) {
	markdown, err := md.ConvertString(htmlStr)
	if err != nil {
		return "", err
	}
	return collapseBlankLines(markdown), nil
}

func extractTitle(htmlStr string) string {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return ""
	}
	var title string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
			title = strings.TrimSpace(n.FirstChild.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if title != "" {
				return
			}
			walk(c)
		}
	}
	walk(doc)
	return title
}

func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	prevBlank := false
	for _, line := range lines {
		trimmed := strings.TrimRightFunc(line, unicode.IsSpace)
		isBlank := trimmed == ""
		if isBlank {
			if !prevBlank && len(out) > 0 {
				out = append(out, "")
				prevBlank = true
			}
		} else {
			out = append(out, trimmed)
			prevBlank = false
		}
	}
	// Trim leading/trailing blank lines
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}
