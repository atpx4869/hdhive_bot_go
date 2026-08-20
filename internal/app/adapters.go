package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
		items = append(items, telegram.TMDBItem{ID: item.ID, MediaType: item.MediaType, Title: title, OriginalTitle: original, ReleaseDate: date, Overview: item.Overview, VoteAverage: item.VoteAverage, PosterPath: item.PosterPath})
	}
	return items, result.TotalPages, nil
}

type HDHiveAdapter struct {
	Client    *hdhive.Client
	Store     *store.Store
	mu        sync.RWMutex
	resources map[string]telegram.Resource
	unlocked  map[int64]map[string]telegram.Resource
}

func NewHDHiveAdapter(client *hdhive.Client, db *store.Store) *HDHiveAdapter {
	return &HDHiveAdapter{Client: client, Store: db, resources: make(map[string]telegram.Resource), unlocked: make(map[int64]map[string]telegram.Resource)}
}
func (a *HDHiveAdapter) Search(ctx context.Context, item telegram.TMDBItem, page int, category telegram.ResourceCategory, userID int64) (telegram.ResourcePage, error) {
	raw, err := a.Client.Resources(ctx, item.MediaType, item.ID)
	if err != nil {
		return telegram.ResourcePage{}, err
	}
	all := make([]telegram.Resource, 0, len(raw))
	a.mu.Lock()
	for i, value := range raw {
		r := resourceFromMap(value, item.ID, i)
		all = append(all, r)
		a.resources[r.ID] = r
	}
	a.mu.Unlock()

	// 标记本地已解锁资源（按钮显示 ✅）
	if a.Store != nil {
		for i := range all {
			if record, err := a.Store.GetUnlockRecord(ctx, userID, all[i].ID); err == nil && record.Status == "success" {
				all[i].Unlocked = true
			}
		}
	}

	filtered := filterAndSortResources(all, category)
	const pageSize = 8
	if page < 1 {
		page = 1
	}
	total := (len(filtered) + pageSize - 1) / pageSize
	if total < 1 {
		total = 1
	}
	if page > total {
		page = total
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	if end > len(filtered) {
		end = len(filtered)
	}
	return telegram.ResourcePage{Items: filtered[start:end], Page: page, TotalPages: total, Total: len(filtered)}, nil
}
func (a *HDHiveAdapter) Detail(ctx context.Context, userID int64, id string) (telegram.Resource, error) {
	a.mu.RLock()
	if perUser := a.unlocked[userID]; perUser != nil {
		if r, ok := perUser[id]; ok {
			a.mu.RUnlock()
			return r, nil
		}
	}
	r, ok := a.resources[id]
	a.mu.RUnlock()
	if !ok {
		return telegram.Resource{}, telegram.ErrNotFound
	}
	if a.Store != nil {
		record, err := a.Store.GetUnlockRecord(ctx, userID, id)
		if err == nil && record.Status == "success" && len(record.Result) > 0 {
			if json.Unmarshal(record.Result, &r) == nil {
				a.mu.Lock()
				if a.unlocked[userID] == nil {
					a.unlocked[userID] = make(map[string]telegram.Resource)
				}
				a.unlocked[userID][id] = r
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
			_ = a.Store.SetUnlockRecord(ctx, store.UnlockRecord{UserID: userID, ResourceID: id, Status: "unknown"})
		}
		return telegram.Resource{}, err
	}
	r.Unlocked, r.ShareURL, r.ShareCode, r.ReceiveCode = true, result.URL, result.ShareCode, result.ReceiveCode
	a.mu.Lock()
	if a.unlocked[userID] == nil {
		a.unlocked[userID] = make(map[string]telegram.Resource)
	}
	a.unlocked[userID][id] = r
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

func (a *HDHiveAdapter) DetailForUser(userID int64, id string) (telegram.Resource, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	r, ok := a.unlocked[userID][id]
	return r, ok
}
func resourceFromMap(m map[string]any, tmdbID int64, index int) telegram.Resource {
	id := first(m, "slug", "resource_slug", "url", "id", "_id")
	if id == "" {
		id = fmt.Sprintf("%d-%d", tmdbID, index)
	}
	fee, feeKnown := integer(m, "unlock_points", "fee", "price", "cost", "points", "coin")
	subtitle := joinList(first(m, "subtitle_language"), first(m, "subtitle_type"))
	r := telegram.Resource{
		ID:          id,
		UnlockSlug:  id,
		Title:       defaultString(first(m, "remark", "title", "name", "slug", "id"), "未知资源"),
		Quality:     first(m, "video_resolution", "quality", "resolution", "video_quality"),
		Size:        first(m, "share_size", "size", "file_size"),
		Subtitle:    subtitle,
		Description: first(m, "description"),
		Fee:         fee,
		FeeKnown:    feeKnown,
		Unlocked:    boolean(m, "unlocked", "is_unlocked"),
		PanType:     first(m, "pan_type", "website", "storage"),
		Source:      first(m, "source", "channel", "origin"),
	}
	return r
}

// first returns the first non-empty value among the given keys, joining list values.
func first(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if s := asText(v); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

// asText converts a scalar or list value to a compact string.
func asText(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			if s := asText(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " · ")
	case []string:
		parts := make([]string, 0, len(x))
		for _, s := range x {
			if strings.TrimSpace(s) != "" {
				parts = append(parts, strings.TrimSpace(s))
			}
		}
		return strings.Join(parts, " · ")
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func joinList(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}
	return strings.Join(out, " · ")
}

// panTypeRank orders pan types: 115=0, ed2k=1, others=2 (filtered out).
func panTypeRank(p string) int {
	p = strings.ToLower(strings.TrimSpace(p))
	switch {
	case strings.Contains(p, "115"):
		return 0
	case strings.Contains(p, "ed2k"):
		return 1
	default:
		return 2
	}
}

// isOfficialGroup detects resources from official release groups.
func isOfficialGroup(r telegram.Resource) bool {
	s := strings.ToLower(r.Source + " " + r.Title)
	return strings.Contains(s, "官组") || strings.Contains(s, "官方") || strings.Contains(s, "official")
}

// filterAndSortResources 按分类过滤并排序：
// 默认=115+ed2k(非蓝光原盘/ISO)、iso=蓝光原盘/ISO、other=其他网盘类型。
// 排序：网盘类型（115 > ed2k）→ 官组优先 → 稳定保持原序。
func filterAndSortResources(all []telegram.Resource, category telegram.ResourceCategory) []telegram.Resource {
	filtered := make([]telegram.Resource, 0, len(all))
	for _, r := range all {
		switch category {
		case telegram.CatISO:
			if strings.Contains(r.Source, "蓝光原盘/ISO") {
				filtered = append(filtered, r)
			}
		case telegram.CatOther:
			if panTypeRank(r.PanType) >= 2 {
				filtered = append(filtered, r)
			}
		default:
			if panTypeRank(r.PanType) < 2 && !strings.Contains(r.Source, "蓝光原盘/ISO") {
				filtered = append(filtered, r)
			}
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		pi, pj := panTypeRank(filtered[i].PanType), panTypeRank(filtered[j].PanType)
		if pi != pj {
			return pi < pj
		}
		oi, oj := isOfficialGroup(filtered[i]), isOfficialGroup(filtered[j])
		if oi != oj {
			return oi
		}
		return false
	})
	return filtered
}

func isNon115Link(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(l, "ed2k:") || strings.HasPrefix(l, "magnet:")
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

// TransferAdapter implements telegram.TransferService without exposing p115 details to handlers.
type TransferAdapter struct {
	HTTP   p115.HTTPDoer
	Logger p115.Logger
	HDHive *HDHiveAdapter
}

func (a TransferAdapter) Transfer115(ctx context.Context, userID int64, cfg store.P115Config, r telegram.Resource) (string, error) {
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
	client, err := p115.New(cfg.Cookie, a.HTTP, a.Logger)
	if err != nil {
		return "", err
	}
	share := p115.Share{ShareCode: r.ShareCode, ReceiveCode: r.ReceiveCode}
	if share.ShareCode == "" {
		if strings.TrimSpace(r.ShareURL) == "" || isNon115Link(r.ShareURL) {
			return "", errors.New("该资源不是 115 分享（可能是 ed2k/磁力），无法转存到 115")
		}
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
