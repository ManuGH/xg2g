package watchdog

import (
	"testing"
	"time"
)

// L17: ParseLine can be driven by the stderr scanner before Run() has set w.cancel.
// "progress=end" must not call a nil cancel func (which panicked).
func TestParseLineProgressEndBeforeRunDoesNotPanic(t *testing.T) {
	w := New(time.Second, time.Second)
	// No Run() yet → w.cancel is nil. Must not panic.
	w.ParseLine("progress=end")
}
