package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
)

func (s *Service) PolicyGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "logistics.usecase.PolicyGet")
	defer span.End()
	if !privileged(c) {
		return nil, status.Error(codes.PermissionDenied, "administrator or service required")
	}
	p, e := s.Repo.Policy(ctx)
	return platform.Result{"config": p, "version": p.Version}, e
}
func (s *Service) PolicyUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "logistics.usecase.PolicyUpsert")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		Config commercepolicy.Shipping `json:"config"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	p := in.Config
	validMoney := func(v int64) bool { return v >= 0 && v <= 1000000000 }
	validRegion := func(v string) bool { return strings.TrimSpace(v) != "" && len([]rune(v)) <= 100 }
	if strings.TrimSpace(p.Name) == "" || len([]rune(p.Name)) > 120 || (p.Mode != "flat" && p.Mode != "per_item") || !validMoney(p.BaseFee) || !validMoney(p.AdditionalFee) || !validMoney(p.FreeThreshold) || len(p.AllowedRegions) > 100 || len(p.BlockedRegions) > 100 || len(p.Regions) > 40 {
		return nil, status.Error(codes.InvalidArgument, "invalid shipping template")
	}
	for _, v := range append(append([]string{}, p.AllowedRegions...), p.BlockedRegions...) {
		if !validRegion(v) {
			return nil, status.Error(codes.InvalidArgument, "invalid shipping region")
		}
	}
	seen := map[string]bool{}
	for _, r := range p.Regions {
		if !validRegion(r.Region) || seen[r.Region] || !validMoney(r.BaseFee) || !validMoney(r.AdditionalFee) || !validMoney(r.FreeThreshold) {
			return nil, status.Error(codes.InvalidArgument, "invalid or duplicate regional shipping rule")
		}
		seen[r.Region] = true
	}
	return s.Repo.PolicyUpsert(ctx, c, p)
}
