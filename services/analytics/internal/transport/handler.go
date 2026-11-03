package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/analytics/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceAnalytics, map[string]platform.Operation{
		constants.ActionSkuSales:      s.SkuSales,
		constants.ActionCustomerStats: s.CustomerStats,
		constants.ActionTrack:         s.Track, constants.ActionDrilldown: s.Drilldown,
		constants.ActionAdminList:   s.AdminList,
		constants.ActionSyncHistory: s.SyncHistory,
		constants.ActionDashboard:   s.Dashboard,
		constants.ActionRecord:      s.Record,
		constants.ActionList:        s.List,
	}, nil)
}
