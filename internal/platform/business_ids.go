package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Business IDs use 41 milliseconds + 10 node bits + 12 sequence bits, as
// decimal strings. Keeping the prefix and number in one string prevents JS
// number precision loss. A durable cursor is shared by a domain's replicas.
const snowflakeEpoch int64 = 1735689600000 // 2025-01-01 UTC
const snowflakeMaxMS int64 = 1<<41 - 1

type BusinessIDCursor struct {
	Prefix      string `gorm:"primaryKey;size:32"`
	Node        int64  `gorm:"primaryKey"`
	Millisecond int64
	Sequence    int64
}
type BusinessIDAssignment struct {
	Key        string `gorm:"primaryKey;size:64"`
	BusinessID string `gorm:"size:80;index"`
	CreatedAt  time.Time
}

var businessPrefixes = map[string]string{
	"order": "ORD", "ord": "ORD", "refund": "RFND", "payment": "PAY", "pay": "PAY",
	"aftersale": "AFS", "invoice": "INV", "shipment": "SHP", "tracking": "TRK",
	"group": "GRP", "flash": "FLS", "quote": "QTE", "coupon": "CPN", "addr": "ADR",
	"ticket": "TKT", "file": "MED", "recon": "RCN", "inspection": "INS", "exchange": "EXC",
	"review": "RVW", "question": "QST", "notice": "CNT", "content": "CNT", "popup": "POP",
	"coupon-template": "CPT", "user": "USR", "reversal": "RVS", "alert": "ALT", "notification": "NTF", "stock-adjustment": "STK",
}
var prefixPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,31}$`)

func businessPrefix(prefix string) (string, error) {
	if v, ok := businessPrefixes[prefix]; ok {
		return v, nil
	}
	prefix = strings.ToUpper(strings.ReplaceAll(prefix, "-", "_"))
	if !prefixPattern.MatchString(prefix) {
		return "", status.Error(codes.InvalidArgument, "invalid business ID prefix")
	}
	return prefix, nil
}
func businessNode(prefix string) (int64, error) {
	// Types have distinct stable node bits; replicas of one domain additionally
	// share the persisted cursor. An explicit node supports separately allocated
	// writer groups without relying on a random or hashed machine identity.
	known := []string{"ORD", "RFND", "PAY", "AFS", "INV", "SHP", "TRK", "GRP", "FLS", "QTE", "CPN", "ADR", "TKT", "MED", "RCN", "INS", "EXC", "RVW", "QST", "CNT", "POP", "CPT", "USR", "RVS", "ALT", "NTF", "STK"}
	if raw := os.Getenv("SNOWFLAKE_NODE_ID"); raw != "" {
		n, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || n < 0 || n > 1023 {
			return 0, status.Error(codes.FailedPrecondition, "SNOWFLAKE_NODE_ID must be 0..1023")
		}
		return n, nil
	}
	for i, v := range known {
		if prefix == v {
			return int64(i), nil
		}
	}
	return 1023, nil
}
func MigrateBusinessIDs(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).AutoMigrate(&BusinessIDCursor{}, &BusinessIDAssignment{})
}
func snowflakeNext(last, sequence, node, now int64) (int64, int64, int64, error) {
	elapsed := now - snowflakeEpoch
	if elapsed < 0 || elapsed > snowflakeMaxMS {
		return 0, 0, 0, status.Error(codes.FailedPrecondition, "clock outside snowflake epoch")
	}
	if last > elapsed+5000 {
		return 0, 0, 0, status.Error(codes.Unavailable, "clock moved backwards more than 5 seconds")
	}
	if elapsed > last {
		sequence = 0
	} else {
		elapsed = last
		sequence++
		if sequence > 4095 {
			elapsed++
			sequence = 0
		}
	}
	if elapsed > snowflakeMaxMS {
		return 0, 0, 0, status.Error(codes.OutOfRange, "snowflake time range exhausted")
	}
	return elapsed<<22 | node<<12 | sequence, elapsed, sequence, nil
}
func nextBusinessID(ctx context.Context, tx *gorm.DB, prefix string) (string, error) {
	node, e := businessNode(prefix)
	if e != nil {
		return "", e
	}
	cursor := BusinessIDCursor{Prefix: prefix, Node: node}
	if e = tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&cursor).Error; e != nil {
		return "", e
	}
	if e = tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&cursor, "prefix = ? AND node = ?", prefix, node).Error; e != nil {
		return "", e
	}
	number, millis, sequence, e := snowflakeNext(cursor.Millisecond, cursor.Sequence, node, time.Now().UnixMilli())
	if e != nil {
		return "", e
	}
	if e = tx.WithContext(ctx).Model(&BusinessIDCursor{}).Where("prefix = ? AND node = ?", prefix, node).Updates(map[string]any{"millisecond": millis, "sequence": sequence}).Error; e != nil {
		return "", e
	}
	return prefix + "_" + strconv.FormatInt(number, 10), nil
}
func NextBusinessID(ctx context.Context, db *gorm.DB, prefix string) (string, error) {
	ctx, span := Start(ctx, "platform.repository.NextBusinessID")
	defer span.End()
	canonical, e := businessPrefix(prefix)
	if e != nil {
		return "", e
	}
	var id string
	e = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { var err error; id, err = nextBusinessID(ctx, tx, canonical); return err })
	return id, e
}
func assignmentKey(prefix, key string) string {
	sum := sha256.Sum256([]byte(prefix + "\x00" + key))
	return hex.EncodeToString(sum[:])
}

// ResolveBusinessID separates deduplication from public numbering. legacyID and
// table are trusted repository constants, never a query supplied by the client.
func ResolveBusinessID(ctx context.Context, db *gorm.DB, prefix, key, legacyID, table string) (string, error) {
	ctx, span := Start(ctx, "platform.repository.ResolveBusinessID")
	defer span.End()
	canonical, e := businessPrefix(prefix)
	if e != nil {
		return "", e
	}
	if key == "" {
		return NextBusinessID(ctx, db, canonical)
	}
	var id string
	e = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := BusinessIDAssignment{Key: assignmentKey(canonical, key)}
		if e := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; e != nil {
			return e
		}
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "`key` = ?", row.Key).Error; e != nil {
			return e
		}
		if row.BusinessID != "" {
			id = row.BusinessID
			return nil
		}
		if legacyID != "" && table != "" {
			var count int64
			if e := tx.WithContext(ctx).Table(table).Where("id = ?", legacyID).Count(&count).Error; e != nil {
				return e
			}
			if count > 0 {
				id = legacyID
			}
		}
		if id == "" {
			var e error
			id, e = nextBusinessID(ctx, tx, canonical)
			if e != nil {
				return e
			}
		}
		return tx.WithContext(ctx).Model(&BusinessIDAssignment{}).Where("`key` = ?", row.Key).Update("business_id", id).Error
	})
	return id, e
}
func LookupBusinessID(ctx context.Context, db *gorm.DB, prefix, key string) (string, error) {
	canonical, e := businessPrefix(prefix)
	if e != nil {
		return "", e
	}
	var row BusinessIDAssignment
	result := db.WithContext(ctx).Where("`key` = ?", assignmentKey(canonical, key)).Find(&row)
	return row.BusinessID, result.Error
}
func IsBusinessID(id, prefix string) bool {
	canonical, e := businessPrefix(prefix)
	if e != nil || !strings.HasPrefix(id, canonical+"_") {
		return false
	}
	raw := strings.TrimPrefix(id, canonical+"_")
	n, e := strconv.ParseInt(raw, 10, 64)
	return e == nil && n > 0 && fmt.Sprint(n) == raw
}
