package persist

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MarkdownEntry is a single key-value fact stored in a markdown file.
type MarkdownEntry struct {
	Section    string
	Key        string
	Value      string
	Source     string // provenance evidence (extractor source_evidence), may be empty
	Version    int
	Confidence float64
	UpdatedAt  time.Time
}

// MarkdownPersist implements IPersist and adds structured markdown read/write.
// The embedded IPersist backend handles raw byte storage.
type MarkdownPersist struct {
	backend IPersist
}

// NewMarkdownPersist creates a MarkdownPersist backed by the given IPersist.
func NewMarkdownPersist(backend IPersist) *MarkdownPersist {
	return &MarkdownPersist{backend: backend}
}

// Backend returns the underlying IPersist for direct access.
func (m *MarkdownPersist) Backend() IPersist { return m.backend }

// ---- IPersist implementation (delegates to backend) ----

func (m *MarkdownPersist) Store(ctx context.Context, key string, data []byte) error {
	return m.backend.Store(ctx, key, data)
}
func (m *MarkdownPersist) Load(ctx context.Context, key string) ([]byte, error) {
	return m.backend.Load(ctx, key)
}
func (m *MarkdownPersist) Delete(ctx context.Context, key string) error {
	return m.backend.Delete(ctx, key)
}
func (m *MarkdownPersist) List(ctx context.Context, prefix string) ([]string, error) {
	return m.backend.List(ctx, prefix)
}
func (m *MarkdownPersist) Exists(ctx context.Context, key string) bool {
	return m.backend.Exists(ctx, key)
}

// ---- Extended methods ----

// ReadAll parses a markdown file into structured entries.
func (m *MarkdownPersist) ReadAll(path string) ([]MarkdownEntry, error) {
	data, err := m.backend.Load(context.Background(), path)
	if err != nil {
		return nil, err
	}
	return parseMarkdown(data)
}

// WriteAll formats entries as markdown and stores the file.
func (m *MarkdownPersist) WriteAll(ctx context.Context, path string, entries []MarkdownEntry) error {
	data := formatMarkdown(entries)
	return m.backend.Store(ctx, path, data)
}

// ---- Markdown format helpers ----

func parseMarkdown(data []byte) ([]MarkdownEntry, error) {
	var entries []MarkdownEntry
	var currentSection string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			currentSection = strings.TrimSpace(trimmed[3:])
			continue
		}
		if strings.HasPrefix(trimmed, "- ") && currentSection != "" {
			rest := trimmed[2:]
			idx := strings.Index(rest, ": ")
			if idx <= 0 {
				continue
			}
			key := strings.TrimSpace(rest[:idx])
			restValue := strings.TrimSpace(rest[idx+2:])

			value := restValue
			version := 1
			confidence := 1.0
			updatedAt := time.Now()
			source := ""

			if metaIdx := strings.LastIndex(restValue, "`v"); metaIdx > 0 {
				meta := restValue[metaIdx:]
				value = strings.TrimSpace(restValue[:metaIdx])
				parseMarkdownMeta(meta, &version, &confidence, &updatedAt, &source)
			}

			entries = append(entries, MarkdownEntry{
				Section:    currentSection,
				Key:        key,
				Value:      value,
				Source:     source,
				Version:    version,
				Confidence: confidence,
				UpdatedAt:  updatedAt,
			})
		}
	}
	return entries, scanner.Err()
}

func formatMarkdown(entries []MarkdownEntry) []byte {
	// Group by case-insensitive section name; preserve the first-seen casing as
	// the section title and merge entries so nothing is dropped (P15).
	type section struct {
		title   string
		entries []MarkdownEntry
	}
	var order []string
	groups := make(map[string]*section)
	for _, e := range entries {
		key := strings.ToLower(strings.TrimSpace(e.Section))
		g, ok := groups[key]
		if !ok {
			g = &section{title: e.Section}
			groups[key] = g
			order = append(order, key)
		}
		g.entries = append(g.entries, e)
	}

	var sb strings.Builder
	sb.WriteString("# Project Memory\n\n")
	for _, key := range order {
		g := groups[key]
		fmt.Fprintf(&sb, "## %s\n", g.title)
		for _, e := range g.entries {
			meta := fmt.Sprintf("`v%d c%.2f %s`", e.Version, e.Confidence, e.UpdatedAt.Format("2006-01-02"))
			if e.Source != "" {
				meta = fmt.Sprintf("`v%d c%.2f %s src=%s`", e.Version, e.Confidence, e.UpdatedAt.Format("2006-01-02"), url.QueryEscape(e.Source))
			}
			fmt.Fprintf(&sb, "- %s: %s  %s\n", e.Key, e.Value, meta)
		}
		sb.WriteString("\n")
	}
	return []byte(sb.String())
}

func parseMarkdownMeta(meta string, version *int, confidence *float64, updatedAt *time.Time, source *string) {
	meta = strings.Trim(meta, "`")
	parts := strings.Fields(meta)
	for _, p := range parts {
		if v, ok := strings.CutPrefix(p, "src="); ok {
			if s, err := url.QueryUnescape(v); err == nil {
				*source = s
			}
		}
		if strings.HasPrefix(p, "v") {
			if v, err := strconv.Atoi(p[1:]); err == nil {
				*version = v
			}
		}
		if strings.HasPrefix(p, "c") {
			if c, err := strconv.ParseFloat(p[1:], 64); err == nil {
				*confidence = c
			}
		}
		if t, err := time.Parse("2006-01-02", p); err == nil {
			*updatedAt = t
		}
	}
}
