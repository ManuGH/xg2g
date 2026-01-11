package v3

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/stretchr/testify/assert"
)

// TestIsNil_TypedNilTrap provides mechanical proof that the isNil helper
// correctly identifies "typed nil" interfaces, which would otherwise
// bypass a simple 'i == nil' check.
func TestIsNil_TypedNilTrap(t *testing.T) {
	// 1. Literal nil
	assert.True(t, isNil(nil), "Literal nil must be identified as nil")

	// 2. Typed nil pointer as interface
	var b bus.Bus = (*bus.MemoryBus)(nil)

	// Traditional check fails:
	// assert.False(t, b == nil)

	// Our robust check passes:
	assert.True(t, isNil(b), "Typed nil pointer ((*MemoryBus)(nil)) must be identified as nil")

	// 3. Typed nil slice
	var s []string = nil
	assert.True(t, isNil(s), "Nil slice must be identified as nil")

	// 4. Non-nil value
	assert.False(t, isNil("hello"), "Non-nil string must NOT be identified as nil")
}

// TestSetDependencies_HandlesTypedNil provides proof that SetDependencies
// uses isNil to safely handle incoming components, preventing the server
// from storing a typed nil interface which would panic on call.
func TestSetDependencies_HandlesTypedNil(t *testing.T) {
	srv := &Server{}

	// Create a typed nil
	var typedNilBus bus.Bus = (*bus.MemoryBus)(nil)

	// Insecure assignment would do: srv.v3Bus = typedNilBus (srv.v3Bus != nil)
	// Secure assignment via SetDependencies:
	srv.SetDependencies(
		typedNilBus, // bus
		nil,         // store
		nil,         // resume
		nil,         // scan
		nil,         // rpm
		nil,         // cm
		nil,         // sm
		nil,         // se
		nil,         // vm
		nil,         // epg
		nil,         // hm
		nil,         // ls
		nil,         // ss
		nil,         // ds
		nil,         // svs
		nil,         // ts
		nil,         // requestShutdown
		nil,         // preflightCheck
	)

	assert.Nil(t, srv.v3Bus, "v3Bus field must be TRULY nil after receiving a typed nil pointer")
}
