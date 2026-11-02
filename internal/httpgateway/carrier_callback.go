package httpgateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

func (g *gateway) carrierCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, r.Context(), status.Error(codes.InvalidArgument, "POST required"))
		return
	}
	key := os.Getenv("LOGISTICS_CARRIER_CALLBACK_KEY")
	if len(key) < 32 {
		writeError(w, r.Context(), status.Error(codes.FailedPrecondition, "carrier callback is not configured"))
		return
	}
	stamp := r.Header.Get("X-Lumo-Timestamp")
	seconds, e := strconv.ParseInt(stamp, 10, 64)
	now := time.Now().Unix()
	if e != nil || seconds < now-300 || seconds > now+300 {
		writeError(w, r.Context(), status.Error(codes.Unauthenticated, "callback timestamp invalid"))
		return
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if e != nil {
		writeError(w, r.Context(), status.Error(codes.InvalidArgument, "callback body too large"))
		return
	}
	signature, e := hex.DecodeString(r.Header.Get("X-Lumo-Signature"))
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(stamp + "."))
	mac.Write(body)
	if e != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		writeError(w, r.Context(), status.Error(codes.Unauthenticated, "callback signature invalid"))
		return
	}
	var data struct {
		EventID     string `json:"event_id"`
		TrackingID  string `json:"tracking_id"`
		Provider    string `json:"provider"`
		Status      string `json:"status"`
		Location    string `json:"location"`
		Description string `json:"description"`
		OccurredAt  string `json:"occurred_at"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&data); e != nil {
		writeError(w, r.Context(), status.Error(codes.InvalidArgument, "invalid callback payload"))
		return
	}
	if e = decoder.Decode(&struct{}{}); e != io.EOF {
		writeError(w, r.Context(), status.Error(codes.InvalidArgument, "trailing callback content"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	out, e := g.rpc.Call(
		ctx,
		"logistics",
		platform.Command{
			Action: "tracking_update",
			ID:     data.EventID,
			UserID: "carrier",
			Role:   "system",
			Data: map[string]any{
				"tracking_id": data.TrackingID,
				"provider":    data.Provider,
				"status":      data.Status,
				"location":    data.Location,
				"description": data.Description,
				"occurred_at": data.OccurredAt,
				"_source":     "carrier",
			},
		},
	)
	if e != nil {
		writeError(w, r.Context(), e)
		return
	}
	writeJSON(w, r.Context(), http.StatusOK, map[string]any{"data": out})
}
