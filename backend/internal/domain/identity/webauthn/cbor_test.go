// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package webauthn

import (
	"errors"
	"testing"
)

func TestDecodeCBORRejectsLengthsLargerThanInput(t *testing.T) {
	tests := map[string][]byte{
		"byte string": {0x5a, 0xff, 0xff, 0xff, 0xff},
		"text string": {0x7a, 0xff, 0xff, 0xff, 0xff},
		"array":       {0x9a, 0xff, 0xff, 0xff, 0xff},
		"map":         {0xba, 0xff, 0xff, 0xff, 0xff},
	}

	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeCBOR(payload)
			if !errors.Is(err, ErrInvalidCBOR) {
				t.Fatalf("expected ErrInvalidCBOR, got %v", err)
			}
		})
	}
}

func TestDecodeCBORRejectsExcessiveNesting(t *testing.T) {
	payload := make([]byte, maxCBORNestingDepth+2)
	for i := 0; i < len(payload)-1; i++ {
		payload[i] = 0xc0 // tag 0 wrapping the following value
	}
	payload[len(payload)-1] = 0xf6 // null

	_, err := DecodeCBOR(payload)
	if !errors.Is(err, ErrInvalidCBOR) {
		t.Fatalf("expected ErrInvalidCBOR, got %v", err)
	}
}

func TestDecodeCBORRejectsIntegersOutsideInt64Domain(t *testing.T) {
	payloads := [][]byte{
		{0x1b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		{0x3b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	}

	for _, payload := range payloads {
		_, err := DecodeCBOR(payload)
		if !errors.Is(err, ErrInvalidCBOR) {
			t.Fatalf("expected ErrInvalidCBOR, got %v", err)
		}
	}
}
