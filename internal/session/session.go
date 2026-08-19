package session

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var (
	ErrCapacity        = errors.New("session capacity reached")
	ErrNotFound        = errors.New("session not found")
	ErrCallbackOwner   = errors.New("callback belongs to another user")
	ErrUnlockDuplicate = errors.New("unlock already pending or in flight")
)

type UnlockStatus string

const (
	UnlockPending  UnlockStatus = "pending"
	UnlockInFlight UnlockStatus = "in_flight"
	UnlockSuccess  UnlockStatus = "success"
	UnlockRejected UnlockStatus = "rejected"
	UnlockUnknown  UnlockStatus = "unknown"
)

type State struct {
	UserID    int64
	Kind      string
	Data      map[string]string
	Unlocks   map[string]UnlockStatus
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time
}

type Callback struct {
	UserID    int64
	Action    string
	Value     string
	ExpiresAt time.Time
}

type Manager struct {
	mu        sync.Mutex
	ttl       time.Duration
	max       int
	now       func() time.Time
	sessions  map[int64]State
	callbacks map[string]Callback
}

func New(ttl time.Duration, max int) *Manager {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	if max <= 0 {
		max = 1000
	}
	return &Manager{ttl: ttl, max: max, now: time.Now, sessions: make(map[int64]State), callbacks: make(map[string]Callback)}
}

func (m *Manager) Set(userID int64, kind string, data map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	if _, exists := m.sessions[userID]; !exists && len(m.sessions) >= m.max {
		return ErrCapacity
	}
	now := m.now()
	created := now
	unlocks := make(map[string]UnlockStatus)
	if old, ok := m.sessions[userID]; ok {
		created, unlocks = old.CreatedAt, cloneUnlocks(old.Unlocks)
	}
	m.sessions[userID] = State{UserID: userID, Kind: kind, Data: cloneData(data), Unlocks: unlocks, CreatedAt: created, UpdatedAt: now, ExpiresAt: now.Add(m.ttl)}
	return nil
}

func (m *Manager) Get(userID int64) (State, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	s, ok := m.sessions[userID]
	if !ok {
		return State{}, false
	}
	s.Data, s.Unlocks = cloneData(s.Data), cloneUnlocks(s.Unlocks)
	return s, true
}

func (m *Manager) ClearInteraction(userID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[userID]
	if !ok {
		return
	}
	s.Kind = ""
	s.Data = make(map[string]string)
	now := m.now()
	s.UpdatedAt, s.ExpiresAt = now, now.Add(m.ttl)
	m.sessions[userID] = s
}

func (m *Manager) Delete(userID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, userID)
}

func (m *Manager) BindCallback(userID int64, action, value string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	for i := 0; i < 4; i++ {
		var raw [12]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", err
		}
		token := base64.RawURLEncoding.EncodeToString(raw[:])
		if _, exists := m.callbacks[token]; !exists {
			m.callbacks[token] = Callback{UserID: userID, Action: action, Value: value, ExpiresAt: m.now().Add(m.ttl)}
			return token, nil
		}
	}
	return "", errors.New("could not allocate callback token")
}

func (m *Manager) ResolveCallback(token string, userID int64) (Callback, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	cb, ok := m.callbacks[token]
	if !ok {
		return Callback{}, ErrNotFound
	}
	if cb.UserID != userID {
		return Callback{}, ErrCallbackOwner
	}
	return cb, nil
}

func (m *Manager) DeleteCallback(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.callbacks, token)
}

func (m *Manager) BeginUnlock(userID int64, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	now := m.now()
	s, ok := m.sessions[userID]
	if !ok {
		if len(m.sessions) >= m.max {
			return ErrCapacity
		}
		s = State{UserID: userID, CreatedAt: now, Data: make(map[string]string), Unlocks: make(map[string]UnlockStatus)}
	}
	if s.Unlocks == nil {
		s.Unlocks = make(map[string]UnlockStatus)
	}
	if status := s.Unlocks[key]; status == UnlockPending || status == UnlockInFlight || status == UnlockSuccess || status == UnlockUnknown {
		return ErrUnlockDuplicate
	}
	s.Unlocks[key] = UnlockPending
	s.UpdatedAt, s.ExpiresAt = now, now.Add(m.ttl)
	m.sessions[userID] = s
	return nil
}

func (m *Manager) TransitionUnlock(userID int64, key string, from, to UnlockStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	s, ok := m.sessions[userID]
	if !ok {
		return ErrNotFound
	}
	if s.Unlocks[key] != from {
		return ErrUnlockDuplicate
	}
	s.Unlocks[key] = to
	now := m.now()
	s.UpdatedAt, s.ExpiresAt = now, now.Add(m.ttl)
	m.sessions[userID] = s
	return nil
}

func (m *Manager) SetUnlockStatus(userID int64, key string, status UnlockStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	s, ok := m.sessions[userID]
	if !ok {
		return ErrNotFound
	}
	if s.Unlocks == nil {
		s.Unlocks = make(map[string]UnlockStatus)
	}
	s.Unlocks[key] = status
	now := m.now()
	s.UpdatedAt, s.ExpiresAt = now, now.Add(m.ttl)
	m.sessions[userID] = s
	return nil
}

func (m *Manager) ResetUnlock(userID int64, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[userID]
	if !ok || s.Unlocks == nil {
		return
	}
	delete(s.Unlocks, key)
	now := m.now()
	s.UpdatedAt, s.ExpiresAt = now, now.Add(m.ttl)
	m.sessions[userID] = s
}

// CleanupExpiredUnlocks 清理过期的 in_flight 状态
// 返回被清理的 (userID, key) 列表
func (m *Manager) CleanupExpiredUnlocks(maxInFlightDuration time.Duration) []struct {
	UserID int64
	Key    string
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 不调用 cleanupLocked()，因为我们需要检查可能已过期的 session 中的 unlock 状态

	var cleaned []struct {
		UserID int64
		Key    string
	}
	now := m.now()
	for userID, s := range m.sessions {
		if s.Unlocks == nil {
			continue
		}
		for key, status := range s.Unlocks {
			// 只清理 in_flight 状态，且超过最大持续时间
			if status == UnlockInFlight && now.Sub(s.UpdatedAt) > maxInFlightDuration {
				delete(s.Unlocks, key)
				cleaned = append(cleaned, struct {
					UserID int64
					Key    string
				}{userID, key})
			}
		}
		if len(s.Unlocks) == 0 {
			s.Unlocks = nil
		}
		// 更新 session 时间以保持活跃
		s.UpdatedAt = now
		s.ExpiresAt = now.Add(m.ttl)
		m.sessions[userID] = s
	}
	return cleaned
}

// GetUnlockWithTimestamp 获取解锁状态及其更新时间
func (m *Manager) GetUnlockWithTimestamp(userID int64, key string) (UnlockStatus, time.Time, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	s, ok := m.sessions[userID]
	if !ok || s.Unlocks == nil {
		return "", time.Time{}, false
	}
	status, exists := s.Unlocks[key]
	if !exists {
		return "", time.Time{}, false
	}
	return status, s.UpdatedAt, true
}

func (m *Manager) UnlockStatus(userID int64, key string) UnlockStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	if s, ok := m.sessions[userID]; ok {
		return s.Unlocks[key]
	}
	return ""
}

func (m *Manager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	return len(m.sessions)
}

func (m *Manager) cleanupLocked() {
	now := m.now()
	for id, s := range m.sessions {
		if !s.ExpiresAt.After(now) {
			delete(m.sessions, id)
		}
	}
	for token, cb := range m.callbacks {
		if !cb.ExpiresAt.After(now) {
			delete(m.callbacks, token)
		}
	}
}

func cloneData(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneUnlocks(in map[string]UnlockStatus) map[string]UnlockStatus {
	out := make(map[string]UnlockStatus, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
