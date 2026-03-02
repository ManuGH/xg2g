package violation

import "github.com/ManuGH/xg2g/internal/domain/session/model"

func Violate() {
	// Violation 1: String Literal
	if "context canceled" == "foo" {
	}

	// Violation 2: Constant Usage
	var c = model.DContextCanceled
	_ = c

	// Violation 3: Contains check (implied by literal)
	// strings.Contains(s, "deadline exceeded")
	_ = "deadline exceeded"
}
