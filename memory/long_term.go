package memory

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"kugelblitz/core"
)

// Fact is a single versioned entry in long-term memory.
// Confidence decays exponentially over time; new facts start at 1.0.
// When a conflict occurs (same section+key, different value),
// the version with higher confidence wins.
type Fact struct {
	Section    string
	Key        string
	Value      string
	Version    int       // starts at 1
	Confidence float64   // 0.0–1.0, decays over time
	UpdatedAt  time.Time // last update timestamp
}

// confidenceDecayPerDay is the daily decay factor.
// confidence *= 0.95^(days_since_update)
const confidenceDecayPerDay = 0.95

// LongTermMemory stores facts in MEMORY.md. See package doc.
type LongTermMemory struct {
	facts []Fact
	store VectorStore // optional ChromaDB
	mu    sync.RWMutex
}

func memoryPath() string { return core.GetWorkspace().MemoryFile() }

func NewLongTermMemory() (*LongTermMemory, error) {
	ltm := &LongTermMemory{}
	if c := NewChromaClientOrNil(); c != nil {
		ltm.store = c
	}
	if err := ltm.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return ltm, nil
}

// ---- CRUD ----

// Store upserts a fact with confidence-based conflict resolution.
// Returns the winning fact and whether a conflict existed.
func (ltm *LongTermMemory) Store(section, key, value string) (winner Fact, conflict *Fact, _ error) {
	ltm.mu.Lock()
	defer ltm.mu.Unlock()
	now := time.Now()

	section = ltm.normalize(section)
	// Find existing entry
	var curIdx = -1
	for i := range ltm.facts {
		if ltm.normalize(ltm.facts[i].Section) == section && ltm.facts[i].Key == key {
			curIdx = i
			break
		}
	}

	newFact := Fact{Section: section, Key: key, Value: value, Version: 1, Confidence: 1.0, UpdatedAt: now}

	if curIdx < 0 {
		// New fact — no conflict
		ltm.facts = append(ltm.facts, newFact)
		ltm.write()
		go ltm.syncStore()
		return newFact, nil, nil
	}

	// Existing fact found
	existing := ltm.facts[curIdx]
	decayed := ltm.decayConfidence(existing)

	isSame := existing.Value == value || ltm.isSemanticMatch(existing.Value, value)

	switch {
	case isSame:
		// Same (or semantically equivalent) value — bump confidence, use new wording
		if existing.Value != value {
			existing.Value = value // prefer the newer phrasing
		}
		existing.Confidence = math.Min(1.0, math.Max(decayed.Confidence, newFact.Confidence)+0.1)
		existing.UpdatedAt = now
		existing.Version++
		ltm.facts[curIdx] = existing
		ltm.write()
		go ltm.syncStore()
		return existing, nil, nil

	case newFact.Confidence > decayed.Confidence:
		// New fact has higher confidence — it wins
		newFact.Version = existing.Version + 1
		conflictCopy := ltm.facts[curIdx]
		ltm.facts[curIdx] = newFact
		ltm.write()
		go ltm.syncStore()
		return newFact, &conflictCopy, nil

	default:
		// Old fact has higher confidence — keep it, report conflict
		c := existing // copy
		return c, &Fact{Section: section, Key: key, Value: value, Version: c.Version + 1, Confidence: 1.0}, nil
	}
}

// Get returns the current value for a key (with decayed.Confidence confidence).
func (ltm *LongTermMemory) Get(section, key string) (Fact, bool) {
	ltm.mu.RLock()
	defer ltm.mu.RUnlock()

	section = ltm.normalize(section)
	for _, f := range ltm.facts {
		if ltm.normalize(f.Section) == section && f.Key == key {
			d := ltm.decayConfidence(f)
			return d, true
		}
	}
	return Fact{}, false
}

// Remove permanently deletes a fact — use with caution.
func (ltm *LongTermMemory) Remove(section, key string) error {
	ltm.mu.Lock()
	defer ltm.mu.Unlock()

	section = ltm.normalize(section)
	for i, f := range ltm.facts {
		if ltm.normalize(f.Section) == section && f.Key == key {
			ltm.facts = append(ltm.facts[:i], ltm.facts[i+1:]...)
			ltm.write()
			go ltm.syncStore()
			return nil
		}
	}
	return nil
}

func (ltm *LongTermMemory) GetSection(section string) []Fact {
	ltm.mu.RLock()
	defer ltm.mu.RUnlock()

	var result []Fact
	section = ltm.normalize(section)
	for _, f := range ltm.facts {
		if ltm.normalize(f.Section) == section {
			result = append(result, ltm.decayConfidence(f))
		}
	}
	return result
}

func (ltm *LongTermMemory) Search(query string) []Fact {
	return ltm.SearchWithMode(query, SearchBM25)
}

func (ltm *LongTermMemory) SearchWithMode(query string, mode SearchMode) []Fact {
	ltm.mu.RLock()
	defer ltm.mu.RUnlock()

	if ltm.store != nil && (mode == SearchSemantic || mode == SearchHybrid) {
		var facts []Fact
		results, err := ltm.store.Search(query, mode, 10)
		if err == nil && len(results) > 0 {
			for _, r := range results {
				section, _ := r.Metadata["section"].(string)
				key, _ := r.Metadata["key"].(string)
				if section == "" || key == "" {
					continue
				}
				fact, _ := ltm.Get(section, key)
				if fact.Key != "" {
					facts = append(facts, fact)
				}
			}
			return facts
		}
		if mode == SearchSemantic {
			return facts
		}
	}

	q := strings.ToLower(query)
	var results []Fact
	for _, f := range ltm.facts {
		if strings.Contains(strings.ToLower(f.Section), q) ||
			strings.Contains(strings.ToLower(f.Key), q) ||
			strings.Contains(strings.ToLower(f.Value), q) {
			results = append(results, ltm.decayConfidence(f))
		}
	}
	return results
}

// ---- Confidence decay ----

func (ltm *LongTermMemory) decayConfidence(f Fact) Fact {
	if f.Confidence <= 0 {
		return f
	}
	days := time.Since(f.UpdatedAt).Hours() / 24
	if days <= 0 {
		return f
	}
	f.Confidence = math.Max(0.1, f.Confidence*math.Pow(confidenceDecayPerDay, days))
	return f
}

// ---- File I/O ----

func (ltm *LongTermMemory) load() error {
	f, err := os.Open(memoryPath())
	if err != nil {
		return err
	}
	defer f.Close()

	var currentSection string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			currentSection = strings.TrimSpace(trimmed[3:])
			continue
		}

		if strings.HasPrefix(trimmed, "- ") && currentSection != "" {
			rest := trimmed[2:]
			// Parse: key: value  (and optionally metadata like v2 c0.95)
			idx := strings.Index(rest, ": ")
			if idx <= 0 {
				continue
			}
			key := strings.TrimSpace(rest[:idx])
			restValue := strings.TrimSpace(rest[idx+2:])

			// Extract optional metadata: `v2 c0.95 2026-06-21`
			value := restValue
			version := 1
			confidence := 1.0
			updatedAt := time.Now()

			if metaIdx := strings.LastIndex(restValue, "`v"); metaIdx > 0 {
				meta := restValue[metaIdx:]
				value = strings.TrimSpace(restValue[:metaIdx])
				// parse `v2 c0.95 2026-06-21`
				ltm.parseMetadata(meta, &version, &confidence, &updatedAt)
			}

			ltm.facts = append(ltm.facts, Fact{
				Section:    currentSection,
				Key:        key,
				Value:      value,
				Version:    version,
				Confidence: confidence,
				UpdatedAt:  updatedAt,
			})
		}
	}
	return scanner.Err()
}

func (ltm *LongTermMemory) parseMetadata(meta string, version *int, confidence *float64, updatedAt *time.Time) {
	meta = strings.Trim(meta, "`")
	parts := strings.Fields(meta)
	for _, p := range parts {
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

func (ltm *LongTermMemory) write() error {
	var sections []string
	seen := make(map[string]bool)
	entries := make(map[string][]Fact)

	for _, f := range ltm.facts {
		if !seen[f.Section] {
			sections = append(sections, f.Section)
			seen[f.Section] = true
		}
		entries[f.Section] = append(entries[f.Section], f)
	}

	var sb strings.Builder
	sb.WriteString("# Project Memory\n\n")

	for _, sec := range sections {
		sb.WriteString(fmt.Sprintf("## %s\n", sec))
		for _, f := range entries[sec] {
			meta := fmt.Sprintf("`v%d c%.2f %s`", f.Version, f.Confidence, f.UpdatedAt.Format("2006-01-02"))
			sb.WriteString(fmt.Sprintf("- %s: %s  %s\n", f.Key, f.Value, meta))
		}
		sb.WriteString("\n")
	}

	return os.WriteFile(memoryPath(), []byte(sb.String()), 0644)
}

// semanticJudge is a function that determines if two values are semantically equivalent.
// Set externally to use an LLM provider; defaults to heuristic comparison.
var semanticJudge func(oldValue, newValue string) bool

// SetSemanticJudge configures LLM-based semantic comparison.
func SetSemanticJudge(fn func(oldValue, newValue string) bool) {
	semanticJudge = fn
}

// isSemanticMatch returns true if two values are semantically equivalent.
// Uses the configured LLM judge if available; falls back to heuristics.
func (ltm *LongTermMemory) isSemanticMatch(a, b string) bool {
	if a == b {
		return true
	}
	la, lb := strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	if la == lb {
		return true
	}
	if (strings.Contains(la, lb) || strings.Contains(lb, la)) {
		return true
	}
	if semanticJudge != nil {
		return semanticJudge(la, lb)
	}
	if ltm.store != nil {
		results, err := ltm.store.Search(la, SearchSemantic, 1)
		if err == nil && len(results) > 0 && results[0].Score > 0.85 {
			return true
		}
	}
	return false
}

func (ltm *LongTermMemory) syncStore() {
	if ltm.store == nil {
		return
	}
	ltm.mu.RLock()
	facts := make([]Fact, len(ltm.facts))
	copy(facts, ltm.facts)
	ltm.mu.RUnlock()
	_ = ltm.store.Sync(facts)
}

func (ltm *LongTermMemory) normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
