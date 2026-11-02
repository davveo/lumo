package tests

import (
	"context"
	"encoding/base64"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestCatalogImportPreviewCommitConflictAndOwnership(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	m := c.modules["catalog"]
	call := func(action, id string, data map[string]any) (platform.Result, error) {
		return c.rpc.Call(
			ctx,
			"catalog",
			platform.Command{Action: action, ID: id, UserID: "admin", Role: "admin", Data: data},
		)
	}
	preview := func(csv, mode string) platform.Result {
		v, e := call(
			"import_preview",
			"",
			map[string]any{
				"filename":    "products.csv",
				"file_base64": base64.StdEncoding.EncodeToString([]byte(csv)),
				"mode":        mode,
			},
		)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	job := preview(
		"id,name,price_cents,active\nimport-one,Import One,1999,false\nimport-two,Import Two,2999,true\n",
		"create",
	)
	id := platform.Text(job, "id")
	if job["status"] != "PREVIEW" {
		t.Fatal(job)
	}
	if _, e := call("import_commit", id, nil); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e := m.Tick(ctx); e != nil {
			t.Fatal(e)
		}
	}
	v, e := call("import_get", id, nil)
	if e != nil || v["status"] != "COMPLETED" || platform.Int(v, "succeeded") != 2 {
		t.Fatal(v, e)
	}
	if _, e = call("import_commit", id, nil); e != nil {
		t.Fatal("duplicate commit", e)
	}
	invalid := preview("id,name,price_cents\nbad,Bad,invalid\n", "create")
	if invalid["status"] != "VALIDATION_FAILED" {
		t.Fatal(invalid)
	}
	if _, e = call("import_commit", platform.Text(invalid, "id"), nil); status.Code(e) != codes.FailedPrecondition {
		t.Fatal(e)
	}
	conflict := preview("id,name,price_cents\nimport-two,New Name,3999\n", "update")
	if _, e = call("upsert", "import-two", map[string]any{"name": "Concurrent Change", "price_cents": 5999, "active": true}); e != nil {
		t.Fatal(e)
	}
	if _, e = call("import_commit", platform.Text(conflict, "id"), nil); e != nil {
		t.Fatal(e)
	}
	if e = m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	v, e = call("import_get", platform.Text(conflict, "id"), nil)
	if e != nil || platform.Int(v, "failed") != 1 {
		t.Fatal(v, e)
	}
	item, e := call("get", "import-two", nil)
	if e != nil || platform.Text(item, "name") != "Concurrent Change" {
		t.Fatal(item, e)
	}
	_, e = c.rpc.Call(
		ctx,
		"catalog",
		platform.Command{Action: "import_get", ID: id, UserID: "other-admin", Role: "admin"},
	)
	if status.Code(e) != codes.NotFound {
		t.Fatal("job leaked", e)
	}
}
