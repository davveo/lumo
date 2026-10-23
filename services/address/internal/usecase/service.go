package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/address/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "address.usecase."+c.Action)
	defer span.End()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	switch c.Action {
	case "list":
		return s.list(ctx, c, c.UserID, "address", "")
	case "get":
		r, e := s.repo.Get(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		if e = own(c, r); e != nil {
			return nil, e
		}
		return view(r), nil
	case "create", "update":
		var r domain.Record
		if c.Action == "update" {
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
		if recipient == "" || len(recipient) > 100 || len(phone) < 5 || len(phone) > 30 || address == "" || len(address) > 1000 {
			return nil, bad("recipient, phone and address required")
		}
		owner := c.UserID
		if c.Action == "update" {
			owner = r.UserID
		}
		v := record(c.ID, owner, "address", "ACTIVE", map[string]any{"recipient": recipient, "phone": phone, "address": address, "province": platform.Text(c.Data, "province"), "city": platform.Text(c.Data, "city"), "district": platform.Text(c.Data, "district")})
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
	case "default":
		if e := s.repo.SetDefault(ctx, c.ID, c.UserID); e != nil {
			return nil, e
		}
		return platform.Result{"id": c.ID, "is_default": true}, nil
	case "delete":
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
