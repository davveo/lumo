package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
)

// Calculate uses the same authoritative calculation and immutable quote as
// checkout. It neither claims a coupon nor reserves inventory or points.
func (s *Service) Calculate(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "coupon.usecase.Calculate")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	data := map[string]any{}
	for key, value := range c.Data {
		data[key] = value
	}
	if platform.Text(data, "coupon_mode") == "" {
		data["coupon_mode"] = "auto"
	}
	return platform.NewClients(s.RPC).Pricing.Quote(ctx, platform.Request{UserID: c.UserID, Role: c.Role, Data: data})
}
