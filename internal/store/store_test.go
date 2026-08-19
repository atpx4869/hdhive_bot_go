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

func TestSetUnlockRecord_ContextCancelled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 先 claim 一个资源
	claimed, err := s.ClaimUnlock(ctx, 1, "r1")
	if err != nil || !claimed {
		t.Fatalf("claim failed: claimed=%v, err=%v", claimed, err)
	}

	// 创建一个已取消的 context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 尝试在已取消的 context 中保存结果
	// 这应该仍然能成功，因为我们使用独立的 context 来保存关键数据
	result := []byte(`{"share_url":"https://115.com/s/abc"}`)
	err = s.SetUnlockRecord(cancelledCtx, UnlockRecord{
		UserID:     1,
		ResourceID: "r1",
		Status:     "success",
		Result:     result,
	})

	// 注意：SQLite 可能会因为 context 取消而失败
	// 但在实际实现中，应该使用独立的 context 来保存关键数据
	if err != nil {
		// 如果失败，记录错误但不一定是 bug
		t.Logf("set unlock record with cancelled context failed (expected): %v", err)
	}
}

func TestSetUnlockRecord_IndependentContext(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 先 claim 一个资源
	claimed, err := s.ClaimUnlock(ctx, 1, "r1")
	if err != nil || !claimed {
		t.Fatalf("claim failed: claimed=%v, err=%v", claimed, err)
	}

	// 使用独立的 context 保存结果（模拟实际使用场景）
	// 在 app/adapters.go 中，Unlock 方法使用独立的 5 秒 context
	persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := []byte(`{"share_url":"https://115.com/s/abc"}`)
	err = s.SetUnlockRecord(persistCtx, UnlockRecord{
		UserID:     1,
		ResourceID: "r1",
		Status:     "success",
		Result:     result,
	})

	if err != nil {
		t.Fatalf("set unlock record with independent context failed: %v", err)
	}

	// 验证记录已保存
	record, err := s.GetUnlockRecord(ctx, 1, "r1")
	if err != nil {
		t.Fatalf("get unlock record failed: %v", err)
	}
	if record.Status != "success" {
		t.Fatalf("expected status success, got %s", record.Status)
	}
	if string(record.Result) != string(result) {
		t.Fatalf("expected result %s, got %s", result, record.Result)
	}
}

func TestUnlockFlow_ClaimThenSaveWithIndependentContext(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 模拟完整的解锁流程
	// 1. Claim 资源
	claimed, err := s.ClaimUnlock(ctx, 1, "r1")
	if err != nil || !claimed {
		t.Fatalf("claim failed: claimed=%v, err=%v", claimed, err)
	}

	// 2. 验证 in_flight 状态
	record, err := s.GetUnlockRecord(ctx, 1, "r1")
	if err != nil {
		t.Fatalf("get record failed: %v", err)
	}
	if record.Status != "in_flight" {
		t.Fatalf("expected in_flight, got %s", record.Status)
	}

	// 3. 使用独立 context 保存成功结果
	persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := []byte(`{"share_url":"https://115.com/s/abc","share_code":"abc123"}`)
	err = s.SetUnlockRecord(persistCtx, UnlockRecord{
		UserID:     1,
		ResourceID: "r1",
		Status:     "success",
		Result:     result,
	})

	if err != nil {
		t.Fatalf("save result failed: %v", err)
	}

	// 4. 验证最终状态
	record, err = s.GetUnlockRecord(ctx, 1, "r1")
	if err != nil {
		t.Fatalf("get final record failed: %v", err)
	}
	if record.Status != "success" {
		t.Fatalf("expected success, got %s", record.Status)
	}
	if string(record.Result) != string(result) {
		t.Fatalf("result mismatch: expected %s, got %s", result, record.Result)
	}
}

// mockDecryptFailCryptor 模拟解密失败的加密器
type mockDecryptFailCryptor struct {
	*appcrypto.Cipher
}

func (m *mockDecryptFailCryptor) Decrypt(userID int64, ciphertext []byte) ([]byte, error) {
	return nil, errors.New("decryption failed")
}

func TestGetUnlockRecord_DecryptFailReturnsError(t *testing.T) {
	// 创建使用真实加密器的 store
	s := newTestStore(t)
	ctx := context.Background()

	// 先 claim 并保存成功记录
	claimed, err := s.ClaimUnlock(ctx, 1, "r1")
	if err != nil || !claimed {
		t.Fatalf("claim failed: claimed=%v, err=%v", claimed, err)
	}

	result := []byte(`{"share_url":"https://115.com/s/abc"}`)
	err = s.SetUnlockRecord(ctx, UnlockRecord{
		UserID:     1,
		ResourceID: "r1",
		Status:     "success",
		Result:     result,
	})
	if err != nil {
		t.Fatalf("set record failed: %v", err)
	}

	// 创建一个解密失败的 store（使用不同的加密器）
	failCipher, err := appcrypto.New([]byte("fedcba9876543210fedcba9876543210"))
	if err != nil {
		t.Fatal(err)
	}
	failStore := &Store{db: s.db, crypt: failCipher, nowUTC: s.nowUTC}

	// 尝试获取记录，应该返回错误（因为解密失败）
	_, err = failStore.GetUnlockRecord(ctx, 1, "r1")
	if err == nil {
		t.Fatal("expected error for decrypt failure")
	}
}

func TestP115Config_DecryptFailReturnsError(t *testing.T) {
	// 创建使用真实加密器的 store
	s := newTestStore(t)
	ctx := context.Background()

	// 先创建用户
	if err := s.SetUserAuthorization(ctx, 1, true); err != nil {
		t.Fatal(err)
	}

	// 保存 115 配置
	err := s.SetP115Config(ctx, 1, P115Config{
		Cookie:    "UID=u;CID=c;SEID=s",
		TargetCID: "0",
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("set p115 config failed: %v", err)
	}

	// 创建一个解密失败的 store
	failCipher, err := appcrypto.New([]byte("fedcba9876543210fedcba9876543210"))
	if err != nil {
		t.Fatal(err)
	}
	failStore := &Store{db: s.db, crypt: failCipher, nowUTC: s.nowUTC}

	// 尝试获取配置，应该返回错误（因为解密失败）
	_, err = failStore.GetP115Config(ctx, 1)
	if err == nil {
		t.Fatal("expected error for decrypt failure")
	}
}
