package media

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/media/internal/domain"
	"github.com/davveo/lumo/services/media/internal/repository"
	"github.com/davveo/lumo/services/media/internal/storage"
	"github.com/davveo/lumo/services/media/internal/transport"
	"github.com/davveo/lumo/services/media/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	store, e := storage.FromEnv()
	return compose(d, store, e)
}

// NewWithStore enables adapter contract tests with a controlled object store.
func NewWithStore(d platform.Dependencies, store domain.ObjectStore) platform.Module {
	return compose(d, store, nil)
}
func compose(d platform.Dependencies, store domain.ObjectStore, err error) platform.Module {
	s := usecase.New(repository.New(d.DB), d.RPC)
	s.ConfigureStore(store, err)
	t := transport.New(s)
	return platform.Module{Name: constants.ServiceMedia, Migrate: s.Migrate, Seed: s.Seed, Handle: t.Handle}
}
