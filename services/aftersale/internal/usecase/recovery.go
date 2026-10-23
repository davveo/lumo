package usecase

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) Tick(ctx context.Context) error {
	ctx, span := platform.Start(ctx, "usecase.aftersale.RecoverPending")
	defer span.End()
	rows, err := s.Repo.Pending(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, row := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c := platform.Command{ID: row.ID, UserID: row.UserID, Role: "system", Data: map[string]any{}}
		switch row.Status {
		case "INSPECTING":
			c.Action = "inspect"
			c.Data["condition"] = row.Condition
		case "REFUND_REQUESTED":
			c.Action = "refund"
		case "EXCHANGE_PREPARING":
			c.Action = "exchange"
		}
		if _, err = s.Handle(ctx, c); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
