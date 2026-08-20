package p115

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseShare(t *testing.T) {
	s, err := ParseShare("https://115.com/s/Abc123?password=P9")
	if err != nil || s.ShareCode != "Abc123" || s.ReceiveCode != "P9" {
		t.Fatalf("share=%#v err=%v", s, err)
	}
	if _, err = ParseShare("nope"); !IsKind(err, KindInvalidShare) {
		t.Fatalf("err=%v", err)
	}
}

func TestParseShareFormats(t *testing.T) {
	cases := []struct {
		input     string
		shareCode string
		pwd       string
		wantErr   bool
	}{
		{"https://115.com/s/Abc123?password=P9", "Abc123", "P9", false},
		{"https://www.115cdn.com/share/xyz789", "xyz789", "", false},
		{"anxia.com/s/code12345", "code12345", "", false},
		{"115://sharecode123", "sharecode123", "", false},
		{"anxia://sharecode456", "sharecode456", "", false},
		{"share_code=abc12345", "abc12345", "", false},
		{"裸码 abcdefghij", "abcdefghij", "", false},
		{"ed2k://abc123", "", "", true},
		{"magnet:?xt=urn:btih:abc", "", "", true},
	}
	for _, c := range cases {
		s, err := ParseShare(c.input)
		if c.wantErr {
			if err == nil {
				t.Fatalf("input=%q expected error, got %#v", c.input, s)
			}
			continue
		}
		if err != nil || s.ShareCode != c.shareCode || s.ReceiveCode != c.pwd {
			t.Fatalf("input=%q share=%#v err=%v", c.input, s, err)
		}
	}
}

func TestListPaginationRootAndReceiveStrategies(t *testing.T) {
	var receiveBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/share/snap":
			if r.URL.Query().Get("share_code") == "receive" {
				_, _ = w.Write([]byte(`{"data":{"list":[{"fc":"0","cid":"dir1","n":"folder"}]}}`))
				return
			}
			cid := r.URL.Query().Get("cid")
			limit := r.URL.Query().Get("limit")
			offset := r.URL.Query().Get("offset")
			if cid == "0" && limit == "20" {
				_, _ = w.Write([]byte(`{"data":{"list":[{"fc":"0","cid":"dir1","n":"folder"}]}}`))
				return
			}
			if cid == "0" && offset == "0" {
				items := make([]string, 100)
				for i := range items {
					items[i] = fmt.Sprintf(`{"fc":"1","fid":"f%d","n":"%d.mkv","s":"100"}`, i, i)
				}
				_, _ = w.Write([]byte(`{"data":{"list":[` + strings.Join(items, ",") + `]}}`))
				return
			}
			if cid == "0" && offset == "100" {
				_, _ = w.Write([]byte(`{"data":{"list":[{"fc":"0","cid":"d","n":"sub"}]}}`))
				return
			}
			if cid == "d" {
				_, _ = w.Write([]byte(`{"data":{"list":[{"fc":"1","fid":"last","n":"last.mp4","s":"999"}]}}`))
				return
			}
			t.Fatalf("snap query=%s", r.URL.RawQuery)
		case "/files":
			_, _ = w.Write([]byte(`{"data":[{"fc":"0","cid":"rootdir","n":"movies"}]}`))
		case "/share/receive":
			_ = r.ParseForm()
			receiveBody = r.Form.Encode()
			_, _ = w.Write([]byte(`{"state":true,"data":{}}`))
		default:
			t.Fatalf("path=%s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c, _ := NewWithBaseURL(srv.URL, "UID=secret-cookie", srv.Client(), nil)
	files, err := c.ListShare(context.Background(), Share{ShareCode: "share", ReceiveCode: "pwd"})
	if err != nil || len(files) != 101 {
		t.Fatalf("len=%d err=%v", len(files), err)
	}
	root, err := c.RootFiles(context.Background())
	if err != nil || len(root) != 1 || !root[0].IsDir {
		t.Fatalf("root=%#v err=%v", root, err)
	}
	res, err := c.Receive(context.Background(), Share{ShareCode: "receive"}, ReceiveOptions{TargetCID: "42", Strategy: StrategyAuto})
	if err != nil || res.ReceivedID != "dir1" || !strings.Contains(receiveBody, "file_id=dir1") {
		t.Fatalf("res=%#v body=%s err=%v", res, receiveBody, err)
	}
}

type captureLogger struct{ text strings.Builder }

func (l *captureLogger) Log(_ context.Context, _ slog.Level, msg string, args ...any) {
	fmt.Fprint(&l.text, msg, args)
}
func TestErrorClassificationAndRedactedLogs(t *testing.T) {
	logger := &captureLogger{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/share/snap" {
			_, _ = w.Write([]byte(`{"data":{"list":[{"fc":"1","fid":"file1","n":"file.mkv"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"state":false,"errno":4200045,"error":"已接收"}`))
	}))
	defer srv.Close()
	c, _ := NewWithBaseURL(srv.URL, "SUPER-SECRET-COOKIE", srv.Client(), logger)
	res, err := c.Receive(context.Background(), Share{ShareCode: "verysecretshare", ReceiveCode: "secretpwd"}, ReceiveOptions{Strategy: StrategyWholeShare})
	if err != nil || !res.Already {
		t.Fatalf("res=%#v err=%v", res, err)
	}
	text := logger.text.String()
	for _, secret := range []string{"SUPER-SECRET-COOKIE", "verysecretshare", "secretpwd"} {
		if strings.Contains(text, secret) {
			t.Fatalf("log leaked %q: %s", secret, text)
		}
	}
}
