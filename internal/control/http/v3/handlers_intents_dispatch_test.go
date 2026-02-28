package v3

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func TestIntentHandlerForType(t *testing.T) {
	if handler, ok := intentHandlerForType(model.IntentTypeStreamStart); !ok || handler == nil {
		t.Fatalf("expected start intent handler, got ok=%v", ok)
	}

	if handler, ok := intentHandlerForType(model.IntentTypeStreamStop); !ok || handler == nil {
		t.Fatalf("expected stop intent handler, got ok=%v", ok)
	}

	if handler, ok := intentHandlerForType(model.IntentType("unknown.intent")); ok || handler != nil {
		t.Fatalf("expected unknown intent to have no handler, got ok=%v handler_nil=%v", ok, handler == nil)
	}
}
