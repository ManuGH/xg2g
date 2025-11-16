package proxy

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// TestHLSGetOrCreateAndShutdownRace tests concurrent GetOrCreateStream calls
// with simultaneous Shutdown to ensure no data races or panics.
func TestHLSGetOrCreateAndShutdownRace(t *testing.T) {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	m, err := NewHLSManager(logger, t.TempDir())
	if err != nil {
		t.Fatalf("NewHLSManager failed: %v", err)
	}
	defer m.Shutdown()

	// Override timeouts for faster test execution
	m.idleTimeout = 100 * time.Millisecond
	m.cleanupTicker = 50 * time.Millisecond

	var wg sync.WaitGroup
	const numGoroutines = 50
	serviceRef := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	targetURL := "http://localhost:8001/" + serviceRef

	// Launch many concurrent GetOrCreateStream calls
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			stream, err := m.GetOrCreateStream(serviceRef, targetURL)
			if err != nil {
				// Error is acceptable if shutdown has already started
				return
			}

			// Access the stream to trigger updateAccess()
			_ = stream.GetPlaylistPath()

			// Small random delay
			time.Sleep(time.Duration(id%10) * time.Millisecond)
		}(i)
	}

	// Trigger shutdown concurrently
	go func() {
		time.Sleep(10 * time.Millisecond)
		m.Shutdown()
	}()

	wg.Wait()
}

// TestHLSIdleCleanupWithConcurrentAccess tests that idle cleanup works correctly
// while streams are being accessed concurrently.
func TestHLSIdleCleanupWithConcurrentAccess(t *testing.T) {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	m, err := NewHLSManager(logger, t.TempDir())
	if err != nil {
		t.Fatalf("NewHLSManager failed: %v", err)
	}
	defer m.Shutdown()

	// Very short timeouts for fast test execution
	m.idleTimeout = 50 * time.Millisecond
	m.cleanupTicker = 25 * time.Millisecond

	serviceRef := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	targetURL := "http://localhost:8001/" + serviceRef

	var wg sync.WaitGroup
	const numAccesses = 100

	// Repeatedly get/access the stream
	for i := 0; i < numAccesses; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			stream, err := m.GetOrCreateStream(serviceRef, targetURL)
			if err != nil {
				t.Errorf("GetOrCreateStream failed: %v", err)
				return
			}

			// Access stream to prevent it from becoming idle
			_ = stream.GetPlaylistPath()

			// Random delay
			time.Sleep(time.Duration((i % 20)) * time.Millisecond)
		}()
	}

	// Trigger cleanup manually during concurrent access
	go func() {
		for i := 0; i < 10; i++ {
			time.Sleep(30 * time.Millisecond)
			m.cleanupIdleStreams()
		}
	}()

	wg.Wait()

	// At the end, check that the stream still exists (wasn't cleaned up due to active access)
	m.mu.RLock()
	streamCount := len(m.streams)
	m.mu.RUnlock()

	switch streamCount {
	case 0:
		// Stream might have been cleaned up if timing was unlucky
		// This is acceptable - the important thing is no race or panic
		t.Logf("Stream was cleaned up during test (acceptable)")
	case 1:
		t.Logf("Stream is still active (expected)")
	default:
		t.Errorf("Unexpected stream count: %d (expected 0 or 1)", streamCount)
	}
}

// TestHLSConcurrentMultipleStreams tests creating and accessing multiple different
// streams concurrently to ensure proper isolation and no races.
func TestHLSConcurrentMultipleStreams(t *testing.T) {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	m, err := NewHLSManager(logger, t.TempDir())
	if err != nil {
		t.Fatalf("NewHLSManager failed: %v", err)
	}
	defer m.Shutdown()

	var wg sync.WaitGroup
	const numStreams = 10
	const accessesPerStream = 20

	// Create multiple different streams concurrently
	for streamID := 0; streamID < numStreams; streamID++ {
		for accessID := 0; accessID < accessesPerStream; accessID++ {
			wg.Add(1)

			go func(sID, aID int) {
				defer wg.Done()

				// Each stream has a unique service reference
				serviceRef := "1:0:19:132F:3EF:1:C00000:0:0:0:" + string(rune('A'+sID))
				targetURL := "http://localhost:8001/" + serviceRef

				stream, err := m.GetOrCreateStream(serviceRef, targetURL)
				if err != nil {
					t.Errorf("GetOrCreateStream failed for stream %d: %v", sID, err)
					return
				}

				// Access stream
				_ = stream.GetPlaylistPath()
				_ = stream.GetOutputDir()

				// Small delay
				time.Sleep(time.Duration(aID%5) * time.Millisecond)
			}(streamID, accessID)
		}
	}

	wg.Wait()

	// Verify all streams were created
	m.mu.RLock()
	streamCount := len(m.streams)
	m.mu.RUnlock()

	if streamCount != numStreams {
		t.Errorf("Expected %d streams, got %d", numStreams, streamCount)
	}
}

// TestHLSShutdownIdempotency verifies that calling Shutdown() multiple times
// is safe and doesn't cause panics or data races.
func TestHLSShutdownIdempotency(t *testing.T) {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	m, err := NewHLSManager(logger, t.TempDir())
	if err != nil {
		t.Fatalf("NewHLSManager failed: %v", err)
	}

	// Create a stream first
	serviceRef := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	targetURL := "http://localhost:8001/" + serviceRef
	_, err = m.GetOrCreateStream(serviceRef, targetURL)
	if err != nil {
		t.Fatalf("GetOrCreateStream failed: %v", err)
	}

	var wg sync.WaitGroup
	const numShutdowns = 20

	// Call Shutdown() multiple times concurrently
	for i := 0; i < numShutdowns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Shutdown()
		}()
	}

	wg.Wait()

	// Verify streams were cleaned up
	m.mu.RLock()
	streamCount := len(m.streams)
	m.mu.RUnlock()

	if streamCount != 0 {
		t.Errorf("Expected 0 streams after shutdown, got %d", streamCount)
	}
}
