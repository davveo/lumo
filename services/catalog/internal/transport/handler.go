package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/usecase"
)

// New binds transport operations to independent business methods.
func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceCatalog, map[string]platform.Operation{
		constants.ActionAdminList:                s.AdminList,
		constants.ActionSearchProducts:           s.SearchProducts,
		constants.ActionRecommendationCandidates: s.RecommendationCandidates,
		constants.ActionBatchGet:                 s.BatchGet,
		constants.ActionList:                     s.List,
		constants.ActionGet:                      s.Get,
		constants.ActionUpsert:                   s.Upsert,
	}, nil)
}
