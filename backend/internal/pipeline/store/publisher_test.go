package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestShadowPublisher_PublishAndClose(t *testing.T) {
	ram, _ := NewRAMShadowStore(100, 1000)
	logger := zerolog.Nop()

	pub, _ := NewShadowPublisher(ram, 10, 100, logger)
	pub.Start()

	pub.Publish(context.Background(), "s1", Object{Name: "1", Data: []byte("1234")})
	pub.Publish(context.Background(), "s1", Object{Name: "2", Data: []byte("5678")})

	err := pub.Close(context.Background())
	if err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	if ram.currentBytes != 8 {
		t.Errorf("expected 8 bytes in store, got %d", ram.currentBytes)
	}

	// Post-close publish should fail
	err = pub.Publish(context.Background(), "s1", Object{Name: "3", Data: []byte("xxx")})
	if err != ErrPublisherClosed {
		t.Errorf("expected ErrPublisherClosed, got %v", err)
	}
}

func TestShadowPublisher_CloseBeforeStart(t *testing.T) {
	ram, _ := NewRAMShadowStore(100, 1000)
	pub, _ := NewShadowPublisher(ram, 10, 100, zerolog.Nop())

	err := pub.Close(context.Background())
	if err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}
}

func TestShadowPublisher_BufferOwnership(t *testing.T) {
	ram, _ := NewRAMShadowStore(100, 1000)
	logger := zerolog.Nop()
	pub, _ := NewShadowPublisher(ram, 10, 100, logger)
	pub.Start()

	buf := []byte("original")
	pub.Publish(context.Background(), "s1", Object{Name: "1", Data: buf})

	// Mutate buffer immediately
	buf[0] = 'X'

	pub.Close(context.Background())

	obj, err := ram.Get(context.Background(), "s1", "1")
	if err != nil {
		t.Fatal(err)
	}
	if string(obj.Data) != "original" {
		t.Errorf("buffer ownership failure, expected 'original', got %s", string(obj.Data))
	}
}

func TestShadowPublisher_QueueSaturation(t *testing.T) {
	ram, _ := NewRAMShadowStore(100, 1000)
	logger := zerolog.Nop()

	// maxQueueBytes is 10, channel capacity 10
	pub, _ := NewShadowPublisher(ram, 10, 10, logger)
	// We DO NOT Start() the publisher to guarantee the queue backs up!

	err1 := pub.Publish(context.Background(), "s1", Object{Name: "1", Data: []byte("1234")})
	err2 := pub.Publish(context.Background(), "s1", Object{Name: "2", Data: []byte("5678")})

	if err1 != nil || err2 != nil {
		t.Fatalf("expected nil errors, got %v, %v", err1, err2)
	}

	// 8 bytes in queue. This one should be dropped because 8+4 > 10.
	err3 := pub.Publish(context.Background(), "s1", Object{Name: "3", Data: []byte("9012")})
	if err3 != ErrQueueFull {
		t.Errorf("expected ErrQueueFull, got %v", err3)
	}

	// Fill remaining channel capacity (capacity=10, used=2, remaining=8)
	// We want to delete "1" and "2".
	for i := 0; i < 7; i++ {
		if err := pub.Delete(context.Background(), "s1", "1"); err != nil {
			t.Fatalf("unexpected delete error: %v", err)
		}
	}

	if err := pub.Delete(context.Background(), "s1", "2"); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}

	// Queue is now completely full (10 elements)
	errDelete := pub.Delete(context.Background(), "s1", "2")
	if errDelete != ErrQueueFull {
		t.Errorf("expected ErrQueueFull on delete, got %v", errDelete)
	}

	// Start it now to process the queue
	pub.Start()
	pub.Close(context.Background())

	if ram.currentBytes != 0 {
		t.Errorf("expected 0 bytes because deletes should have been processed, got %d", ram.currentBytes)
	}
	if _, err := ram.Get(context.Background(), "s1", "3"); err != ErrNotFound {
		t.Errorf("expected object 3 to be dropped")
	}
}

func TestShadowPublisher_ParallelPublish(t *testing.T) {
	ram, _ := NewRAMShadowStore(1000, 1000)
	pub, _ := NewShadowPublisher(ram, 100, 1000, zerolog.Nop())
	pub.Start()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pub.Publish(context.Background(), "s1", Object{Name: "1", Data: []byte("a")})
		}(i)
	}
	wg.Wait()
	pub.Close(context.Background())
}

func TestShadowPublisher_DeleteOrdering(t *testing.T) {
	ram, _ := NewRAMShadowStore(100, 1000)
	pub, _ := NewShadowPublisher(ram, 10, 100, zerolog.Nop())
	// DO NOT Start() yet to ensure strict ordering in queue

	pub.Publish(context.Background(), "s1", Object{Name: "seg", Data: []byte("abc")})
	pub.Delete(context.Background(), "s1", "seg")

	pub.Start()
	pub.Close(context.Background())

	if ram.currentBytes != 0 {
		t.Errorf("expected 0 bytes, got %d", ram.currentBytes)
	}
	if _, err := ram.Get(context.Background(), "s1", "seg"); err != ErrNotFound {
		t.Errorf("expected segment to be deleted")
	}
}

func TestShadowPublisher_CloseRace(t *testing.T) {
	ram, _ := NewRAMShadowStore(100, 1000)
	pub, _ := NewShadowPublisher(ram, 100, 1000, zerolog.Nop())
	pub.Start()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(1 * time.Millisecond)
			pub.Publish(context.Background(), "s1", Object{Name: "1", Data: []byte("a")})
		}()
	}
	// Call close concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		pub.Close(context.Background())
	}()

	wg.Wait()
	// Queued bytes should end up at 0
	if pub.queuedBytes != 0 {
		t.Errorf("expected 0 queued bytes, got %d", pub.queuedBytes)
	}
}

func TestShadowPublisherRejectsOversizedObject(t *testing.T) {
	ram, _ := NewRAMShadowStore(1024, 1000)

	publisher, _ := NewShadowPublisher(
		ram,
		10,
		8,
		zerolog.Nop(),
	)

	err := publisher.Publish(
		context.Background(),
		"stream",
		Object{
			Name: "large.m4s",
			Data: make([]byte, 9),
		},
	)

	if err != ErrQueueFull {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
}

func TestShadowPublisherConcurrentClose(t *testing.T) {
	ram, _ := NewRAMShadowStore(1024, 1000)
	publisher, _ := NewShadowPublisher(ram, 10, 1024, zerolog.Nop())
	publisher.Start()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(
				context.Background(),
				time.Second,
			)
			defer cancel()

			if err := publisher.Close(ctx); err != nil {
				t.Errorf("close failed: %v", err)
			}
		}()
	}

	wg.Wait()
}
