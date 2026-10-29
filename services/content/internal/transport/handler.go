package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/content/internal/usecase"
)

// New binds transport operations to independent business methods.
func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceContent, map[string]platform.Operation{
		constants.ActionAdminList: s.AdminList, constants.ActionAdminUpsert: s.AdminUpsert,
		constants.ActionList:           s.List,
		constants.ActionGet:            s.Get,
		constants.ActionUpsert:         s.Upsert,
		constants.ActionPopupList:      s.PopupList,
		constants.ActionPopupUpsert:    s.PopupUpsert,
		constants.ActionPopupConfigure: s.PopupConfigure,
		constants.ActionPopupDecide:    s.PopupDecide,
		constants.ActionPopupEvent:     s.PopupEvent,
		constants.ActionPopupClaim:     s.PopupClaim,
	}, nil)
}
