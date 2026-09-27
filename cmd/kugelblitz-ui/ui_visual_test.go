package main

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readCSS loads the source stylesheet. The test binary runs in the package dir,
// so static/style.css resolves directly.
func readCSS(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("static/style.css")
	require.NoError(t, err, "static/style.css must exist")
	return string(b)
}

// rule extracts the first CSS rule whose selector contains sel.
func rule(t *testing.T, css, sel string) string {
	t.Helper()
	idx := strings.Index(css, sel)
	require.NotEqual(t, -1, idx, "rule %q must exist", sel)
	end := strings.Index(css[idx:], "}")
	require.NotEqual(t, -1, end, "rule %q must be closed", sel)
	return css[idx : idx+end+1]
}

// TestStyleContract_VisualFixes pins the visual/accessibility fixes so they can't
// silently regress.
func TestStyleContract_VisualFixes(t *testing.T) {
	css := readCSS(t)

	// B — tertiary text brightened to meet WCAG AA on dark surfaces.
	assert.Contains(t, css, "--text-tertiary: #7a8296", "tertiary text must meet contrast")

	// C — media popover must be anchored to the input wrapper.
	assert.Contains(t, rule(t, css, ".input-wrapper {"), "position:relative",
		"input wrapper must be positioned so the media popover anchors to it")
	assert.Contains(t, css, "bottom:calc(100% + 8px)", "popover must open above the input row")

	// D — dead #mode-select rule removed; --amber token defined in :root.
	assert.NotContains(t, css, "#mode-select", "dead #mode-select rule must be removed")
	assert.Contains(t, css, "--amber:", "--amber token must be defined in :root")

	// E — config labels readable (>= 11px) and no redundant ::before accent bar.
	assert.Contains(t, rule(t, css, ".config-form label {"), "font-size:11px",
		"config labels must be at least 11px")
	assert.NotContains(t, css, ".config-title::before", "redundant accent bar must be removed")

	// Token hygiene — colors must come from :root, no hardcoded hex in rules.
	assert.Contains(t, css, "--text-inverse:", "--text-inverse token must be defined in :root")
	assert.NotContains(t, css, "color:#fff", "must not hardcode #fff; use --text-inverse")
	assert.NotContains(t, css, "var(--accent),#a78bfa", "must not hardcode --purple's value in the send-button gradient")
	assert.NotContains(t, css, "color:#0d0f16", "must not hardcode --bg-root as text color")

	// Dead selector — media query must target the real sidebar id.
	assert.NotContains(t, css, "#settings-sidebar {", "dead #settings-sidebar selector must be removed")
}

// TestStaticAssets_IndexHasNoExternalFonts guards against regressing to
// CDN-hosted web fonts, which fail to load offline and in some networks.
func TestStaticAssets_IndexHasNoExternalFonts(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, "GET", "/", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "fonts.googleapis.com",
		"web fonts must not be fetched from a CDN")
	assert.NotContains(t, rec.Body.String(), "fonts.gstatic.com",
		"web fonts must not be fetched from a CDN")
}

// TestStaticAssets_VendorMarkedServed guards the local vendoring of the Markdown
// renderer: message rendering must not depend on an external CDN.
func TestStaticAssets_VendorMarkedServed(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, "GET", "/vendor/marked.min.js", "")
	require.Equal(t, http.StatusOK, rec.Code, "vendored marked.min.js must be served locally")
	assert.Contains(t, rec.Body.String(), "marked", "vendored file must contain the marked library")
}

// TestStaticAssets_IndexHasNoMarkdownCDN guards against regressing to CDN-hosted
// marked.js, which breaks offline/self-hosted rendering.
func TestStaticAssets_IndexHasNoMarkdownCDN(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, "GET", "/", "")
	require.Equal(t, http.StatusOK, rec.Code)
	html := rec.Body.String()
	assert.NotContains(t, html, "cdn.jsdelivr.net", "marked must not come from a CDN")
	assert.Contains(t, html, "/vendor/marked.min.js", "index.html must reference the local vendored marked")
	assert.Contains(t, html, "/vendor/purify.min.js", "index.html must reference the local vendored DOMPurify")
}

// TestStaticAssets_VendorPurifyServed guards the local vendoring of the HTML
// sanitizer used to neutralize LLM output.
func TestStaticAssets_VendorPurifyServed(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, "GET", "/vendor/purify.min.js", "")
	require.Equal(t, http.StatusOK, rec.Code, "vendored DOMPurify must be served locally")
	assert.Contains(t, rec.Body.String(), "DOMPurify", "vendored file must contain DOMPurify")
}

// TestChatJS_SanitizesMarkdown guards the XSS fix: rendered LLM output must pass
// through DOMPurify (marked v12 does not sanitize on its own).
func TestChatJS_SanitizesMarkdown(t *testing.T) {
	b, err := os.ReadFile("static/chat.js")
	require.NoError(t, err, "static/chat.js must exist")
	js := string(b)

	assert.Contains(t, js, "DOMPurify.sanitize", "chat.js must sanitize rendered markdown")
	assert.Contains(t, js, "function md(", "chat.js must route markdown through a sanitizing helper")

	// Rollback banner must be a class, not inline style with hardcoded fallbacks.
	assert.NotContains(t, js, "style=\"background:var(--yellow-bg", "rollback must not use inline style")
}
