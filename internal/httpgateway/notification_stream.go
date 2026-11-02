package httpgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http"
	"strings"
	"sync"
	"time"
)

type streamLimits struct {
	sync.Mutex
	total  int
	owners map[string]int
}

func (g *gateway) notificationStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, r.Context(), status.Error(codes.InvalidArgument, "GET required"))
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || len(token) != 64 {
		writeError(w, r.Context(), status.Error(codes.Unauthenticated, "Bearer token required"))
		return
	}
	validate := func(parent context.Context) (platform.Result, error) {
		ctx, cancel := context.WithTimeout(parent, 3*time.Second)
		defer cancel()
		return g.rpc.Call(ctx, "iam", platform.Command{Action: "validate", Data: map[string]any{"token": token}})
	}
	principal, err := validate(r.Context())
	if err != nil {
		writeError(w, r.Context(), err)
		return
	}
	owner := platform.Text(principal, "user_id")
	if owner == "" {
		writeError(w, r.Context(), status.Error(codes.Unauthenticated, "invalid principal"))
		return
	}
	g.streams.Lock()
	if g.streams.total >= 256 || g.streams.owners[owner] >= 2 {
		g.streams.Unlock()
		writeError(w, r.Context(), status.Error(codes.ResourceExhausted, "消息连接过多，请稍后重试"))
		return
	}
	g.streams.total++
	g.streams.owners[owner]++
	g.streams.Unlock()
	defer func() {
		g.streams.Lock()
		g.streams.total--
		g.streams.owners[owner]--
		if g.streams.owners[owner] == 0 {
			delete(g.streams.owners, owner)
		}
		g.streams.Unlock()
	}()
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r.Context(), status.Error(codes.Unavailable, "stream unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = fmt.Fprint(w, "retry: 2000\n\n")
	flusher.Flush()
	streamCtx, cancelStream := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancelStream()
	type frame struct {
		body     string
		terminal bool
	}
	frames := make(chan frame, 1)
	send := func(body string, terminal bool) bool {
		select {
		case frames <- frame{body, terminal}:
			return true
		case <-streamCtx.Done():
			return false
		}
	}
	last := r.Header.Get("Last-Event-ID")
	var poll func()
	poll = func() {
		if streamCtx.Err() != nil {
			return
		}
		current, e := validate(streamCtx)
		if e != nil || platform.Text(current, "user_id") != owner {
			send("event: auth_required\ndata: {}\n\n", true)
			return
		}
		ctx, cancel := context.WithTimeout(streamCtx, 3*time.Second)
		value, e := g.rpc.Call(
			ctx,
			"notification",
			platform.Command{
				Action: "list",
				UserID: owner,
				Role:   platform.Text(current, "role"),
				Data:   map[string]any{"limit": 20},
			},
		)
		cancel()
		if e != nil {
			send("event: reconnect\ndata: {}\n\n", true)
			return
		}
		raw, _ := json.Marshal(value)
		sum := sha256.Sum256(raw)
		id := hex.EncodeToString(sum[:])
		body := ": heartbeat\n\n"
		if id != last {
			body = fmt.Sprintf("id: %s\nevent: notifications\ndata: %s\n\n", id, raw)
			last = id
		}
		if send(body, false) && streamCtx.Err() == nil {
			time.AfterFunc(2*time.Second, poll)
		}
	}
	go poll()
	for {
		select {
		case <-streamCtx.Done():
			return
		case item := <-frames:
			if _, err := fmt.Fprint(w, item.body); err != nil {
				return
			}
			flusher.Flush()
			if item.terminal {
				return
			}
		}
	}
}
