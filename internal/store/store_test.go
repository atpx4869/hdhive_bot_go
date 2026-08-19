package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	appcrypto "github.com/atpx4869/hdhive_bot_go/internal/crypto"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	cipher, err := appcrypto.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	dsn := fmt.Sprintf("file:test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	store, err := Open(context.Background(), dsn, cipher)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestMigrationsAndUserCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, table := range []string{"users", "p115_accounts", "activity_logs"} {
		var name string
		if err := s.db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("migration missing table %s: %v", table, err)
		}
	}
	var foreignKeys int
	if err := s.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign keys not enabled: value=%d err=%v", foreignKeys, err)
	}

	if _, err := s.GetUser(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetUser() missing error = %v", err)
	}
	if err := s.SetUserAuthorization(ctx, 42, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserNote(ctx, 42, "trusted user"); err != nil {
		t.Fatal(err)
	}
	user, err := s.GetUser(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	if !user.Authorized || user.Note != "trusted user" || user.ID != 42 {
		t.Fatalf("unexpected user: %#v", user)
	}
	if err := s.SetUserAuthorization(ctx, 42, false); err != nil {
		t.Fatal(err)
	}
	user, err = s.GetUser(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	if user.Authorized {
		t.Fatal("authorization was not revoked")
	}
}

func TestP115ConfigCRUDEncryptedAtRest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := P115Config{Cookie: "UID=secret-cookie", TargetCID: "0", Enabled: true}
	if err := s.SetP115Config(ctx, 7, first); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := s.db.QueryRowContext(ctx, `SELECT encrypted_config FROM p115_accounts WHERE user_id = 7`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(first.Cookie)) {
		t.Fatal("p115 cookie was stored in plaintext")
	}
	got, err := s.GetP115Config(ctx, 7)
	if err != nil || got != first {
		t.Fatalf("GetP115Config() = %#v, %v", got, err)
	}

	updated := P115Config{Cookie: "UID=new-secret", TargetCID: "0", Enabled: true}
	if err := s.SetP115Config(ctx, 7, updated); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetP115Config(ctx, 7)
	if err != nil || got != updated {
		t.Fatalf("updated GetP115Config() = %#v, %v", got, err)
	}
	if _, err := s.GetP115Config(ctx, 8); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing GetP115Config() error = %v", err)
	}
	if err := s.DisableP115Config(ctx, 7); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetP115Config(ctx, 7)
	if err != nil || got.Enabled {
		t.Fatalf("disabled GetP115Config() = %#v, %v", got, err)
	}
	if err := s.DisableP115Config(ctx, 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second DisableP115Config() error = %v", err)
	}
	if err := s.DeleteP115Config(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetP115Config(ctx, 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted GetP115Config() error = %v", err)
	}
	if err := s.DeleteP115Config(ctx, 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second DeleteP115Config() error = %v", err)
	}
}

func TestP115ConfigBoundToUserID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.SetP115Config(ctx, 7, P115Config{Cookie: "secret", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.ensureUser(ctx, 8); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE p115_accounts SET user_id = 8 WHERE user_id = 7`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetP115Config(ctx, 8); err == nil {
		t.Fatal("GetP115Config() decrypted data under a different user ID")
	}
}

func TestResetUnlockRecordOnlyAllowsUnknown(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.SetUnlockRecord(ctx, UnlockRecord{UserID: 7, ResourceID: "unknown", Status: "unknown"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetUnlockRecord(ctx, 7, "unknown"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetUnlockRecord(ctx, 7, "unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown record was not reset: %v", err)
	}
	for _, status := range []string{"in_flight", "success"} {
		if err := s.SetUnlockRecord(ctx, UnlockRecord{UserID: 7, ResourceID: status, Status: status, Result: map[bool][]byte{true: []byte("ok"), false: nil}[status == "success"]}); err != nil {
			t.Fatal(err)
		}
		if err := s.ResetUnlockRecord(ctx, 7, status); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s record should not reset: %v", status, err)
		}
	}
}

func TestActivityLogQuery(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	current := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s.nowUTC = func() time.Time {
		value := current
		current = current.Add(time.Second)
		return value
	}

	entries := []struct {
		userID int64
		action string
		detail string
	}{{1, "authorize", "granted"}, {2, "authorize", "granted"}, {1, "config", "updated"}}
	for _, entry := range entries {
		if _, err := s.AddActivityLog(ctx, entry.userID, entry.action, entry.detail); err != nil {
			t.Fatal(err)
		}
	}

	userID := int64(1)
	logs, err := s.QueryActivityLogs(ctx, ActivityQuery{UserID: &userID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[0].Action != "config" || logs[1].Action != "authorize" {
		t.Fatalf("unexpected user logs: %#v", logs)
	}
	logs, err = s.QueryActivityLogs(ctx, ActivityQuery{Action: "authorize", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].UserID != 1 {
		t.Fatalf("unexpected paged logs: %#v", logs)
	}
	since := time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC)
	logs, err = s.QueryActivityLogs(ctx, ActivityQuery{Since: &since})
	if err != nil || len(logs) != 2 {
		t.Fatalf("since query = %#v, %v", logs, err)
	}
	if _, err := s.QueryActivityLogs(ctx, ActivityQuery{Limit: 501}); err == nil {
		t.Fatal("QueryActivityLogs() accepted an excessive limit")
	}
}

func TestConcurrentClaimUnlock(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 并发 claim 同一个资源
	const goroutines = 10
	results := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			claimed, err := s.ClaimUnlock(ctx, 1, "resource-1")
			if err != nil {
				results <- false
				return
			}
			results <- claimed
		}()
	}

	// 统计结果
	claimedCount := 0
	for i := 0; i < goroutines; i++ {
		if <-results {
			claimedCount++
		}
	}

	// 只有一个 goroutine 应该成功 claim
	if claimedCount != 1 {
		t.Fatalf("expected exactly 1 claim success, got %d", claimedCount)
	}
}

func TestClaimUnlock_DifferentResources(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 不同资源应该都能 claim
	claimed1, err := s.ClaimUnlock(ctx, 1, "resource-1")
	if err != nil || !claimed1 {
		t.Fatalf("first claim failed: claimed=%v, err=%v", claimed1, err)
	}

	claimed2, err := s.ClaimUnlock(ctx, 1, "resource-2")
	if err != nil || !claimed2 {
		t.Fatalf("second claim failed: claimed=%v, err=%v", claimed2, err)
	}
}

func TestClaimUnlock_DifferentUsers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 不同用户 claim 同一资源应该都能成功
	claimed1, err := s.ClaimUnlock(ctx, 1, "resource-1")
	if err != nil || !claimed1 {
		t.Fatalf("user 1 claim failed: claimed=%v, err=%v", claimed1, err)
	}

	claimed2, err := s.ClaimUnlock(ctx, 2, "resource-1")
	if err != nil || !claimed2 {
		t.Fatalf("user 2 claim failed: claimed=%v, err=%v", claimed2, err)
	}
}

func TestClaimUnlock_AlreadyClaimed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 第一次 claim
	claimed1, err := s.ClaimUnlock(ctx, 1, "resource-1")
	if err != nil || !claimed1 {
		t.Fatalf("first claim failed: claimed=%v, err=%v", claimed1, err)
	}

	// 同一用户再次 claim 应该失败
	claimed2, err := s.ClaimUnlock(ctx, 1, "resource-1")
	if err != nil {
		t.Fatalf("second claim error: %v", err)
	}
	if claimed2 {
		t.Fatal("second claim should fail")
	}
}

func TestResetUnlockRecord_OnlyUnknown(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 设置 unknown 状态
	if err := s.SetUnlockRecord(ctx, UnlockRecord{UserID: 1, ResourceID: "r1", Status: "unknown"}); err != nil {
		t.Fatal(err)
	}

	// 应该能重置 unknown
	if err := s.ResetUnlockRecord(ctx, 1, "r1"); err != nil {
		t.Fatalf("reset unknown failed: %v", err)
	}

	// 设置 in_flight 状态
	if err := s.SetUnlockRecord(ctx, UnlockRecord{UserID: 1, ResourceID: "r2", Status: "in_flight"}); err != nil {
		t.Fatal(err)
	}

	// 不应该能重置 in_flight
	if err := s.ResetUnlockRecord(ctx, 1, "r2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reset in_flight should fail with ErrNotFound, got: %v", err)
	}

	// 设置 success 状态
	if err := s.SetUnlockRecord(ctx, UnlockRecord{UserID: 1, ResourceID: "r3", Status: "success", Result: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}

	// 不应该能重置 success
	if err := s.ResetUnlockRecord(ctx, 1, "r3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reset success should fail with ErrNotFound, got: %v", err)
	}
}
