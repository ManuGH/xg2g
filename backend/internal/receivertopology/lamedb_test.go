// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"os"
	"path/filepath"
	"testing"
)

// The fixtures live in ./topologytest so other packages can embed the same captures.
// They are the transponder sections of the real service database of the
// Vu+ Uno4K this project is developed against (OpenATV, OWIF 2.4.0), captured via
// OpenWebIF's /file endpoint. Both formats describe the same 161 transponders, so
// they double as a cross-check that the two parsers agree.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("topologytest", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func findTransponder(tps []LamedbTransponder, ns uint32, tsid, onid uint16) (LamedbTransponder, bool) {
	for _, tp := range tps {
		if tp.DVBNamespace == ns && tp.TSID == tsid && tp.ONID == onid {
			return tp, true
		}
	}
	return LamedbTransponder{}, false
}

func TestParseLamedb_RealReceiverBothFormatsAgree(t *testing.T) {
	v4, err := ParseLamedb(loadFixture(t, "lamedb_v4_vuuno4k.txt"))
	if err != nil {
		t.Fatalf("parse lamedb v4: %v", err)
	}
	v5, err := ParseLamedb(loadFixture(t, "lamedb5_vuuno4k.txt"))
	if err != nil {
		t.Fatalf("parse lamedb v5: %v", err)
	}

	if v4.Version != 4 || v5.Version != 5 {
		t.Fatalf("version detection failed: v4=%d v5=%d", v4.Version, v5.Version)
	}
	if v4.Malformed != 0 || v5.Malformed != 0 {
		t.Fatalf("real receiver database must parse cleanly, got malformed v4=%d v5=%d", v4.Malformed, v5.Malformed)
	}
	if len(v4.Transponders) != len(v5.Transponders) {
		t.Fatalf("formats disagree on transponder count: v4=%d v5=%d", len(v4.Transponders), len(v5.Transponders))
	}
	if len(v4.Transponders) == 0 {
		t.Fatal("no transponders parsed from the real receiver database")
	}

	// Same physical carriers, so every key must be identical across the two formats.
	for _, a := range v4.Transponders {
		b, ok := findTransponder(v5.Transponders, a.DVBNamespace, a.TSID, a.ONID)
		if !ok {
			t.Fatalf("transponder %08X:%04X:%04X present in v4 but missing from v5", a.DVBNamespace, a.TSID, a.ONID)
		}
		if a.Key != b.Key {
			t.Fatalf("transponder %08X:%04X:%04X differs: v4=%+v v5=%+v", a.DVBNamespace, a.TSID, a.ONID, a.Key, b.Key)
		}
	}
}

// The exact carrier ORF1 HD is broadcast on. This is the channel the live streaming
// work is verified against, and the frequency the retired static table got wrong.
func TestParseLamedb_ORFTransponderMatchesHardware(t *testing.T) {
	snap, err := ParseLamedb(loadFixture(t, "lamedb_v4_vuuno4k.txt"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	tp, ok := findTransponder(snap.Transponders, 0x00C00000, 0x03EF, 0x0001)
	if !ok {
		t.Fatal("ORF transponder 00C00000:03EF:0001 not found")
	}

	want := TransponderKey{
		DeliverySystem:  DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     11302750000,
		Polarization:    PolarizationHorizontal,
		StreamID:        -1,
	}
	if tp.Key != want {
		t.Fatalf("ORF transponder mismatch:\n got %+v\nwant %+v", tp.Key, want)
	}
}

func TestParseLamedb_FrontendParameterVariants(t *testing.T) {
	// One synthetic database covering the shapes the real Uno4K does not carry:
	// a multistream DVB-S2 carrier, cable, terrestrial, ATSC (unmodelled), and a
	// record that claims a known delivery system but is unreadable.
	const doc = "eDVB services /5/\n" +
		"# comment line\n" +
		"t:00c00000:03ef:0001,s:11302750:22000000:0:2:192:2:0:1:2:0:2\n" +
		"t:00c00000:0455:0001,s:12551500:22000000:1:2:192:2:0:1:2:0:2,MIS/PLS:3:131070:1\n" +
		"t:0000ffff:1234:0011,c:410000:6900000:0:3:0:0:0\n" +
		"t:eeee0000:5678:2000,t:474000000:0:1:1:2:2:1:0:2:0:1:5\n" +
		"t:11110000:0001:0002,a:551000000:0:1:0:0\n" +
		"t:22220000:0003:0004,s:notanumber:22000000:0:2:192:2:0\n" +
		"s:132f:00c00000:03ef:0001:25:0:0,\"ORF1 HD\",p:ORF\n"

	snap, err := ParseLamedb([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(snap.Transponders) != 4 {
		t.Fatalf("expected 4 modelled transponders, got %d", len(snap.Transponders))
	}
	if snap.Skipped != 1 {
		t.Fatalf("expected the ATSC record to be counted as skipped, got %d", snap.Skipped)
	}
	if snap.Malformed != 1 {
		t.Fatalf("expected the unreadable satellite record to be counted as malformed, got %d", snap.Malformed)
	}

	multistream, ok := findTransponder(snap.Transponders, 0x00C00000, 0x0455, 0x0001)
	if !ok {
		t.Fatal("multistream transponder missing")
	}
	want := TransponderKey{
		DeliverySystem:  DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     12551500000,
		Polarization:    PolarizationVertical,
		StreamID:        3,
		PLSMode:         PLSModeGold,
		PLSCode:         131070,
	}
	if multistream.Key != want {
		t.Fatalf("multistream mismatch:\n got %+v\nwant %+v", multistream.Key, want)
	}

	cable, ok := findTransponder(snap.Transponders, 0x0000FFFF, 0x1234, 0x0011)
	if !ok {
		t.Fatal("cable transponder missing")
	}
	if cable.Key.DeliverySystem != DeliverySystemDVBC || cable.Key.FrequencyHz != 410000000 {
		t.Fatalf("cable mismatch: %+v", cable.Key)
	}

	// Terrestrial frequency is the one enigma2 stores in Hz rather than kHz.
	terr, ok := findTransponder(snap.Transponders, 0xEEEE0000, 0x5678, 0x2000)
	if !ok {
		t.Fatal("terrestrial transponder missing")
	}
	if terr.Key.DeliverySystem != DeliverySystemDVBT2 || terr.Key.FrequencyHz != 474000000 || terr.Key.StreamID != 5 {
		t.Fatalf("terrestrial mismatch: %+v", terr.Key)
	}
}

func TestParseLamedb_MultistreamRoundTripsAcrossFormats(t *testing.T) {
	// Format 4 appends MIS/PLS to the same colon-separated run; format 5 gives it
	// its own comma group. Both must produce the same carrier identity.
	const v4 = "eDVB services /4/\n" +
		"transponders\n" +
		"00c00000:0455:0001\n" +
		"\ts 12551500:22000000:1:2:192:2:0:1:2:0:2:3:131070:1\n" +
		"/\n" +
		"end\n"
	const v5 = "eDVB services /5/\n" +
		"t:00c00000:0455:0001,s:12551500:22000000:1:2:192:2:0:1:2:0:2,MIS/PLS:3:131070:1\n"

	a, err := ParseLamedb([]byte(v4))
	if err != nil {
		t.Fatalf("parse v4: %v", err)
	}
	b, err := ParseLamedb([]byte(v5))
	if err != nil {
		t.Fatalf("parse v5: %v", err)
	}
	if len(a.Transponders) != 1 || len(b.Transponders) != 1 {
		t.Fatalf("expected one transponder each, got v4=%d v5=%d", len(a.Transponders), len(b.Transponders))
	}
	if a.Transponders[0].Key != b.Transponders[0].Key {
		t.Fatalf("multistream differs across formats:\n v4=%+v\n v5=%+v", a.Transponders[0].Key, b.Transponders[0].Key)
	}
	if a.Transponders[0].Key.Canonical() != b.Transponders[0].Key.Canonical() {
		t.Fatalf("canonical identity differs: %q vs %q",
			a.Transponders[0].Key.Canonical(), b.Transponders[0].Key.Canonical())
	}
}

func TestParseLamedb_RejectsForeignPayload(t *testing.T) {
	for name, payload := range map[string]string{
		"empty":            "",
		"html error page":  "<html><head><title>OpenWebif</title></head><body><h1>Error 404: Not found</h1></body></html>",
		"json":             `{"result": true}`,
		"unknown version":  "eDVB services /9/\ntransponders\nend\n",
		"missing sections": "eDVB services /4/\nservices\nend\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseLamedb([]byte(payload)); err == nil {
				t.Fatalf("expected %s to be rejected", name)
			}
		})
	}
}
