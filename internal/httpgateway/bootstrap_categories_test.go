package httpgateway

import (
	"context"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func categoryContentRow() platform.Result {
	return platform.Result{
		"id":     storefrontCategoriesID,
		"kind":   "storefront",
		"status": "PUBLISHED",
		"body":   `{"version":1,"groups":[]}`,
	}
}

func categoryListFragment(items []platform.Result, err error) platform.Result {
	request := readRequest{
		Key:     "content-page",
		Service: "content",
		Action:  "list",
		Command: readCommand{Data: map[string]any{"kind": "storefront", "limit": 100}},
	}
	return readResult(request, platform.Result{"items": items}, err)
}

func assertCategoryCacheKey(t *testing.T, row platform.Result) {
	t.Helper()
	command, ok := row["command"].(readCommand)
	if row["service"] != "content" || row["action"] != "get" || row["auth"] != false || !ok ||
		command.ID != storefrontCategoriesID ||
		len(command.Data) != 0 {
		t.Fatalf("category fragment cannot prime the exact public content/get cache key: %#v", row)
	}
}

func TestBootstrapCategoryNavigationReusesExistingStorefrontList(t *testing.T) {
	caller := &aggregationCaller{}
	g := &gateway{rpc: caller}
	config := categoryContentRow()
	fragments := []platform.Result{
		categoryListFragment(
			[]platform.Result{{"id": "storefront-main", "kind": "storefront", "body": `{}`}, config},
			nil,
		),
	}
	got := g.bootstrapCategoryNavigation(
		context.Background(),
		fragments,
		platform.Command{UserID: "admin", Role: "admin"},
	)
	if len(got) != 2 || len(caller.calls) != 0 {
		t.Fatalf("existing content read was repeated: %#v calls=%#v", got, caller.calls)
	}
	assertCategoryCacheKey(t, got[1])
	if platform.Text(platform.Map(got[1]["data"]), "body") != config["body"] {
		t.Fatal("backend navigation body was changed")
	}
}

func TestBootstrapCategoryNavigationUsesOneBoundedPublicLookupWhenListOmitsIt(t *testing.T) {
	for _, test := range []struct {
		name string
		list platform.Result
	}{
		{
			"more than one content page",
			categoryListFragment([]platform.Result{{"id": "other-page", "kind": "storefront"}}, nil),
		},
		{
			"list temporarily failed",
			categoryListFragment(nil, status.Error(codes.Unavailable, "content list unavailable")),
		},
		{
			"wrong kind cannot prime navigation",
			categoryListFragment([]platform.Result{{"id": storefrontCategoriesID, "kind": "faq"}}, nil),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			caller := &aggregationCaller{
				handle: func(_ context.Context, service string, command platform.Command) (platform.Result, error) {
					if service != "content" || command.Action != "get" || command.ID != storefrontCategoriesID ||
						command.UserID != "" ||
						command.Role != "" ||
						len(command.Data) != 0 {
						t.Fatalf("fallback changed target or inherited principal: %#v", command)
					}
					return categoryContentRow(), nil
				},
			}
			got := (&gateway{rpc: caller}).bootstrapCategoryNavigation(
				context.Background(),
				[]platform.Result{test.list},
				platform.Command{UserID: "admin", Role: "admin"},
			)
			if len(got) != 2 || caller.count("content", "get") != 1 {
				t.Fatalf("lookup was not bounded: results=%#v calls=%#v", got, caller.calls)
			}
			assertCategoryCacheKey(t, got[1])
			if platform.Text(platform.Map(got[1]["data"]), "id") != storefrontCategoriesID {
				t.Fatal("backend document missing")
			}
		})
	}
}

func TestBootstrapCategoryNavigationKeepsReadFailureWithoutInventedDefaults(t *testing.T) {
	caller := &aggregationCaller{handle: func(context.Context, string, platform.Command) (platform.Result, error) {
		return nil, status.Error(codes.NotFound, "navigation not configured")
	}}
	got := (&gateway{rpc: caller}).bootstrapCategoryNavigation(context.Background(), nil, platform.Command{})
	if len(got) != 1 || caller.count("content", "get") != 1 || got[0]["data"] != nil ||
		platform.Text(platform.Map(got[0]["error"]), "code") != "NotFound" {
		t.Fatalf("failure was replaced by fallback display data: %#v", got)
	}
	assertCategoryCacheKey(t, got[0])
}

func TestBootstrapCategoryNavigationCanceledLookupDoesNotReachBackend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	caller := &aggregationCaller{}
	got := (&gateway{rpc: caller}).bootstrapCategoryNavigation(ctx, nil, platform.Command{})
	if len(caller.calls) != 0 || len(got) != 1 || platform.Text(platform.Map(got[0]["error"]), "code") != "Canceled" {
		t.Fatalf("canceled navigation fetch reached backend: %#v calls=%#v", got, caller.calls)
	}
}
