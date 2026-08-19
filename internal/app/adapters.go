package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/atpx4869/hdhive_bot_go/internal/hdhive"
	"github.com/atpx4869/hdhive_bot_go/internal/p115"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
	"github.com/atpx4869/hdhive_bot_go/internal/telegram"
	"github.com/atpx4869/hdhive_bot_go/internal/tmdb"
)

type TMDBAdapter struct{ Client *tmdb.Client }

func (a TMDBAdapter) Search(ctx context.Context, query string, page int) ([]telegram.TMDBItem, int, error) {
	result, err := a.Client.Search(ctx, query, tmdb.SearchOptions{Page: page, Language: "zh-CN"})
	if err != nil {
		return nil, 0, err
	}
	items := make([]telegram.TMDBItem, 0, len(result.Results))
	for _, item := range result.Results {
		title, original, date := item.Title, item.OriginalTitle, item.ReleaseDate
		if item.MediaType == "tv" {
			title, original, date = item.Name, item.OriginalName, item.FirstAirDate
		}
		items = append(items, telegram.TMDBItem{ID: item.ID, MediaType: item.MediaType, Title: title, OriginalTitle: original, ReleaseDate: date, Overview: item.Overview, VoteAverage: item.VoteAverage})
	}
	return items, result.TotalPages, nil
}

type cacheEntry[T any] struct {
	value     T
	expiresAt time.Time
}

type HDHiveAdapter struct {
	Client    *hdhive.Client
	Store     *store.Store
	mu        sync.RWMutex
	resources map[string]cacheEntry[telegram.Resource]
	unlocked  map[int64]map[string]cacheEntry[telegram.Resource]
	cacheTTL  time.Duration
}

func NewHDHiveAdapter(client *hdhive.Client, db *store.Store) *HDHiveAdapter {
	return &HDHiveAdapter{
		Client:    client,
		Store:     db,
		resources: make(map[string]cacheEntry[telegram.Resource]),
		unlocked:  make(map[int64]map[string]cacheEntry[telegram.Resource]),
		cacheTTL:  30 * time.Minute,
	}
}

func (a *HDHiveAdapter) cacheResource(r telegram.Resource) {
	a.resources[r.ID] = cacheEntry[telegram.Resource]{value: r, expiresAt: time.Now().Add(a.cacheTTL)}
}

func (a *HDHiveAdapter) getCachedResource(id string) (telegram.Resource, bool) {
	e, ok := a.resources[id]
	if !ok || time.Now().After(e.expiresAt) {
		return telegram.Resource{}, false
	}
	return e.value, true
}

func (a *HDHiveAdapter) cacheUnlocked(userID int64, r telegram.Resource) {
	if a.unlocked[userID] == nil {
		a.unlocked[userID] = make(map[string]cacheEntry[telegram.Resource])
	}
	a.unlocked[userID][r.ID] = cacheEntry[telegram.Resource]{value: r, expiresAt: time.Now().Add(a.cacheTTL)}
}

func (a *HDHiveAdapter) getCachedUnlocked(userID int64, id string) (telegram.Resource, bool) {
	perUser := a.unlocked[userID]
	if perUser == nil {
		return telegram.Resource{}, false
	}
	e, ok := perUser[id]
	if !ok || time.Now().After(e.expiresAt) {
		return telegram.Resource{}, false
	}
	return e.value, true
}

// evictExpired removes expired cache entries. Should be called with mu held.
func (a *HDHiveAdapter) evictExpired() {
	now := time.Now()
	for k, e := range a.resources {
		if now.After(e.expiresAt) {
			delete(a.resources, k)
		}
	}
	for uid, perUser := range a.unlocked {
		for k, e := range perUser {
			if now.After(e.expiresAt) {
				delete(perUser, k)
			}
		}
		if len(perUser) == 0 {
			delete(a.unlocked, uid)
		}
	}
}
func (a *HDHiveAdapter) Search(ctx context.Context, item telegram.TMDBItem, page int) (telegram.ResourcePage, error) {
	raw, err := a.Client.Resources(ctx, item.MediaType, item.ID)
	if err != nil {
		return telegram.ResourcePage{}, err
	}
	all := make([]telegram.Resource, 0, len(raw))
	a.mu.Lock()
	defer a.mu.Unlock()
	a.evictExpired()
	for i, value := range raw {
		r := resourceFromMap(value, item.ID, i)
		all = append(all, r)
		a.cacheResource(r)
	}
	const pageSize = 6
	if page < 1 {
		page = 1
	}
	total := (len(all) + pageSize - 1) / pageSize
	if total < 1 {
		total = 1
	}
	if page > total {
		page = total
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(all) {
		start = len(all)
	}
	if end > len(all) {
		end = len(all)
	}
	return telegram.ResourcePage{Items: all[start:end], Page: page, TotalPages: total}, nil
}
func (a *HDHiveAdapter) Detail(ctx context.Context, userID int64, id string) (telegram.Resource, error) {
	a.mu.RLock()
	if r, ok := a.getCachedUnlocked(userID, id); ok {
		a.mu.RUnlock()
		return r, nil
	}
	r, ok := a.getCachedResource(id)
	a.mu.RUnlock()
	if !ok {
		return telegram.Resource{}, telegram.ErrNotFound
	}
	if a.Store != nil {
		record, err := a.Store.GetUnlockRecord(ctx, userID, id)
		if err == nil && record.Status == "success" && len(record.Result) > 0 {
			if json.Unmarshal(record.Result, &r) == nil {
				a.mu.Lock()
				a.cacheUnlocked(userID, r)
				a.mu.Unlock()
				return r, nil
			}
		}
		if err == nil && (record.Status == "in_flight" || record.Status == "unknown") {
			return telegram.Resource{}, errors.New("unlock state requires manual verification")
		}
	}
	return r, nil
}
func (a *HDHiveAdapter) Unlock(ctx context.Context, userID int64, id string) (telegram.Resource, error) {
	if a.Store != nil {
		record, err := a.Store.GetUnlockRecord(ctx, userID, id)
		if err == nil {
			if record.Status == "success" && len(record.Result) > 0 {
				var saved telegram.Resource
				if json.Unmarshal(record.Result, &saved) == nil {
					return saved, nil
				}
			}
			return telegram.Resource{}, errors.New("unlock already attempted; manual verification required")
		}
		if !errors.Is(err, store.ErrNotFound) {
			return telegram.Resource{}, err
		}
		claimed, err := a.Store.ClaimUnlock(ctx, userID, id)
		if err != nil {
			return telegram.Resource{}, err
		}
		if !claimed {
			return telegram.Resource{}, errors.New("unlock already attempted; manual verification required")
		}
	}
	a.mu.RLock()
	r, ok := a.resources[id]
	a.mu.RUnlock()
	if !ok {
		return telegram.Resource{}, telegram.ErrNotFound
	}
	result, err := a.Client.Unlock(ctx, r.UnlockSlug)
	if err != nil {
		if a.Store != nil {
			// 区分业务拒绝和网络不确定错误
			status := "unknown"
			var apiErr *hdhive.APIError
			if errors.As(err, &apiErr) && apiErr.Business {
				status = "rejected"
			}
			_ = a.Store.SetUnlockRecord(ctx, store.UnlockRecord{UserID: userID, ResourceID: id, Status: status})
		}
		return telegram.Resource{}, err
	}
	r.Unlocked, r.ShareURL, r.ShareCode, r.ReceiveCode = true, result.URL, result.ShareCode, result.ReceiveCode
	a.mu.Lock()
	a.cacheUnlocked(userID, r)
	a.mu.Unlock()
	if a.Store != nil {
		raw, _ := json.Marshal(r)
		persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.Store.SetUnlockRecord(persistCtx, store.UnlockRecord{UserID: userID, ResourceID: id, Status: "success", Result: raw}); err != nil {
			return telegram.Resource{}, err
		}
	}
	return r, nil
}

func (a *HDHiveAdapter) ResetUnlockRecord(ctx context.Context, userID int64, resourceID string) error {
	if a.Store == nil {
		return store.ErrNotFound
	}
	if err := a.Store.ResetUnlockRecord(ctx, userID, resourceID); err != nil {
		return err
	}
	a.mu.Lock()
	if perUser := a.unlocked[userID]; perUser != nil {
		delete(perUser, resourceID)
		if len(perUser) == 0 {
			delete(a.unlocked, userID)
		}
	}
	a.mu.Unlock()
	return nil
}

func (a *HDHiveAdapter) DetailForUser(userID int64, id string) (telegram.Resource, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.getCachedUnlocked(userID, id)
}

func (a *HDHiveAdapter) GetUnknownUnlockRecords(ctx context.Context, limit int) ([]store.UnlockRecordWithUser, error) {
	if a.Store == nil {
		return nil, nil
	}
	return a.Store.GetUnknownUnlockRecords(ctx, limit)
}

func resourceFromMap(m map[string]any, tmdbID int64, index int) telegram.Resource {
	id := first(m, "slug", "resource_slug", "url", "id", "_id")
	if id == "" {
		id = fmt.Sprintf("%d-%d", tmdbID, index)
	}
	fee, feeKnown := integer(m, "fee", "price", "cost", "points", "coin")
	subtitle := joinValues(m, "subtitle_language", "subtitle_type")
	source := joinValues(m, "source")
	r := telegram.Resource{ID: id, UnlockSlug: id, Title: defaultString(first(m, "title", "name", "resource_name"), "未命名资源"), Quality: first(m, "quality", "resolution", "video_quality"), Size: first(m, "size", "file_size"), Description: first(m, "description", "remark", "note"), Subtitle: subtitle, Source: source, Fee: fee, FeeKnown: feeKnown, Unlocked: boolean(m, "unlocked", "is_unlocked")}
	return r
}

func joinValues(m map[string]any, keys ...string) string {
	var parts []string
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch val := v.(type) {
		case []any:
			for _, item := range val {
				if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
					parts = append(parts, s)
				}
			}
		case string:
			if s := strings.TrimSpace(val); s != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, " · ")
}
func first(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}
func integer(m map[string]any, keys ...string) (int, bool) {
	s := first(m, keys...)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return int(f), err == nil
}
func boolean(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch x := v.(type) {
			case bool:
				return x
			case string:
				b, _ := strconv.ParseBool(x)
				return b
			case float64:
				return x != 0
			}
		}
	}
	return false
}
func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

type transferCall struct {
	done   chan struct{}
	result string
	err    error
}

// TransferAdapter implements telegram.TransferService without exposing p115 details to handlers.
type TransferAdapter struct {
	HTTP      p115.HTTPDoer
	Logger    p115.Logger
	HDHive    *HDHiveAdapter
	UserAgent string
	mu        sync.Mutex
	calls     map[string]*transferCall
}

func (a *TransferAdapter) Transfer115(ctx context.Context, userID int64, cfg store.P115Config, r telegram.Resource) (string, error) {
	key := fmt.Sprintf("%d:%s", userID, r.ID)
	a.mu.Lock()
	if a.calls == nil {
		a.calls = make(map[string]*transferCall)
	}
	if existing := a.calls[key]; existing != nil {
		a.mu.Unlock()
		select {
		case <-existing.done:
			return existing.result, existing.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	call := &transferCall{done: make(chan struct{})}
	a.calls[key] = call
	a.mu.Unlock()

	call.result, call.err = a.transfer115(ctx, userID, cfg, r)
	close(call.done)
	a.mu.Lock()
	if a.calls[key] == call {
		delete(a.calls, key)
	}
	a.mu.Unlock()
	return call.result, call.err
}

func (a *TransferAdapter) transfer115(ctx context.Context, userID int64, cfg store.P115Config, r telegram.Resource) (string, error) {
	if a.HDHive == nil {
		return "", errors.New("hdhive unlock state is unavailable")
	}
	unlocked, ok := a.HDHive.DetailForUser(userID, r.ID)
	if !ok || (!unlocked.Unlocked && unlocked.ShareCode == "" && unlocked.ShareURL == "") {
		return "", errors.New("resource has not been unlocked by this user")
	}
	r = unlocked
	if !cfg.Enabled {
		return "", errors.New("115 account is disabled")
	}
	client, err := p115.New(cfg.Cookie, a.HTTP, a.Logger, a.UserAgent)
	if err != nil {
		return "", err
	}
	share := p115.Share{ShareCode: r.ShareCode, ReceiveCode: r.ReceiveCode}
	if share.ShareCode == "" {
		share, err = p115.ParseShare(r.ShareURL)
		if err != nil {
			return "", err
		}
		if share.ReceiveCode == "" {
			share.ReceiveCode = r.ReceiveCode
		}
	}
	result, err := client.Receive(ctx, share, p115.ReceiveOptions{TargetCID: cfg.TargetCID, Strategy: p115.StrategyAuto})
	if err != nil {
		return "", err
	}
	if result.Already {
		return "资源已存在", nil
	}
	if result.ReceivedID != "" {
		return result.ReceivedID, nil
	}
	return "已接收", nil
}
