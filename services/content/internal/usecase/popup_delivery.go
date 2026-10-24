package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"strings"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/content/internal/domain"
	"github.com/davveo/lumo/services/content/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type popupFacts struct {
	RegisteredAt       string   `json:"registered_at"`
	Level              string   `json:"level"`
	Growth             *int64   `json:"growth"`
	Points             *int64   `json:"points"`
	InterestCategories []string `json:"interest_categories"`
}
type popupIdentity struct {
	subject, session string
	newGuest         bool
	facts            popupFacts
}

func (s *Service) popupIdentity(ctx context.Context, c platform.Request, visit bool) (popupIdentity, error) {
	var identity popupIdentity
	visitor, session := platform.Text(c.Data, "visitor_id"), platform.Text(c.Data, "session_id")
	if len(visitor) < 16 || len(session) < 16 || !popupIDPattern.MatchString(visitor) ||
		!popupIDPattern.MatchString(session) {
		return identity, bad("valid visitor and session identity required")
	}
	identity.session = stable("popup-session-id", session)
	if c.UserID != "" {
		identity.subject = stable("popup-member", c.UserID)
	} else {
		identity.subject = stable("popup-guest", visitor)
	}
	if raw := c.Data["profile"]; raw != nil {
		b, _ := json.Marshal(raw)
		if len(b) > 32768 || json.Unmarshal(b, &identity.facts) != nil {
			return identity, bad("invalid trusted popup profile")
		}
	}
	if c.UserID == "" && visit {
		v, err := s.repo.PopupVisitor(ctx, stable("popup-visitor", visitor), identity.session)
		if err != nil {
			return identity, err
		}
		identity.newGuest = v.FirstSession == identity.session
	}
	return identity, nil
}
func popupContains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func popupActive(p popupConfig, now time.Time) bool {
	if p.Status != constants.PopupStatusPublished {
		return false
	}
	if p.StartsAt != "" {
		at, err := time.Parse(time.RFC3339, p.StartsAt)
		if err != nil || now.Before(at) {
			return false
		}
	}
	if p.EndsAt != "" {
		at, err := time.Parse(time.RFC3339, p.EndsAt)
		if err != nil || !now.Before(at) {
			return false
		}
	}
	return true
}
func popupMatches(p popupConfig, c platform.Request, id popupIdentity, now time.Time) bool {
	if !popupContains(p.Placements, platform.Text(c.Data, "placement")) {
		return false
	}
	a := p.Audience
	f := id.facts
	if popupContains(a.ExcludeUserIDs, c.UserID) ||
		len(a.IncludeUserIDs) > 0 && !popupContains(a.IncludeUserIDs, c.UserID) {
		return false
	}
	if (a.MinGrowth > 0 || a.MaxGrowth > 0 || a.MinPoints > 0 || a.MaxPoints > 0 || len(a.InterestCategories) > 0) &&
		c.UserID == "" {
		return false
	}
	if (a.MinGrowth > 0 || a.MaxGrowth > 0) &&
		(f.Growth == nil || *f.Growth < a.MinGrowth || a.MaxGrowth > 0 && *f.Growth > a.MaxGrowth) {
		return false
	}
	if (a.MinPoints > 0 || a.MaxPoints > 0) &&
		(f.Points == nil || *f.Points < a.MinPoints || a.MaxPoints > 0 && *f.Points > a.MaxPoints) {
		return false
	}
	if len(a.InterestCategories) > 0 {
		matched := false
		for _, wanted := range a.InterestCategories {
			for _, have := range f.InterestCategories {
				if strings.EqualFold(strings.TrimSpace(wanted), strings.TrimSpace(have)) {
					matched = true
				}
			}
		}
		if !matched {
			return false
		}
	}
	member := c.UserID != ""
	ageKnown, newMember := false, false
	if member {
		at, err := time.Parse(time.RFC3339, f.RegisteredAt)
		if err == nil && !at.After(now) {
			ageKnown = true
			newMember = now.Sub(at) < time.Duration(a.NewMemberDays)*24*time.Hour
		}
	}
	for _, segment := range a.Segments {
		if segment == constants.PopupSegmentAll || segment == constants.PopupSegmentGuest && !member ||
			segment == constants.PopupSegmentNewGuest && !member && id.newGuest ||
			segment == constants.PopupSegmentReturningGuest && !member && !id.newGuest ||
			segment == constants.PopupSegmentMember && member ||
			segment == constants.PopupSegmentNewMember && member && ageKnown && newMember ||
			segment == constants.PopupSegmentReturningMember && member && ageKnown && !newMember ||
			segment == constants.PopupSegmentGoldMember && member && f.Level == constants.MemberLevelGold {
			return true
		}
	}
	return false
}

var popupBeijing = time.FixedZone("Asia/Shanghai", 8*60*60)

func popupDay(t time.Time) string { return t.In(popupBeijing).Format("2006-01-02") }
func popupLimited(f popupFrequency, v, session domain.PopupCounter, now time.Time) bool {
	if v.Impressions >= f.MaxPerUser || v.Day == popupDay(now) && v.DayImpressions >= f.MaxPerDay ||
		f.OncePerSession && session.Impressions > 0 ||
		f.StopAfterClaim && v.Claimed {
		return true
	}
	if v.LastShownAt != nil && now.Sub(*v.LastShownAt) < time.Duration(f.CooldownHours)*time.Hour {
		return true
	}
	return v.LastClosedAt != nil && now.Sub(*v.LastClosedAt) < time.Duration(f.CloseCooldownHours)*time.Hour
}

func popupGlobalLimited(
	settings domain.PopupSettings,
	u domain.PopupSubject,
	session domain.PopupCounter,
	now time.Time,
) bool {
	return !settings.Enabled || session.Impressions >= settings.MaxPerSession ||
		u.LastShownAt != nil && now.Sub(*u.LastShownAt) < time.Duration(settings.MinGapSeconds)*time.Second
}
func popupCounterIDs(campaign string, id popupIdentity) (string, string, string) {
	return stable(
			"popup-campaign-user",
			campaign,
			id.subject,
		), stable(
			"popup-campaign-session",
			campaign,
			id.subject,
			id.session,
		), stable(
			"popup-global-session",
			id.subject,
			id.session,
		)
}
func popupRows(raw any) []platform.Result {
	b, _ := json.Marshal(raw)
	var items []platform.Result
	_ = json.Unmarshal(b, &items)
	return items
}
func (s *Service) PopupDecide(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "content.usecase.popup_decide")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := popupInternal(c); err != nil {
		return nil, err
	}
	empty := platform.Result{"popup": nil}
	if !popupPlacement(platform.Text(c.Data, "placement")) {
		return empty, nil
	}
	id, err := s.popupIdentity(ctx, c, true)
	if err != nil {
		return nil, err
	}
	now := s.popupNow()
	settings, err := s.repo.PopupSettings(ctx)
	if err != nil {
		return nil, err
	}
	u, err := s.repo.PopupSubject(ctx, id.subject)
	if err != nil {
		return nil, err
	}
	_, _, globalID := popupCounterIDs("", id)
	global, err := s.repo.PopupCounter(ctx, globalID)
	if err != nil {
		return nil, err
	}
	if popupGlobalLimited(settings, u, global, now) {
		return empty, nil
	}
	rows, err := s.repo.PopupCampaigns(ctx, true)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		row    domain.PopupCampaign
		config popupConfig
	}
	candidates := []candidate{}
	templateIDs := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		p, e := popupConfigFrom(row)
		if e != nil || !popupActive(p, now) || !popupMatches(p, c, id, now) {
			continue
		}
		counterID, sessionID, _ := popupCounterIDs(row.ID, id)
		counter, e := s.repo.PopupCounter(ctx, counterID)
		if e != nil {
			return nil, e
		}
		session, e := s.repo.PopupCounter(ctx, sessionID)
		if e != nil {
			return nil, e
		}
		if popupLimited(p.Frequency, counter, session, now) {
			continue
		}
		candidates = append(candidates, candidate{row, p})
		tid := p.Content.CouponTemplateID
		if tid != "" && !seen[tid] {
			seen[tid] = true
			templateIDs = append(templateIDs, tid)
		}
	}
	templates := map[string]platform.Result{}
	for offset := 0; offset < len(templateIDs); offset += 50 {
		end := offset + 50
		if end > len(templateIDs) {
			end = len(templateIDs)
		}
		result, e := platform.NewClients(s.rpc).Coupon.TemplatesByIds(ctx, platform.Request{
			UserID: c.UserID,
			Role:   constants.RoleSystem,
			Data:   map[string]any{"ids": templateIDs[offset:end]},
		},
		)
		if e != nil {
			return nil, e
		}
		for _, item := range popupRows(result["items"]) {
			templates[platform.Text(item, "id")] = item
		}
	}
	for _, candidate := range candidates {
		p := candidate.config
		var coupon platform.Result
		if tid := p.Content.CouponTemplateID; tid != "" {
			coupon = templates[tid]
			if coupon == nil {
				continue
			}
			state := platform.Text(coupon, "claim_status")
			if state != constants.CouponClaimStatusAvailable && state != constants.CouponClaimStatusLoginRequired &&
				state != constants.CouponClaimStatusClaimed ||
				state == constants.CouponClaimStatusClaimed && p.Frequency.StopAfterClaim {
				continue
			}
		}
		delivery := domain.PopupDelivery{
			ID:         platform.ID("popup-delivery"),
			CampaignID: candidate.row.ID,
			Version:    candidate.row.Version,
			SubjectID:  id.subject,
			SessionID:  id.session,
			ExpiresAt:  now.Add(5 * time.Minute),
		}
		if err := s.repo.CreatePopupDelivery(ctx, &delivery); err != nil {
			return nil, err
		}
		return platform.Result{
			"popup": platform.Result{
				"delivery_id": delivery.ID,
				"campaign_id": delivery.CampaignID,
				"content":     p.Content,
				"coupon":      coupon,
				"expires_at":  delivery.ExpiresAt.UTC().Format(time.RFC3339),
			},
		}, nil
	}
	return empty, nil
}
func (s *Service) popupNow() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}
func popupDeliveryOwned(d domain.PopupDelivery, id popupIdentity, view string) bool {
	return d.SubjectID == id.subject && d.SessionID == id.session && (d.ViewID == "" || d.ViewID == view)
}
func popupDeliveryError() error {
	return status.Error(codes.FailedPrecondition, "popup delivery unavailable; refresh the page")
}
func (s *Service) PopupEvent(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "content.usecase.popup_event")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := popupInternal(c); err != nil {
		return nil, err
	}
	event := platform.Text(c.Data, "event")
	if event != constants.PopupEventImpression && event != constants.PopupEventClick &&
		event != constants.PopupEventDismiss &&
		event != constants.PopupEventAutoClose {
		return nil, bad("invalid popup event")
	}
	view := platform.Text(c.Data, "view_id")
	deliveryID := platform.Text(c.Data, "delivery_id")
	if !popupIDPattern.MatchString(view) || !popupIDPattern.MatchString(deliveryID) {
		return nil, bad("valid popup delivery and view required")
	}
	id, err := s.popupIdentity(ctx, c, false)
	if err != nil {
		return nil, err
	}
	delivery, err := s.repo.PopupDelivery(ctx, deliveryID)
	if status.Code(err) == codes.NotFound {
		return platform.Result{"allowed": false}, nil
	}
	if err != nil {
		return nil, err
	}
	if !popupDeliveryOwned(delivery, id, view) {
		return platform.Result{"allowed": false}, nil
	}
	now := s.popupNow()
	allowed := false
	err = s.repo.PopupTransaction(
		ctx,
		id.subject,
		delivery.CampaignID,
		delivery.ID,
		func(tx *repository.PopupTx, u *domain.PopupSubject, row *domain.PopupCampaign, d *domain.PopupDelivery) error {
			if !popupDeliveryOwned(*d, id, view) || !now.Before(d.ExpiresAt) {
				return nil
			}
			p, e := popupConfigFrom(*row)
			if e != nil || row.Version != d.Version || !popupActive(p, now) {
				return nil
			}
			if event != constants.PopupEventImpression {
				if d.ShownAt == nil || d.ViewID != view {
					return nil
				}
				switch event {
				case constants.PopupEventClick:
					if d.ClickedAt == nil {
						d.ClickedAt = &now
						row.Clicks++
					}
				case constants.PopupEventDismiss, constants.PopupEventAutoClose:
					if d.ClosedAt == nil {
						d.ClosedAt = &now
						row.Dismissals++
						counterID, _, _ := popupCounterIDs(row.ID, id)
						counter, e := tx.Counter(counterID)
						if e != nil {
							return e
						}
						counter.LastClosedAt = &now
						if e = tx.Save(&counter); e != nil {
							return e
						}
					}
				}
				allowed = true
				if e = tx.Save(row); e != nil {
					return e
				}
				return tx.Save(d)
			}
			if d.ShownAt != nil {
				allowed = d.ViewID == view && d.ClosedAt == nil
				return nil
			}
			settings, e := tx.Settings()
			if e != nil {
				return e
			}
			counterID, sessionID, globalID := popupCounterIDs(row.ID, id)
			counter, e := tx.Counter(counterID)
			if e != nil {
				return e
			}
			session, e := tx.Counter(sessionID)
			if e != nil {
				return e
			}
			global, e := tx.Counter(globalID)
			if e != nil {
				return e
			}
			if popupGlobalLimited(settings, *u, global, now) || popupLimited(p.Frequency, counter, session, now) {
				return nil
			}
			if counter.Day != popupDay(now) {
				counter.Day = popupDay(now)
				counter.DayImpressions = 0
			}
			counter.Impressions++
			counter.DayImpressions++
			counter.LastShownAt = &now
			session.Impressions++
			global.Impressions++
			u.LastShownAt = &now
			d.ViewID = view
			d.ShownAt = &now
			row.Impressions++
			for _, value := range []any{&counter, &session, &global, u, row, d} {
				if e = tx.Save(value); e != nil {
					return e
				}
			}
			allowed = true
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return platform.Result{"allowed": allowed}, nil
}
func (s *Service) PopupClaim(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "content.usecase.popup_claim")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := popupInternal(c); err != nil {
		return nil, err
	}
	if c.UserID == "" {
		return nil, status.Error(codes.Unauthenticated, "login required to claim coupon")
	}
	view, deliveryID := platform.Text(c.Data, "view_id"), platform.Text(c.Data, "delivery_id")
	if !popupIDPattern.MatchString(view) || !popupIDPattern.MatchString(deliveryID) {
		return nil, bad("valid popup delivery and view required")
	}
	id, err := s.popupIdentity(ctx, c, false)
	if err != nil {
		return nil, err
	}
	delivery, err := s.repo.PopupDelivery(ctx, deliveryID)
	if status.Code(err) == codes.NotFound {
		return nil, popupDeliveryError()
	}
	if err != nil {
		return nil, err
	}
	now := s.popupNow()
	row, err := s.repo.PopupCampaign(ctx, delivery.CampaignID)
	if err != nil {
		return nil, popupDeliveryError()
	}
	p, err := popupConfigFrom(row)
	if err != nil || !popupDeliveryOwned(delivery, id, view) || delivery.ViewID != view || delivery.ShownAt == nil ||
		!now.Before(delivery.ExpiresAt) ||
		row.Version != delivery.Version ||
		!popupActive(p, now) ||
		p.Content.ActionType != constants.PopupActionClaimCoupon ||
		p.Content.CouponTemplateID == "" {
		return nil, popupDeliveryError()
	}
	// The coupon service is authoritative for quotas, timing and eligibility. A
	// network retry is safe because its user/template claim is idempotent.
	claimed, err := platform.NewClients(s.rpc).Coupon.Claim(ctx, platform.Request{
		UserID:         c.UserID,
		Role:           constants.RoleSystem,
		ID:             p.Content.CouponTemplateID,
		IdempotencyKey: stable("popup-claim", c.UserID, p.Content.CouponTemplateID),
		Data:           map[string]any{"template_id": p.Content.CouponTemplateID},
	},
	)
	if err != nil {
		return nil, err
	}
	err = s.repo.PopupTransaction(
		ctx,
		id.subject,
		row.ID,
		delivery.ID,
		func(
			tx *repository.PopupTx,
			u *domain.PopupSubject,
			campaign *domain.PopupCampaign,
			d *domain.PopupDelivery,
		) error {
			if !popupDeliveryOwned(*d, id, view) || d.ShownAt == nil {
				return popupDeliveryError()
			}
			if d.ClaimedAt != nil {
				return nil
			}
			d.ClaimedAt = &now
			counterID, _, _ := popupCounterIDs(campaign.ID, id)
			counter, e := tx.Counter(counterID)
			if e != nil {
				return e
			}
			if !counter.Claimed {
				campaign.Claims++
			}
			counter.Claimed = true
			for _, value := range []any{d, campaign, &counter} {
				if e = tx.Save(value); e != nil {
					return e
				}
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return platform.Result{"coupon": claimed, "already_claimed": claimed["already_claimed"] == true}, nil
}
