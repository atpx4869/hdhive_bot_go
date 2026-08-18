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
