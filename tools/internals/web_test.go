package internals

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebFetch_StaticMarkdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html>
<html><head><title>Test Page</title></head>
<body>
  <h1>Hello World</h1>
  <p>This is a <strong>bold</strong> paragraph.</p>
  <ul><li>Item 1</li><li>Item 2</li></ul>
  <script>console.log('hidden')</script>
  <style>body { color: red; }</style>
  <a href="/more">Read more</a>
</body></html>`))
	}))
	defer srv.Close()

	tool := &WebFetch{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID:   "t1",
		Args: map[string]any{"url": srv.URL},
	})

	require.NotContains(t, result.Outputs, "error")
	assert.Equal(t, "Test Page", result.Outputs["title"])

	md := result.Outputs["markdown"].(string)
	t.Logf("Markdown output:\n%s", md)

	// Markdown should preserve headings
	assert.Contains(t, md, "Hello World")
	// Should preserve bold
	assert.Contains(t, md, "**bold**")
	// Should have list items
	assert.Contains(t, md, "Item 1")
	// Scripts and styles should be stripped
	assert.NotContains(t, md, "console.log")
	assert.NotContains(t, md, "body {")
	// Links should be preserved
	assert.Contains(t, md, "Read more")
	assert.Contains(t, md, "/more")
}

func TestWebFetch_MissingURL(t *testing.T) {
	tool := &WebFetch{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID:   "t2",
		Args: map[string]any{},
	})
	_, hasErr := result.Outputs["error"]
	assert.True(t, hasErr)
}

func TestWebFetch_BadURL(t *testing.T) {
	tool := &WebFetch{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID:   "t3",
		Args: map[string]any{"url": "not-a-valid-url"},
	})
	_, hasErr := result.Outputs["error"]
	assert.True(t, hasErr)
}

func TestWebFetch_Truncation(t *testing.T) {
	// Generate a page with lots of content
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html><html><body><p>`))
		for i := 0; i < 5000; i++ {
			w.Write([]byte("word "))
		}
		w.Write([]byte(`</p></body></html>`))
	}))
	defer srv.Close()

	tool := &WebFetch{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID:   "t5",
		Args: map[string]any{"url": srv.URL},
	})

	require.NotContains(t, result.Outputs, "error")
	md := result.Outputs["markdown"].(string)
	assert.LessOrEqual(t, len(md), maxMDLen+50, "markdown should be truncated")
}

func TestWebFetch_PlainText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello plain world"))
	}))
	defer srv.Close()

	tool := &WebFetch{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID:   "t4",
		Args: map[string]any{"url": srv.URL},
	})

	require.NotContains(t, result.Outputs, "error")
	md := result.Outputs["markdown"].(string)
	assert.Contains(t, md, "Hello plain world")
}

func TestWebFetch_StatusCodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	tool := &WebFetch{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID:   "t6",
		Args: map[string]any{"url": srv.URL},
	})
	_, hasErr := result.Outputs["error"]
	assert.True(t, hasErr)
}

func TestWebFetch_RenderJS_Fallback(t *testing.T) {
	// Chromedp requires Chrome installed; if not available, we expect an error.
	// This test just ensures the code path doesn't panic.
	tool := &WebFetch{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID:   "t7",
		Args: map[string]any{"url": "https://example.com", "render_js": true},
	})

	// May succeed (Chrome installed) or fail (no Chrome); both are fine.
	t.Logf("render_js result: %v", result.Outputs)
}

func TestCollapseBlankLines(t *testing.T) {
	input := "line1\n\n\n\nline2\n\n\nline3\n\n"
	output := collapseBlankLines(input)
	assert.Equal(t, "line1\n\nline2\n\nline3", output)
}

func TestExtractTitle(t *testing.T) {
	title := extractTitle("<html><head><title>My Title</title></head><body></body></html>")
	assert.Equal(t, "My Title", title)

	title = extractTitle("<html><body>no title</body></html>")
	assert.Equal(t, "", title)
}

func TestWebSearch_DuckDuckGo(t *testing.T) {
	tool := newWebSearch(nil)

	def := tool.Definition()
	assert.Equal(t, "web_search", def.Name)

	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID:   "ws1",
		Args: map[string]any{"query": "Go programming language"},
	})

	if _, hasErr := result.Outputs["error"]; hasErr {
		t.Logf("DDG search returned error (may be rate-limited): %v", result.Outputs["error"])
	} else {
		assert.Equal(t, "Go programming language", result.Outputs["query"])
		results, ok := result.Outputs["results"].([]map[string]any)
		if ok {
			t.Logf("Got %d results", len(results))
			for _, r := range results {
				t.Logf("  - %s (%s)", r["title"], r["url"])
			}
		}
	}
}

func TestWebSearch_LimitClamp(t *testing.T) {
	tool := newWebSearch(nil)
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID:   "ws2",
		Args: map[string]any{"query": "test", "limit": float64(100)},
	})
	if _, hasErr := result.Outputs["error"]; !hasErr {
		results, ok := result.Outputs["results"].([]map[string]any)
		if ok {
			assert.LessOrEqual(t, len(results), 10, "limit should be clamped to 10")
		}
	}
}

func TestStripTags(t *testing.T) {
	input := `<a href="/">Hello <b>World</b></a>`
	output := stripTags(input)
	assert.Equal(t, "Hello World", output)
}

func TestDecodeEntities(t *testing.T) {
	input := "A &amp; B &lt; C &gt; D"
	output := decodeEntities(input)
	assert.Equal(t, "A & B < C > D", output)
}
