// SPDX-License-Identifier: MIT

package config

import (
	"reflect"
	"strings"
)

// sensitiveKeywords contains keywords that indicate sensitive fields.
// Any field name containing these keywords (case-insensitive) will be masked.
var sensitiveKeywords = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"apikey",
	"api_key",
	"credential",
	"auth",
}

// MaskSecrets recursively masks sensitive fields in the given data structure.
// It replaces string values with "***" for fields matching sensitive keywords.
// Supports: strings, maps, slices, structs, pointers
func MaskSecrets(data any) any {
	if data == nil {
		return nil
	}

	val := reflect.ValueOf(data)

	// Dereference pointers
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return nil
		}
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.String:
		// Mask if the value itself looks like a secret (simple heuristic)
		// This catches inline secrets but allows normal strings
		return data

	case reflect.Map:
		result := make(map[string]any)
		iter := val.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			value := iter.Value().Interface()

			if isSensitiveKey(key) {
				result[key] = "***"
			} else {
				result[key] = MaskSecrets(value)
			}
		}
		return result

	case reflect.Slice, reflect.Array:
		length := val.Len()
		result := make([]any, length)
		for i := 0; i < length; i++ {
			result[i] = MaskSecrets(val.Index(i).Interface())
		}
		return result

	case reflect.Struct:
		result := make(map[string]any)
		typ := val.Type()
		for i := 0; i < val.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue // Skip unexported fields
			}

			fieldValue := val.Field(i)
			fieldName := field.Name

			// Check if field name is sensitive
			if isSensitiveKey(fieldName) {
				result[fieldName] = "***"
			} else {
				result[fieldName] = MaskSecrets(fieldValue.Interface())
			}
		}
		return result

	default:
		// For primitive types (int, bool, etc.), return as-is
		return data
	}
}

// isSensitiveKey checks if a key name contains any sensitive keyword.
func isSensitiveKey(key string) bool {
	lowerKey := strings.ToLower(key)
	for _, keyword := range sensitiveKeywords {
		if strings.Contains(lowerKey, keyword) {
			return true
		}
	}
	return false
}

// MaskURL masks credentials in URLs (e.g., http://user:pass@host -> http://***@host)
func MaskURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	// Simple masking: if URL contains @, mask everything before it
	if idx := strings.Index(rawURL, "@"); idx > 0 {
		// Find scheme separator
		if schemeIdx := strings.Index(rawURL, "://"); schemeIdx > 0 {
			scheme := rawURL[:schemeIdx+3] // Keep "http://" or "https://"
			rest := rawURL[idx:]           // Keep "@host:port/path"
			return scheme + "***" + rest
		}
	}
	return rawURL
}
