package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "address.usecase.list")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	return s.list(ctx, c, c.UserID, constants.AddressKindAddress, "")
}
func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "address.usecase.get")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	r, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if e = own(c, r); e != nil {
		return nil, e
	}
	return view(r), nil
}
func (s *Service) Create(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "address.usecase.create")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	c.ID = platform.ID("addr")
	recipient := platform.Text(c.Data, "recipient")
	phone := platform.Text(c.Data, "phone")
	address := platform.Text(c.Data, "address")
	if recipient == "" || len(recipient) > 100 || len(phone) < 5 || len(phone) > 30 || address == "" ||
		len(address) > 1000 {
		return nil, bad("recipient, phone and address required")
	}
	owner := c.UserID

	v := record(
		c.ID,
		owner,
		constants.AddressKindAddress,
		constants.AddressStatusActive,
		map[string]any{
			"recipient": recipient,
			"phone":     phone,
			"address":   address,
			"province":  platform.Text(c.Data, "province"),
			"city":      platform.Text(c.Data, "city"),
			"district":  platform.Text(c.Data, "district"),
		},
	)
	if e := s.repo.Save(ctx, &v, nil); e != nil {
		return nil, e
	}
	if isDefault, _ := c.Data["is_default"].(bool); isDefault {
		if e := s.repo.SetDefault(ctx, v.ID, owner); e != nil {
			return nil, e
		}
		v.IsDefault = true
	}
	return view(v), nil
}
func (s *Service) Update(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "address.usecase.update")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	r, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if e = own(c, r); e != nil {
		return nil, e
	}
	recipient := platform.Text(c.Data, "recipient")
	phone := platform.Text(c.Data, "phone")
	address := platform.Text(c.Data, "address")
	if recipient == "" || len(recipient) > 100 || len(phone) < 5 || len(phone) > 30 || address == "" ||
		len(address) > 1000 {
		return nil, bad("recipient, phone and address required")
	}
	owner := r.UserID
	v := record(
		c.ID,
		owner,
		constants.AddressKindAddress,
		constants.AddressStatusActive,
		map[string]any{
			"recipient": recipient,
			"phone":     phone,
			"address":   address,
			"province":  platform.Text(c.Data, "province"),
			"city":      platform.Text(c.Data, "city"),
			"district":  platform.Text(c.Data, "district"),
		},
	)
	v.IsDefault = r.IsDefault
	if e := s.repo.Save(ctx, &v, nil); e != nil {
		return nil, e
	}
	if isDefault, _ := c.Data["is_default"].(bool); isDefault {
		if e := s.repo.SetDefault(ctx, v.ID, owner); e != nil {
			return nil, e
		}
		v.IsDefault = true
	}
	return view(v), nil
}
func (s *Service) Default(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "address.usecase.default")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	if e := s.repo.SetDefault(ctx, c.ID, c.UserID); e != nil {
		return nil, e
	}
	return platform.Result{"id": c.ID, "is_default": true}, nil
}
func (s *Service) Delete(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "address.usecase.delete")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	if e := s.repo.Delete(ctx, c.ID, c.UserID); e != nil {
		return nil, e
	}
	return platform.Result{"deleted": true}, nil
}

func (s *Service) seed(context.Context) error                  { return nil }
func (s *Service) event(context.Context, platform.Event) error { return nil }
