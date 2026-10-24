package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type Command struct {
	Action         string         `json:"action"`
	ID             string         `json:"id,omitempty"`
	UserID         string         `json:"user_id,omitempty"`
	Role           string         `json:"role,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
}
type Result = map[string]any
type Caller interface {
	Call(context.Context, string, Command) (Result, error)
}
type Dependencies struct {
	DB    *gorm.DB
	RPC   Caller
	Redis *redis.Client
}
type Module struct {
	Name    string
	Migrate func(context.Context) error
	Seed    func(context.Context) error
	Handle  func(context.Context, Command) (Result, error)
	// RegisterRPC installs explicit domain RPCs alongside the legacy Execute adapter.
	RegisterRPC func(grpc.ServiceRegistrar)
	RPCActions  map[string]string
	Topics      []string
	OnEvent     func(context.Context, Event) error
	Tick        func(context.Context) error
}
type Event struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	AggregateID string         `json:"aggregate_id"`
	UserID      string         `json:"user_id"`
	Data        map[string]any `json:"data"`
}

// ID creates a non-sequential business identifier. Randomness failure is returned
// by the operating system only under catastrophic host failure.
func ID(prefix string) string {
	var b [16]byte
	// Go's crypto/rand.Read fills the entire slice; the runtime terminates on an
	// unrecoverable entropy-source failure rather than returning weak IDs.
	_, _ = rand.Read(b[:])
	return prefix + "_" + hex.EncodeToString(b[:])
}
func Decode(data map[string]any, dst any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return status.Error(codes.InvalidArgument, "invalid payload")
	}
	if err = json.Unmarshal(b, dst); err != nil {
		return status.Error(codes.InvalidArgument, "invalid payload field types")
	}
	return nil
}
func Map(value any) Result {
	b, err := json.Marshal(value)
	if err != nil {
		return Result{}
	}
	var m Result
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if decoder.Decode(&m) != nil || m == nil {
		return Result{}
	}
	return m
}
func Text(m map[string]any, key string) string { s, _ := m[key].(string); return s }
func Int(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case int32:
		return int64(v)
	case float64:
		if v == float64(int64(v)) {
			return int64(v)
		}
	case json.Number:
		i, _ := v.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(v, 10, 64)
		return i
	}
	return 0
}
func RequireUser(c Command) error {
	if c.UserID == "" {
		return status.Error(codes.Unauthenticated, "authentication required")
	}
	return nil
}
func RequireAdmin(c Command) error {
	if err := RequireUser(c); err != nil {
		return err
	}
	if c.Role != "admin" {
		return status.Error(codes.PermissionDenied, "administrator required")
	}
	return nil
}
