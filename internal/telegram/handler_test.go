package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/atpx4869/hdhive_bot_go/internal/session"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

type fakeMessenger struct {
	sent        []Outgoing
	answered    []string
	deletedChat int64
	deletedID   int
	deleteErr   error
}

func (f *fakeMessenger) Send(_ context.Context, _ int64, o Outgoing) error {
	f.sent = append(f.sent, o)
	return nil
}
func (f *fakeMessenger) AnswerCallback(_ context.Context, _ string, text string) error {
	f.answered = append(f.answered, text)
	return nil
}
func (f *fakeMessenger) DeleteMessage(_ context.Context, chatID int64, messageID int) error {
	f.deletedChat, f.deletedID = chatID, messageID
	return f.deleteErr
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

type fakeAccounts struct{ configs map[int64]store.P115Config }

func (f *fakeAccounts) SetP115Config(_ context.Context, id int64, cfg store.P115Config) error {
	if f.configs == nil {
		f.configs = map[int64]store.P115Config{}
	}
	f.configs[id] = cfg
	return nil
}
func (f *fakeAccounts) GetP115Config(_ context.Context, id int64) (store.P115Config, error) {
	cfg, ok := f.configs[id]
	if !ok {
		return store.P115Config{}, store.ErrNotFound
	}
	return cfg, nil
}
func (f *fakeAccounts) DisableP115Config(_ context.Context, id int64) error {
	cfg, ok := f.configs[id]
	if !ok || !cfg.Enabled {
		return store.ErrNotFound
	}
	cfg.Enabled = false
	f.configs[id] = cfg
	return nil
}

type fakeTMDB struct{}

func (fakeTMDB) Search(context.Context, string, int) ([]TMDBItem, int, error) {
	return []TMDBItem{{ID: 1, MediaType: "movie", Title: "片名"}}, 1, nil
}

type fakeHive struct {
	unlocks  int
	feeKnown bool
	fee      int
	resets   []string
	resetErr error
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
func (f *fakeHive) ResetUnlockRecord(_ context.Context, userID int64, resourceID string) error {
	f.resets = append(f.resets, fmt.Sprintf("%d:%s", userID, resourceID))
	return f.resetErr
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

func TestAdminCanResetUnknownUnlockState(t *testing.T) {
	m := &fakeMessenger{}
	hive := &fakeHive{}
	sm := session.New(time.Minute, 10)
	_ = sm.BeginUnlock(1, "r1")
	_ = sm.TransitionUnlock(1, "r1", session.UnlockPending, session.UnlockInFlight)
	_ = sm.SetUnlockStatus(1, "r1", session.UnlockUnknown)
	h, _ := NewHandler(Services{Users: &fakeUsers{users: map[int64]store.User{}}, HDHive: hive}, sm, m, []int64{9})
	if err := h.HandleText(context.Background(), 9, 9, "/unlockreset 1 r1"); err != nil {
		t.Fatal(err)
	}
	if len(hive.resets) != 1 || hive.resets[0] != "1:r1" {
		t.Fatalf("reset was not delegated: %#v", hive.resets)
	}
	if got := sm.UnlockStatus(1, "r1"); got != "" {
		t.Fatalf("memory state was not reset: %s", got)
	}
}

func TestAdminCannotResetNonUnknownUnlockState(t *testing.T) {
	m := &fakeMessenger{}
	hive := &fakeHive{resetErr: store.ErrNotFound}
	sm := session.New(time.Minute, 10)
	_ = sm.BeginUnlock(1, "r1")
	_ = sm.TransitionUnlock(1, "r1", session.UnlockPending, session.UnlockInFlight)
	h, _ := NewHandler(Services{Users: &fakeUsers{users: map[int64]store.User{}}, HDHive: hive}, sm, m, []int64{9})
	if err := h.HandleText(context.Background(), 9, 9, "/unlockreset 1 r1"); err != nil {
		t.Fatal(err)
	}
	if got := sm.UnlockStatus(1, "r1"); got != session.UnlockInFlight {
		t.Fatalf("in-flight memory state must remain locked: %s", got)
	}
	if !strings.Contains(m.sent[len(m.sent)-1].Text, "只有 unknown") {
		t.Fatalf("unexpected response: %s", m.sent[len(m.sent)-1].Text)
	}
}

func TestSet115CommandWithCookieArgumentIsDeletedAndRejected(t *testing.T) {
	m := &fakeMessenger{}
	h, _ := NewHandler(Services{Users: &fakeUsers{users: map[int64]store.User{1: {ID: 1, Authorized: true}}}}, session.New(time.Minute, 10), m, nil)
	if err := h.HandleMessage(context.Background(), 1, 1, 20, "/set115 UID=u;CID=c;SEID=s"); err != nil {
		t.Fatal(err)
	}
	if m.deletedChat != 1 || m.deletedID != 20 {
		t.Fatalf("unsafe command message was not deleted: chat=%d id=%d", m.deletedChat, m.deletedID)
	}
	if !strings.Contains(m.sent[len(m.sent)-1].Text, "不要把 Cookie 放在命令后") {
		t.Fatalf("unexpected response: %s", m.sent[len(m.sent)-1].Text)
	}
}

func TestSet115CommandWithCookieArgumentIsDeletedBeforeAuthorizationCheck(t *testing.T) {
	m := &fakeMessenger{}
	h, _ := NewHandler(Services{Users: &fakeUsers{users: map[int64]store.User{}}}, session.New(time.Minute, 10), m, nil)
	if err := h.HandleMessage(context.Background(), 99, -100, 21, "/set115 UID=u;CID=c;SEID=s"); err != nil {
		t.Fatal(err)
	}
	if m.deletedChat != -100 || m.deletedID != 21 {
		t.Fatalf("unsafe unauthorized/group message was not deleted: chat=%d id=%d", m.deletedChat, m.deletedID)
	}
}

func TestSet115SessionCookieSentInGroupIsDeleted(t *testing.T) {
	m := &fakeMessenger{}
	h, _ := NewHandler(Services{Users: &fakeUsers{users: map[int64]store.User{1: {ID: 1, Authorized: true}}}}, session.New(time.Minute, 10), m, nil)
	ctx := context.Background()
	_ = h.HandleMessage(ctx, 1, 1, 10, "/set115")
	if err := h.HandleMessage(ctx, 1, -100, 22, "UID=u;CID=c;SEID=s"); err != nil {
		t.Fatal(err)
	}
	if m.deletedChat != -100 || m.deletedID != 22 {
		t.Fatalf("group cookie was not deleted: chat=%d id=%d", m.deletedChat, m.deletedID)
	}
	if !strings.Contains(m.sent[len(m.sent)-1].Text, "只能在 Bot 私聊") {
		t.Fatalf("unexpected response: %s", m.sent[len(m.sent)-1].Text)
	}
}

func TestSet115CommandWithWhitespaceCookieArgumentIsDeleted(t *testing.T) {
	for _, text := range []string{"/set115\nUID=u;CID=c;SEID=s", "/set115\tUID=u;CID=c;SEID=s"} {
		m := &fakeMessenger{}
		h, _ := NewHandler(Services{Users: &fakeUsers{users: map[int64]store.User{}}}, session.New(time.Minute, 10), m, nil)
		if err := h.HandleMessage(context.Background(), 99, -100, 23, text); err != nil {
			t.Fatal(err)
		}
		if m.deletedID != 23 {
			t.Fatalf("whitespace variant was not deleted: %q", text)
		}
	}
}

func TestSet115SessionGroupCookieDeletedEvenAfterRevocation(t *testing.T) {
	m := &fakeMessenger{}
	users := &fakeUsers{users: map[int64]store.User{1: {ID: 1, Authorized: true}}}
	h, _ := NewHandler(Services{Users: users}, session.New(time.Minute, 10), m, nil)
	ctx := context.Background()
	_ = h.HandleMessage(ctx, 1, 1, 10, "/set115")
	users.users[1] = store.User{ID: 1, Authorized: false}
	if err := h.HandleMessage(ctx, 1, -100, 24, "UID=u;CID=c;SEID=s"); err != nil {
		t.Fatal(err)
	}
	if m.deletedID != 24 {
		t.Fatal("revoked user's group cookie was not deleted")
	}
}

func TestSet115TwoStepAndMy115DoesNotExposeCookie(t *testing.T) {
	m := &fakeMessenger{}
	accounts := &fakeAccounts{configs: map[int64]store.P115Config{}}
	h, _ := NewHandler(Services{Users: &fakeUsers{users: map[int64]store.User{1: {ID: 1, Authorized: true}}}, Accounts: accounts}, session.New(time.Minute, 10), m, nil)
	ctx := context.Background()
	if err := h.HandleMessage(ctx, 1, 1, 10, "/set115"); err != nil {
		t.Fatal(err)
	}
	if err := h.HandleMessage(ctx, 1, 1, 11, "UID=u; CID=c; SEID=s; KID=k"); err != nil {
		t.Fatal(err)
	}
	if m.deletedChat != 1 || m.deletedID != 11 {
		t.Fatalf("cookie message was not deleted correctly: chat=%d id=%d", m.deletedChat, m.deletedID)
	}
	if err := h.HandleMessage(ctx, 1, 1, 12, "12345"); err != nil {
		t.Fatal(err)
	}
	cfg := accounts.configs[1]
	if cfg.Cookie != "UID=u;CID=c;SEID=s;KID=k" || cfg.TargetCID != "12345" || !cfg.Enabled {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if err := h.HandleText(ctx, 1, 1, "/my115"); err != nil {
		t.Fatal(err)
	}
	last := m.sent[len(m.sent)-1].Text
	if strings.Contains(last, "UID=") || !strings.Contains(last, "已配置目录") {
		t.Fatalf("unsafe my115 output: %s", last)
	}
}

func TestSet115DeleteFailureWarnsUser(t *testing.T) {
	m := &fakeMessenger{deleteErr: errors.New("delete failed")}
	accounts := &fakeAccounts{configs: map[int64]store.P115Config{}}
	h, _ := NewHandler(Services{Users: &fakeUsers{users: map[int64]store.User{1: {ID: 1, Authorized: true}}}, Accounts: accounts}, session.New(time.Minute, 10), m, nil)
	ctx := context.Background()
	_ = h.HandleMessage(ctx, 1, 1, 10, "/set115")
	if err := h.HandleMessage(ctx, 1, 1, 11, "UID=u;CID=c;SEID=s"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.sent[len(m.sent)-1].Text, "手动删除") {
		t.Fatalf("delete failure warning missing: %s", m.sent[len(m.sent)-1].Text)
	}
}

func TestUnset115AdminProtectedAndUserRequiresConfirmation(t *testing.T) {
	ctx := context.Background()
	m := &fakeMessenger{}
	accounts := &fakeAccounts{configs: map[int64]store.P115Config{1: {Cookie: "UID=u;CID=c;SEID=s", TargetCID: "0", Enabled: true}, 9: {Cookie: "UID=a;CID=c;SEID=s", Enabled: true}}}
	users := &fakeUsers{users: map[int64]store.User{1: {ID: 1, Authorized: true}}}
	sm := session.New(time.Minute, 20)
	h, _ := NewHandler(Services{Users: users, Accounts: accounts}, sm, m, []int64{9})
	if err := h.HandleText(ctx, 9, 9, "/unset115"); err != nil {
		t.Fatal(err)
	}
	if !accounts.configs[9].Enabled || !strings.Contains(m.sent[len(m.sent)-1].Text, "管理员不能") {
		t.Fatal("admin config should remain enabled")
	}
	if err := h.HandleText(ctx, 1, 1, "/unset115"); err != nil {
		t.Fatal(err)
	}
	buttons := m.sent[len(m.sent)-1].Buttons
	if len(buttons) == 0 || len(buttons[0]) < 1 {
		t.Fatal("confirmation buttons missing")
	}
	if err := h.HandleCallback(ctx, 1, 1, "cb", buttons[0][0].CallbackData); err != nil {
		t.Fatal(err)
	}
	if accounts.configs[1].Enabled {
		t.Fatal("user config should be disabled")
	}
}

func TestUnlockFailureUsesUnknownAdminMessage(t *testing.T) {
	m := &fakeMessenger{}
	hive := &errorHive{}
	h, _ := NewHandler(Services{Users: &fakeUsers{users: map[int64]store.User{1: {ID: 1, Authorized: true}}}, HDHive: hive}, session.New(time.Minute, 10), m, nil)
	h.sessions.BeginUnlock(1, "r1")
	if err := h.unlock(context.Background(), 1, 1, "r1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.sent[len(m.sent)-1].Text, "解锁结果不确定") {
		t.Fatalf("unexpected message: %s", m.sent[len(m.sent)-1].Text)
	}
}

func TestAdminAuthorityFromEnvOnly(t *testing.T) {
	// 验证管理员只能从环境变量（构造函数参数）加载，不能从数据库提升
	m := &fakeMessenger{}
	users := &fakeUsers{users: map[int64]store.User{
		1: {ID: 1, Authorized: true}, // 普通授权用户
		2: {ID: 2, Authorized: true}, // 另一个授权用户
	}}
	// 只有 ID=9 是管理员
	h, _ := NewHandler(Services{Users: users}, session.New(time.Minute, 10), m, []int64{9})

	// 授权用户不能执行管理员命令
	if err := h.HandleText(context.Background(), 1, 1, "/authorize 3"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.sent[len(m.sent)-1].Text, "无权限") {
		t.Fatalf("non-admin should not authorize: %s", m.sent[len(m.sent)-1].Text)
	}

	// 授权用户不能查看用户列表
	if err := h.HandleText(context.Background(), 1, 1, "/users"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.sent[len(m.sent)-1].Text, "无权限") {
		t.Fatalf("non-admin should not list users: %s", m.sent[len(m.sent)-1].Text)
	}

	// 授权用户不能查看日志
	if err := h.HandleText(context.Background(), 1, 1, "/logs"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.sent[len(m.sent)-1].Text, "无权限") {
		t.Fatalf("non-admin should not view logs: %s", m.sent[len(m.sent)-1].Text)
	}

	// 授权用户不能重置解锁状态
	if err := h.HandleText(context.Background(), 1, 1, "/unlockreset 2 r1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.sent[len(m.sent)-1].Text, "无权限") {
		t.Fatalf("non-admin should not reset unlock: %s", m.sent[len(m.sent)-1].Text)
	}
}

func TestAuthorizedUserCannotEscalateToAdmin(t *testing.T) {
	// 验证授权用户不能通过 /authorize 将自己或他人提升为管理员
	m := &fakeMessenger{}
	users := &fakeUsers{users: map[int64]store.User{
		1: {ID: 1, Authorized: true},
	}}
	h, _ := NewHandler(Services{Users: users}, session.New(time.Minute, 10), m, []int64{9})

	// 授权用户尝试授权他人
	if err := h.HandleText(context.Background(), 1, 1, "/authorize 3"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.sent[len(m.sent)-1].Text, "无权限") {
		t.Fatalf("authorized user should not authorize others: %s", m.sent[len(m.sent)-1].Text)
	}

	// 验证管理员可以授权
	if err := h.HandleText(context.Background(), 9, 9, "/authorize 3"); err != nil {
		t.Fatal(err)
	}
	if !users.users[3].Authorized {
		t.Fatal("admin should be able to authorize")
	}
}

func TestAdminListIsImmutable(t *testing.T) {
	// 验证管理员列表在运行时不会改变
	m := &fakeMessenger{}
	users := &fakeUsers{users: map[int64]store.User{}}
	sm := session.New(time.Minute, 10)
	h, _ := NewHandler(Services{Users: users}, sm, m, []int64{9})

	// 管理员授权用户
	if err := h.HandleText(context.Background(), 9, 9, "/authorize 1"); err != nil {
		t.Fatal(err)
	}
	if !users.users[1].Authorized {
		t.Fatal("user should be authorized")
	}

	// 被授权的用户仍然不能执行管理员命令
	if err := h.HandleText(context.Background(), 1, 1, "/users"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.sent[len(m.sent)-1].Text, "无权限") {
		t.Fatalf("authorized user should not become admin: %s", m.sent[len(m.sent)-1].Text)
	}
}

type errorHive struct{}

func (*errorHive) Search(context.Context, TMDBItem, int) (ResourcePage, error) {
	return ResourcePage{}, errors.New("failed")
}
func (*errorHive) Detail(context.Context, int64, string) (Resource, error) {
	return Resource{ID: "r1"}, nil
}
func (*errorHive) Unlock(context.Context, int64, string) (Resource, error) {
	return Resource{}, errors.New("failed")
}

var _ = errors.New
