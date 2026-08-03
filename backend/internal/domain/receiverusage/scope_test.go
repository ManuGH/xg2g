// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package receiverusage

import (
	"errors"
	"testing"
)

func TestFormatRestrictedAccessScope_Valid(t *testing.T) {
	scope, err := FormatRestrictedAccessScope("rec-vuplus-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "receiver:rec-vuplus-1:restricted-access:0"
	if scope != want {
		t.Fatalf("expected scope %q, got %q", want, scope)
	}
}

func TestFormatRestrictedAccessScope_Invalid(t *testing.T) {
	tests := []struct {
		recID     string
		slot      int
		wantErrIs error
	}{
		{"", 0, ErrInvalidReceiverID},
		{"rec@invalid", 0, ErrInvalidReceiverID},
		{"rec space", 0, ErrInvalidReceiverID},
		{"rec-1", -1, ErrInvalidSlotNumber},
	}

	for _, tt := range tests {
		_, err := FormatRestrictedAccessScope(tt.recID, tt.slot)
		if err == nil {
			t.Errorf("FormatRestrictedAccessScope(%q, %d) expected error, got nil", tt.recID, tt.slot)
		} else if !errors.Is(err, tt.wantErrIs) {
			t.Errorf("FormatRestrictedAccessScope(%q, %d) expected error %v, got %v", tt.recID, tt.slot, tt.wantErrIs, err)
		}
	}
}

func TestParseRestrictedAccessScope_Valid(t *testing.T) {
	recID, slot, err := ParseRestrictedAccessScope("receiver:rec-box-99:restricted-access:3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recID != "rec-box-99" {
		t.Errorf("expected recID %q, got %q", "rec-box-99", recID)
	}
	if slot != 3 {
		t.Errorf("expected slot 3, got %d", slot)
	}
}

func TestParseRestrictedAccessScope_Invalid(t *testing.T) {
	invalidScopes := []string{
		"",
		"invalid-prefix:rec-1:restricted-access:0",
		"receiver:rec-1:wrong-middle:0",
		"receiver::restricted-access:0",
		"receiver:rec-1:restricted-access:",
		"receiver:rec-1:restricted-access:-1",
		"receiver:rec-1:restricted-access:abc",
		"receiver:rec-1:restricted-access:01", // Non-canonical leading zero
		"receiver:rec-1:restricted-access:00", // Non-canonical leading zeros
	}

	for _, scope := range invalidScopes {
		t.Run("Scope_"+scope, func(t *testing.T) {
			_, _, err := ParseRestrictedAccessScope(scope)
			if err == nil {
				t.Errorf("ParseRestrictedAccessScope(%q) expected error, got nil", scope)
			} else if !errors.Is(err, ErrMalformedScopeKey) {
				t.Errorf("ParseRestrictedAccessScope(%q) expected error wrapping ErrMalformedScopeKey, got %v", scope, err)
			}
		})
	}
}

func TestParseRestrictedAccessScope_CanonicalRoundtrip(t *testing.T) {
	validScope := "receiver:rec-canon-1:restricted-access:12"
	recID, slot, err := ParseRestrictedAccessScope(validScope)
	if err != nil {
		t.Fatalf("unexpected error parsing valid scope: %v", err)
	}
	canonical, err := FormatRestrictedAccessScope(recID, slot)
	if err != nil {
		t.Fatalf("unexpected error formatting canonical scope: %v", err)
	}
	if canonical != validScope {
		t.Fatalf("canonical roundtrip mismatch: got %q, want %q", canonical, validScope)
	}
}

func TestFormatRestrictedAccessSlots_Valid(t *testing.T) {
	scopes, err := FormatRestrictedAccessSlots("rec-1", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scopes) != 3 {
		t.Fatalf("expected 3 scopes, got %d", len(scopes))
	}
	want := []string{
		"receiver:rec-1:restricted-access:0",
		"receiver:rec-1:restricted-access:1",
		"receiver:rec-1:restricted-access:2",
	}
	for i, s := range scopes {
		if s != want[i] {
			t.Errorf("slot %d got %q, want %q", i, s, want[i])
		}
	}
}

func TestFormatRestrictedAccessSlots_Zero(t *testing.T) {
	scopes, err := FormatRestrictedAccessSlots("rec-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scopes) != 0 {
		t.Fatalf("expected 0 scopes for maxSlots=0, got %d", len(scopes))
	}
}

func TestFormatRestrictedAccessSlots_Negative(t *testing.T) {
	_, err := FormatRestrictedAccessSlots("rec-1", -2)
	if err == nil {
		t.Fatalf("expected error for negative maxSlots, got nil")
	}
	if !errors.Is(err, ErrInvalidSlotQuantity) {
		t.Fatalf("expected ErrInvalidSlotQuantity, got %v", err)
	}
}

func TestFormatRestrictedAccessSlots_ExceedsMaxBound(t *testing.T) {
	_, err := FormatRestrictedAccessSlots("rec-1", MaxRestrictedAccessSlots+1)
	if err == nil {
		t.Fatalf("expected error for maxSlots > MaxRestrictedAccessSlots, got nil")
	}
	if !errors.Is(err, ErrInvalidSlotQuantity) {
		t.Fatalf("expected ErrInvalidSlotQuantity, got %v", err)
	}
}
