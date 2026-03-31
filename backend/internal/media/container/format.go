package container

import "github.com/ManuGH/xg2g/internal/media/codec"

// Format identifies a concrete media container or segment format.
type Format uint8

const (
	FormatUnknown Format = iota
	MPEGTS
	FMP4
	MP4
	MKV
)

// DeliveryMethod identifies how a client receives media.
type DeliveryMethod uint8

const (
	DeliveryUnknown DeliveryMethod = iota
	DirectFile
	HLS
)

// CanCarry returns the practical container-level support matrix xg2g should
// reason about. This is intentionally domain knowledge, not a theoretical muxing
// matrix.
func (f Format) CanCarry(id codec.ID) bool {
	switch f {
	case MPEGTS:
		switch id {
		case codec.IDH264, codec.IDHEVC, codec.IDMPEG2, codec.IDAAC, codec.IDAC3, codec.IDEAC3, codec.IDMP2, codec.IDMP3:
			return true
		default:
			return false
		}
	case FMP4, MP4:
		switch id {
		case codec.IDH264, codec.IDHEVC, codec.IDAV1, codec.IDAAC, codec.IDAC3, codec.IDEAC3, codec.IDMP3:
			return true
		default:
			return false
		}
	case MKV:
		switch id {
		case codec.IDH264, codec.IDHEVC, codec.IDAV1, codec.IDMPEG2, codec.IDVP9, codec.IDAAC, codec.IDAC3, codec.IDEAC3, codec.IDMP2, codec.IDMP3:
			return true
		default:
			return false
		}
	default:
		return false
	}
}
