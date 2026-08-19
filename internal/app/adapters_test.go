package app

import (
	"testing"
	"time"

	"github.com/atpx4869/hdhive_bot_go/internal/telegram"
)

func TestResourceFromMap(t *testing.T) {
	m := map[string]any{
		"slug":              "test-slug",
		"title":             "测试资源",
		"quality":           "1080p",
		"size":              "5GB",
		"fee":               10,
		"unlocked":          true,
		"subtitle_language": []any{"中文", "英文"},
		"source":            []any{"BluRay", "REMUX"},
	}

	r := resourceFromMap(m, 12345, 0)
	if r.ID != "test-slug" {
		t.Fatalf("expected test-slug, got %s", r.ID)
	}
	if r.Title != "测试资源" {
		t.Fatalf("expected 测试资源, got %s", r.Title)
	}
	if r.Quality != "1080p" {
		t.Fatalf("expected 1080p, got %s", r.Quality)
	}
	if r.Size != "5GB" {
		t.Fatalf("expected 5GB, got %s", r.Size)
	}
	if r.Fee != 10 {
		t.Fatalf("expected 10, got %d", r.Fee)
	}
	if !r.FeeKnown {
		t.Fatal("expected fee known")
	}
	if !r.Unlocked {
		t.Fatal("expected unlocked")
	}
	if r.Subtitle != "中文 · 英文" {
		t.Fatalf("expected subtitle, got %s", r.Subtitle)
	}
	if r.Source != "BluRay · REMUX" {
		t.Fatalf("expected source, got %s", r.Source)
	}
}

func TestResourceFromMap_Defaults(t *testing.T) {
	m := map[string]any{
		"id": "test-id",
	}

	r := resourceFromMap(m, 12345, 0)
	if r.ID != "test-id" {
		t.Fatalf("expected test-id, got %s", r.ID)
	}
	if r.Title != "未命名资源" {
		t.Fatalf("expected 未命名资源, got %s", r.Title)
	}
	if r.FeeKnown {
		t.Fatal("expected fee unknown")
	}
}

func TestResourceFromMap_FallbackID(t *testing.T) {
	m := map[string]any{
		"title": "资源",
	}

	r := resourceFromMap(m, 12345, 2)
	if r.ID != "12345-2" {
		t.Fatalf("expected 12345-2, got %s", r.ID)
	}
}

func TestJoinValues(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		keys []string
		want string
	}{
		{
			name: "array values",
			m:    map[string]any{"subtitle_language": []any{"中文", "英文"}},
			keys: []string{"subtitle_language"},
			want: "中文 · 英文",
		},
		{
			name: "string value",
			m:    map[string]any{"source": "BluRay"},
			keys: []string{"source"},
			want: "BluRay",
		},
		{
			name: "multiple keys",
			m:    map[string]any{"a": []any{"1"}, "b": "2"},
			keys: []string{"a", "b"},
			want: "1 · 2",
		},
		{
			name: "empty",
			m:    map[string]any{},
			keys: []string{"a"},
			want: "",
		},
		{
			name: "nil value",
			m:    map[string]any{"a": nil},
			keys: []string{"a"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinValues(tt.m, tt.keys...)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestFirst(t *testing.T) {
	m := map[string]any{
		"a": "value-a",
		"b": 123,
		"c": nil,
		"d": "",
		"e": "  ",
	}

	if got := first(m, "a"); got != "value-a" {
		t.Fatalf("expected value-a, got %s", got)
	}
	if got := first(m, "b"); got != "123" {
		t.Fatalf("expected 123, got %s", got)
	}
	if got := first(m, "c", "a"); got != "value-a" {
		t.Fatalf("expected value-a fallback, got %s", got)
	}
	if got := first(m, "missing"); got != "" {
		t.Fatalf("expected empty, got %s", got)
	}
}

func TestInteger(t *testing.T) {
	tests := []struct {
		m      map[string]any
		keys   []string
		want   int
		wantOK bool
	}{
		{map[string]any{"fee": 10}, []string{"fee"}, 10, true},
		{map[string]any{"fee": "5"}, []string{"fee"}, 5, true},
		{map[string]any{"fee": 3.14}, []string{"fee"}, 3, true},
		{map[string]any{}, []string{"fee"}, 0, false},
		{map[string]any{"fee": "abc"}, []string{"fee"}, 0, false},
	}

	for _, tt := range tests {
		got, ok := integer(tt.m, tt.keys...)
		if got != tt.want || ok != tt.wantOK {
			t.Fatalf("integer(%v, %v) = %d, %v; want %d, %v", tt.m, tt.keys, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestBoolean(t *testing.T) {
	tests := []struct {
		m    map[string]any
		key  string
		want bool
	}{
		{map[string]any{"v": true}, "v", true},
		{map[string]any{"v": false}, "v", false},
		{map[string]any{"v": "true"}, "v", true},
		{map[string]any{"v": "false"}, "v", false},
		{map[string]any{"v": float64(1)}, "v", true},
		{map[string]any{"v": float64(0)}, "v", false},
		{map[string]any{}, "v", false},
	}

	for _, tt := range tests {
		got := boolean(tt.m, tt.key)
		if got != tt.want {
			t.Fatalf("boolean(%v, %q) = %v; want %v", tt.m, tt.key, got, tt.want)
		}
	}
}

func TestDefaultString(t *testing.T) {
	if got := defaultString("", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback, got %s", got)
	}
	if got := defaultString("value", "fallback"); got != "value" {
		t.Fatalf("expected value, got %s", got)
	}
}

func TestResourceDetailForUser(t *testing.T) {
	now := time.Now()
	adapter := &HDHiveAdapter{
		resources: map[string]cacheEntry[telegram.Resource]{
			"r1": {value: telegram.Resource{ID: "r1", Title: "资源1"}, expiresAt: now.Add(time.Hour)},
		},
		unlocked: map[int64]map[string]cacheEntry[telegram.Resource]{
			1: {
				"r1": {value: telegram.Resource{ID: "r1", Title: "资源1", Unlocked: true, ShareURL: "https://115.com/s/abc"}, expiresAt: now.Add(time.Hour)},
			},
		},
		cacheTTL: 30 * time.Minute,
	}

	// 测试已解锁用户的资源
	r, ok := adapter.DetailForUser(1, "r1")
	if !ok {
		t.Fatal("expected found")
	}
	if !r.Unlocked {
		t.Fatal("expected unlocked")
	}
	if r.ShareURL != "https://115.com/s/abc" {
		t.Fatalf("expected share URL, got %s", r.ShareURL)
	}

	// 测试未解锁用户的资源
	_, ok = adapter.DetailForUser(2, "r1")
	if ok {
		t.Fatal("expected not found")
	}
}
