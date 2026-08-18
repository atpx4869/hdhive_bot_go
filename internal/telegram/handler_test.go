package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/atpx4869/hdhive_bot_go/internal/session"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

type fakeMessenger struct {
	sent     []Outgoing
	answered []string
}

func (f *fakeMessenger) Send(_ context.Context, _ int64, o Outgoing) error {
	f.sent = append(f.sent, o)
	return nil
}
func (f *fakeMessenger) AnswerCallback(_ context.Context, _ string, text string) error {
	f.answered = append(f.answered, text)
	return nil
}

type fakeUsers struct{ users map[int64]store.User }

func (f *fakeUsers) GetUser(_ context.Context, id int64) (store.User, error) {
	u, ok := f.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return u, nil
}
func (f *fakeUsers) SetUserAuthorization(_ context.Context, id int64, a bool) error {
	u := f.users[id]
	u.ID = id
	u.Authorized = a
	f.users[id] = u
	return nil
}
func (f *fakeUsers) SetUserNote(_ context.Context, id int64, n string) error {
	u := f.users[id]
	u.ID = id
	u.Note = n
	f.users[id] = u
	return nil
}
func (f *fakeUsers) ListUsers(context.Context, int, int) ([]store.User, error) {
	var out []store.User
	for _, u := range f.users {
		out = append(out, u)
	}
	return out, nil
}

type fakeTMDB struct{}

func (fakeTMDB) Search(context.Context, string, int) ([]TMDBItem, int, error) {
	return []TMDBItem{{ID: 1, MediaType: "movie", Title: "片名"}}, 1, nil
}

type fakeHive struct {
	unlocks  int
	feeKnown bool
	fee      int
}

func (f *fakeHive) Search(context.Context, TMDBItem, int) (ResourcePage, error) {
	return ResourcePage{Items: []Resource{{ID: "r1", Title: "资源"}}, Page: 1, TotalPages: 1}, nil
}
func (f *fakeHive) Detail(context.Context, int64, string) (Resource, error) {
	return Resource{ID: "r1", Title: "资源", FeeKnown: f.feeKnown, Fee: f.fee}, nil
}
func (f *fakeHive) Unlock(context.Context, int64, string) (Resource, error) {
	f.unlocks++
	return Resource{ID: "r1", Title: "资源", Unlocked: true}, nil
}

func TestUnauthorizedKeywordRejected(t *testing.T) {
	m := &fakeMessenger{}
	h, _ := NewHandler(Services{Users: &fakeUsers{users: map[int64]store.User{}}}, session.New(time.Minute, 10), m, nil)
	if err := h.HandleText(context.Background(), 1, 1, "电影"); err != nil {
		t.Fatal(err)
	}
	if len(m.sent) != 1 || !strings.Contains(m.sent[0].Text, "尚未获得授权") {
		t.Fatalf("%+v", m.sent)
	}
}
func TestAdminAuthorize(t *testing.T) {
	m := &fakeMessenger{}
	u := &fakeUsers{users: map[int64]store.User{}}
	h, _ := NewHandler(Services{Users: u}, session.New(time.Minute, 10), m, []int64{9})
	if err := h.HandleText(context.Background(), 9, 9, "/authorize 7"); err != nil {
		t.Fatal(err)
	}
	if !u.users[7].Authorized {
		t.Fatal("not authorized")
	}
}
func TestCallbackOwnerBinding(t *testing.T) {
	m := &fakeMessenger{}
	sm := session.New(time.Minute, 10)
	h, _ := NewHandler(Services{Users: &fakeUsers{users: map[int64]store.User{1: {ID: 1, Authorized: true}}}}, sm, m, nil)
	token, _ := sm.BindCallback(1, "detail", "r1")
	if err := h.HandleCallback(context.Background(), 2, 2, "cb", token); err != nil {
		t.Fatal(err)
	}
	if len(m.answered) == 0 || !strings.Contains(m.answered[0], "不属于你") {
		t.Fatalf("%v", m.answered)
	}
}
func TestPaidUnlockRequiresConfirmationAndNoDuplicate(t *testing.T) {
	m := &fakeMessenger{}
	sm := session.New(time.Minute, 10)
	hive := &fakeHive{feeKnown: true, fee: 5}
	h, _ := NewHandler(Services{Users: &fakeUsers{users: map[int64]store.User{1: {ID: 1, Authorized: true}}}, HDHive: hive}, sm, m, nil)
	token, _ := sm.BindCallback(1, "unlock", "r1")
	if err := h.HandleCallback(context.Background(), 1, 1, "cb", token); err != nil {
		t.Fatal(err)
	}
	if hive.unlocks != 0 || len(m.sent) == 0 || !strings.Contains(m.sent[len(m.sent)-1].Text, "是否确认") {
		t.Fatalf("unlocks=%d sent=%+v", hive.unlocks, m.sent)
	}
	if err := h.HandleCallback(context.Background(), 1, 1, "cb", token); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.sent[len(m.sent)-1].Text, "请勿重复") {
		t.Fatalf("%+v", m.sent)
	}
}

var _ = errors.New
