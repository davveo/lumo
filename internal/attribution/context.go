// Package attribution validates optional, browser-reported shopping context.
// It never supplies identities or financial facts.
package attribution

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var token = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,80}$`)
var mail = regexp.MustCompile(`[^\s@]+@[^\s@]+\.[^\s@]+`)
var digits = regexp.MustCompile(`[0-9]{7,}`)

type Context struct {
	SessionID string `json:"session_id"`
	VisitorID string `json:"visitor_id"`
	Source    string `json:"source"`
	Query     string `json:"query"`
}

func CleanQuery(s string) string {
	r := []rune(strings.TrimSpace(digits.ReplaceAllString(mail.ReplaceAllString(s, ""), "")))
	return string(r[:min(100, len(r))])
}
func (c Context) Valid() bool {
	return token.MatchString(c.SessionID) && token.MatchString(c.VisitorID)
}
func (c Context) Normalized() Context {
	c.Query = CleanQuery(c.Query)
	switch c.Source {
	case "direct", "search", "category", "recommendation", "campaign", "external":
	default:
		c.Source = "unknown"
	}
	return c
}
func (c Context) SessionKey() string {
	if !c.Valid() {
		return ""
	}
	h := sha256.Sum256([]byte(c.VisitorID + "\x00" + c.SessionID))
	return hex.EncodeToString(h[:])
}
