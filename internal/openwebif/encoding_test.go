// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT
package openwebif

import (
	"testing"
	"unicode/utf8"
)

func TestConvertLatin1ToUTF8(t *testing.T) {
	tests := []struct {
		name     string
		latin1   []byte
		expected string
	}{
		{
			name:     "Möglichkeiten",
			latin1:   []byte{0x4D, 0xF6, 0x67, 0x6C, 0x69, 0x63, 0x68, 0x6B, 0x65, 0x69, 0x74, 0x65, 0x6E},
			expected: "Möglichkeiten",
		},
		{
			name:     "Kitzbühel",
			latin1:   []byte{0x4B, 0x69, 0x74, 0x7A, 0x62, 0xFC, 0x68, 0x65, 0x6C},
			expected: "Kitzbühel",
		},
		{
			name:     "Österreich",
			latin1:   []byte{0xD6, 0x73, 0x74, 0x65, 0x72, 0x72, 0x65, 0x69, 0x63, 0x68},
			expected: "Österreich",
		},
		{
			name:     "ASCII only",
			latin1:   []byte("Hello World"),
			expected: "Hello World",
		},
		{
			name:     "Mixed ASCII and umlauts",
			latin1:   []byte{0x48, 0x65, 0x6C, 0x6C, 0x6F, 0x20, 0xE4, 0xF6, 0xFC},
			expected: "Hello äöü",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertLatin1ToUTF8(tt.latin1)
			resultStr := string(result)

			if resultStr != tt.expected {
				t.Errorf("convertLatin1ToUTF8() = %q, want %q", resultStr, tt.expected)
			}

			if !utf8.Valid(result) {
				t.Errorf("convertLatin1ToUTF8() produced invalid UTF-8")
			}
		})
	}
}

func TestNeedsLatin1Conversion(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		contentType string
		expected    bool
	}{
		{
			name:        "UTF-8 declared",
			data:        []byte("Möglichkeiten"),
			contentType: "text/html; charset=utf-8",
			expected:    false,
		},
		{
			name:        "ISO-8859-1 declared",
			data:        []byte{0x4D, 0xF6, 0x67},
			contentType: "text/html; charset=iso-8859-1",
			expected:    true,
		},
		{
			name:        "Latin-1 declared",
			data:        []byte{0x4D, 0xF6, 0x67},
			contentType: "text/html; charset=latin1",
			expected:    true,
		},
		{
			name:        "Invalid UTF-8 without declaration",
			data:        []byte{0x4D, 0xF6, 0x67}, // Latin-1 "Mög"
			contentType: "text/html",
			expected:    true,
		},
		{
			name:        "Valid UTF-8 without declaration",
			data:        []byte("Möglichkeiten"),
			contentType: "text/html",
			expected:    false,
		},
		{
			name:        "ASCII only",
			data:        []byte("Hello"),
			contentType: "text/html",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := needsLatin1Conversion(tt.data, tt.contentType)
			if result != tt.expected {
				t.Errorf("needsLatin1Conversion() = %v, want %v", result, tt.expected)
			}
		})
	}
}
