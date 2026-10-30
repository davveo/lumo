package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) AdminList(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "catalog.usecase.AdminList")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	rows, total, f, e := s.repo.AdminList(ctx, c)
	if e != nil {
		return nil, e
	}
	items := []platform.Result{}
	for _, row := range rows {
		items = append(items, view(row))
	}
	return adminquery.Result(items, total, f), nil
}
