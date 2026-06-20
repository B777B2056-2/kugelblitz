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

// ConvertMapToJSONString serializes a map to a JSON string.
func ConvertMapToJSONString(m map[string]any) string {
	data, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(data)
}
