package dream

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScoreCandidate_AllMax(t *testing.T) {
	assert.InDelta(t, 1.0, scoreCandidate(1.0, 5, 10, 10), 1e-9)
}

func TestScoreCandidate_AllMin(t *testing.T) {
	assert.InDelta(t, 0.0, scoreCandidate(0.0, 0, 0, 0), 1e-9)
}

func TestScoreCandidate_DeterministicCanOutweighLowLLM(t *testing.T) {
	// High recency + frequency + connectivity lift a low-LLM item to promotion.
	s := scoreCandidate(1.0, 5, 10, 1)
	assert.GreaterOrEqual(t, s, promoteThreshold)
	assert.InDelta(t, 0.55, s, 1e-9)
}

func TestScoreCandidate_StaleConfidencePullsDownHighLLM(t *testing.T) {
	// A stale, low-confidence, unconnected item is not promoted despite LLM=10.
	s := scoreCandidate(0.1, 1, 0, 10)
	assert.Less(t, s, promoteThreshold)
}

func TestScoreCandidate_ClampsOverflow(t *testing.T) {
	// Oversized version/degree must not exceed 1.0.
	s := scoreCandidate(1.0, 100, 100, 10)
	assert.InDelta(t, 1.0, s, 1e-9)
}

func TestScoreCandidate_DeprecationBoundary(t *testing.T) {
	// Low recency + no connectivity + low LLM → below the deprecation bar.
	s := scoreCandidate(0.1, 1, 0, 1)
	assert.LessOrEqual(t, s, deprecateThreshold)
}
