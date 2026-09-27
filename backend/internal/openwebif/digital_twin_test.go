// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package openwebif

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDigitalTwin_VuUno4KProfile(t *testing.T) {
	twin := NewVuUno4KTwin(t)

	// Verify /api/about contract
	resp, err := http.Get(twin.URL() + "/api/about")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, "Vu+ Uno 4K")
	assert.Contains(t, bodyStr, "OpenATV 7.6.0")
	assert.Contains(t, bodyStr, `"tuners_count":2`)

	// Verify /api/statusinfo idle state
	respStatus, err := http.Get(twin.URL() + "/api/statusinfo")
	require.NoError(t, err)
	defer respStatus.Body.Close()
	assert.Equal(t, http.StatusOK, respStatus.StatusCode)

	statusBody, err := io.ReadAll(respStatus.Body)
	require.NoError(t, err)
	assert.Contains(t, string(statusBody), `"isStreaming":"false"`)
	assert.Contains(t, string(statusBody), `"tuners_in_use":0`)

	twin.AssertNoAssertionFailure(t)
}

func TestDigitalTwin_TransponderSharing(t *testing.T) {
	twin := NewVuUno4KTwin(t, WithPhysicalTuners(2))

	// Two services on the SAME transponder (3FB:1:C00000)
	sRef1 := "1:0:19:283D:3FB:1:C00000:0:0:0:" // Das Erste HD
	sRef2 := "1:0:19:283E:3FB:1:C00000:0:0:0:" // ZDF HD

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()

	req1, err := http.NewRequestWithContext(ctx1, http.MethodGet, twin.StreamURL(sRef1), nil)
	require.NoError(t, err)
	resp1, err := http.DefaultClient.Do(req1)
	require.NoError(t, err)
	defer resp1.Body.Close()
	assert.Equal(t, http.StatusOK, resp1.StatusCode)

	// Tuner 1 in use
	assert.Equal(t, 1, twin.ActiveTunersCount())

	// Start second stream on SAME transponder
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	req2, err := http.NewRequestWithContext(ctx2, http.MethodGet, twin.StreamURL(sRef2), nil)
	require.NoError(t, err)
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	// Invariant: Both streams must share 1 physical tuner
	assert.Equal(t, 1, twin.ActiveTunersCount(), "Services on same transponder must share 1 physical tuner")

	// Disconnect stream 1
	cancel1()
	resp1.Body.Close()
	time.Sleep(10 * time.Millisecond)

	// Transponder still held by stream 2
	assert.Equal(t, 1, twin.ActiveTunersCount())

	// Disconnect stream 2
	cancel2()
	resp2.Body.Close()
	time.Sleep(10 * time.Millisecond)

	// All tuners freed
	assert.Equal(t, 0, twin.ActiveTunersCount())
	twin.AssertNoAssertionFailure(t)
}

func TestDigitalTwin_TunerExhaustionAndAssertionCrash(t *testing.T) {
	twin := NewVuUno4KTwin(t,
		WithPhysicalTuners(2),
		WithCrashOnTunerExhaustion(true),
	)

	// 3 services on 3 DIFFERENT transponders
	sRef1 := "1:0:19:283D:3FB:1:C00000:0:0:0:" // TP 1: 3FB:1:C00000
	sRef2 := "1:0:1:6DCA:44D:1:C00000:0:0:0:"  // TP 2: 44D:1:C00000
	sRef3 := "1:0:1:6DCB:453:1:C00000:0:0:0:"  // TP 3: 453:1:C00000

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	req1, _ := http.NewRequestWithContext(ctx1, http.MethodGet, twin.StreamURL(sRef1), nil)
	resp1, err := http.DefaultClient.Do(req1)
	require.NoError(t, err)
	defer resp1.Body.Close()
	assert.Equal(t, http.StatusOK, resp1.StatusCode)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	req2, _ := http.NewRequestWithContext(ctx2, http.MethodGet, twin.StreamURL(sRef2), nil)
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	assert.Equal(t, 2, twin.ActiveTunersCount(), "Both physical tuners should now be occupied")

	// 3rd stream requests a 3rd distinct transponder -> MUST trigger assertion crash
	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	req3, _ := http.NewRequestWithContext(ctx3, http.MethodGet, twin.StreamURL(sRef3), nil)
	resp3, err := http.DefaultClient.Do(req3)
	require.NoError(t, err)
	defer resp3.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp3.StatusCode)
	body3, _ := io.ReadAll(resp3.Body)
	assert.Contains(t, string(body3), OpenATVAssertionMessage)

	// Verify twin forensic state
	twin.AssertTunerCrashOccurred(t)
	assert.Equal(t, 2, twin.PeakTunersUsed())
}

func TestDigitalTwin_MPEGTS_StreamContent(t *testing.T) {
	twin := NewVuUno4KTwin(t, WithMaxStreamBatches(3))

	sRef := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	resp, err := http.Get(twin.StreamURL(sRef))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "video/mp2t", resp.Header.Get("Content-Type"))

	streamBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// 3 batches * 4 packets/batch * 188 bytes = 2256 bytes
	assert.Equal(t, 2256, len(streamBytes))
	assert.Equal(t, 0, len(streamBytes)%188, "Stream must consist of exact 188-byte TS packets")

	// Verify all TS packets start with sync byte 0x47
	for i := 0; i < len(streamBytes); i += 188 {
		assert.Equal(t, uint8(0x47), streamBytes[i], "Packet at %d must start with 0x47 sync byte", i)
	}

	// Verify first packet is PAT (PID 0)
	patPkt := streamBytes[0:188]
	pidPAT := (uint16(patPkt[1]&0x1F) << 8) | uint16(patPkt[2])
	assert.Equal(t, uint16(0x0000), pidPAT)

	// Verify second packet is PMT (PID 0x1000)
	pmtPkt := streamBytes[188:376]
	pidPMT := (uint16(pmtPkt[1]&0x1F) << 8) | uint16(pmtPkt[2])
	assert.Equal(t, uint16(0x1000), pidPMT)

	twin.AssertNoAssertionFailure(t)
}

func TestDigitalTwin_CAMGraceWindow(t *testing.T) {
	// Set 50ms CAM grace window where stream is scrambled
	twin := NewVuUno4KTwin(t,
		WithCAMGraceWindow(50*time.Millisecond),
		WithMaxStreamBatches(100),
	)

	sRef := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	resp, err := http.Get(twin.StreamURL(sRef))
	require.NoError(t, err)
	defer resp.Body.Close()

	buf := make([]byte, 188)

	// Read initial video packet (packet 3 in first batch)
	// Skip PAT and PMT
	_, err = io.ReadFull(resp.Body, buf) // PAT
	require.NoError(t, err)
	_, err = io.ReadFull(resp.Body, buf) // PMT
	require.NoError(t, err)
	_, err = io.ReadFull(resp.Body, buf) // Video
	require.NoError(t, err)

	// Video packet byte 3 carries scrambling control: bits 6-7
	scrambleBits := (buf[3] >> 6) & 0x03
	assert.Equal(t, uint8(0x02), scrambleBits, "Initial video packet must be scrambled (transport_scrambling_control = 2)")

	// Sleep past the grace window and drain socket buffer
	time.Sleep(70 * time.Millisecond)

	// Read until we get a packet generated after the grace window
	var laterScrambleBits uint8
	foundClear := false
	for i := 0; i < 200; i++ {
		_, err = io.ReadFull(resp.Body, buf)
		if err != nil {
			break
		}
		pid := (uint16(buf[1]&0x1F) << 8) | uint16(buf[2])
		if pid == 0x0100 { // Video
			bits := (buf[3] >> 6) & 0x03
			if bits == 0x00 {
				laterScrambleBits = bits
				foundClear = true
				break
			}
		}
	}

	assert.True(t, foundClear, "Must observe unscrambled video packet once grace window has elapsed")
	assert.Equal(t, uint8(0x00), laterScrambleBits, "Video packet after grace window must be clear (unscrambled)")
	twin.AssertNoAssertionFailure(t)
}

func TestDigitalTwin_WebIFStreamM3U(t *testing.T) {
	twin := NewVuUno4KTwin(t)

	sRef := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	reqURL := twin.URL() + "/web/stream.m3u?ref=" + sRef + "&name=DasErsteHD"
	resp, err := http.Get(reqURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/vnd.apple.mpegurl", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	bodyStr := string(body)

	assert.True(t, strings.HasPrefix(bodyStr, "#EXTM3U"))
	assert.Contains(t, bodyStr, "#EXTINF:-1,DasErsteHD")
	assert.Contains(t, bodyStr, twin.StreamURL(sRef))

	twin.AssertNoAssertionFailure(t)
}

func TestDigitalTwin_OpenWebIFClientCompatibility(t *testing.T) {
	twin := NewVuUno4KTwin(t)

	client := New(twin.URL())

	ctx := context.Background()

	// 1. About
	about, err := client.About(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Vu+ Uno 4K", about.Info.Model)
	assert.Equal(t, "OpenATV 7.6.0", about.Info.ImageVer)
	assert.Equal(t, 2, about.Info.TunersCount)

	// 2. StreamURL
	streamURL, err := client.StreamURL(ctx, "1:0:19:283D:3FB:1:C00000:0:0:0:", "Das Erste HD")
	require.NoError(t, err)
	assert.Contains(t, streamURL, "1:0:19:283D:3FB:1:C00000:0:0:0:")

	// 3. Status
	status, err := client.GetStatusInfo(ctx)
	require.NoError(t, err)
	assert.Equal(t, "false", status.InStandby)
	assert.Equal(t, "false", status.IsStreaming)

	// 4. Signal
	signal, err := client.GetSignal(ctx)
	require.NoError(t, err)
	assert.Equal(t, 88, signal.SNR)

	// 5. Bouquets
	bouquets, err := client.Bouquets(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, bouquets)

	twin.AssertNoAssertionFailure(t)
}
