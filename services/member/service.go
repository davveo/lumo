package member

import (
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/member/internal/repository"
	"github.com/davveo/lumo/services/member/internal/transport"
	"github.com/davveo/lumo/services/member/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	s := usecase.New(repository.New(d.DB), d.RPC)
	t := transport.New(s)
	return platform.Module{Name: "member", Migrate: s.Migrate, Seed: s.Seed, Handle: t.Handle, Topics: []string{"OrderCompleted"}, OnEvent: s.OnEvent}
}
