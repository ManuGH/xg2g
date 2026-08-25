// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Enigma2 records every transponder it has ever tuned in its own service database,
// /etc/enigma2/lamedb (format 4) and /etc/enigma2/lamedb5 (format 5). That file is
// the receiver's authoritative answer to "which physical carrier does this transport
// stream live on" - it is what the frontend is driven from, so it cannot disagree
// with the hardware the way a hand-maintained table can.
//
// Both formats carry the same frontend parameters; they differ only in how records
// are laid out. Format 5 documents its own field order in a comment header:
//
//	DVBS  FEPARMS:  s:frequency:symbol_rate:polarisation:fec:orbital_position:inversion:flags
//	DVBS2 FEPARMS:  s:...:flags:system:modulation:rolloff:pilot[,MIS/PLS:is_id:pls_code:pls_mode][,T2MI:...]
//	DVBT  FEPARMS:  t:frequency:bandwidth:code_rate_HP:code_rate_LP:modulation:transmission_mode:guard_interval:hierarchy:inversion:flags:system:plp_id
//	DVBC  FEPARMS:  c:frequency:symbol_rate:inversion:modulation:fec_inner:flags:system
//	ATSC  FEPARMS:  a:frequency:inversion:modulation:flags:system

// LamedbTransponder is one physical transponder as recorded by the receiver itself,
// keyed by the same DVB triple that a service reference carries.
type LamedbTransponder struct {
	DVBNamespace uint32
	TSID         uint16
	ONID         uint16
	Key          TransponderKey
}

// LamedbSnapshot is the outcome of parsing one service database.
//
// Skipped and Malformed are reported rather than swallowed: a caller that receives
// transponders but also a high malformed count is looking at a format this parser
// does not fully understand, and should say so instead of silently tuning on a
// partial picture.
type LamedbSnapshot struct {
	Version      int
	Transponders []LamedbTransponder
	// Skipped counts records whose delivery system this build does not model (ATSC).
	Skipped int
	// Malformed counts records that claimed a known delivery system but could not be read.
	Malformed int
}

// ErrLamedbFormat indicates the payload is not a service database this parser understands.
var ErrLamedbFormat = fmt.Errorf("unrecognized enigma2 service database format")

// lamedb polarisation and delivery-system encodings, as written by enigma2.
var lamedbPolarization = map[string]Polarization{
	"0": PolarizationHorizontal,
	"1": PolarizationVertical,
	"2": PolarizationCircularL,
	"3": PolarizationCircularR,
}

var lamedbPLSMode = map[string]PLSMode{
	"0": PLSModeRoot,
	"1": PLSModeGold,
	"2": PLSModeCombo,
}

// ParseLamedb reads an Enigma2 service database and returns every transponder in it.
//
// Only the transponder section is read. The service section is deliberately ignored:
// a service reference already carries its own TSID, ONID and namespace, so the
// transponder table alone is enough to resolve one, and skipping the (much larger)
// service section keeps this independent of service-name encoding quirks.
func ParseLamedb(data []byte) (LamedbSnapshot, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	// Service names in the section we skip can be long; give the scanner room so a
	// single oversized line cannot abort the parse with ErrTooLong.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if !sc.Scan() {
		return LamedbSnapshot{}, fmt.Errorf("%w: empty payload", ErrLamedbFormat)
	}

	version, err := lamedbVersion(sc.Text())
	if err != nil {
		return LamedbSnapshot{}, err
	}

	switch version {
	case 4:
		return parseLamedbV4(sc)
	case 5:
		return parseLamedbV5(sc)
	default:
		return LamedbSnapshot{}, fmt.Errorf("%w: unsupported version %d", ErrLamedbFormat, version)
	}
}

// lamedbVersion reads the "eDVB services /N/" banner that opens both formats.
func lamedbVersion(header string) (int, error) {
	h := strings.TrimSpace(strings.TrimPrefix(header, "\ufeff"))
	const prefix = "eDVB services /"
	if !strings.HasPrefix(h, prefix) {
		return 0, fmt.Errorf("%w: missing banner, got %q", ErrLamedbFormat, truncateForError(h))
	}
	v := strings.TrimSuffix(strings.TrimPrefix(h, prefix), "/")
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, fmt.Errorf("%w: unreadable version %q", ErrLamedbFormat, truncateForError(v))
	}
	return n, nil
}

// parseLamedbV4 walks the line-oriented format:
//
//	transponders
//	<namespace>:<tsid>:<onid>
//		<feparms>
//	/
//	...
//	end
func parseLamedbV4(sc *bufio.Scanner) (LamedbSnapshot, error) {
	snap := LamedbSnapshot{Version: 4}

	const (
		beforeSection = iota
		expectKey
		expectFEPARMS
		expectTerminator
	)
	state := beforeSection
	var key string

	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimSpace(line)

		switch state {
		case beforeSection:
			if trimmed == "transponders" {
				state = expectKey
			}
		case expectKey:
			// The transponder section closes with "end"; the service section follows
			// and is of no interest here.
			if trimmed == "end" {
				return finishLamedb(snap, sc)
			}
			if trimmed == "" {
				continue
			}
			key = trimmed
			state = expectFEPARMS
		case expectFEPARMS:
			if trimmed == "" {
				continue
			}
			tp, kind, ok := parseLamedbRecord(key, trimmed)
			switch {
			case ok:
				snap.Transponders = append(snap.Transponders, tp)
			case kind == "a":
				snap.Skipped++
			default:
				snap.Malformed++
			}
			state = expectTerminator
		case expectTerminator:
			// Enigma2 closes each record with "/". Anything else here means the record
			// carried more lines than this parser models, so resynchronize on the
			// terminator rather than mistaking a payload line for the next key.
			if trimmed == "/" {
				state = expectKey
			}
		}
	}

	if err := sc.Err(); err != nil {
		return snap, fmt.Errorf("read service database: %w", err)
	}
	if state == beforeSection {
		return snap, fmt.Errorf("%w: no transponder section", ErrLamedbFormat)
	}
	return snap, nil
}

// parseLamedbV5 walks the one-record-per-line format:
//
//	t:<namespace>:<tsid>:<onid>,<feparms>[,<extra group>]*
//
// Service records share the file and open with "s:", comments with "#"; both are ignored.
func parseLamedbV5(sc *bufio.Scanner) (LamedbSnapshot, error) {
	snap := LamedbSnapshot{Version: 5}

	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimRight(sc.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasPrefix(line, "t:") {
			continue
		}

		comma := strings.Index(line, ",")
		if comma < 0 {
			snap.Malformed++
			continue
		}

		key := strings.TrimPrefix(line[:comma], "t:")
		tp, kind, ok := parseLamedbRecord(key, line[comma+1:])
		switch {
		case ok:
			snap.Transponders = append(snap.Transponders, tp)
		case kind == "a":
			snap.Skipped++
		default:
			snap.Malformed++
		}
	}

	if err := sc.Err(); err != nil {
		return snap, fmt.Errorf("read service database: %w", err)
	}
	return snap, nil
}

// finishLamedb drains the remainder of the reader so a scanner error in the
// (ignored) service section still surfaces rather than being reported as success.
func finishLamedb(snap LamedbSnapshot, sc *bufio.Scanner) (LamedbSnapshot, error) {
	for sc.Scan() { //nolint:revive // draining to observe a late read error
	}
	if err := sc.Err(); err != nil {
		return snap, fmt.Errorf("read service database: %w", err)
	}
	return snap, nil
}

// parseLamedbRecord turns one "<namespace>:<tsid>:<onid>" key plus its frontend
// parameters into a transponder. The returned kind is the delivery-system letter,
// so a caller can tell "we do not model this" apart from "this is broken".
func parseLamedbRecord(key, feparms string) (LamedbTransponder, string, bool) {
	ns, tsid, onid, ok := parseLamedbKey(key)
	if !ok {
		return LamedbTransponder{}, "", false
	}

	kind, fields, extras, ok := splitFEPARMS(feparms)
	if !ok {
		return LamedbTransponder{}, "", false
	}

	var tpKey TransponderKey
	switch kind {
	case "s":
		tpKey, ok = satelliteKey(fields, extras)
	case "c":
		tpKey, ok = cableKey(fields)
	case "t":
		tpKey, ok = terrestrialKey(fields, extras)
	default:
		// ATSC ("a") and anything newer: no TransponderKey shape models it, and
		// guessing one would be worse than admitting the gap.
		return LamedbTransponder{}, kind, false
	}
	if !ok {
		return LamedbTransponder{}, kind, false
	}

	return LamedbTransponder{DVBNamespace: ns, TSID: tsid, ONID: onid, Key: tpKey}, kind, true
}

// parseLamedbKey reads the hexadecimal "<namespace>:<tsid>:<onid>" triple that
// identifies a transponder in both formats.
func parseLamedbKey(key string) (uint32, uint16, uint16, bool) {
	parts := strings.Split(strings.TrimSpace(key), ":")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	ns, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	tsid, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return 0, 0, 0, false
	}
	onid, err := strconv.ParseUint(parts[2], 16, 16)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint32(ns), uint16(tsid), uint16(onid), true
}

// splitFEPARMS normalizes both spellings of a frontend-parameter payload:
// format 4 writes "s <fields>", format 5 writes "s:<fields>" followed by optional
// comma-separated extra groups such as "MIS/PLS:is_id:pls_code:pls_mode".
func splitFEPARMS(feparms string) (kind string, fields []string, extras map[string][]string, ok bool) {
	groups := strings.Split(strings.TrimSpace(feparms), ",")
	head := strings.TrimSpace(groups[0])
	if head == "" {
		return "", nil, nil, false
	}

	var rest string
	if sp := strings.IndexAny(head, " \t"); sp >= 0 {
		kind, rest = head[:sp], strings.TrimSpace(head[sp+1:])
	} else if colon := strings.Index(head, ":"); colon >= 0 {
		kind, rest = head[:colon], head[colon+1:]
	} else {
		return "", nil, nil, false
	}
	if kind == "" || rest == "" {
		return "", nil, nil, false
	}

	fields = strings.Split(rest, ":")
	if len(groups) > 1 {
		extras = make(map[string][]string, len(groups)-1)
		for _, g := range groups[1:] {
			g = strings.TrimSpace(g)
			colon := strings.Index(g, ":")
			if colon <= 0 {
				continue
			}
			extras[strings.ToUpper(g[:colon])] = strings.Split(g[colon+1:], ":")
		}
	}
	return kind, fields, extras, true
}

// satelliteKey reads DVB-S/S2 frontend parameters. Enigma2 stores satellite
// frequency in kHz and orbital position in tenths of a degree, signed west-negative.
func satelliteKey(fields []string, extras map[string][]string) (TransponderKey, bool) {
	if len(fields) < 7 {
		return TransponderKey{}, false
	}
	freqKHz, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || freqKHz == 0 {
		return TransponderKey{}, false
	}
	pol, ok := lamedbPolarization[strings.TrimSpace(fields[2])]
	if !ok {
		return TransponderKey{}, false
	}
	orbital, err := strconv.Atoi(strings.TrimSpace(fields[4]))
	if err != nil {
		return TransponderKey{}, false
	}

	// The short (7-field) form predates DVB-S2 and is written for DVB-S only.
	system := DeliverySystemDVBS
	if len(fields) > 7 && strings.TrimSpace(fields[7]) == "1" {
		system = DeliverySystemDVBS2
	}

	key := TransponderKey{
		DeliverySystem:  system,
		OrbitalPosition: orbital,
		FrequencyHz:     freqKHz * 1000,
		Polarization:    pol,
		StreamID:        -1,
	}
	applyMultistream(&key, fields, extras)
	return key, true
}

// applyMultistream reads the DVB-S2 multiple-input-stream and physical-layer-scrambling
// parameters. Format 5 carries them in a "MIS/PLS" group, format 4 appends them to the
// same colon-separated run; a transponder without them keeps StreamID -1, which
// TransponderKey.Canonical() treats as "not a multistream carrier".
func applyMultistream(key *TransponderKey, fields []string, extras map[string][]string) {
	mis := extras["MIS/PLS"]
	if len(mis) == 0 && len(fields) >= 12 {
		mis = fields[11:]
	}
	if len(mis) == 0 {
		return
	}
	if id, err := strconv.Atoi(strings.TrimSpace(mis[0])); err == nil {
		key.StreamID = id
	}
	if len(mis) > 1 {
		if code, err := strconv.ParseUint(strings.TrimSpace(mis[1]), 10, 32); err == nil {
			key.PLSCode = uint32(code)
		}
	}
	if len(mis) > 2 {
		if mode, ok := lamedbPLSMode[strings.TrimSpace(mis[2])]; ok {
			key.PLSMode = mode
		}
	}
}

// cableKey reads DVB-C/C2 frontend parameters. Cable frequency is stored in kHz.
//
// Not verified against cable hardware: the field order follows the format-5 header
// written by the receiver itself.
func cableKey(fields []string) (TransponderKey, bool) {
	if len(fields) < 6 {
		return TransponderKey{}, false
	}
	freqKHz, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || freqKHz == 0 {
		return TransponderKey{}, false
	}
	system := DeliverySystemDVBC
	if len(fields) > 6 && strings.TrimSpace(fields[6]) == "1" {
		system = DeliverySystemDVBC2
	}
	return TransponderKey{
		DeliverySystem: system,
		FrequencyHz:    freqKHz * 1000,
		StreamID:       -1,
	}, true
}

// terrestrialKey reads DVB-T/T2 frontend parameters. Unlike satellite and cable,
// enigma2 stores terrestrial frequency in Hz.
//
// Not verified against terrestrial hardware: the field order follows the format-5
// header written by the receiver itself.
func terrestrialKey(fields []string, extras map[string][]string) (TransponderKey, bool) {
	if len(fields) < 10 {
		return TransponderKey{}, false
	}
	freqHz, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || freqHz == 0 {
		return TransponderKey{}, false
	}
	system := DeliverySystemDVBT
	if len(fields) > 10 && strings.TrimSpace(fields[10]) == "1" {
		system = DeliverySystemDVBT2
	}
	key := TransponderKey{
		DeliverySystem: system,
		FrequencyHz:    freqHz,
		StreamID:       -1,
	}
	// The PLP identifier is the terrestrial equivalent of a satellite input stream id,
	// and TransponderKey.Canonical() already folds it into the DVB-T2 identity.
	if plp := terrestrialPLP(fields, extras); plp >= 0 {
		key.StreamID = plp
	}
	return key, true
}

func terrestrialPLP(fields []string, extras map[string][]string) int {
	if t2mi := extras["T2MI"]; len(t2mi) > 0 {
		if plp, err := strconv.Atoi(strings.TrimSpace(t2mi[0])); err == nil {
			return plp
		}
	}
	if len(fields) > 11 {
		if plp, err := strconv.Atoi(strings.TrimSpace(fields[11])); err == nil {
			return plp
		}
	}
	return -1
}

func truncateForError(s string) string {
	const max = 48
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
