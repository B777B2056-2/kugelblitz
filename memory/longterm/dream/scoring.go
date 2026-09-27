package dream

// Hybrid value scoring blends three deterministic write-side signals with the
// LLM's qualitative value rating.
//
// The three deterministic signals are derived from data the memory system already
// tracks: confidence (recency proxy), version (frequency of updates), and graph
// degree (connectivity / relevance proxy). The LLM score is the sole qualitative
// signal — its intrinsic-value rating (1-10).
//
// Calibration note: relevance and query-diversity would normally come from
// read-side tracking (recall counts / query logs), which this refactor
// deliberately defers. With only write-side signals available, the LLM score
// must carry the dominant weight as our best relevance proxy; otherwise a fresh
// item (version=1, degree=0) could never be promoted. The deterministic signals
// therefore modulate rather than gate: they push a borderline item over the
// promotion line or pull a stale one down, but the LLM remains primary.
//
// Weights sum to 1.0.
const (
	weightQualitative  = 0.50
	weightConnectivity = 0.20
	weightFrequency    = 0.15
	weightRecency      = 0.15
)

// Promotion and deprecation thresholds on the 0..1 hybrid score. A fresh
// memory (confidence=1.0, version=1, degree=0) maps to: LLM >= 8 → promote,
// LLM <= 2 → deprecate, LLM 3-7 → decided by the deterministic signals.
const (
	promoteThreshold   = 0.55
	deprecateThreshold = 0.30
)

// scoreCandidate computes a hybrid 0..1 value score for a memory item.
// confidence (0..1), version (>=1), graphDegree (>=0), and llmScore (1..10)
// are normalized and blended by the configured weights. The result drives
// consolidation (>= promoteThreshold) and deprecation (<= deprecateThreshold).
func scoreCandidate(confidence float64, version, graphDegree int, llmScore int) float64 {
	qualitative := clamp01(float64(llmScore) / 10.0)
	connectivity := clamp01(float64(graphDegree) / 10.0)
	frequency := clamp01(float64(version) / 5.0)
	recency := clamp01(confidence)

	return weightQualitative*qualitative +
		weightConnectivity*connectivity +
		weightFrequency*frequency +
		weightRecency*recency
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
