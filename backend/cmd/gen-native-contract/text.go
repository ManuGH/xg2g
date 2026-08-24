// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"strings"
	"unicode"
)

// docLines normalises an OpenAPI description into comment lines: trailing
// whitespace removed, leading and trailing blank lines dropped, interior blank
// lines kept because they carry the paragraph structure of the spec text.
func docLines(doc string) []string {
	if strings.TrimSpace(doc) == "" {
		return nil
	}

	raw := strings.Split(doc, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		lines = append(lines, strings.TrimRight(line, " \t"))
	}

	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// splitWireWords breaks a wire value into its words: `local_https` and `P-256`
// both split on their separator, `androidTv` splits on the case change.
func splitWireWords(value string) []string {
	var words []string
	var current strings.Builder

	flush := func() {
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}

	runes := []rune(value)
	for i, r := range runes {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r) && i > 0 && unicode.IsLower(runes[i-1]):
			flush()
			current.WriteRune(r)
		default:
			current.WriteRune(r)
		}
	}
	flush()

	return words
}

func titleWord(word string) string {
	if word == "" {
		return ""
	}
	runes := []rune(strings.ToLower(word))
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// pascalCase renders a wire name as an UpperCamelCase identifier fragment:
// `crv` becomes `Crv`, `device_jwk` becomes `DeviceJwk`.
func pascalCase(value string) string {
	var b strings.Builder
	for _, word := range splitWireWords(value) {
		b.WriteString(titleWord(word))
	}
	return b.String()
}

// lowerCamelCase renders a wire name as a lowerCamelCase identifier:
// `access_token` becomes `accessToken`, and a name that is already camelCase —
// `pairingId`, `startXmltv` — comes back unchanged.
func lowerCamelCase(value string) string {
	words := splitWireWords(value)
	if len(words) == 0 {
		return value
	}
	var b strings.Builder
	b.WriteString(strings.ToLower(words[0]))
	for _, word := range words[1:] {
		b.WriteString(titleWord(word))
	}
	return b.String()
}
