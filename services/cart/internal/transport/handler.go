package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/cart/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceCart, map[string]platform.Operation{
		constants.ActionList:   s.List,
		constants.ActionAdd:    s.Add,
		constants.ActionSet:    s.Set,
		constants.ActionRemove: s.Remove,
		constants.ActionClear:  s.Clear,
		constants.ActionMerge:  s.Merge,
	}, nil)
}
