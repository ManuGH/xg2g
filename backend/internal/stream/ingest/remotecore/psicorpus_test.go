// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// A reader for the shared PSI corpus.
//
// The corpus is written by the mediafacts tests and read by the Rust core's
// tests; this is the third reader of the same file, and it exists because the
// differential this package runs needs the same calls in the same order that
// both implementations were already held to separately.
//
// It is deliberately small. It reads the calls and the authored expectations and
// does no interpreting of its own: a reader that understood PSI would be a third
// implementation, and the whole point of the file is that there are only two.
//
// Fail-closed, for the reason the Rust reader is: a reader that finds three cases
// and passes is worse than one that fails, because it reports success for a
// differential that never ran.

const corpusPath = "../../../../../testdata/psi-corpus/corpus.txt"

// corpusFormatVersion is the format this reader understands. A file declaring
// anything else is refused rather than read with the wrong meanings.
const corpusFormatVersion = "2"

// corpusMinimumCases is a floor rather than an equality, so cases added later
// run here without a change - and a floor at all, so a reader bug that finds
// three cases cannot pass.
const corpusMinimumCases = 79

type corpusCall struct {
	// chunk is the bytes for an Ingest, or nil when this call is a target change.
	chunk []byte
	// target is the programme for a SetTargetProgram call.
	target  uint16
	isChunk bool

	// want is the authored expectation for this call, in the corpus's own line
	// form. It is kept as text on purpose: comparing rendered lines is what the
	// two implementations already do, and re-deriving structured values here
	// would be a third opinion about what the file says.
	wantFacts  string
	wantTracks []string
	wantEvents string
	wantPSI    string
}

type corpusCase struct {
	name    string
	initial uint16
	calls   []corpusCall
}

func loadPSICorpus(t *testing.T) []corpusCase {
	t.Helper()

	f, err := os.Open(corpusPath)
	if err != nil {
		t.Fatalf("open the shared corpus: %v", err)
	}
	defer func() { _ = f.Close() }()

	var (
		cases   []corpusCase
		current *corpusCase
		call    *corpusCall
		version string
	)

	// finish attaches the call being read to the case being read. A call is
	// complete when its psi line has arrived, which is the last of the four.
	finish := func() {
		if current != nil && call != nil {
			current.calls = append(current.calls, *call)
			call = nil
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		keyword, rest, _ := strings.Cut(text, " ")

		switch keyword {
		case "version":
			if version != "" {
				t.Fatalf("line %d: the corpus declares its version twice", line)
			}
			if rest != corpusFormatVersion {
				t.Fatalf("line %d: corpus format version %q, this reader understands %q",
					line, rest, corpusFormatVersion)
			}
			version = rest

		case "case":
			if version == "" {
				t.Fatalf("line %d: a case before the format version", line)
			}
			if current != nil {
				t.Fatalf("line %d: a case inside a case", line)
			}
			current = &corpusCase{name: rest}

		case "desc", "note":
			if current == nil {
				t.Fatalf("line %d: %q outside a case", line, keyword)
			}

		case "init":
			if current == nil {
				t.Fatalf("line %d: init outside a case", line)
			}
			current.initial = corpusTargetField(t, line, rest)

		case "chunk":
			if current == nil {
				t.Fatalf("line %d: chunk outside a case", line)
			}
			finish()
			raw, err := hex.DecodeString(rest)
			if err != nil {
				t.Fatalf("line %d: chunk hex: %v", line, err)
			}
			call = &corpusCall{chunk: raw, isChunk: true}

		case "target":
			if current == nil {
				t.Fatalf("line %d: target outside a case", line)
			}
			finish()
			n, err := strconv.ParseUint(rest, 10, 16)
			if err != nil {
				t.Fatalf("line %d: target: %v", line, err)
			}
			call = &corpusCall{target: uint16(n)}

		case "facts":
			if call == nil {
				t.Fatalf("line %d: facts before a call", line)
			}
			call.wantFacts = rest
		case "track":
			if call == nil {
				t.Fatalf("line %d: track before a call", line)
			}
			call.wantTracks = append(call.wantTracks, rest)
		case "events":
			if call == nil {
				t.Fatalf("line %d: events before a call", line)
			}
			call.wantEvents = rest
		case "psi":
			if call == nil {
				t.Fatalf("line %d: psi before a call", line)
			}
			call.wantPSI = rest

		case "end":
			if current == nil {
				t.Fatalf("line %d: end outside a case", line)
			}
			finish()
			if len(current.calls) == 0 {
				t.Fatalf("line %d: case %q has no calls", line, current.name)
			}
			cases = append(cases, *current)
			current = nil

		default:
			t.Fatalf("line %d: %q is not a keyword this reader knows", line, keyword)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read the corpus: %v", err)
	}
	if current != nil {
		t.Fatal("the corpus ends inside a case")
	}
	if version == "" {
		t.Fatal("the corpus never declared its format version")
	}
	if len(cases) < corpusMinimumCases {
		t.Fatalf("the reader found %d cases and the corpus had at least %d; "+
			"a differential that runs three of them is not a differential",
			len(cases), corpusMinimumCases)
	}
	return cases
}

func corpusTargetField(t *testing.T, line int, rest string) uint16 {
	t.Helper()
	key, value, ok := strings.Cut(rest, "=")
	if !ok || key != "target" {
		t.Fatalf("line %d: init says %q, want target=<n>", line, rest)
	}
	n, err := strconv.ParseUint(value, 10, 16)
	if err != nil {
		t.Fatalf("line %d: init target: %v", line, err)
	}
	return uint16(n)
}

// corpusCalls counts what a run of the corpus actually exercised, so a report
// can say how much ran rather than that something did.
func corpusCalls(cases []corpusCase) int {
	n := 0
	for _, c := range cases {
		n += len(c.calls)
	}
	return n
}

func (c corpusCall) String() string {
	if c.isChunk {
		return fmt.Sprintf("chunk of %d packets", len(c.chunk)/188)
	}
	return fmt.Sprintf("target %d", c.target)
}
