package enigma2

import "errors"

var (
	ErrReadyTimeout        = errors.New("enigma2: ready timeout")
	ErrNotLocked           = errors.New("enigma2: not locked")
	ErrWrongServiceRef     = errors.New("enigma2: wrong service reference")
	ErrUpstreamUnavailable = errors.New("enigma2: upstream unavailable")
)
