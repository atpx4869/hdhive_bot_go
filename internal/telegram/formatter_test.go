package telegram

import (
	"strings"
	"testing"
	"time"

	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

func TestMaskSecret(t *testing.T) {
	if got := MaskSecret("1234567890abcdef"); got != "✅ 已配置" {
		t.Fatalf("got %q", got)
	}
	if got := MaskSecret(""); got != "❌ 未配置" {
		t.Fatalf("got %q", got)
	}
}
func TestFormatUsersAndLogs(t *testing.T) {
	u := FormatUsers([]store.User{{ID: 42, Authorized: true, Note: "tester"}})
	if !strings.Contains(u, "<code>42</code>") || !strings.Contains(u, "🟩 已授权") || !strings.Contains(u, "tester") {
		t.Fatal(u)
	}
	l := FormatLogs([]store.ActivityLog{{UserID: 42, Action: "unlock", Detail: "r1", CreatedAt: time.Unix(0, 0)}})
	if !strings.Contains(l, "<code>42</code>") || !strings.Contains(l, "unlock") || !strings.Contains(l, "r1") {
		t.Fatal(l)
	}
}
func TestFormatResourceUnknownFee(t *testing.T) {
	if got := FormatResource(Resource{Title: "Movie"}); !strings.Contains(got, "<b>Movie</b>") {
		t.Fatal(got)
	}
}
