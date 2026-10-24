package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/address/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "address.usecase."+c.Action)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	switch c.Action {
	case constants.ActionList:
		return s.list(ctx, c, c.UserID, constants.AddressKindAddress, "")
	case constants.ActionGet:
		r, e := s.repo.Get(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		if e = own(c, r); e != nil {
			return nil, e
		}
		return view(r), nil
	case constants.ActionCreate, constants.ActionUpdate:
		var r domain.Record
		if c.Action == constants.ActionUpdate {
			var e error
			r, e = s.repo.Get(ctx, c.ID)
			if e != nil {
				return nil, e
			}
			if e = own(c, r); e != nil {
				return nil, e
			}
		} else {
			c.ID = platform.ID("addr")
		}
		recipient := platform.Text(c.Data, "recipient")
		phone := platform.Text(c.Data, "phone")
		address := platform.Text(c.Data, "address")
		if recipient == "" || len(recipient) > 100 || len(phone) < 5 || len(phone) > 30 || address == "" ||
			len(address) > 1000 {
			return nil, bad("recipient, phone and address required")
		}
		owner := c.UserID
		if c.Action == constants.ActionUpdate {
			owner = r.UserID
		}
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
	case constants.ActionDefault:
		if e := s.repo.SetDefault(ctx, c.ID, c.UserID); e != nil {
			return nil, e
		}
		return platform.Result{"id": c.ID, "is_default": true}, nil
	case constants.ActionDelete:
		if e := s.repo.Delete(ctx, c.ID, c.UserID); e != nil {
			return nil, e
		}
		return platform.Result{"deleted": true}, nil
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported address action")
	}
}
func (s *Service) seed(context.Context) error                  { return nil }
func (s *Service) event(context.Context, platform.Event) error { return nil }
