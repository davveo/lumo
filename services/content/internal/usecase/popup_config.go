package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/content/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type popupAudience struct {
	Segments           []string `json:"segments"`
	NewMemberDays      int      `json:"new_member_days"`
	MinGrowth          int64    `json:"min_growth"`
	MaxGrowth          int64    `json:"max_growth"`
	MinPoints          int64    `json:"min_points"`
	MaxPoints          int64    `json:"max_points"`
	IncludeUserIDs     []string `json:"include_user_ids"`
	ExcludeUserIDs     []string `json:"exclude_user_ids"`
	InterestCategories []string `json:"interest_categories"`
}
type popupFrequency struct {
	MaxPerUser         int  `json:"max_per_user"`
	MaxPerDay          int  `json:"max_per_day"`
	CooldownHours      int  `json:"cooldown_hours"`
	OncePerSession     bool `json:"once_per_session"`
	CloseCooldownHours int  `json:"close_cooldown_hours"`
	StopAfterClaim     bool `json:"stop_after_claim"`
}
type popupContent struct {
	Title            string `json:"title"`
	Subtitle         string `json:"subtitle"`
	Body             string `json:"body"`
	Theme            string `json:"theme"`
	CouponTemplateID string `json:"coupon_template_id"`
	ButtonText       string `json:"button_text"`
	ActionType       string `json:"action_type"`
	ActionTarget     string `json:"action_target"`
	AutoCloseSeconds int    `json:"auto_close_seconds"`
	DelaySeconds     int    `json:"delay_seconds"`
}
type popupConfig struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Status     string           `json:"status"`
	Priority   int              `json:"priority"`
	StartsAt   string           `json:"starts_at"`
	EndsAt     string           `json:"ends_at"`
	Placements []string         `json:"placements"`
	Audience   popupAudience    `json:"audience"`
	Frequency  popupFrequency   `json:"frequency"`
	Content    popupContent     `json:"content"`
	Version    int64            `json:"version,omitempty"`
	CreatedAt  string           `json:"created_at,omitempty"`
	UpdatedAt  string           `json:"updated_at,omitempty"`
	Stats      map[string]int64 `json:"stats,omitempty"`
}

var popupIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

var popupTargetPattern = regexp.MustCompile(
	`^#/(?:coupons|member/coupons|promotions(?:/(?:flash|group|exchange|reduction|discount|quantity_discount|direct)(?:/[A-Za-z0-9_-]+)?)?|product/(?:[A-Za-z0-9_.:-]|%[A-Fa-f0-9]{2}){1,180})?$`,
)

func popupDecode(v any, out any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return bad("invalid popup configuration")
	}
	if len(b) > 65536 {
		return bad("popup configuration exceeds 64 KiB")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(out); err != nil {
		return bad("invalid popup configuration: " + err.Error())
	}
	if err = d.Decode(&struct{}{}); err != io.EOF {
		return bad("invalid popup configuration")
	}
	return nil
}
func popupStrings(v []string, limit, width int, required bool) ([]string, error) {
	if len(v) > limit || required && len(v) == 0 {
		return nil, bad("invalid popup condition list size")
	}
	result := make([]string, 0, len(v))
	seen := map[string]bool{}
	for _, s := range v {
		s = strings.TrimSpace(s)
		if s == "" || utf8.RuneCountInString(s) > width ||
			strings.IndexFunc(s, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
			return nil, bad("invalid popup condition text")
		}
		if !seen[s] {
			result = append(result, s)
			seen[s] = true
		}
	}
	return result, nil
}
func popupText(s string, max int, required bool) (string, error) {
	s = strings.TrimSpace(s)
	if required && s == "" || utf8.RuneCountInString(s) > max || strings.ContainsRune(s, '\x00') {
		return "", bad("invalid popup text length")
	}
	return s, nil
}
func validatePopup(p *popupConfig) error {
	var err error
	if p.ID != "" && !popupIDPattern.MatchString(p.ID) {
		return bad("invalid popup ID")
	}
	if p.Name, err = popupText(p.Name, 120, true); err != nil {
		return err
	}
	if p.Status != constants.PopupStatusDraft && p.Status != constants.PopupStatusPublished &&
		p.Status != constants.PopupStatusPaused {
		return bad("invalid popup status")
	}
	if p.Priority < 0 || p.Priority > 1000 {
		return bad("popup priority must be 0..1000")
	}
	var start, end time.Time
	if p.StartsAt != "" {
		start, err = time.Parse(time.RFC3339, p.StartsAt)
		if err != nil {
			return bad("invalid popup start time")
		}
		p.StartsAt = start.UTC().Format(time.RFC3339)
	}
	if p.EndsAt != "" {
		end, err = time.Parse(time.RFC3339, p.EndsAt)
		if err != nil {
			return bad("invalid popup end time")
		}
		p.EndsAt = end.UTC().Format(time.RFC3339)
	}
	if !start.IsZero() && !end.IsZero() && !start.Before(end) {
		return bad("popup start must precede end")
	}
	if p.Placements, err = popupStrings(p.Placements, 4, 20, true); err != nil {
		return err
	}
	for _, v := range p.Placements {
		if !popupPlacement(v) {
			return bad("invalid popup placement")
		}
	}
	a := &p.Audience
	if a.Segments, err = popupStrings(a.Segments, 8, 32, true); err != nil {
		return err
	}
	allowed := map[string]bool{
		constants.PopupSegmentAll:             true,
		constants.PopupSegmentGuest:           true,
		constants.PopupSegmentNewGuest:        true,
		constants.PopupSegmentReturningGuest:  true,
		constants.PopupSegmentMember:          true,
		constants.PopupSegmentNewMember:       true,
		constants.PopupSegmentReturningMember: true,
		constants.PopupSegmentGoldMember:      true,
	}
	for _, v := range a.Segments {
		if !allowed[v] {
			return bad("invalid popup audience segment")
		}
	}
	if a.NewMemberDays == 0 {
		a.NewMemberDays = 7
	}
	if a.NewMemberDays < 1 || a.NewMemberDays > 365 {
		return bad("new member period must be 1..365 days")
	}
	if a.MinGrowth < 0 || a.MaxGrowth < 0 || a.MinPoints < 0 || a.MaxPoints < 0 ||
		a.MaxGrowth > 0 && a.MaxGrowth < a.MinGrowth ||
		a.MaxPoints > 0 && a.MaxPoints < a.MinPoints {
		return bad("invalid popup membership range")
	}
	if a.IncludeUserIDs, err = popupStrings(a.IncludeUserIDs, 500, 128, false); err != nil {
		return err
	}
	if a.ExcludeUserIDs, err = popupStrings(a.ExcludeUserIDs, 500, 128, false); err != nil {
		return err
	}
	if a.InterestCategories, err = popupStrings(a.InterestCategories, 40, 80, false); err != nil {
		return err
	}
	f := p.Frequency
	if f.MaxPerUser < 1 || f.MaxPerUser > 100 || f.MaxPerDay < 1 || f.MaxPerDay > 10 || f.CooldownHours < 0 ||
		f.CooldownHours > 8760 ||
		f.CloseCooldownHours < 0 ||
		f.CloseCooldownHours > 8760 {
		return bad("invalid popup frequency limits")
	}
	c := &p.Content
	if c.Title, err = popupText(c.Title, 80, true); err != nil {
		return err
	}
	if c.Subtitle, err = popupText(c.Subtitle, 160, false); err != nil {
		return err
	}
	if c.Body, err = popupText(c.Body, 500, false); err != nil {
		return err
	}
	if c.ButtonText, err = popupText(c.ButtonText, 24, true); err != nil {
		return err
	}
	if c.Theme != constants.PopupThemeRed && c.Theme != constants.PopupThemePeach &&
		c.Theme != constants.PopupThemePlum {
		return bad("invalid popup theme")
	}
	if c.CouponTemplateID != "" && !popupIDPattern.MatchString(c.CouponTemplateID) {
		return bad("invalid coupon template ID")
	}
	if c.ActionType != constants.PopupActionClaimCoupon && c.ActionType != constants.PopupActionCouponCenter &&
		c.ActionType != constants.PopupActionLink {
		return bad("invalid popup action")
	}
	if c.ActionType == constants.PopupActionClaimCoupon && c.CouponTemplateID == "" {
		return bad("coupon popup requires a template")
	}
	if c.ActionTarget == "" {
		c.ActionTarget = "#/coupons"
	}
	if !popupTargetPattern.MatchString(c.ActionTarget) {
		return bad("popup target must be an allowed internal shopping route")
	}
	if c.AutoCloseSeconds != 0 && (c.AutoCloseSeconds < 5 || c.AutoCloseSeconds > 60) || c.DelaySeconds < 0 ||
		c.DelaySeconds > 30 {
		return bad("invalid popup timing")
	}
	return nil
}
func popupPlacement(s string) bool {
	return s == constants.PopupPlacementHome || s == constants.PopupPlacementCategory ||
		s == constants.PopupPlacementProduct ||
		s == constants.PopupPlacementCoupons
}
func popupConfigFrom(row domain.PopupCampaign) (popupConfig, error) {
	var p popupConfig
	err := json.Unmarshal([]byte(row.Payload), &p)
	return p, err
}
func popupCampaignView(row domain.PopupCampaign) platform.Result {
	p, _ := popupConfigFrom(row)
	p.ID = row.ID
	p.Name = row.Name
	p.Status = row.Status
	p.Priority = row.Priority
	p.Version = row.Version
	p.CreatedAt = row.CreatedAt.UTC().Format(time.RFC3339)
	p.UpdatedAt = row.UpdatedAt.UTC().Format(time.RFC3339)
	p.Stats = map[string]int64{
		"impressions": row.Impressions,
		"clicks":      row.Clicks,
		"dismissals":  row.Dismissals,
		"claims":      row.Claims,
	}
	b, _ := json.Marshal(p)
	var out platform.Result
	_ = json.Unmarshal(b, &out)
	return out
}
func (s *Service) PopupList(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "content.usecase."+constants.ActionPopupList)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if err := platform.RequireAdmin(c); err != nil {
		return nil, err
	}
	settings, err := s.repo.PopupSettings(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.PopupCampaigns(ctx, false)
	if err != nil {
		return nil, err
	}
	items := make([]platform.Result, 0, len(rows))
	for _, row := range rows {
		items = append(items, popupCampaignView(row))
	}
	return platform.Result{"items": items, "settings": settings}, nil
}
func (s *Service) PopupUpsert(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "content.usecase."+constants.ActionPopupUpsert)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if err := platform.RequireAdmin(c); err != nil {
		return nil, err
	}
	var p popupConfig
	if err := popupDecode(c.Data, &p); err != nil {
		return nil, err
	}
	if c.ID != "" {
		if p.ID != "" && p.ID != c.ID {
			return nil, bad("popup ID mismatch")
		}
		p.ID = c.ID
	}
	if err := validatePopup(&p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		p.ID = platform.ID("popup")
	}
	version := p.Version
	p.Version = 0
	p.Stats = nil
	p.CreatedAt = ""
	p.UpdatedAt = ""
	b, _ := json.Marshal(p)
	row := domain.PopupCampaign{
		ID:       p.ID,
		Name:     p.Name,
		Status:   p.Status,
		Priority: p.Priority,
		Payload:  string(b),
		Version:  version,
	}
	if err := s.repo.SavePopupCampaign(ctx, &row, false); err != nil {
		return nil, err
	}
	return popupCampaignView(row), nil
}
func (s *Service) PopupConfigure(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "content.usecase."+constants.ActionPopupConfigure)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if err := platform.RequireAdmin(c); err != nil {
		return nil, err
	}
	var v domain.PopupSettings
	if err := popupDecode(c.Data, &v); err != nil {
		return nil, err
	}
	if v.MaxPerSession < 1 || v.MaxPerSession > 10 || v.MinGapSeconds < 0 || v.MinGapSeconds > 86400 {
		return nil, bad("invalid global popup limits")
	}
	if _, err := s.repo.PopupSettings(ctx); err != nil {
		return nil, err
	}
	if err := s.repo.SavePopupSettings(ctx, v); err != nil {
		return nil, err
	}
	return platform.Result{
		"enabled":         v.Enabled,
		"max_per_session": v.MaxPerSession,
		"min_gap_seconds": v.MinGapSeconds,
	}, nil
}
func popupInternal(c platform.Request) error {
	if c.Role != constants.RoleSystem {
		return status.Error(codes.PermissionDenied, "popup delivery requires trusted gateway")
	}
	return nil
}
