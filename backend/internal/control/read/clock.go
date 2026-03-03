package read

import "time"

// Clock provides an interface for time-based operations.
type Clock interface {
	Now() time.Time
}

// RealClock implements Clock using time.Now().
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }
