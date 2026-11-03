package tests_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon"
	"github.com/davveo/lumo/services/iam"
	"github.com/davveo/lumo/services/inventory"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMySQLRegistrationAndAuthRateInitialization(t *testing.T) {
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("INTERNAL_RPC_TOKEN", "isolated-mysql-auth-rate-test")
	t.Setenv("SMS_WEBHOOK_URL", "")
	t.Setenv("WECHAT_LOGIN_APP_ID", "isolated-app")
	t.Setenv("WECHAT_LOGIN_APP_SECRET", "isolated-secret")
	t.Setenv("WECHAT_LOGIN_REDIRECT_URI", "https://shop.example.test/index.html")
	ctx, db := adminMySQLScratch(t, "registration")
	var mode string
	if err := db.Raw("SELECT @@SESSION.sql_mode").Scan(&mode).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mode, "STRICT_TRANS_TABLES") || !strings.Contains(mode, "NO_ZERO_DATE") {
		t.Fatalf("regression requires MySQL strict date validation: %s", mode)
	}
	module := iam.New(platform.Dependencies{DB: db})
	if err := module.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := module.Handle(ctx, adminCommand("policy_upsert", "", "admin", "admin", map[string]any{
		"config": map[string]any{
			"password_enabled": true, "phone_enabled": true, "wechat_enabled": true,
			"allow_registration": true, "otp_ttl_seconds": 300, "otp_cooldown_seconds": 60,
		},
		"version": 0, "reason": "isolated MySQL authentication regression",
	})); err != nil {
		t.Fatal(err)
	}
	t.Run("registration", func(t *testing.T) {
		ip := "203.0.113.10"
		register := func(name string) (platform.Result, error) {
			return module.Handle(ctx, adminCommand("register", "", "", "", map[string]any{
				"username": name, "password": "Member@2026", "role": "admin", "_client_ip": ip,
			}))
		}
		account, err := register("New.Buyer")
		if err != nil || platform.Text(account, "role") != "user" || platform.Text(account, "username") != "new.buyer" {
			t.Fatalf("fresh registration failed: %v %v", account, err)
		}
		login, err := module.Handle(ctx, adminCommand("login", "", "", "", map[string]any{
			"username": "new.buyer", "password": "Member@2026", "_client_ip": ip,
		}))
		if err != nil || platform.Text(login, "user_id") != platform.Text(account, "user_id") {
			t.Fatalf("registered member cannot log in: %v", err)
		}
		if _, err := register("new.buyer"); status.Code(err) != codes.AlreadyExists {
			t.Fatalf("duplicate registration returned %v", err)
		}
		rateID := fmt.Sprintf("%x", sha256.Sum256([]byte("register:"+ip)))
		if err := db.Table("auth_rates").Where("id = ?", rateID).Update("count", 19).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := register("limit.member"); err != nil {
			t.Fatalf("last allowed registration failed: %v", err)
		}
		if _, err := register("blocked.member"); status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("registration limit bypassed: %v", err)
		}
		if err := db.Table("auth_rates").Where("id = ?", rateID).
			Update("day", time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := register("nextday.member"); err != nil {
			t.Fatalf("registration did not recover next day: %v", err)
		}
	})
	t.Run("sms", func(t *testing.T) {
		data := map[string]any{"phone": "13800138001", "_client_ip": "203.0.113.11"}
		challenge, err := module.Handle(ctx, adminCommand("otp_send", "", "", "", data))
		if err != nil || len(platform.Text(challenge, "demo_code")) != 6 {
			t.Fatalf("first SMS request failed: %v", err)
		}
		if _, err := module.Handle(ctx, adminCommand("otp_send", "", "", "", data)); status.Code(
			err,
		) != codes.ResourceExhausted {
			t.Fatalf("SMS cooldown bypassed: %v", err)
		}
		login, err := module.Handle(ctx, adminCommand("otp_login", "", "", "", map[string]any{
			"phone": data["phone"], "challenge_id": challenge["challenge_id"], "code": challenge["demo_code"],
		}))
		if err != nil || platform.Text(login, "role") != "user" {
			t.Fatalf("SMS login failed: %v", err)
		}
	})
	t.Run("oauth", func(t *testing.T) {
		for i := 0; i < 2; i++ {
			result, err := module.Handle(ctx, adminCommand("oauth_start", "", "", "", map[string]any{
				"provider": "wechat", "proof": strings.Repeat("p", 32), "_client_ip": "203.0.113.12",
			}))
			if err != nil || len(platform.Text(result, "oauth_state")) != 64 {
				t.Fatalf("OAuth request %d failed: %v", i+1, err)
			}
		}
	})
}

func TestMySQLPasswordLoginRates(t *testing.T) {
	t.Setenv("DEMO_MODE", "true")
	ctx, db := adminMySQLScratch(t, "password")
	module := iam.New(platform.Dependencies{DB: db})
	if err := module.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := module.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	login := func(password string) (platform.Result, error) {
		return module.Handle(
			ctx,
			platform.Command{
				Action: "login",
				Data:   map[string]any{"username": "admin", "password": password, "_client_ip": "203.0.113.1"},
			},
		)
	}
	for i := 0; i < 5; i++ {
		if _, err := login("incorrect-password"); status.Code(err) != codes.Unauthenticated {
			t.Fatal("wrong failure result", err)
		}
	}
	if _, err := login("Admin@2026"); status.Code(err) != codes.ResourceExhausted {
		t.Fatal("password backoff bypassed", err)
	}
	if err := db.Table("iam_password_rates").Where("1 = 1").Updates(map[string]any{"blocked_until": nil, "window_started": time.Now().Add(-2 * time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	if result, err := login("Admin@2026"); err != nil || platform.Text(result, "role") != "admin" {
		t.Fatal("login did not recover after rate window", err)
	}
}

func adminMySQLScratch(t *testing.T, name string) (context.Context, *gorm.DB) {
	t.Helper()
	if os.Getenv("LUMO_MYSQL_ADMIN_TEST") != "1" {
		t.Skip("requires explicitly enabled MySQL scratch-schema regression")
	}
	address := os.Getenv("LUMO_MYSQL_TEST_ADDR")
	if address == "" {
		address = "127.0.0.1:3306"
	}
	password := os.Getenv("MYSQL_ROOT_PASSWORD")
	if password == "" {
		t.Fatal("local MySQL credential missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	dsn := "root:" + password + "@tcp(" + address + ")/"
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
		if err = db.Raw("SELECT COLUMN_NAME AS column_name FROM information_schema.STATISTICS WHERE " +
			"TABLE_SCHEMA=DATABASE() AND TABLE_NAME='adjustments' AND " +
			"INDEX_NAME='idx_inventory_adjustment_history' ORDER BY SEQ_IN_INDEX").Scan(&columns).Error; err != nil {
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
