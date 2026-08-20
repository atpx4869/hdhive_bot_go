package app

import (
	"testing"

	"github.com/atpx4869/hdhive_bot_go/internal/telegram"
)

func TestResourceFromMapFieldMapping(t *testing.T) {
	m := map[string]any{
		"slug":              "slug-abc",
		"title":             "千与千寻",
		"pan_type":          "115",
		"video_resolution":  []any{"1080P", "4K"},
		"share_size":        "5.47GB",
		"subtitle_language": []any{"简中", "繁中"},
		"subtitle_type":     []any{"内封"},
		"source":            []any{"蓝光原盘/REMUX"},
		"unlock_points":     5,
	}
	r := resourceFromMap(m, 1, 0)
	if r.ID != "slug-abc" || r.UnlockSlug != "slug-abc" {
		t.Fatalf("id=%#v", r)
	}
	if r.Title != "千与千寻" || r.PanType != "115" {
		t.Fatalf("title=%q pan=%q", r.Title, r.PanType)
	}
	if r.Quality != "1080P · 4K" {
		t.Fatalf("quality=%q", r.Quality)
	}
	if r.Size != "5.47GB" {
		t.Fatalf("size=%q", r.Size)
	}
	if r.Subtitle != "简中 · 繁中 · 内封" {
		t.Fatalf("subtitle=%q", r.Subtitle)
	}
	if r.Source != "蓝光原盘/REMUX" {
		t.Fatalf("source=%q", r.Source)
	}
	if !r.FeeKnown || r.Fee != 5 {
		t.Fatalf("fee=%d known=%v", r.Fee, r.FeeKnown)
	}
}

func TestResourceTitlePrefersRemark(t *testing.T) {
	m := map[string]any{
		"title":  "流浪地球",
		"remark": "4K 蓝光原盘 REMUX · DV&HDR",
		"slug":   "slug-1",
	}
	r := resourceFromMap(m, 1, 0)
	if r.Title != "4K 蓝光原盘 REMUX · DV&HDR" {
		t.Fatalf("title=%q", r.Title)
	}
	// 没有 remark 时退回 title
	r2 := resourceFromMap(map[string]any{"title": "千与千寻"}, 1, 0)
	if r2.Title != "千与千寻" {
		t.Fatalf("title=%q", r2.Title)
	}
}

func TestFilterAndSortResources(t *testing.T) {
	in := []telegram.Resource{
		{ID: "a", PanType: "guangYa", Title: "gua"},
		{ID: "b", PanType: "ed2k", Title: "ed1"},
		{ID: "c", PanType: "115", Title: "p1"},
		{ID: "d", PanType: "ed2k", Title: "ed2", Source: "官组"},
		{ID: "e", PanType: "115", Title: "p2", Source: "官方"},
	}
	got := filterAndSortResources(in, telegram.CatDefault)
	// 期望顺序：官组115 > 115 > 官组ed2k > ed2k；guangYa 被过滤
	want := []string{"e", "c", "d", "b"}
	if len(got) != len(want) {
		t.Fatalf("len=%d got=%+v", len(got), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("order[%d]=%s want %s (got=%+v)", i, got[i].ID, id, got)
		}
	}
}

func TestFilterByCategory(t *testing.T) {
	in := []telegram.Resource{
		{ID: "iso115", PanType: "115", Source: "蓝光原盘/ISO"},
		{ID: "isoEd2k", PanType: "ed2k", Source: "蓝光原盘/ISO"},
		{ID: "normal115", PanType: "115", Source: "蓝光原盘/REMUX"},
		{ID: "normalEd2k", PanType: "ed2k", Source: "BDRip"},
		{ID: "other", PanType: "guangYa", Source: "xxx"},
	}

	// 默认：115+ed2k 非蓝光原盘/ISO
	def := filterAndSortResources(in, telegram.CatDefault)
	if len(def) != 2 || def[0].ID != "normal115" || def[1].ID != "normalEd2k" {
		t.Fatalf("default=%+v", def)
	}

	// ISO：只认 "蓝光原盘/ISO" 这几个字
	iso := filterAndSortResources(in, telegram.CatISO)
	if len(iso) != 2 || iso[0].ID != "iso115" || iso[1].ID != "isoEd2k" {
		t.Fatalf("iso=%+v", iso)
	}

	// 其他：非 115/ed2k
	other := filterAndSortResources(in, telegram.CatOther)
	if len(other) != 1 || other[0].ID != "other" {
		t.Fatalf("other=%+v", other)
	}
}

func TestPanTypeRank(t *testing.T) {
	if panTypeRank("115") != 0 || panTypeRank("115网盘") != 0 {
		t.Fatal("115 rank should be 0")
	}
	if panTypeRank("ed2k") != 1 || panTypeRank("ED2K") != 1 {
		t.Fatal("ed2k rank should be 1")
	}
	if panTypeRank("guangYa") != 2 || panTypeRank("夸克") != 2 {
		t.Fatal("other pan types should be 2")
	}
}
