package ports

// Platform provides OS and filesystem operations abstracted from the domain.
// This prevents direct coupling to os/filepath packages, enabling testing
// and future platform-specific implementations (e.g., K8s, remote workers).
type Platform interface {
	// Identity returns a stable worker identity string (replacing os.Hostname/Getpid/uuid composition).
	// Format is typically "hostname-pid-uuid" but implementation-defined.
	Identity() (string, error)

	// RemoveAll removes path and any children (like os.RemoveAll).
	// Path should be absolute; implementations should enforce safety checks.
	RemoveAll(path string) error

	// Join joins path elements (like filepath.Join).
	Join(elem ...string) string
}
