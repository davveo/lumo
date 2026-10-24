package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/content/internal/domain"
	"github.com/davveo/lumo/services/content/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type popupRPC struct {
	mu        sync.Mutex
	states    map[string]string
	batches   [][]string
	claims    map[string]int
	failClaim bool
}

func (f *popupRPC) Call(ctx context.Context, target string, c platform.Command) (platform.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if target != "coupon" {
		return nil, fmt.Errorf("unexpected peer %s", target)
	}
	switch c.Action {
	case "templates_by_ids":
		b, _ := json.Marshal(c.Data["ids"])
		var ids []string
		_ = json.Unmarshal(b, &ids)
		f.batches = append(f.batches, ids)
		items := []platform.Result{}
		if c.Role != "system" || len(ids) > 50 {
			return nil, fmt.Errorf("invalid internal batch")
		}
		for _, id := range ids {
			state := f.states[id]
			if state == "missing" {
				continue
			}
			if state == "" {
				state = "available"
			}
			items = append(items, platform.Result{"id": id, "name": "优惠券 " + id, "amount_cents": 1000, "min_cents": 10000, "claim_status": state, "claimable": state == "available", "expires_at": "2030-01-01T00:00:00Z"})
		}
		return platform.Result{"items": items}, nil
	case "claim":
		if f.failClaim {
			return nil, status.Error(codes.Unavailable, "coupon peer unavailable")
		}
		if c.Role != "system" || c.UserID == "" || c.ID != platform.Text(c.Data, "template_id") {
			return nil, fmt.Errorf("invalid authoritative claim")
		}
		key := c.UserID + ":" + c.ID
		before := f.claims[key]
		f.claims[key]++
		return platform.Result{"id": "coupon-" + c.ID, "template_id": c.ID, "already_claimed": before > 0}, nil
	}
	return nil, fmt.Errorf("unexpected coupon action %s", c.Action)
}
func popupFixture(t *testing.T) (*Service, *gorm.DB, *popupRPC, *time.Time) {
	t.Helper()
	db, err := platform.OpenDB(context.Background(), "sqlite", filepath.Join(t.TempDir(), "popups.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	rpc := &popupRPC{states: map[string]string{}, claims: map[string]int{}}
	s := New(repository.New(db), rpc)
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	if err = s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, db, rpc, &now
}
func popupMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}
func popupTestConfig(id string) popupConfig {
	return popupConfig{ID: id, Name: "测试弹窗", Status: "PUBLISHED", Priority: 100, Placements: []string{"home"}, Audience: popupAudience{Segments: []string{"all"}, NewMemberDays: 7}, Frequency: popupFrequency{MaxPerUser: 10, MaxPerDay: 10}, Content: popupContent{Title: "生活好物，见面有礼", Subtitle: "按人群送上专属礼券", Body: "以实际券面为准", Theme: "red", CouponTemplateID: "everyday-10", ButtonText: "立即领取", ActionType: "claim_coupon", ActionTarget: "#/coupons"}}
}
func popupAdmin(c popupConfig) platform.Command {
	return platform.Command{Action: "popup_upsert", UserID: "admin", Role: "admin", Data: popupMap(c)}
}
func popupSave(t *testing.T, s *Service, p popupConfig) platform.Result {
	t.Helper()
	result, err := s.Execute(context.Background(), popupAdmin(p))
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func popupCommand(user, session string) platform.Command {
	return platform.Command{Action: "popup_decide", UserID: user, Role: "system", Data: map[string]any{"visitor_id": "visitor-0000000000000001", "session_id": "session-0000000000000" + session, "placement": "home", "profile": map[string]any{"registered_at": "2026-10-23T12:00:00Z", "level": "member", "growth": 100, "points": 500, "interest_categories": []string{"手机数码"}}}}
}
func popupDecision(t *testing.T, s *Service, c platform.Command) platform.Result {
	t.Helper()
	r, err := s.Execute(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if r["popup"] == nil {
		return nil
	}
	return popupMap(r["popup"])
}
func popupEventCommand(c platform.Command, p platform.Result, event, view string) platform.Command {
	c.Action = "popup_event"
	c.Data = popupMap(c.Data)
	c.Data["delivery_id"] = p["delivery_id"]
	c.Data["view_id"] = view
	c.Data["event"] = event
	delete(c.Data, "profile")
	return c
}
func popupEventOK(t *testing.T, s *Service, c platform.Command, p platform.Result, event, view string) bool {
	t.Helper()
	r, err := s.Execute(context.Background(), popupEventCommand(c, p, event, view))
	if err != nil {
		t.Fatal(err)
	}
	return r["allowed"] == true
}
func popupSetGlobal(t *testing.T, s *Service, enabled bool, max, gap int) {
	t.Helper()
	_, err := s.Execute(context.Background(), platform.Command{Action: "popup_configure", UserID: "admin", Role: "admin", Data: map[string]any{"enabled": enabled, "max_per_session": max, "min_gap_seconds": gap}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPopupPrivateConfigurationOptimisticUpdatesAndLifecycleInvalidation(t *testing.T) {
	s, db, _, _ := popupFixture(t)
	p := popupTestConfig("private-popup")
	p.Audience.IncludeUserIDs = []string{"buyer"}
	saved := popupSave(t, s, p)
	for _, action := range []string{"popup_list", "popup_upsert", "popup_configure"} {
		_, err := s.Execute(context.Background(), platform.Command{Action: action, UserID: "buyer", Role: "user", Data: popupMap(p)})
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("%s allowed shopper configuration: %v", action, err)
		}
	}
	for _, action := range []string{"popup_decide", "popup_event", "popup_claim"} {
		_, err := s.Execute(context.Background(), platform.Command{Action: action, UserID: "buyer", Role: "admin"})
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("%s skipped internal boundary: %v", action, err)
		}
	}
	list, err := s.Execute(context.Background(), platform.Command{Action: "list", Data: map[string]any{"kind": "popup"}})
	if err != nil || len(list["items"].([]platform.Result)) != 0 {
		t.Fatalf("private config leaked through public list: %v %v", list, err)
	}
	if _, err = s.Execute(context.Background(), platform.Command{Action: "get", ID: p.ID}); status.Code(err) != codes.NotFound {
		t.Fatalf("private config leaked public get: %v", err)
	}
	c := popupCommand("buyer", "one")
	delivery := popupDecision(t, s, c)
	raw, _ := json.Marshal(delivery)
	for _, secret := range []string{"include_user_ids", "buyer", "audience", "frequency"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("delivery leaked %s: %s", secret, raw)
		}
	}
	stale := saved
	p.Status = "PAUSED"
	p.Version = platform.Int(saved, "version")
	updated := popupSave(t, s, p)
	if platform.Int(updated, "version") != 2 || updated["created_at"] != saved["created_at"] {
		t.Fatal("version/creation metadata lost")
	}
	if popupEventOK(t, s, c, delivery, "impression", "view-1") {
		t.Fatal("paused stale delivery shown")
	}
	_, err = s.Execute(context.Background(), platform.Command{Action: "popup_upsert", UserID: "admin", Role: "admin", Data: stale})
	if status.Code(err) != codes.Aborted {
		t.Fatalf("stale admin overwrite: %v", err)
	}
	p.Status = "PUBLISHED"
	p.Version = 2
	popupSave(t, s, p)
	delivery = popupDecision(t, s, c)
	popupSetGlobal(t, s, false, 1, 60)
	if popupEventOK(t, s, c, delivery, "impression", "view-2") {
		t.Fatal("disabled global settings ignored")
	}
	var count int64
	_ = db.Model(&domain.PopupCampaign{}).Count(&count).Error
	if count != 1 {
		t.Fatalf("expected one campaign, got %d", count)
	}
	restarted := New(repository.New(db), s.rpc)
	if err = restarted.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	settings, err := restarted.repo.PopupSettings(context.Background())
	if err != nil || settings.Enabled {
		t.Fatal("restart reset administrator settings")
	}
}

func TestPopupRejectsInvalidCampaignsBeforeWrite(t *testing.T) {
	s, db, _, _ := popupFixture(t)
	cases := map[string]func(*popupConfig){"unknown status": func(p *popupConfig) { p.Status = "ENABLED" }, "unknown segment": func(p *popupConfig) { p.Audience.Segments = []string{"purchaser"} }, "missing segment": func(p *popupConfig) { p.Audience.Segments = nil }, "payment placement": func(p *popupConfig) { p.Placements = []string{"payment"} }, "external target": func(p *popupConfig) { p.Content.ActionTarget = "https://attacker.invalid" }, "script target": func(p *popupConfig) { p.Content.ActionTarget = "javascript:alert(1)" }, "account target": func(p *popupConfig) { p.Content.ActionTarget = "#/member" }, "negative priority": func(p *popupConfig) { p.Priority = -1 }, "no template": func(p *popupConfig) { p.Content.CouponTemplateID = "" }, "invalid timing": func(p *popupConfig) { p.Content.AutoCloseSeconds = 1 }, "negative cap": func(p *popupConfig) { p.Frequency.MaxPerDay = -1 }, "reversed time": func(p *popupConfig) { p.StartsAt = "2026-10-25T00:00:00Z"; p.EndsAt = "2026-10-24T00:00:00Z" }, "reversed range": func(p *popupConfig) { p.Audience.MinGrowth = 50; p.Audience.MaxGrowth = 10 }, "oversized text": func(p *popupConfig) { p.Content.Title = strings.Repeat("字", 81) }, "oversized allowlist": func(p *popupConfig) { p.Audience.IncludeUserIDs = make([]string, 501) }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := popupTestConfig("invalid")
			mutate(&p)
			_, err := s.Execute(context.Background(), popupAdmin(p))
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("got %v", err)
			}
		})
	}
	c := popupAdmin(popupTestConfig("invalid"))
	c.Data["unknown"] = true
	if _, err := s.Execute(context.Background(), c); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unknown config key accepted: %v", err)
	}
	var count int64
	_ = db.Model(&domain.PopupCampaign{}).Count(&count).Error
	if count != 0 {
		t.Fatal("invalid configuration persisted")
	}
}

func TestPopupAudienceMatchesTrustedAgeMembershipAndConditions(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	p := popupTestConfig("rules")
	c := popupCommand("buyer", "one")
	id := popupIdentity{newGuest: true}
	b, _ := json.Marshal(c.Data["profile"])
	_ = json.Unmarshal(b, &id.facts)
	for _, tc := range []struct {
		name, segment, user, registered, level string
		newGuest, want                         bool
	}{
		{"all guest", "all", "", "", "", true, true}, {"first visitor", "new_guest", "", "", "", true, true}, {"returning visitor", "new_guest", "", "", "", false, false}, {"returning visitor preset", "returning_guest", "", "", "", false, true}, {"member is not guest", "guest", "buyer", "", "", true, false}, {"new actual account", "new_member", "buyer", "2026-10-23T12:00:00Z", "member", false, true}, {"old actual account", "new_member", "buyer", "2025-01-01T00:00:00Z", "member", false, false}, {"old account matches old", "returning_member", "buyer", "2025-01-01T00:00:00Z", "member", false, true}, {"exact boundary is old", "returning_member", "buyer", "2026-10-17T12:00:00Z", "member", false, true}, {"unknown age not new", "new_member", "buyer", "", "member", false, false}, {"unknown age not old", "returning_member", "buyer", "", "member", false, false}, {"future account not new", "new_member", "buyer", "2027-01-01T00:00:00Z", "member", false, false}, {"gold member", "gold_member", "buyer", "", "gold", false, true}, {"ordinary member", "gold_member", "buyer", "", "member", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p.Audience.Segments = []string{tc.segment}
			c.UserID = tc.user
			id.newGuest = tc.newGuest
			id.facts.RegisteredAt = tc.registered
			id.facts.Level = tc.level
			if got := popupMatches(p, c, id, now); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	c = popupCommand("buyer", "one")
	_ = json.Unmarshal(b, &id.facts)
	p.Audience = popupAudience{Segments: []string{"member", "guest"}, NewMemberDays: 7, MinGrowth: 100, MaxGrowth: 200, MinPoints: 500, MaxPoints: 1000, IncludeUserIDs: []string{"buyer"}, InterestCategories: []string{"手机数码"}}
	if !popupMatches(p, c, id, now) {
		t.Fatal("valid combined audience rejected")
	}
	p.Audience.ExcludeUserIDs = []string{"buyer"}
	if popupMatches(p, c, id, now) {
		t.Fatal("exclusion did not override allowlist")
	}
	p.Audience.ExcludeUserIDs = nil
	id.facts.Growth = nil
	if popupMatches(p, c, id, now) {
		t.Fatal("missing trusted growth treated as zero")
	}
	p.Audience.MinGrowth = 0
	p.Audience.MaxGrowth = 200
	if popupMatches(p, c, id, now) {
		t.Fatal("missing growth satisfied max-only condition")
	}
	_ = json.Unmarshal(b, &id.facts)
	p.Audience.InterestCategories = []string{"宠物"}
	if popupMatches(p, c, id, now) {
		t.Fatal("unmatched interest accepted")
	}
}

func TestPopupGuestHistoryAndCouponFallbackBatching(t *testing.T) {
	s, _, rpc, _ := popupFixture(t)
	first := popupTestConfig("first")
	first.Priority = 200
	first.Audience.Segments = []string{"new_guest"}
	popupSave(t, s, first)
	returning := popupTestConfig("returning")
	returning.Audience.Segments = []string{"returning_guest"}
	popupSave(t, s, returning)
	c := popupCommand("", "one")
	if p := popupDecision(t, s, c); p["campaign_id"] != "first" {
		t.Fatal(p)
	}
	if p := popupDecision(t, s, c); p["campaign_id"] != "first" {
		t.Fatal("same session should remain first visit")
	}
	c.Data["session_id"] = "session-0000000000000two"
	if p := popupDecision(t, s, c); p["campaign_id"] != "returning" {
		t.Fatal(p)
	}
	s2, _, rpc2, _ := popupFixture(t)
	for i := 0; i < 61; i++ {
		p := popupTestConfig(fmt.Sprintf("batch-%02d", i))
		p.Priority = 500 - i
		p.Content.CouponTemplateID = fmt.Sprintf("coupon-%02d", i)
		rpc2.states[p.Content.CouponTemplateID] = "ended"
		if i == 60 {
			rpc2.states[p.Content.CouponTemplateID] = "available"
		}
		popupSave(t, s2, p)
	}
	p := popupDecision(t, s2, popupCommand("buyer", "one"))
	if p["campaign_id"] != "batch-60" {
		t.Fatalf("did not fall through ended coupons: %v", p)
	}
	if len(rpc2.batches) != 2 || len(rpc2.batches[0]) != 50 || len(rpc2.batches[1]) != 11 {
		t.Fatalf("coupon eligibility N+1: %+v", rpc2.batches)
	}
	rpc.states["everyday-10"] = "claimed"
	first.Version = 1
	first.Frequency.StopAfterClaim = true
	popupSave(t, s, first)
	newVisitor := popupCommand("", "new")
	newVisitor.Data["visitor_id"] = "brand-new-visitor-00000001"
	if p := popupDecision(t, s, newVisitor); p != nil {
		t.Fatalf("already-claimed coupon offered: %v", p)
	}
}

func TestPopupImpressionSerializedAcrossDeliveriesAndViews(t *testing.T) {
	s, db, _, _ := popupFixture(t)
	popupSave(t, s, popupTestConfig("concurrent"))
	c := popupCommand("buyer", "one")
	deliveries := make([]platform.Result, 16)
	for i := range deliveries {
		deliveries[i] = popupDecision(t, s, c)
	}
	// A second service object shares only persistence, as a separate replica does.
	replica := New(repository.New(db), s.rpc)
	replica.now = s.now
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	winner := 0
	errs := []error{}
	for i, p := range deliveries {
		wg.Add(1)
		go func(i int, p platform.Result) {
			defer wg.Done()
			service := s
			if i%2 == 0 {
				service = replica
			}
			r, err := service.Execute(context.Background(), popupEventCommand(c, p, "impression", fmt.Sprintf("view-%d", i)))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
			} else if r["allowed"] == true {
				allowed++
				winner = i
			}
		}(i, p)
	}
	wg.Wait()
	if len(errs) > 0 || allowed != 1 {
		t.Fatalf("caps under concurrent replicas: allowed=%d errors=%v", allowed, errs)
	}
	p := deliveries[winner]
	if !popupEventOK(t, s, c, p, "impression", fmt.Sprintf("view-%d", winner)) {
		t.Fatal("same view impression retry should be idempotent")
	}
	if popupEventOK(t, s, c, p, "impression", "another-view") {
		t.Fatal("another tab reused shown delivery")
	}
	wrong := popupCommand("another-user", "one")
	if popupEventOK(t, s, wrong, p, "click", fmt.Sprintf("view-%d", winner)) {
		t.Fatal("delivery crossed accounts")
	}
	wrong = popupCommand("buyer", "other")
	if popupEventOK(t, s, wrong, p, "click", fmt.Sprintf("view-%d", winner)) {
		t.Fatal("delivery crossed sessions")
	}
	if popupDecision(t, s, c) != nil {
		t.Fatal("session cap not applied to next decision")
	}
	var row domain.PopupCampaign
	_ = db.First(&row, "id = ?", "concurrent").Error
	if row.Impressions != 1 {
		t.Fatalf("double counted retries: %+v", row)
	}
}

func TestPopupFrequencyDailyCooldownAndLifetimePersist(t *testing.T) {
	s, db, _, now := popupFixture(t)
	popupSetGlobal(t, s, true, 10, 0)
	p := popupTestConfig("frequency")
	p.Frequency = popupFrequency{MaxPerUser: 3, MaxPerDay: 1, CooldownHours: 0, CloseCooldownHours: 4}
	popupSave(t, s, p)
	c := popupCommand("buyer", "one")
	delivery := popupDecision(t, s, c)
	if !popupEventOK(t, s, c, delivery, "impression", "view-first") {
		t.Fatal("first denied")
	}
	if !popupEventOK(t, s, c, delivery, "dismiss", "view-first") || !popupEventOK(t, s, c, delivery, "auto_close", "view-first") {
		t.Fatal("dismiss retry failed")
	}
	if popupDecision(t, s, c) != nil {
		t.Fatal("daily limit ignored")
	}
	// Beijing date boundary, followed by a close cooldown that spans the day.
	*now = time.Date(2026, 10, 24, 16, 30, 0, 0, time.UTC)
	delivery = popupDecision(t, s, c)
	if delivery == nil {
		t.Fatal("Beijing next day should reset daily cap after cooldown")
	}
	if !popupEventOK(t, s, c, delivery, "impression", "view-second") {
		t.Fatal("next day denied")
	}
	*now = now.Add(24 * time.Hour)
	delivery = popupDecision(t, s, c)
	if delivery == nil || !popupEventOK(t, s, c, delivery, "impression", "view-third") {
		t.Fatal("third daily impression denied")
	}
	*now = now.Add(24 * time.Hour)
	replica := New(repository.New(db), s.rpc)
	replica.now = s.now
	if popupDecision(t, replica, c) != nil {
		t.Fatal("restart/later day reset lifetime cap")
	}
	var row domain.PopupCampaign
	_ = db.First(&row, "id = ?", p.ID).Error
	if row.Impressions != 3 || row.Dismissals != 1 {
		t.Fatalf("event idempotency/stats: %+v", row)
	}
	// The account lifetime cap survives a different anonymous browser cookie.
	c.Data["visitor_id"] = "different-visitor-00000001"
	c.Data["session_id"] = "different-session-00000001"
	if popupDecision(t, replica, c) != nil {
		t.Fatal("new browser bypassed account cap")
	}
}

func TestPopupClaimsAuthoritativeIdempotentBoundAndNoFalseStats(t *testing.T) {
	s, db, rpc, now := popupFixture(t)
	p := popupTestConfig("claim")
	p.Frequency.StopAfterClaim = true
	popupSave(t, s, p)
	c := popupCommand("buyer", "one")
	delivery := popupDecision(t, s, c)
	claim := popupEventCommand(c, delivery, "click", "view-claim")
	claim.Action = "popup_claim"
	claim.Data["template_id"] = "forged-template"
	if _, err := s.Execute(context.Background(), claim); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("claim accepted before shown: %v", err)
	}
	if !popupEventOK(t, s, c, delivery, "impression", "view-claim") {
		t.Fatal("impression denied")
	}
	rpc.failClaim = true
	if _, err := s.Execute(context.Background(), claim); status.Code(err) != codes.Unavailable {
		t.Fatal(err)
	}
	var row domain.PopupCampaign
	_ = db.First(&row, "id = ?", p.ID).Error
	if row.Claims != 0 {
		t.Fatal("failed coupon RPC counted as claimed")
	}
	rpc.failClaim = false
	for i := 0; i < 2; i++ {
		r, err := s.Execute(context.Background(), claim)
		if err != nil || platform.Text(popupMap(r["coupon"]), "template_id") != "everyday-10" {
			t.Fatalf("claim did not bind campaign template: %v %v", r, err)
		}
	}
	_ = db.First(&row, "id = ?", p.ID).Error
	if row.Claims != 1 {
		t.Fatal("claim retry double counted")
	}
	if rpc.claims["buyer:forged-template"] != 0 {
		t.Fatal("client overrode template")
	}
	wrong := claim
	wrong.UserID = "other"
	if _, err := s.Execute(context.Background(), wrong); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("claim crossed users: %v", err)
	}
	wrong.UserID = ""
	if _, err := s.Execute(context.Background(), wrong); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("guest issued coupon: %v", err)
	}
	*now = now.Add(6 * time.Minute)
	if _, err := s.Execute(context.Background(), claim); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expired delivery claimed: %v", err)
	}
}

func TestPopupDemoSeedInsertOnceAndNoProductionCampaigns(t *testing.T) {
	t.Setenv("DEMO_MODE", "false")
	s, _, _, _ := popupFixture(t)
	if err := s.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := s.repo.PopupCampaigns(context.Background(), false)
	if err != nil || len(rows) != 0 {
		t.Fatal("production auto-published demo marketing")
	}
	t.Setenv("DEMO_MODE", "true")
	if err := s.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.repo.PopupCampaigns(context.Background(), false)
	if len(rows) != 4 {
		t.Fatalf("expected differentiated demo audience campaigns, got %d", len(rows))
	}
	p, _ := popupConfigFrom(rows[0])
	p.Version = rows[0].Version
	p.Status = "PAUSED"
	p.Name = "运营手工修改"
	popupSave(t, s, p)
	if err := s.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := s.repo.PopupCampaign(context.Background(), p.ID)
	if got.Status != "PAUSED" || got.Name != "运营手工修改" || got.Version != 2 {
		t.Fatal("seed overwrote admin configuration")
	}
}

func TestPopupScheduleExpiryVersionAndClosedDeliveryCannotReopen(t *testing.T) {
	s, _, _, now := popupFixture(t)
	p := popupTestConfig("window")
	p.StartsAt = now.Add(time.Hour).Format(time.RFC3339)
	p.EndsAt = now.Add(2 * time.Hour).Format(time.RFC3339)
	saved := popupSave(t, s, p)
	c := popupCommand("buyer", "one")
	if popupDecision(t, s, c) != nil {
		t.Fatal("future campaign shown")
	}
	*now = now.Add(time.Hour)
	delivery := popupDecision(t, s, c)
	if delivery == nil {
		t.Fatal("start boundary not inclusive")
	}
	p.Version = platform.Int(saved, "version")
	p.Content.Title = "运营已更新内容"
	popupSave(t, s, p)
	if popupEventOK(t, s, c, delivery, "impression", "stale-view") {
		t.Fatal("updated version delivery shown")
	}
	delivery = popupDecision(t, s, c)
	*now = now.Add(6 * time.Minute)
	if popupEventOK(t, s, c, delivery, "impression", "expired-view") {
		t.Fatal("expired delivery shown")
	}
	delivery = popupDecision(t, s, c)
	if !popupEventOK(t, s, c, delivery, "impression", "shown-view") {
		t.Fatal("fresh delivery denied")
	}
	if !popupEventOK(t, s, c, delivery, "dismiss", "shown-view") {
		t.Fatal("close denied")
	}
	if popupEventOK(t, s, c, delivery, "impression", "shown-view") {
		t.Fatal("closed delivery reopened by impression retry")
	}
	*now = time.Date(2026, 10, 24, 14, 0, 0, 0, time.UTC)
	if popupDecision(t, s, c) != nil {
		t.Fatal("end boundary must be exclusive")
	}
}

func TestPopupCloseCooldownSessionAndGlobalGapBoundaries(t *testing.T) {
	s, _, _, now := popupFixture(t)
	popupSetGlobal(t, s, true, 10, 60)
	p := popupTestConfig("cooldown")
	p.Frequency.CloseCooldownHours = 2
	p.Frequency.OncePerSession = true
	popupSave(t, s, p)
	c := popupCommand("buyer", "one")
	delivery := popupDecision(t, s, c)
	if !popupEventOK(t, s, c, delivery, "impression", "view-1") || !popupEventOK(t, s, c, delivery, "dismiss", "view-1") {
		t.Fatal("initial show/close failed")
	}
	*now = now.Add(61 * time.Second)
	c.Data["session_id"] = "session-0000000000000two"
	if popupDecision(t, s, c) != nil {
		t.Fatal("new session bypassed close cooldown")
	}
	*now = now.Add(2*time.Hour - time.Minute - time.Second)
	delivery = popupDecision(t, s, c)
	if delivery == nil || !popupEventOK(t, s, c, delivery, "impression", "view-2") {
		t.Fatal("cooldown exact boundary rejected")
	}
	*now = now.Add(time.Hour)
	if popupDecision(t, s, c) != nil {
		t.Fatal("campaign once-per-session ignored")
	}
	c.Data["session_id"] = "session-0000000000000three"
	if popupDecision(t, s, c) == nil {
		t.Fatal("new session should allow next exposure")
	}
	// A distinct campaign must still share the global minimum gap.
	p2 := popupTestConfig("gap")
	p2.Priority = 900
	popupSave(t, s, p2)
	*now = now.Add(time.Hour)
	next := popupDecision(t, s, c)
	if next == nil || !popupEventOK(t, s, c, next, "impression", "view-gap") {
		t.Fatal("second campaign failed")
	}
	c.Data["session_id"] = "session-0000000000000four"
	if popupDecision(t, s, c) != nil {
		t.Fatal("global minimum gap bypassed by campaign/session")
	}
	*now = now.Add(time.Minute)
	if popupDecision(t, s, c) == nil {
		t.Fatal("exact global gap boundary should permit candidate")
	}
}

func TestPopupTargetMatchesExistingShoppingRoutes(t *testing.T) {
	for _, target := range []string{"#/", "#/coupons", "#/member/coupons", "#/promotions", "#/promotions/quantity_discount/all", "#/promotions/group/today", "#/product/sku_1:blue.2", "#/product/%E7%94%9F%E6%B4%BB"} {
		p := popupTestConfig("target")
		p.Content.ActionTarget = target
		if err := validatePopup(&p); err != nil {
			t.Fatalf("safe shopping route %q rejected: %v", target, err)
		}
	}
	for _, target := range []string{"#/category/appliances", "#/admin", "#/member", "#/promotions/mygroups", "#/promotions/unknown", "#/product/%ZZ", "#/product/%1", "https://example.invalid/", "//example.invalid/", "javascript:alert(1)"} {
		p := popupTestConfig("target")
		p.Content.ActionTarget = target
		if err := validatePopup(&p); err == nil {
			t.Fatalf("unsupported route %q accepted", target)
		}
	}
}
