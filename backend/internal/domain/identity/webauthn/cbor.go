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
	"math"
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

	maxCBORPayloadBytes   = 1 << 20
	maxCBORContainerItems = 4096
	maxCBORNestingDepth   = 32
)

// DecodeCBOR parses a CBOR payload into native Go types (int64, []byte, string, []any, map[any]any).
func DecodeCBOR(data []byte) (any, error) {
	if len(data) > maxCBORPayloadBytes {
		return nil, fmt.Errorf("%w: payload exceeds %d bytes", ErrInvalidCBOR, maxCBORPayloadBytes)
	}
	r := bytes.NewReader(data)
	val, err := decodeItem(r, 0)
	if err != nil {
		return nil, err
	}
	return val, nil
}

func decodeItem(r *bytes.Reader, depth int) (any, error) {
	if depth > maxCBORNestingDepth {
		return nil, fmt.Errorf("%w: nesting exceeds %d levels", ErrInvalidCBOR, maxCBORNestingDepth)
	}

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
		if val > math.MaxInt64 {
			return nil, fmt.Errorf("%w: unsigned integer exceeds int64", ErrInvalidCBOR)
		}
		return int64(val), nil

	case cborTypeNint:
		// Negative integer: -1 - val
		if val > math.MaxInt64 {
			return nil, fmt.Errorf("%w: negative integer exceeds int64", ErrInvalidCBOR)
		}
		return -1 - int64(val), nil

	case cborTypeBytes:
		length, err := boundedCBORLength(val, r.Len(), maxCBORPayloadBytes, "byte string")
		if err != nil {
			return nil, err
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return buf, nil

	case cborTypeText:
		length, err := boundedCBORLength(val, r.Len(), maxCBORPayloadBytes, "text string")
		if err != nil {
			return nil, err
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf), nil

	case cborTypeArray:
		length, err := boundedCBORLength(val, r.Len(), maxCBORContainerItems, "array")
		if err != nil {
			return nil, err
		}
		arr := make([]any, length)
		for i := 0; i < length; i++ {
			elem, err := decodeItem(r, depth+1)
			if err != nil {
				return nil, err
			}
			arr[i] = elem
		}
		return arr, nil

	case cborTypeMap:
		length, err := boundedCBORLength(val, r.Len()/2, maxCBORContainerItems, "map")
		if err != nil {
			return nil, err
		}
		m := make(map[any]any, length)
		for i := 0; i < length; i++ {
			k, err := decodeItem(r, depth+1)
			if err != nil {
				return nil, err
			}
			v, err := decodeItem(r, depth+1)
			if err != nil {
				return nil, err
			}
			m[k] = v
		}
		return m, nil

	case cborTypeTag:
		// Skip tag and decode tagged item
		return decodeItem(r, depth+1)

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

func boundedCBORLength(value uint64, remaining, maximum int, kind string) (int, error) {
	if remaining < 0 || maximum < 0 {
		return 0, fmt.Errorf("%w: invalid %s length bounds", ErrInvalidCBOR, kind)
	}
	maxValue := uint64(maximum) // #nosec G115 -- maximum is checked non-negative above.
	if value > maxValue {
		return 0, fmt.Errorf("%w: %s length %d exceeds limit %d", ErrInvalidCBOR, kind, value, maximum)
	}
	remainingValue := uint64(remaining) // #nosec G115 -- bytes.Reader.Len and derived values are non-negative.
	if value > remainingValue {
		return 0, fmt.Errorf("%w: %s length %d exceeds remaining input", ErrInvalidCBOR, kind, value)
	}
	length := int(value) // #nosec G115 -- value is bounded by a small positive maximum above.
	return length, nil
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
