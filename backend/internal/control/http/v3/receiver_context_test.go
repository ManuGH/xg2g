package v3

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReceiverContextFromAbout(t *testing.T) {
	about := &openwebif.AboutInfo{}
	about.Info.Brand = "VU+"
	about.Info.Model = "Uno 4K SE"
	about.Info.ImageDistro = "openatv"
	about.Info.FriendlyImageDistro = "OpenATV"
	about.Info.ImageVer = "7.4"
	about.Info.KernelVer = "6.1.0"
	about.Info.EnigmaVer = "2026-03-30"
	about.Info.WebIFVer = "1.5.2"

	ctx := receiverContextFromAbout(about)
	require.NotNil(t, ctx)
	assert.Equal(t, "enigma2", ctx.Platform)
	assert.Equal(t, "VU+", ctx.Brand)
	assert.Equal(t, "Uno 4K SE", ctx.Model)
	assert.Equal(t, "OpenATV", ctx.OSName)
	assert.Equal(t, "7.4", ctx.OSVersion)
	assert.Equal(t, "6.1.0", ctx.KernelVersion)
	assert.Equal(t, "2026-03-30", ctx.EnigmaVersion)
	assert.Equal(t, "1.5.2", ctx.WebInterfaceVersion)
}
