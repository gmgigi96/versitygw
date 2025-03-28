package plugins

import "github.com/versity/versitygw/backend"

type BackendPlugin interface {
	New(_ map[string]string) (backend.Backend, error)
}
