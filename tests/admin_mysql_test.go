package tests_test

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon"
	"github.com/davveo/lumo/services/iam"
	"github.com/davveo/lumo/services/inventory"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func adminMySQLScratch(t *testing.T, name string) (context.Context, *gorm.DB) {
	t.Helper()
	if os.Getenv("LUMO_MYSQL_ADMIN_TEST") != "1" {
		t.Skip("requires explicitly enabled MySQL scratch-schema regression")
	}
	password := os.Getenv("MYSQL_ROOT_PASSWORD")
	if password == "" {
		t.Fatal("local MySQL credential missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	dsn := "root:" + password + "@tcp(127.0.0.1:3306)/"
	admin, err := platform.OpenDB(ctx, "mysql", dsn+"?charset=utf8mb4&parseTime=true&loc=UTC")
	if err != nil {
		t.Fatal(err)
	}
	rawAdmin, _ := admin.DB()
	t.Cleanup(func() { rawAdmin.Close() })
	schema := fmt.Sprintf("lumo_admin_probe_%s_%d", name, time.Now().UnixNano())
	if err = admin.Exec("CREATE DATABASE " + schema + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP DATABASE " + schema).Error; err != nil {
			t.Error(err)
		}
	})
	db, err := platform.OpenDB(ctx, "mysql", dsn+schema+"?charset=utf8mb4&parseTime=true&loc=UTC")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	return ctx, db
}
func adminCommand(action, id, user, role string, data map[string]any) platform.Command {
	return platform.Command{Action: action, ID: id, UserID: user, Role: role, Data: data}
}
func TestMySQLAdminOperationsMigrationsAndCouponSnapshots(t *testing.T) {
	t.Run("coupon", func(t *testing.T) {
		ctx, db := adminMySQLScratch(t, "coupon")
		module := coupon.New(platform.Dependencies{DB: db})
		if err := module.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err := module.Migrate(ctx); err != nil {
			t.Fatal("idempotent migration", err)
		}
		input := map[string]any{
			"name":               "Scoped discount",
			"coupon_type":        "discount",
			"amount_cents":       0,
			"min_cents":          0,
			"discount_bps":       8500,
			"max_discount_cents": 1250,
			"scope_type":         "category",
			"scope_values":       []string{"手机数码"},
			"valid_days":         7,
			"remaining":          10,
			"active":             true,
			"expires_at":         time.Now().Add(time.Hour),
			"version":            0,
		}
		if _, err := module.Handle(ctx, adminCommand("template_upsert", "typed", "admin", "admin", input)); err != nil {
			t.Fatal(err)
		}
		claimed, err := module.Handle(ctx, adminCommand("claim", "typed", "buyer", "user", nil))
		if err != nil {
			t.Fatal(err)
		}
		wallet, err := module.Handle(ctx, adminCommand("list", "", "buyer", "user", nil))
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Items []struct {
				Type   string   `json:"coupon_type"`
				Values []string `json:"scope_values"`
			}
		}
		if err = platform.Decode(wallet, &decoded); err != nil || len(decoded.Items) != 1 ||
			decoded.Items[0].Type != "discount" ||
			len(decoded.Items[0].Values) != 1 ||
			decoded.Items[0].Values[0] != "手机数码" {
			t.Fatalf("serialized coupon rule did not survive MySQL snapshot: %+v %v", decoded, err)
		}
		hold, err := module.Handle(
			ctx,
			adminCommand(
				"reserve",
				"order",
				"buyer",
				"system",
				map[string]any{
					"coupon_id":      claimed["id"],
					"subtotal_cents": 10000,
					"items": []map[string]any{
						{"sku_id": "sku", "product_id": "product", "category": "手机数码", "amount_cents": 10000},
					},
				},
			),
		)
		if err != nil || platform.Int(hold, "discount_cents") != 1250 {
			t.Fatalf("MySQL reserved benefit differs from quote: %v %v", hold, err)
		}
	})
	t.Run("inventory", func(t *testing.T) {
		ctx, db := adminMySQLScratch(t, "inventory")
		module := inventory.New(platform.Dependencies{DB: db})
		if err := module.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := module.Handle(ctx, adminCommand("adjust", "sku", "admin", "admin", map[string]any{"on_hand": 30, "expected_on_hand": 0, "reason": "入库"})); err != nil {
			t.Fatal(err)
		}
		history, err := module.Handle(ctx, adminCommand("adjustments", "sku", "admin", "admin", nil))
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Items []struct {
				Before, After int64
				Reason        string
			}
		}
		if err = platform.Decode(history, &decoded); err != nil || len(decoded.Items) != 1 ||
			decoded.Items[0].After != 30 {
			t.Fatalf("MySQL adjustment audit missing: %+v %v", decoded, err)
		}
		var columns []struct{ ColumnName string }
		if err = db.Raw("SELECT COLUMN_NAME AS column_name FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='adjustments' AND INDEX_NAME='idx_inventory_adjustment_history' ORDER BY SEQ_IN_INDEX").Scan(&columns).Error; err != nil {
			t.Fatal(err)
		}
		if len(columns) != 3 || columns[0].ColumnName != "sku_id" || columns[1].ColumnName != "created_at" ||
			columns[2].ColumnName != "id" {
			t.Fatalf("inventory history index: %+v", columns)
		}
	})
	t.Run("iam", func(t *testing.T) {
		ctx, db := adminMySQLScratch(t, "iam")
		t.Setenv("DEMO_MODE", "true")
		module := iam.New(platform.Dependencies{DB: db})
		if err := module.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err := module.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := module.Handle(ctx, adminCommand("set_active", "demo", "admin", "admin", map[string]any{"active": false, "expected_active": true, "reason": "隔离验证"})); err != nil {
			t.Fatal(err)
		}
		result, err := module.Handle(
			ctx,
			adminCommand("admin_list", "", "admin", "admin", map[string]any{"status": "user"}),
		)
		if err != nil || platform.Int(result, "total") != 1 {
			t.Fatalf("MySQL admin account read: %v %v", result, err)
		}
	})
}

type mysqlManagementRow struct {
	ID        string `gorm:"primaryKey;size:80"`
	Status    string `gorm:"size:32"`
	Amount    int64
	CreatedAt time.Time
}

func TestMySQLAdminSummaryUsesPlainBusinessDayKeys(t *testing.T) {
	ctx, db := adminMySQLScratch(t, "summary")
	if err := db.AutoMigrate(&mysqlManagementRow{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Create(&mysqlManagementRow{ID: filepath.Base(t.Name()), Status: "SUCCEEDED", Amount: 12345, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	value, err := adminquery.Summary[mysqlManagementRow](
		ctx,
		db,
		map[string]any{"days": 7},
		adminquery.Spec{State: "status", Amount: "amount"},
	)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Daily []struct {
			Day    string
			Count  int64
			Amount int64 `json:"amount_cents"`
		}
	}
	if err = platform.Decode(value, &result); err != nil || len(result.Daily) != 1 || len(result.Daily[0].Day) != 10 ||
		strings.Contains(result.Daily[0].Day, "T") ||
		result.Daily[0].Amount != 12345 {
		t.Fatalf("MySQL day key must match frontend dates: %+v %v", result, err)
	}
}
