package utils

import "encoding/json"

// ConvertJSONStringToMap parses a JSON string into a map.
func ConvertJSONStringToMap(jsonString string) map[string]any {
	var result map[string]any
	err := json.Unmarshal([]byte(jsonString), &result)
	if err != nil {
		return nil
	}
	return result
}

// StrToInt64 converts an interface{} numeric value to int64.
func StrToInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}

// ConvertMapToJSONString serializes a map to a JSON string.
func ConvertMapToJSONString(m map[string]any) string {
	data, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(data)
}
