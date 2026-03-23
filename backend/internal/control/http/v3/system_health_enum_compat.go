package v3

// Compatibility aliases keep pre-regeneration references buildable until all
// call sites switch to the shorter generated enum names.
const (
	SystemHealthStatusDegraded = Degraded
	SystemHealthStatusError    = Error
	SystemHealthStatusOk       = Ok
)
