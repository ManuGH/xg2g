package store

import (
	"context"
	"testing"
	"time"
)

func TestRAMShadowStore_Publish_Eviction(t *testing.T) {
	s, err := NewRAMShadowStore(10, 1000)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sid := StreamID("test-stream")

	s.Publish(ctx, sid, Object{Name: "1.m4s", Data: []byte("1234"), PublishedAt: time.Now()})
	s.Publish(ctx, sid, Object{Name: "2.m4s", Data: []byte("5678"), PublishedAt: time.Now()})

	if s.currentBytes != 8 {
		t.Errorf("expected 8 bytes, got %d", s.currentBytes)
	}

	// This triggers eviction of 1.m4s
	s.Publish(ctx, sid, Object{Name: "3.m4s", Data: []byte("9012"), PublishedAt: time.Now()})

	if s.currentBytes != 8 {
		t.Errorf("expected 8 bytes after eviction, got %d", s.currentBytes)
	}

	if _, err := s.Get(ctx, sid, "1.m4s"); err != ErrNotFound {
		t.Errorf("expected 1.m4s to be evicted, got %v", err)
	}
	if _, err := s.Get(ctx, sid, "2.m4s"); err != nil {
		t.Errorf("expected 2.m4s to remain, got %v", err)
	}
}

func TestRAMShadowStore_Publish_TooLarge(t *testing.T) {
	s, _ := NewRAMShadowStore(10, 1000)
	err := s.Publish(context.Background(), "test", Object{
		Name: "large.ts",
		Data: make([]byte, 20),
	})
	if err != ErrObjectTooLarge {
		t.Errorf("expected ErrObjectTooLarge, got %v", err)
	}
}

func TestRAMShadowStore_Delete(t *testing.T) {
	s, _ := NewRAMShadowStore(100, 1000)
	ctx := context.Background()
	sid := StreamID("s1")

	s.Publish(ctx, sid, Object{Name: "seg", Data: []byte("abc")})
	s.Delete(ctx, sid, "seg")

	if s.currentBytes != 0 {
		t.Errorf("expected 0 bytes, got %d", s.currentBytes)
	}
	if _, err := s.Get(ctx, sid, "seg"); err != ErrNotFound {
		t.Errorf("expected not found")
	}
}

func TestRAMShadowStore_DeleteStream(t *testing.T) {
	s, _ := NewRAMShadowStore(100, 1000)
	ctx := context.Background()
	sid := StreamID("s1")

	s.Publish(ctx, sid, Object{Name: "1", Data: []byte("123")})
	s.Publish(ctx, sid, Object{Name: "2", Data: []byte("456")})

	s.DeleteStream(ctx, sid)

	if s.currentBytes != 0 {
		t.Errorf("expected 0 bytes, got %d", s.currentBytes)
	}
	if _, err := s.Get(ctx, sid, "1"); err != ErrNotFound {
		t.Errorf("expected not found")
	}
	if s.eviction.Len() != 0 {
		t.Errorf("expected empty eviction list")
	}
}

func TestRAMShadowStore_DeterministicEviction(t *testing.T) {
	s, _ := NewRAMShadowStore(4, 1000)
	ctx := context.Background()
	sid1 := StreamID("s1")
	sid2 := StreamID("s2")

	s.Publish(ctx, sid1, Object{Name: "1", Data: []byte("a")})
	s.Publish(ctx, sid2, Object{Name: "2", Data: []byte("b")})
	s.Publish(ctx, sid1, Object{Name: "3", Data: []byte("c")})
	s.Publish(ctx, sid2, Object{Name: "4", Data: []byte("d")})

	// Eviction order should be exactly: 1(s1), 2(s2), 3(s1), 4(s2) based on sequence.
	s.Publish(ctx, sid1, Object{Name: "5", Data: []byte("e")})
	if _, err := s.Get(ctx, sid1, "1"); err != ErrNotFound {
		t.Errorf("expected 1 to be evicted")
	}

	s.Publish(ctx, sid1, Object{Name: "6", Data: []byte("f")})
	if _, err := s.Get(ctx, sid2, "2"); err != ErrNotFound {
		t.Errorf("expected 2 to be evicted")
	}
}

func TestRAMShadowStore_RepeatedPlaylistReplacementIsBounded(t *testing.T) {
	store, _ := NewRAMShadowStore(1024*1024, 1000)

	for i := 0; i < 100_000; i++ {
		err := store.Publish(
			context.Background(),
			"stream",
			Object{
				Name: "index.m3u8",
				Kind: ObjectPlaylist,
				Data: []byte("#EXTM3U"),
			},
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Exactly one object and one eviction element should exist.
	if store.eviction.Len() != 1 {
		t.Errorf("expected 1 eviction element, got %d", store.eviction.Len())
	}
	if store.currentBytes != 7 {
		t.Errorf("expected 7 bytes, got %d", store.currentBytes)
	}
}

func TestRAMShadowStore_DataOwnership(t *testing.T) {
	s, _ := NewRAMShadowStore(100, 1000)
	buf := []byte("original")

	s.Publish(context.Background(), "s1", Object{Name: "x", Data: buf})
	buf[0] = 'X'

	obj, _ := s.Get(context.Background(), "s1", "x")
	if string(obj.Data) != "original" {
		t.Errorf("expected original, got %s", string(obj.Data))
	}
}
