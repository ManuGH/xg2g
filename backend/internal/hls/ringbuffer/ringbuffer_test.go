// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ringbuffer

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuffer_PutAndGet(t *testing.T) {
	buf := NewBuffer("sess1", 3, nil)
	defer buf.Close()

	buf.Put("index.m3u8", []byte("#EXTM3U"))
	buf.Put("init.mp4", []byte("ftyp"))
	buf.Put("seg_000001.ts", []byte("tsdata1"))

	art, ok := buf.Get("index.m3u8")
	if !ok || string(art.Data) != "#EXTM3U" {
		t.Fatalf("expected playlist data, got %v", art)
	}

	art, ok = buf.Get("seg_000001.ts")
	if !ok || string(art.Data) != "tsdata1" {
		t.Fatalf("expected segment data, got %v", art)
	}
}

func TestBuffer_Eviction(t *testing.T) {
	buf := NewBuffer("sess2", 2, nil)
	defer buf.Close()

	buf.Put("index.m3u8", []byte("playlist"))
	buf.Put("init.mp4", []byte("init"))
	buf.Put("seg_000001.ts", []byte("seg1"))
	buf.Put("seg_000002.ts", []byte("seg2"))
	buf.Put("seg_000003.ts", []byte("seg3"))

	// seg_000001.ts should be evicted since maxSegments = 2
	if _, ok := buf.Get("seg_000001.ts"); ok {
		t.Fatalf("expected seg_000001.ts to be evicted")
	}
	if _, ok := buf.Get("seg_000002.ts"); !ok {
		t.Fatalf("expected seg_000002.ts to remain in buffer")
	}
	if _, ok := buf.Get("seg_000003.ts"); !ok {
		t.Fatalf("expected seg_000003.ts to remain in buffer")
	}
	// playlist and init should never be evicted
	if _, ok := buf.Get("index.m3u8"); !ok {
		t.Fatalf("expected playlist to never be evicted")
	}
	if _, ok := buf.Get("init.mp4"); !ok {
		t.Fatalf("expected init segment to never be evicted")
	}
}

func TestBuffer_DVRCallback(t *testing.T) {
	var count int32
	var wg sync.WaitGroup
	wg.Add(3)

	cb := func(sid, fn string, data []byte) {
		atomic.AddInt32(&count, 1)
		wg.Done()
	}

	buf := NewBuffer("sess3", 10, cb)
	defer buf.Close()

	buf.Put("seg_000001.ts", []byte("1"))
	buf.Put("seg_000002.ts", []byte("2"))
	buf.Put("seg_000003.ts", []byte("3"))

	wg.Wait()
	if got := atomic.LoadInt32(&count); got != 3 {
		t.Fatalf("expected 3 DVR callbacks, got %d", got)
	}
}

func TestBuffer_Concurrency(t *testing.T) {
	buf := NewBuffer("sess4", 5, nil)
	defer buf.Close()

	var wg sync.WaitGroup
	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				fn := fmt.Sprintf("seg_%06d.ts", id*100+j)
				buf.Put(fn, []byte(fmt.Sprintf("data-%d", j)))
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}
	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = buf.Get("index.m3u8")
				time.Sleep(500 * time.Microsecond)
			}
		}()
	}
	wg.Wait()
}
