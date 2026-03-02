package auth

import (
	"testing"
	"time"
)

func TestInMemorySessionTokenStore_CreateResolveDelete(t *testing.T) {
	store := NewInMemorySessionTokenStore()

	sessionID, err := store.CreateSession("token-123", time.Minute)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if sessionID == "" {
		t.Fatal("CreateSession() returned empty session ID")
	}
	if sessionID == "token-123" {
		t.Fatal("session ID must be opaque and differ from token")
	}

	token, ok := store.ResolveSessionToken(sessionID)
	if !ok {
		t.Fatal("ResolveSessionToken() session not found")
	}
	if token != "token-123" {
		t.Fatalf("ResolveSessionToken() = %q, want %q", token, "token-123")
	}

	store.InvalidateSession(sessionID)
	if _, ok := store.ResolveSessionToken(sessionID); ok {
		t.Fatal("deleted session must not resolve")
	}
}

func TestInMemorySessionTokenStore_ExpiredSession(t *testing.T) {
	store := NewInMemorySessionTokenStore()

	sessionID, err := store.CreateSession("token-123", time.Millisecond)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	if _, ok := store.ResolveSessionToken(sessionID); ok {
		t.Fatal("expired session must not resolve")
	}
}

func TestInMemorySessionTokenStore_RejectsInvalidInput(t *testing.T) {
	store := NewInMemorySessionTokenStore()

	if _, err := store.CreateSession("", time.Minute); err == nil {
		t.Fatal("CreateSession() must reject empty token")
	}
	if _, err := store.CreateSession("token-123", 0); err == nil {
		t.Fatal("CreateSession() must reject non-positive ttl")
	}
}
