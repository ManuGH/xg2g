package decision

import (
	"math"

	mediacodec "github.com/ManuGH/xg2g/internal/media/codec"
)

func sourceToVideoCapability(src Source) mediacodec.VideoCapability {
	return mediacodec.VideoCapability{
		Codec:        mapVideoCodecID(src.VideoCodec),
		Interlaced:   src.Interlaced,
		MaxRes:       mediacodec.Resolution{Width: src.Width, Height: src.Height},
		MaxFrameRate: frameRateFromFloat(src.FPS),
	}
}

// clientToVideoCapabilityForSource is intentionally source-guided while the
// legacy decision input still models client video support as a flat codec list.
// Once DecisionInput carries typed capabilities directly, this adapter should go
// away entirely.
func clientToVideoCapabilityForSource(caps Capabilities, source Source) mediacodec.VideoCapability {
	sourceCodec := mapVideoCodecID(source.VideoCodec)
	clientCodec := mediacodec.IDUnknown
	for _, rawCodec := range caps.VideoCodecs {
		mapped := mapVideoCodecID(rawCodec)
		if mapped == sourceCodec && mapped != mediacodec.IDUnknown {
			clientCodec = mapped
			break
		}
	}

	var maxRes mediacodec.Resolution
	var maxFrameRate mediacodec.FrameRate
	if caps.MaxVideo != nil {
		maxRes = mediacodec.Resolution{
			Width:  caps.MaxVideo.Width,
			Height: caps.MaxVideo.Height,
		}
		if caps.MaxVideo.FPS > 0 {
			maxFrameRate = mediacodec.FrameRate{
				Numerator:   caps.MaxVideo.FPS,
				Denominator: 1,
			}
		}
	}

	return mediacodec.VideoCapability{
		Codec:        clientCodec,
		MaxRes:       maxRes,
		MaxFrameRate: maxFrameRate,
	}
}

func mapVideoCodecID(raw string) mediacodec.ID {
	switch robustNorm(raw) {
	case "h264", "avc", "avc1", "libx264", "video/avc":
		return mediacodec.IDH264
	case "hevc", "h265", "h.265", "hev1", "hvc1", "libx265", "video/hevc":
		return mediacodec.IDHEVC
	case "av1", "av01", "av1_vaapi", "libsvtav1", "libaom-av1", "video/av01":
		return mediacodec.IDAV1
	case "mpeg2", "mpeg-2", "mpeg2video", "video/mpeg2":
		return mediacodec.IDMPEG2
	case "vp9", "vp09", "libvpx-vp9", "video/x-vnd.on2.vp9":
		return mediacodec.IDVP9
	default:
		return mediacodec.IDUnknown
	}
}

func frameRateFromFloat(fps float64) mediacodec.FrameRate {
	if fps <= 0 {
		return mediacodec.FrameRate{}
	}

	for _, candidate := range []struct {
		value float64
		num   int
		den   int
	}{
		{23.976, 24000, 1001},
		{24, 24, 1},
		{25, 25, 1},
		{29.97, 30000, 1001},
		{30, 30, 1},
		{50, 50, 1},
		{59.94, 60000, 1001},
		{60, 60, 1},
	} {
		if math.Abs(fps-candidate.value) < 0.02 {
			return mediacodec.FrameRate{
				Numerator:   candidate.num,
				Denominator: candidate.den,
			}
		}
	}

	scaled := int(math.Round(fps * 1000))
	if scaled <= 0 {
		return mediacodec.FrameRate{}
	}
	divisor := gcdInt(scaled, 1000)
	return mediacodec.FrameRate{
		Numerator:   scaled / divisor,
		Denominator: 1000 / divisor,
	}
}

func gcdInt(a int, b int) int {
	a = absInt(a)
	b = absInt(b)
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
