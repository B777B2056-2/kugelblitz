package memory

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chromaEnabled() bool {
	return os.Getenv("CHROMA_URL") != ""
}

func TestChromaClient_AddAndSearch(t *testing.T) {
	if !chromaEnabled() {
		t.Skip("CHROMA_URL not set")
	}
	client, err := NewChromaClient(getEnvChromaURL(), "kugelblitz_test")
	require.NoError(t, err)

	// Add documents
	err = client.Add([]string{
		"用户偏好使用 Go 语言开发",
		"项目使用 DeepSeek 作为 LLM 提供商",
		"上次部署的目标是生产环境",
	}, []map[string]any{
		{"key": "language", "section": "user_prefs"},
		{"key": "llm_provider", "section": "project_facts"},
		{"key": "deploy_target", "section": "project_facts"},
	})
	require.NoError(t, err)

	// Semantic search
	results, err := client.Search("编程语言偏好", SearchSemantic, 2)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Contains(t, results[0].Document, "Go")

	// Keyword search (BM25)
	results, err = client.Search("DeepSeek", SearchBM25, 2)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Contains(t, results[0].Document, "DeepSeek")
}

func TestChromaClient_Sync(t *testing.T) {
	if !chromaEnabled() {
		t.Skip("CHROMA_URL not set")
	}
	client, err := NewChromaClient(getEnvChromaURL(), "kugelblitz_sync_test")
	require.NoError(t, err)

	facts := []Fact{
		{Section: "prefs", Key: "lang", Value: "Go"},
		{Section: "facts", Key: "deploy", Value: "production"},
	}

	err = client.Sync(facts)
	require.NoError(t, err)

	results, _ := client.Search("deploy", SearchHybrid, 5)
	assert.NotEmpty(t, results)
}

func TestChromaClient_DisabledFallback(t *testing.T) {
	client := NewChromaClientOrNil()
	assert.Nil(t, client, "should return nil when CHROMA_URL not set")
}

func getEnvChromaURL() string {
	url := os.Getenv("CHROMA_URL")
	if url == "" {
		url = "http://localhost:8000"
	}
	return url
}
