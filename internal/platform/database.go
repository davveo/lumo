package platform

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func OpenDB(ctx context.Context, driver, dsn string) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch driver {
	case "mysql":
		dialector = mysql.Open(dsn)
	case "sqlite":
		dialector = sqlite.Open(dsn)
	default:
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if driver == "sqlite" {
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
	} else {
		sqlDB.SetMaxOpenConns(12)
		sqlDB.SetMaxIdleConns(4)
	}
	sqlDB.SetConnMaxLifetime(15 * time.Minute)
	if err = sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("database ping: %w", err)
	}
	if err = db.Use(
		&SQLTracing{System: driver, Namespace: db.WithContext(ctx).Migrator().CurrentDatabase()},
	); err != nil {
		return nil, err
	}
	return db, nil
}

type SQLTracing struct {
	System    string
	Namespace string
}

func (*SQLTracing) Name() string { return "lumo-sql-tracing" }

type querySpan struct {
	span   trace.Span
	parent context.Context
}

var sqlLiteral = regexp.MustCompile(`'(?:''|\\.|[^'])*'|"(?:""|\\.|[^"])*"|\b\d+(?:\.\d+)?\b`)
var sqlComment = regexp.MustCompile(`(?s)/\*.*?\*/|--[^\r\n]*|#[^\r\n]*`)

// SQLTemplate never expands bound values. It also redacts literal constants for
// raw statements, protecting credentials, personal data and arbitrary filters.
func SQLTemplate(sql string) string {
	sql = sqlLiteral.ReplaceAllString(sql, "?")
	sql = sqlComment.ReplaceAllString(sql, "")
	if len(sql) > 4096 {
		sql = sql[:4096] + "…"
	}
	return sql
}

func (p *SQLTracing) before(op string) func(*gorm.DB) {
	return func(tx *gorm.DB) {
		if tx.Statement == nil {
			return
		}
		parent := tx.Statement.Context
		if parent == nil {
			parent = context.Background()
		}
		table := tx.Statement.Table
		if table == "" {
			table = "database"
		}
		ctx, span := otel.Tracer("lumo/gorm").
			Start(
				parent,
				"SQL "+op+" "+table,
				trace.WithSpanKind(trace.SpanKindClient),
				trace.WithAttributes(
					attribute.String("db.system.name", p.System),
					attribute.String("db.namespace", p.Namespace),
					attribute.String("db.operation.name", op),
					attribute.String("db.collection.name", table),
					attribute.String("code.layer", "database"),
				),
			)
		tx.InstanceSet("lumo:query-span", querySpan{span: span, parent: parent})
		tx.Statement.Context = ctx
	}
}
func (p *SQLTracing) after(tx *gorm.DB) {
	v, ok := tx.InstanceGet("lumo:query-span")
	if !ok {
		return
	}
	q := v.(querySpan)
	q.span.SetAttributes(
		attribute.String("db.query.text", SQLTemplate(tx.Statement.SQL.String())),
		attribute.Int64("db.response.returned_rows", tx.RowsAffected),
	)
	if tx.Error != nil && !errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		q.span.SetStatus(codes.Error, "database operation failed")
		q.span.SetAttributes(attribute.String("error.type", fmt.Sprintf("%T", tx.Error)))
	}
	q.span.End()
	tx.Statement.Context = q.parent
}
func (p *SQLTracing) Initialize(db *gorm.DB) error {
	type registration struct {
		name   string
		before func(string, func(*gorm.DB)) error
		after  func(string, func(*gorm.DB)) error
	}
	// Each callback pair wraps the actual GORM statement execution, preserving the
	// caller's repository span and restoring its context after the query.
	registrations := []registration{
		{"create", func(n string, f func(*gorm.DB)) error {
			return db.Callback().Create().Before("gorm:create").Register(n, f)
		}, func(n string, f func(*gorm.DB)) error {
			return db.Callback().Create().After("gorm:create").Register(n, f)
		}},
		{"query", func(n string, f func(*gorm.DB)) error {
			return db.Callback().Query().Before("gorm:query").Register(n, f)
		}, func(n string, f func(*gorm.DB)) error {
			return db.Callback().Query().After("gorm:query").Register(n, f)
		}},
		{"update", func(n string, f func(*gorm.DB)) error {
			return db.Callback().Update().Before("gorm:update").Register(n, f)
		}, func(n string, f func(*gorm.DB)) error {
			return db.Callback().Update().After("gorm:update").Register(n, f)
		}},
		{"delete", func(n string, f func(*gorm.DB)) error {
			return db.Callback().Delete().Before("gorm:delete").Register(n, f)
		}, func(n string, f func(*gorm.DB)) error {
			return db.Callback().Delete().After("gorm:delete").Register(n, f)
		}},
		{
			"raw",
			func(n string, f func(*gorm.DB)) error { return db.Callback().Raw().Before("gorm:raw").Register(n, f) },
			func(n string, f func(*gorm.DB)) error { return db.Callback().Raw().After("gorm:raw").Register(n, f) },
		},
		{
			"row",
			func(n string, f func(*gorm.DB)) error { return db.Callback().Row().Before("gorm:row").Register(n, f) },
			func(n string, f func(*gorm.DB)) error { return db.Callback().Row().After("gorm:row").Register(n, f) },
		},
	}
	for _, r := range registrations {
		op := strings.ToUpper(r.name)
		switch r.name {
		case "create":
			op = "INSERT"
		case "query", "row":
			op = "SELECT"
		case "raw":
			op = "EXECUTE"
		}
		if err := r.before("lumo:before_"+r.name, p.before(op)); err != nil {
			return err
		}
		if err := r.after("lumo:after_"+r.name, p.after); err != nil {
			return err
		}
	}
	return nil
}
