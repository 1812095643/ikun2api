package service

import "github.com/tidwall/gjson"

// Rebuild one array while copying untouched surrounding bytes only once.
func replaceRawJSONArray(body []byte, input gjson.Result, items []string) []byte {
	size := len(body) - len(input.Raw) + 2
	for index, item := range items {
		size += len(item)
		if index > 0 {
			size++
		}
	}
	result := make([]byte, 0, size)
	result = append(result, body[:input.Index]...)
	result = append(result, '[')
	for index, item := range items {
		if index > 0 {
			result = append(result, ',')
		}
		result = append(result, item...)
	}
	result = append(result, ']')
	return append(result, body[input.Index+len(input.Raw):]...)
}
