package plugins

import "github.com/gmgigi96/versitygw/backend"

type BackendPlugin interface {
	New(_ map[string]string) (backend.Backend, error)
}
