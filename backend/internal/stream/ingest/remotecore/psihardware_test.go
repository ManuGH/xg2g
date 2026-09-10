// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// Step 5d: the same differential as Step 5c, with the bytes coming from a real
// receiver instead of an authored corpus.
//
// Nothing about the comparison changes. The same psiScope is rendered from both
// implementations and every call is checked before the next is made. What
// changes is where the transport came from, and that is the whole point: a
// parser that agrees on transport somebody wrote is not yet known to agree on
// transport a broadcaster produced.
//
// The captures are not in the repository - they are tens of megabytes of real
// broadcast. The manifest carries their sizes and hashes, and a run refuses to
// use bytes that do not match, so a result always names the capture it came
// from.

const hardwareManifestPath = "../../../../../testdata/psi-hardware/manifest.txt"

// hardwareEnv points at the directory holding the captures the manifest names.
const hardwareEnv = "XG2G_PSI_HARDWARE_DIR"

type hardwareCapture struct {
	name   string
	size   int64
	sha256 string
}

type hardwareStep struct {
	// feed names a capture whose bytes are ingested; setTarget is a target
	// change. Exactly one of them is set.
	feed      string
	setTarget uint16
	isFeed    bool
}

type hardwareCase struct {
	name      string
	initial   uint16
	chunkSize int
	steps     []hardwareStep
}

func loadHardwareManifest(t *testing.T) (map[string]hardwareCapture, []hardwareCase) {
	t.Helper()

	f, err := os.Open(hardwareManifestPath)
	if err != nil {
		t.Fatalf("open the hardware manifest: %v", err)
	}
	defer func() { _ = f.Close() }()

	captures := map[string]hardwareCapture{}
	var (
		cases   []hardwareCase
		current *hardwareCase
		version string
	)

	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		switch fields[0] {
		case "version":
			if len(fields) != 2 || fields[1] != "1" {
				t.Fatalf("line %d: manifest version %q, this reader understands \"1\"", line, text)
			}
			version = fields[1]

		case "capture":
			if len(fields) != 4 {
				t.Fatalf("line %d: capture wants name, size and sha256", line)
			}
			size, err := strconv.ParseInt(fields[2], 10, 64)
			if err != nil {
				t.Fatalf("line %d: capture size: %v", line, err)
			}
			captures[fields[1]] = hardwareCapture{name: fields[1], size: size, sha256: fields[3]}

		case "case":
			if version == "" {
				t.Fatalf("line %d: a case before the manifest version", line)
			}
			if current != nil {
				t.Fatalf("line %d: a case inside a case", line)
			}
			current = &hardwareCase{name: fields[1], chunkSize: 65536}

		case "target":
			if current == nil {
				t.Fatalf("line %d: target outside a case", line)
			}
			n, err := strconv.ParseUint(fields[1], 10, 16)
			if err != nil {
				t.Fatalf("line %d: target: %v", line, err)
			}
			current.initial = uint16(n)

		case "chunk":
			if current == nil {
				t.Fatalf("line %d: chunk outside a case", line)
			}
			n, err := strconv.Atoi(fields[1])
			if err != nil || n <= 0 {
				t.Fatalf("line %d: chunk size: %v", line, err)
			}
			current.chunkSize = n

		case "feed":
			if current == nil {
				t.Fatalf("line %d: feed outside a case", line)
			}
			if _, ok := captures[fields[1]]; !ok {
				t.Fatalf("line %d: feed names %q, which the manifest never declared", line, fields[1])
			}
			current.steps = append(current.steps, hardwareStep{feed: fields[1], isFeed: true})

		case "settarget":
			if current == nil {
				t.Fatalf("line %d: settarget outside a case", line)
			}
			n, err := strconv.ParseUint(fields[1], 10, 16)
			if err != nil {
				t.Fatalf("line %d: settarget: %v", line, err)
			}
			current.steps = append(current.steps, hardwareStep{setTarget: uint16(n)})

		case "end":
			if current == nil {
				t.Fatalf("line %d: end outside a case", line)
			}
			if len(current.steps) == 0 {
				t.Fatalf("line %d: case %q has no steps", line, current.name)
			}
			cases = append(cases, *current)
			current = nil

		default:
			t.Fatalf("line %d: %q is not a keyword this reader knows", line, fields[0])
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read the hardware manifest: %v", err)
	}
	if current != nil {
		t.Fatal("the manifest ends inside a case")
	}
	if len(captures) == 0 || len(cases) == 0 {
		t.Fatal("the manifest declared no captures or no cases")
	}
	return captures, cases
}

// readCapture returns the bytes of a named capture, having proved they are the
// bytes the manifest names. A capture that hashes to something else is refused
// rather than read: the identity of the transport is the evidence.
func readCapture(t *testing.T, dir string, c hardwareCapture) []byte {
	t.Helper()
	path := filepath.Join(dir, c.name+".ts")
	data, err := os.ReadFile(path) //nolint:gosec // G304: test fixture path from the manifest
	if err != nil {
		t.Fatalf("read capture %s: %v", c.name, err)
	}
	if int64(len(data)) != c.size {
		t.Fatalf("capture %s is %d bytes, the manifest says %d", c.name, len(data), c.size)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != c.sha256 {
		t.Fatalf("capture %s hashes to %s, the manifest says %s", c.name, got, c.sha256)
	}
	return data
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("... (%d bytes total)", len(s))
}

func requireHardwareDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv(hardwareEnv)
	if dir == "" {
		t.Skipf("%s not set; point it at the directory holding the Step 5d captures", hardwareEnv)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("hardware capture directory not usable: %v", err)
	}
	return dir
}

// psiEvents renders only the events that are inside PSI coverage.
//
// The two cores answer with different coverage on purpose: the reference is
// ParseCoverageComplete, the Rust core is ParseCoveragePSIOnly, and that
// contract says in so many words which fields are authoritative for the
// latter - "ProcessedThroughOffset, the PSI events, the PAT and PMT facts, the
// audio declarations and ActivePSI".
//
// EventRandomAccessPoint is not one of them. It reports an access unit a
// decoder can start on and is produced from the PES offset, so a core covering
// PSI only is right to emit none. The authored 5c corpus never showed this
// because authored PSI fixtures contain no video access units; ten seconds of
// real broadcast contain many, and comparing the raw event lists compared the
// reference's complete coverage against the Rust core's PSI coverage.
//
// So the PSI events are compared, and the non-PSI ones are checked from the
// other side instead: a psi-only core that started emitting them would be
// contradicting its own declared coverage, and rustNonPSIEvents catches that.
func psiEvents(res mediafacts.ParseResult) string {
	names := make([]string, 0, len(res.Events))
	for _, ev := range res.Events {
		if ev.Kind == mediafacts.EventProgramIdentityChanged {
			names = append(names, "identity")
		}
	}
	return strings.Join(names, " ")
}

// rustNonPSIEvents counts events a PSI-only core has no business reporting.
func rustNonPSIEvents(res mediafacts.ParseResult) int {
	n := 0
	for _, ev := range res.Events {
		if ev.Kind != mediafacts.EventProgramIdentityChanged {
			n++
		}
	}
	return n
}

// goRandomAccessPoints counts what the reference saw outside PSI, so the
// evidence can say the real transport actually exercised that path rather than
// that the difference never arose.
func goRandomAccessPoints(res mediafacts.ParseResult) int {
	n := 0
	for _, ev := range res.Events {
		if ev.Kind == mediafacts.EventRandomAccessPoint {
			n++
		}
	}
	return n
}

// TestPSIHardware_TheRealRustCoreAgreesOnRealTransport is Step 5d.
//
// One GoCore and one real Rust process per case, for the case's whole life. A
// zap is a SetTargetProgram on the core that is already running, because that is
// what it is in production: MasterRing builds one core when the ring is created
// and never replaces it. Building a fresh core per service would test something
// the product does not do, and would hide exactly the bug this case exists to
// find - state from the previous service surviving into the next.
func TestPSIHardware_TheRealRustCoreAgreesOnRealTransport(t *testing.T) {
	bin := requireRealCore(t)
	dir := requireHardwareDir(t)
	captures, cases := loadHardwareManifest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Second)
	defer cancel()

	// Read every capture once, verified, before any case runs: a case that
	// fails should fail about PSI, not about a file that was not there.
	bytesFor := map[string][]byte{}
	for name, c := range captures {
		bytesFor[name] = readCapture(t, dir, c)
	}

	compared, raps := 0, 0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			remote, err := Start(ctx, bin, c.initial)
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			defer func() {
				if err := remote.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			}()

			local := mediafacts.NewGoCore(c.initial)
			offset := int64(0)
			call := 0
			identities := 0
			var last mediafacts.ParseResult

			// A case that passes because neither side ever saw PSI would be
			// worthless, so the evidence records what was actually parsed and
			// where each state change came from.
			t.Logf("case %s: initial target %d, chunk %d bytes (%d packets)",
				c.name, c.initial, c.chunkSize, c.chunkSize/188)

			compare := func(what string, goRes, rustRes mediafacts.ParseResult) {
				t.Helper()
				call++
				if !goRes.Covers(mediafacts.ParseCoverageComplete) {
					t.Fatalf("call %d (%s): the reference reported coverage %s", call, what, goRes.Coverage)
				}
				if !rustRes.Covers(mediafacts.ParseCoveragePSIOnly) {
					t.Fatalf("call %d (%s): the real core reported coverage %s", call, what, rustRes.Coverage)
				}
				if n := rustNonPSIEvents(rustRes); n > 0 {
					t.Fatalf("call %d (%s): the psi-only core reported %d event(s) outside PSI coverage",
						call, what, n)
				}
				raps += goRandomAccessPoints(goRes)
				identities += strings.Count(psiEvents(goRes), "identity")
				last = goRes

				goScope, rustScope := scopeOf(goRes), scopeOf(rustRes)
				goScope.events, rustScope.events = psiEvents(goRes), psiEvents(rustRes)
				if bad := goScope.diff(rustScope, "go  ", "rust"); len(bad) > 0 {
					t.Fatalf("call %d (%s):\n%s", call, what, strings.Join(bad, "\n"))
				}
				compared++
			}

			for _, step := range c.steps {
				if !step.isFeed {
					goRes, goErr := local.SetTargetProgram(ctx, step.setTarget)
					rustRes, rustErr := remote.SetTargetProgram(ctx, step.setTarget)
					if goErr != nil {
						t.Fatalf("SetTargetProgram(%d): the reference failed: %v", step.setTarget, goErr)
					}
					if rustErr != nil {
						t.Fatalf("SetTargetProgram(%d): the real core failed: %v", step.setTarget, rustErr)
					}
					t.Logf("  SetTargetProgram(%d) at byte offset %d", step.setTarget, offset)
					compare(fmt.Sprintf("target=%d", step.setTarget), goRes, rustRes)
					continue
				}

				data := bytesFor[step.feed]
				t.Logf("  feed %s: %d bytes starting at offset %d", step.feed, len(data), offset)
				for start := 0; start < len(data); start += c.chunkSize {
					end := start + c.chunkSize
					if end > len(data) {
						end = len(data)
					}
					chunk := data[start:end]

					goRes, goErr := local.Ingest(ctx, offset, chunk)
					rustRes, rustErr := remote.Ingest(ctx, offset, chunk)
					if goErr != nil {
						t.Fatalf("%s+%d: the reference failed: %v", step.feed, start, goErr)
					}
					if rustErr != nil {
						t.Fatalf("%s+%d: the real core failed: %v", step.feed, start, rustErr)
					}
					compare(fmt.Sprintf("%s bytes %d..%d", step.feed, start, end), goRes, rustRes)
					offset += int64(len(chunk))
				}
				t.Logf("  feed %s ended at offset %d", step.feed, offset)
			}

			// What the case proved, in the corpus's own rendering.
			final := scopeOf(last)
			t.Logf("  final facts  %s", final.facts)
			for i, tr := range final.tracks {
				t.Logf("  final track%d %s", i, tr)
			}
			t.Logf("  final psi    %s", truncate(final.psi, 160))
			t.Logf("  identity events across the case: %d", identities)
			if !strings.Contains(final.facts, "hasPAT=1") || !strings.Contains(final.facts, "hasPMT=1") {
				t.Fatalf("case %s never resolved PAT and PMT; it proved nothing", c.name)
			}
		})
	}

	if compared == 0 {
		t.Fatal("no calls were compared; a hardware differential that compares nothing is not evidence")
	}
	t.Logf("hardware differential: %d cases, %d calls, all exact "+
		"(reference also saw %d random access points, which a psi-only core "+
		"does not report and was checked not to)", len(cases), compared, raps)
}

// TestPSIHardware_PacketizationInvariance replays the same captured bytes at
// several chunk sizes.
//
// The ingest contract refuses a slice that is not 188-byte aligned, so a
// boundary can only fall between packets - but a PSI section routinely spans
// packets, so its header, its descriptor loop and its CRC are all straddled by
// the boundaries these sizes produce. One packet per call puts a boundary
// between every pair of packets a section is assembled from.
//
// Two things are asserted, and they are different claims. The two
// implementations must agree at every call, as everywhere else. And the answer
// after the whole capture must not depend on how the capture was handed over:
// the same bytes, differently sliced, must end in the same place.
func TestPSIHardware_PacketizationInvariance(t *testing.T) {
	bin := requireRealCore(t)
	dir := requireHardwareDir(t)
	captures, _ := loadHardwareManifest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Second)
	defer cancel()

	subjects := []struct {
		capture string
		target  uint16
	}{
		{"A_orf1hd", 4911},
		{"B1_puls4", 20007},
	}
	// In packets. One, a 7-packet datagram's worth, the working size, and a
	// chunk far larger than any section.
	sizes := []int{1, 7, 348, 5576}

	total := 0
	for _, subj := range subjects {
		c, ok := captures[subj.capture]
		if !ok {
			t.Fatalf("the manifest does not declare %s", subj.capture)
		}
		data := readCapture(t, dir, c)

		var reference string
		for _, packets := range sizes {
			size := packets * 188
			name := fmt.Sprintf("%s/%dpackets", subj.capture, packets)
			t.Run(name, func(t *testing.T) {
				remote, err := Start(ctx, bin, subj.target)
				if err != nil {
					t.Fatalf("Start: %v", err)
				}
				defer func() {
					if err := remote.Close(); err != nil {
						t.Errorf("Close: %v", err)
					}
				}()
				local := mediafacts.NewGoCore(subj.target)

				var last mediafacts.ParseResult
				offset := int64(0)
				for start := 0; start < len(data); start += size {
					end := start + size
					if end > len(data) {
						end = len(data)
					}
					chunk := data[start:end]

					goRes, goErr := local.Ingest(ctx, offset, chunk)
					rustRes, rustErr := remote.Ingest(ctx, offset, chunk)
					if goErr != nil {
						t.Fatalf("offset %d: the reference failed: %v", offset, goErr)
					}
					if rustErr != nil {
						t.Fatalf("offset %d: the real core failed: %v", offset, rustErr)
					}
					if n := rustNonPSIEvents(rustRes); n > 0 {
						t.Fatalf("offset %d: the psi-only core reported %d event(s) outside PSI", offset, n)
					}
					goScope, rustScope := scopeOf(goRes), scopeOf(rustRes)
					goScope.events, rustScope.events = psiEvents(goRes), psiEvents(rustRes)
					if bad := goScope.diff(rustScope, "go  ", "rust"); len(bad) > 0 {
						t.Fatalf("%s at offset %d:\n%s", name, offset, strings.Join(bad, "\n"))
					}
					last = goRes
					offset += int64(len(chunk))
					total++
				}

				final := scopeOf(last)
				if !strings.Contains(final.facts, "hasPAT=1") || !strings.Contains(final.facts, "hasPMT=1") {
					t.Fatalf("%s never resolved PAT and PMT", name)
				}
				// The chunking must not change where the capture ends up. The
				// first size read becomes the reference the rest are held to.
				rendered := final.facts + "\n" + strings.Join(final.tracks, "\n") + "\n" + final.psi
				if reference == "" {
					reference = rendered
					t.Logf("%s established the reference answer: %s", name, final.facts)
					return
				}
				if rendered != reference {
					t.Fatalf("%s ended somewhere else than the first chunking did:\n  got  %s\n  want %s",
						name, rendered, reference)
				}
				t.Logf("%s agrees with the reference answer", name)
			})
		}
	}
	if total == 0 {
		t.Fatal("no calls were compared")
	}
	t.Logf("packetization invariance: %d subjects x %d chunkings, %d calls, all exact",
		len(subjects), len(sizes), total)
}
