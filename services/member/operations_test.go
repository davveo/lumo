package member_test

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/address"
	"github.com/davveo/lumo/services/catalog"
	"github.com/davveo/lumo/services/content"
	"github.com/davveo/lumo/services/engagement"
	"github.com/davveo/lumo/services/iam"
	"github.com/davveo/lumo/services/interaction"
	"github.com/davveo/lumo/services/media"
	"github.com/davveo/lumo/services/member"
	"github.com/davveo/lumo/services/recommendation"
	"github.com/davveo/lumo/services/search"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"path/filepath"
	"testing"
)

func TestAllAccountCatalogServicesExposeDistinctOperations(t *testing.T) {
	cases := []struct {
		name    string
		module  platform.Module
		actions []string
	}{
		{
			"iam",
			iam.New(platform.Dependencies{}),
			[]string{
				"login",
				"validate",
				"refresh",
				"logout",
				"logout_all",
				"admin_list",
				"admin_summary",
				"set_active", "audit_list", "admin_get",
				"login_methods", "otp_send", "otp_login", "oauth_start", "oauth_callback", "policy_get", "policy_upsert",
			},
		},
		{"member", member.New(platform.Dependencies{}), []string{"get", "update", "policy_get", "policy_upsert"}},
		{
			"address",
			address.New(platform.Dependencies{}),
			[]string{"list", "get", "create", "update", "delete", "default"},
		},
		{
			"catalog",
			catalog.New(platform.Dependencies{}),
			[]string{"search_products", "recommendation_candidates", "batch_get", "list", "get", "upsert", "admin_list"},
		},
		{"media", media.NewWithStore(platform.Dependencies{}, nil), []string{"create", "get", "download", "complete"}},
		{
			"interaction",
			interaction.New(platform.Dependencies{}),
			[]string{"list", "create", "moderate", "answer", "like"},
		},
		{"engagement", engagement.New(platform.Dependencies{}), []string{"list", "put", "delete", "clear", "subscribe", "subscriptions", "unsubscribe", "policy_get", "policy_upsert", "admin_list", "admin_cancel"}},
		{"search", search.New(platform.Dependencies{}), []string{"query", "suggest", "policy_get", "policy_upsert"}},
		{"recommendation", recommendation.New(platform.Dependencies{}), []string{"list", "rank_bootstrap", "policy_get", "policy_upsert"}},
		{
			"content",
			content.New(platform.Dependencies{}),
			[]string{
				"list",
				"get",
				"upsert",
				"popup_list",
				"popup_upsert",
				"popup_configure",
				"popup_decide",
				"popup_event",
				"popup_claim", "admin_list", "admin_upsert",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.module.Operations) != len(tc.actions) {
				t.Fatalf("registered %d operations; want %d", len(tc.module.Operations), len(tc.actions))
			}
			for _, action := range tc.actions {
				if tc.module.Operations[action] == nil {
					t.Errorf("missing operation %s", action)
				}
			}
			for action, want := range map[string]codes.Code{"": codes.InvalidArgument, "unknown-method": codes.Unimplemented} {
				if _, err := tc.module.Handle(context.Background(), platform.Command{Action: action}); status.Code(
					err,
				) != want {
					t.Errorf("boundary %q: %v; want %s", action, err, want)
				}
			}
		})
	}
}
func TestDirectOperationsEnforcePermissionsBeforeDatabaseAccess(t *testing.T) {
	cases := []struct {
		name      string
		operation platform.Operation
		want      codes.Code
	}{
		{"member.get", member.New(platform.Dependencies{}).Operations["get"], codes.Unauthenticated},
		{"member.update", member.New(platform.Dependencies{}).Operations["update"], codes.Unauthenticated},
		{"address.create", address.New(platform.Dependencies{}).Operations["create"], codes.Unauthenticated},
		{"engagement.put", engagement.New(platform.Dependencies{}).Operations["put"], codes.Unauthenticated},
		{
			"media.complete",
			media.NewWithStore(platform.Dependencies{}, nil).Operations["complete"],
			codes.Unauthenticated,
		},
		{"interaction.answer", interaction.New(platform.Dependencies{}).Operations["answer"], codes.Unauthenticated},
		{
			"content.popup_upsert",
			content.New(platform.Dependencies{}).Operations["popup_upsert"],
			codes.Unauthenticated,
		},
		{"catalog.upsert", catalog.New(platform.Dependencies{}).Operations["upsert"], codes.Unauthenticated},
		{
			"catalog.search_products",
			catalog.New(platform.Dependencies{}).Operations["search_products"],
			codes.PermissionDenied,
		},
		{
			"recommendation.rank_bootstrap",
			recommendation.New(platform.Dependencies{}).Operations["rank_bootstrap"],
			codes.PermissionDenied,
		},
		{"iam.logout_all", iam.New(platform.Dependencies{}).Operations["logout_all"], codes.Unauthenticated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.operation(context.Background(), platform.Request{Data: map[string]any{"action": "list"}})
			if status.Code(err) != tc.want {
				t.Fatalf("%v; want %s", err, tc.want)
			}
		})
	}
}
func TestMemberGetAndUpdateCannotBeSelectedByPayload(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "member.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	module := member.New(platform.Dependencies{DB: db})
	if err = module.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	principal := platform.Request{
		UserID: "buyer",
		Role:   "user",
		Data:   map[string]any{"action": "update", "display_name": "injected"},
	}
	initial, err := module.Operations["get"](ctx, principal)
	if err != nil || initial["display_name"] != "buyer" {
		t.Fatalf("get became a mutation: %v %v", initial, err)
	}
	if _, err = module.Operations["update"](ctx, principal); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("update accepted unsupported action field: %v", err)
	}
	principal.Data = map[string]any{"display_name": "updated"}
	changed, err := module.Operations["update"](ctx, principal)
	if err != nil || changed["display_name"] != "updated" {
		t.Fatalf("update failed: %v %v", changed, err)
	}
	principal.ID = "another-user"
	if _, err = module.Operations["get"](ctx, principal); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("get bypassed owner check: %v", err)
	}
	if _, err = module.Operations["update"](ctx, principal); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("update bypassed owner check: %v", err)
	}
}
