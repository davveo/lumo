package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) AdminList(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "helpdesk.usecase.AdminList")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if st := platform.Text(c.Data, "status"); st != "" && st != constants.HelpdeskStatusOpen && st != constants.HelpdeskStatusReplied && st != constants.HelpdeskStatusClosed {
		return nil, bad("unsupported ticket status")
	}
	rows, total, f, e := s.repo.AdminList(ctx, c)
	if e != nil {
		return nil, e
	}
	items := []platform.Result{}
	for _, r := range rows {
		v := view(r)
		v["updated_at"] = r.UpdatedAt
		items = append(items, v)
	}
	return adminquery.Result(items, total, f), nil
}
