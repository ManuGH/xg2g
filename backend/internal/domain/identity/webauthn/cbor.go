// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package webauthn

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var (
	ErrInvalidCBOR = errors.New("invalid cbor data")
	ErrUnsupported = errors.New("unsupported cbor type")
)

// CBOR types per RFC 8949
const (
	cborTypeUint  = 0
	cborTypeNint  = 1
	cborTypeBytes = 2
	cborTypeText  = 3
	cborTypeArray = 4
	cborTypeMap   = 5
	cborTypeTag   = 6
	cborTypeOther = 7
)

// DecodeCBOR parses a CBOR payload into native Go types (int64, []byte, string, []any, map[any]any).
func DecodeCBOR(data []byte) (any, error) {
	r := bytes.NewReader(data)
	val, err := decodeItem(r)
	if err != nil {
		return nil, err
	}
	return val, nil
}

func decodeItem(r *bytes.Reader) (any, error) {
	initial, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	majorType := initial >> 5
	info := initial & 0x1f

	val, err := decodeLength(r, info)
	if err != nil {
		return nil, err
	}

	switch majorType {
	case cborTypeUint:
		return int64(val), nil

	case cborTypeNint:
		// Negative integer: -1 - val
		return -1 - int64(val), nil

	case cborTypeBytes:
		buf := make([]byte, val)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return buf, nil

	case cborTypeText:
		buf := make([]byte, val)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf), nil

	case cborTypeArray:
		arr := make([]any, val)
		for i := uint64(0); i < val; i++ {
			elem, err := decodeItem(r)
			if err != nil {
				return nil, err
			}
			arr[i] = elem
		}
		return arr, nil

	case cborTypeMap:
		m := make(map[any]any, val)
		for i := uint64(0); i < val; i++ {
			k, err := decodeItem(r)
			if err != nil {
				return nil, err
			}
			v, err := decodeItem(r)
			if err != nil {
				return nil, err
			}
			m[k] = v
		}
		return m, nil

	case cborTypeTag:
		// Skip tag and decode tagged item
		return decodeItem(r)

	case cborTypeOther:
		switch info {
		case 20:
			return false, nil
		case 21:
			return true, nil
		case 22:
			return nil, nil
		default:
			return nil, fmt.Errorf("%w: simple value %d", ErrUnsupported, info)
		}

	default:
		return nil, fmt.Errorf("%w: major type %d", ErrUnsupported, majorType)
	}
}

func decodeLength(r *bytes.Reader, info byte) (uint64, error) {
	switch {
	case info < 24:
		return uint64(info), nil
	case info == 24:
		b, err := r.ReadByte()
		return uint64(b), err
	case info == 25:
		var val uint16
		err := binary.Read(r, binary.BigEndian, &val)
		return uint64(val), err
	case info == 26:
		var val uint32
		err := binary.Read(r, binary.BigEndian, &val)
		return uint64(val), err
	case info == 27:
		var val uint64
		err := binary.Read(r, binary.BigEndian, &val)
		return val, err
	default:
		return 0, fmt.Errorf("%w: length info %d", ErrInvalidCBOR, info)
	}
}
