package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConvertJSONStringToMap_Valid(t *testing.T) {
	result := ConvertJSONStringToMap(`{"key": "value", "num": 42}`)
	assert.Equal(t, "value", result["key"])
	assert.Equal(t, float64(42), result["num"])
}

func TestConvertJSONStringToMap_Invalid(t *testing.T) {
	result := ConvertJSONStringToMap(`not json`)
	assert.Nil(t, result)
}

func TestConvertJSONStringToMap_Empty(t *testing.T) {
	result := ConvertJSONStringToMap("")
	assert.Nil(t, result)
}

func TestConvertMapToJSONString_Valid(t *testing.T) {
	result := ConvertMapToJSONString(map[string]any{"key": "value"})
	assert.Contains(t, result, `"key"`)
	assert.Contains(t, result, `"value"`)
}

func TestConvertMapToJSONString_Empty(t *testing.T) {
	result := ConvertMapToJSONString(map[string]any{})
	assert.Equal(t, "{}", result)
}

func TestGenerateMessageID(t *testing.T) {
	id1 := GenerateMessageID()
	id2 := GenerateMessageID()
	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2, "generated IDs should be unique")
	assert.Contains(t, id1, "msg-")
}
