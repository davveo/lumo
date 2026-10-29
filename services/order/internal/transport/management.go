package transport

import (
	"context"
	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"github.com/davveo/lumo/internal/orderwire"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/usecase"
)

func adminRequest(in *mallv1.AdminOrdersRequest) usecase.AdminRequest {
	return usecase.AdminRequest{
		Actor: actor(in.GetActor()),
		Data: map[string]any{
			"page":      in.GetPage(),
			"page_size": in.GetPageSize(),
			"keyword":   in.GetKeyword(),
			"status":    in.GetStatus(),
			"from":      in.GetFrom(),
			"to":        in.GetTo(),
			"days":      in.GetDays(),
		},
	}
}

func (s *orderServer) AdminList(
	ctx context.Context,
	in *mallv1.AdminOrdersRequest,
) (*mallv1.AdminOrdersResponse, error) {
	rows, total, filter, err := s.service.AdminList(ctx, adminRequest(in))
	if err != nil {
		return nil, err
	}
	out := &mallv1.AdminOrdersResponse{
		Total:    total,
		Page:     int32(filter.Page),
		PageSize: int32(filter.PageSize),
		HasMore:  int64(filter.Page*filter.PageSize) < total,
	}
	for _, row := range rows {
		out.Items = append(out.Items, orderMessage(row))
	}
	return out, nil
}

func (s *orderServer) AdminSummary(
	ctx context.Context,
	in *mallv1.AdminOrdersRequest,
) (*mallv1.OrderSummaryResponse, error) {
	result, err := s.service.AdminSummary(ctx, adminRequest(in))
	if err != nil {
		return nil, err
	}
	out := &mallv1.OrderSummaryResponse{}
	if err = platform.Decode(result, out); err != nil {
		return nil, err
	}
	return out, nil
}
func (a legacyAdapter) adminList(ctx context.Context, c platform.Command) (platform.Result, error) {
	rows, total, filter, err := a.service.AdminList(ctx, usecase.AdminRequest{Actor: legacyActor(c), Data: c.Data})
	if err != nil {
		return nil, err
	}
	out := &mallv1.AdminOrdersResponse{
		Total:    total,
		Page:     int32(filter.Page),
		PageSize: int32(filter.PageSize),
		HasMore:  int64(filter.Page*filter.PageSize) < total,
	}
	for _, row := range rows {
		out.Items = append(out.Items, orderMessage(row))
	}
	return orderwire.AdminListResult(out), nil
}
func (a legacyAdapter) adminSummary(ctx context.Context, c platform.Command) (platform.Result, error) {
	return a.service.AdminSummary(ctx, usecase.AdminRequest{Actor: legacyActor(c), Data: c.Data})
}
