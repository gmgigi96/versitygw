package plugins

import "github.com/versity/versitygw/backend"

// BackendPlugin defines an interface for creating backend
// implementation instances.
type BackendPlugin interface {
	// New creates and initializes a new backend.Backend instance.
	// The configuration is passed to the backend in the map config.
	// The specific keys and values depend on the plugin implementation.
	New(config map[string]any) (backend.Backend, error)
}
