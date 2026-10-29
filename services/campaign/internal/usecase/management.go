package usecase

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/campaign/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AdminList includes paused and system-managed rules for the management console.
func (s *Service) AdminList(ctx context.Context, request platform.Request) (result platform.Result, err error) {
	ctx, span := platform.Start(ctx, "campaign.usecase.AdminList")
	defer span.End()
	platform.TraceRequest(ctx, request)
	defer func() { platform.TraceResponse(ctx, result, err) }()
	if err = platform.RequireAdmin(request); err != nil {
		return nil, err
	}
	var input struct {
		Page     int    `json:"page"`
		PageSize int    `json:"page_size"`
		Keyword  string `json:"keyword"`
		Kind     string `json:"kind"`
		Active   *bool  `json:"active"`
	}
	if err = platform.Decode(request.Data, &input); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid activity list filters")
	}
	if input.Page < 1 {
		input.Page = 1
	}
	if input.PageSize < 1 {
		input.PageSize = 10
	}
	if input.Page > 1000000 || input.PageSize > 100 || utf8.RuneCountInString(input.Keyword) > 120 {
		return nil, status.Error(codes.InvalidArgument, "activity list page or keyword exceeds limit")
	}
	return s.Repo.AdminList(ctx, input.Page, input.PageSize, strings.TrimSpace(input.Keyword), input.Kind, input.Active)
}

// Upsert updates configuration only. Held and used counters always remain owned
// by resource reservation and confirmation, never by administrator payloads.
func (s *Service) Upsert(ctx context.Context, request platform.Request) (result platform.Result, err error) {
	return s.upsert(ctx, request, false)
}
func (s *Service) RuleUpsert(ctx context.Context, request platform.Request) (platform.Result, error) {
	return s.upsert(ctx, request, true)
}
func (s *Service) upsert(ctx context.Context, request platform.Request, dedicated bool) (result platform.Result, err error) {
	ctx, span := platform.Start(ctx, "campaign.usecase.Upsert")
	defer span.End()
	platform.TraceRequest(ctx, request)
	defer func() { platform.TraceResponse(ctx, result, err) }()
	if err = platform.RequireAdmin(request); err != nil {
		return nil, err
	}
	var input struct {
		Kind        string `json:"kind"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Threshold   int64  `json:"threshold_cents"`
		Discount    int64  `json:"discount_cents"`
		PayRateBPS  int64  `json:"pay_rate_bps"`
		MinQuantity int64  `json:"min_quantity"`
		Quota       int64  `json:"quota"`
		Active      *bool  `json:"active"`
		Version     int64  `json:"version"`
	}
	if err = platform.Decode(request.Data, &input); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid activity configuration")
	}
	id := request.ID
	if id == "" || strings.TrimSpace(id) != id || utf8.RuneCountInString(id) > 120 || len(id) > 255 ||
		strings.TrimSpace(input.Name) == "" || utf8.RuneCountInString(input.Name) > 120 ||
		utf8.RuneCountInString(input.Description) > 255 || input.Active == nil || input.Version < 0 ||
		input.Quota < 1 || input.Quota > 1000000000 || input.Threshold < 0 || input.Threshold > 1000000000 ||
		input.Discount < 0 || input.Discount > 1000000000 || input.MinQuantity < 1 || input.MinQuantity > 10 {
		return nil, status.Error(
			codes.InvalidArgument,
			"activity name, amounts, quantity, quota, state and version required",
		)
	}
	switch input.Kind {
	case constants.CampaignKindFlash, constants.CampaignKindGroup:
		if !dedicated || input.PayRateBPS != 0 || input.Threshold != 0 || input.MinQuantity != 1 {
			return nil, status.Error(codes.InvalidArgument, "dedicated flash/group quota rules require zero threshold, fixed discount and one item")
		}
	case constants.CampaignKindReduction, constants.CampaignKindDirect:
		if input.Discount < 1 || input.PayRateBPS != 0 {
			return nil, status.Error(
				codes.InvalidArgument,
				"fixed reduction requires a positive discount and zero pay rate",
			)
		}
	case constants.CampaignKindDiscount, constants.CampaignKindQuantityDiscount:
		if input.Discount != 0 || input.PayRateBPS < 1 || input.PayRateBPS >= 10000 {
			return nil, status.Error(
				codes.InvalidArgument,
				"percentage reduction requires pay rate 1..9999 and zero fixed discount",
			)
		}
		if input.Kind == constants.CampaignKindQuantityDiscount && input.MinQuantity < 2 {
			return nil, status.Error(codes.InvalidArgument, "quantity discount requires at least two items")
		}
	default:
		return nil, status.Error(codes.InvalidArgument, "this activity type is managed by its dedicated business flow")
	}
	activity := domain.Activity{ID: id, Kind: input.Kind, Name: strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description), Threshold: input.Threshold, Discount: input.Discount,
		PayRateBPS: input.PayRateBPS, MinQuantity: input.MinQuantity, Quota: input.Quota,
		Active: *input.Active, Version: input.Version}
	reason := strings.TrimSpace(platform.Text(request.Data, "reason"))
	if dedicated && (reason == "" || utf8.RuneCountInString(reason) > 240) {
		return nil, status.Error(codes.InvalidArgument, "activity change reason required")
	}
	return s.Repo.Upsert(ctx, activity, dedicated, request.UserID, reason)
}
