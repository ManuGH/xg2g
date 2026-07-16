package ffmpeg

import (
	"encoding/binary"
	"testing"

	"github.com/ManuGH/xg2g/internal/pipeline/store"
)

func makeBox(typ string, data []byte) []byte {
	box := make([]byte, 8+len(data))
	binary.BigEndian.PutUint32(box[0:4], uint32(len(box)))
	copy(box[4:8], typ)
	copy(box[8:], data)
	return box
}

func makeBox64(typ string, data []byte) []byte {
	box := make([]byte, 16+len(data))
	binary.BigEndian.PutUint32(box[0:4], 1)
	copy(box[4:8], typ)
	binary.BigEndian.PutUint64(box[8:16], uint64(len(box)))
	copy(box[16:], data)
	return box
}

func TestValidCompleteFMP4(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		kind store.ObjectKind
		want bool
	}{
		{
			name: "Valid Segment (moof + mdat)",
			data: append(makeBox("moof", []byte("foo")), makeBox("mdat", []byte("bar"))...),
			kind: store.ObjectSegment,
			want: true,
		},
		{
			name: "Valid Init (ftyp + moov)",
			data: append(makeBox("ftyp", []byte("isom")), makeBox("moov", []byte("..."))...),
			kind: store.ObjectInit,
			want: true,
		},
		{
			name: "Truncated Box Header",
			data: []byte{0, 0, 0, 10, 'm', 'o', 'o'},
			kind: store.ObjectSegment,
			want: false,
		},
		{
			name: "Truncated File (box claims larger size)",
			data: []byte{0, 0, 0, 100, 'm', 'd', 'a', 't', 0, 1}, // only 10 bytes long
			kind: store.ObjectSegment,
			want: false,
		},
		{
			name: "Missing Required Boxes (Segment without mdat)",
			data: makeBox("moof", []byte("foo")),
			kind: store.ObjectSegment,
			want: false,
		},
		{
			name: "Missing Required Boxes (Init without moov)",
			data: makeBox("ftyp", []byte("isom")),
			kind: store.ObjectInit,
			want: false,
		},
		{
			name: "64-bit box valid",
			data: append(makeBox("moof", []byte("foo")), makeBox64("mdat", make([]byte, 100))...),
			kind: store.ObjectSegment,
			want: true,
		},
		{
			name: "64-bit box truncated",
			data: []byte{0, 0, 0, 1, 'm', 'd', 'a', 't', 0, 0, 0, 0, 0, 0, 1, 0}, // 16 bytes, but claims size 256
			kind: store.ObjectSegment,
			want: false,
		},
		{
			name: "Trailing garbage",
			data: append(append(makeBox("moof", []byte("foo")), makeBox("mdat", []byte("bar"))...), 0, 1, 2),
			kind: store.ObjectSegment,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validCompleteFMP4(tt.data, tt.kind); got != tt.want {
				t.Errorf("validCompleteFMP4() = %v, want %v", got, tt.want)
			}
		})
	}
}
