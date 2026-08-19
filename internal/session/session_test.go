package session

import (
	"errors"
	"testing"
	"time"
)

func TestManagerTTLAndCapacity(t *testing.T) {
	m := New(time.Minute, 1)
	now := time.Unix(100, 0)
	m.now = func() time.Time { return now }
	if err := m.Set(1, "x", map[string]string{"a": "b"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(2, "y", nil); !errors.Is(err, ErrCapacity) {
		t.Fatalf("want capacity, got %v", err)
	}
	now = now.Add(2 * time.Minute)
	if _, ok := m.Get(1); ok {
		t.Fatal("expired session remains")
	}
	if err := m.Set(2, "y", nil); err != nil {
		t.Fatal(err)
	}
}

func TestCallbackBoundToUser(t *testing.T) {
	m := New(time.Minute, 10)
	token, err := m.BindCallback(7, "detail", "r1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.ResolveCallback(token, 8); !errors.Is(err, ErrCallbackOwner) {
		t.Fatalf("want owner error, got %v", err)
	}
	cb, err := m.ResolveCallback(token, 7)
	if err != nil || cb.Value != "r1" {
		t.Fatalf("bad callback: %+v %v", cb, err)
	}
}

func TestResetUnlockAllowsNewAttempt(t *testing.T) {
	m := New(time.Minute, 10)
	if err := m.BeginUnlock(1, "r"); err != nil {
		t.Fatal(err)
	}
	if err := m.TransitionUnlock(1, "r", UnlockPending, UnlockInFlight); err != nil {
		t.Fatal(err)
	}
	m.ResetUnlock(1, "r")
	if err := m.BeginUnlock(1, "r"); err != nil {
		t.Fatalf("reset should allow a new guarded attempt: %v", err)
	}
}

func TestUnlockDuplicateAndStatuses(t *testing.T) {
	m := New(time.Minute, 10)
	if err := m.BeginUnlock(1, "r"); err != nil {
		t.Fatal(err)
	}
	if got := m.UnlockStatus(1, "r"); got != UnlockPending {
		t.Fatalf("got %s", got)
	}
	if err := m.BeginUnlock(1, "r"); !errors.Is(err, ErrUnlockDuplicate) {
		t.Fatalf("want duplicate, got %v", err)
	}
	if err := m.TransitionUnlock(1, "r", UnlockPending, UnlockInFlight); err != nil {
		t.Fatal(err)
	}
	if err := m.TransitionUnlock(1, "r", UnlockPending, UnlockInFlight); !errors.Is(err, ErrUnlockDuplicate) {
		t.Fatalf("want atomic transition rejection, got %v", err)
	}
	if err := m.SetUnlockStatus(1, "r", UnlockRejected); err != nil {
		t.Fatal(err)
	}
	if err := m.BeginUnlock(1, "r"); err != nil {
		t.Fatalf("rejected should retry: %v", err)
	}
	for _, status := range []UnlockStatus{UnlockInFlight, UnlockSuccess, UnlockUnknown} {
		if err := m.SetUnlockStatus(1, "r", status); err != nil {
			t.Fatal(err)
		}
		if got := m.UnlockStatus(1, "r"); got != status {
			t.Fatalf("want %s got %s", status, got)
		}
	}
}

func TestCleanupExpiredUnlocks(t *testing.T) {
	m := New(time.Minute, 10)
	now := time.Unix(100, 0)
	m.now = func() time.Time { return now }

	// 设置 in_flight 状态
	if err := m.BeginUnlock(1, "r1"); err != nil {
		t.Fatal(err)
	}
	if err := m.TransitionUnlock(1, "r1", UnlockPending, UnlockInFlight); err != nil {
		t.Fatal(err)
	}

	// 设置另一个 in_flight 状态
	if err := m.BeginUnlock(1, "r2"); err != nil {
		t.Fatal(err)
	}
	if err := m.TransitionUnlock(1, "r2", UnlockPending, UnlockInFlight); err != nil {
		t.Fatal(err)
	}

	// 设置 success 状态（不应该被清理）
	if err := m.BeginUnlock(1, "r3"); err != nil {
		t.Fatal(err)
	}
	if err := m.TransitionUnlock(1, "r3", UnlockPending, UnlockInFlight); err != nil {
		t.Fatal(err)
	}
	if err := m.SetUnlockStatus(1, "r3", UnlockSuccess); err != nil {
		t.Fatal(err)
	}

	// 在超时前清理，应该没有变化
	cleaned := m.CleanupExpiredUnlocks(5 * time.Minute)
	if len(cleaned) != 0 {
		t.Fatalf("expected 0 cleaned, got %d", len(cleaned))
	}

	// 推进时间到超时后（保持 session 活跃）
	now = now.Add(6 * time.Minute)
	// 直接更新 session 的 UpdatedAt 以模拟时间流逝
	m.mu.Lock()
	if s, ok := m.sessions[1]; ok {
		s.UpdatedAt = now.Add(-6 * time.Minute)
		m.sessions[1] = s
	}
	m.mu.Unlock()

	// 清理过期的 in_flight
	cleaned = m.CleanupExpiredUnlocks(5 * time.Minute)
	if len(cleaned) != 2 {
		t.Fatalf("expected 2 cleaned, got %d", len(cleaned))
	}

	// 验证 in_flight 被清理
	if got := m.UnlockStatus(1, "r1"); got != "" {
		t.Fatalf("r1 should be cleaned, got %s", got)
	}
	if got := m.UnlockStatus(1, "r2"); got != "" {
		t.Fatalf("r2 should be cleaned, got %s", got)
	}

	// 验证 success 未被清理
	if got := m.UnlockStatus(1, "r3"); got != UnlockSuccess {
		t.Fatalf("r3 should remain success, got %s", got)
	}
}

func TestGetUnlockWithTimestamp(t *testing.T) {
	m := New(time.Minute, 10)
	now := time.Unix(100, 0)
	m.now = func() time.Time { return now }

	// 设置解锁状态
	if err := m.BeginUnlock(1, "r1"); err != nil {
		t.Fatal(err)
	}

	// 获取状态和时间
	status, ts, ok := m.GetUnlockWithTimestamp(1, "r1")
	if !ok {
		t.Fatal("expected found")
	}
	if status != UnlockPending {
		t.Fatalf("expected pending, got %s", status)
	}
	if !ts.Equal(now) {
		t.Fatalf("expected time %v, got %v", now, ts)
	}

	// 不存在的记录
	_, _, ok = m.GetUnlockWithTimestamp(1, "nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestCleanupExpiredUnlocks_MultipleUsers(t *testing.T) {
	m := New(time.Minute, 10)
	now := time.Unix(100, 0)
	m.now = func() time.Time { return now }

	// 用户1的 in_flight
	if err := m.BeginUnlock(1, "r1"); err != nil {
		t.Fatal(err)
	}
	m.TransitionUnlock(1, "r1", UnlockPending, UnlockInFlight)

	// 用户2的 in_flight
	if err := m.BeginUnlock(2, "r1"); err != nil {
		t.Fatal(err)
	}
	m.TransitionUnlock(2, "r1", UnlockPending, UnlockInFlight)

	// 推进时间
	now = now.Add(6 * time.Minute)

	// 清理
	cleaned := m.CleanupExpiredUnlocks(5 * time.Minute)
	if len(cleaned) != 2 {
		t.Fatalf("expected 2 cleaned, got %d", len(cleaned))
	}

	// 验证两个用户的状态都被清理
	if got := m.UnlockStatus(1, "r1"); got != "" {
		t.Fatalf("user 1 r1 should be cleaned, got %s", got)
	}
	if got := m.UnlockStatus(2, "r1"); got != "" {
		t.Fatalf("user 2 r1 should be cleaned, got %s", got)
	}
}
