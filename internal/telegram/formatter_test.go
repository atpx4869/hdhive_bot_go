package telegram

import (
	"strings"
	"testing"
	"time"

	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

func TestMaskSecret(t *testing.T) {
	if got := MaskSecret("1234567890abcdef"); got != "1234********cdef" {
		t.Fatalf("got %q", got)
	}
	if got := MaskSecret("short"); got != "*****" {
		t.Fatalf("got %q", got)
	}
}
func TestFormatUsersAndLogs(t *testing.T) {
	u := FormatUsers([]store.User{{ID: 42, Authorized: true, Note: "tester"}})
	if !strings.Contains(u, "42｜已授权｜tester") {
		t.Fatal(u)
	}
	l := FormatLogs([]store.ActivityLog{{UserID: 42, Action: "unlock", Detail: "r1", CreatedAt: time.Unix(0, 0)}})
	if !strings.Contains(l, "42｜unlock｜r1") {
		t.Fatal(l)
	}
}
func TestFormatResourceUnknownFee(t *testing.T) {
	if got := FormatResource(Resource{Title: "Movie"}); !strings.Contains(got, "费用：未知") {
		t.Fatal(got)
	}
}
