package p115

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const defaultBaseURL = "https://webapi.115.com"

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type Logger interface {
	Log(context.Context, slog.Level, string, ...any)
}

type ErrorKind string

const (
	KindAuth            ErrorKind = "auth"
	KindInvalidShare    ErrorKind = "invalid_share"
	KindRateLimit       ErrorKind = "rate_limit"
	KindAlreadyReceived ErrorKind = "already_received"
	KindRemote          ErrorKind = "remote"
	KindProtocol        ErrorKind = "protocol"
)

type Error struct {
	Kind       ErrorKind
	StatusCode int
	Errno      string
	Message    string
}

func (e *Error) Error() string { return fmt.Sprintf("115 %s: %s", e.Kind, e.Message) }
func IsKind(err error, kind ErrorKind) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == kind
}

type Share struct {
	ShareCode   string
	ReceiveCode string
}
type Item struct {
	ID, Name, PickCode string
	Size               int64
	IsDir, IsVideo     bool
	Raw                map[string]any
}
type ReceiveStrategy string

const (
	StrategyAuto            ReceiveStrategy = "auto"
	StrategySingleDirectory ReceiveStrategy = "single_directory"
	StrategyWholeShare      ReceiveStrategy = "whole_share"
)

type ReceiveOptions struct {
	TargetCID string
	Strategy  ReceiveStrategy
}
type ReceiveResult struct {
	Already    bool
	ReceivedID string
	Raw        map[string]any
}

type Client struct {
	baseURL, cookie    string
	http               HTTPDoer
	logger             Logger
	pageSize, maxDepth int
}

var (
	// https://115.com/s/xxxxx、https://www.115cdn.com/share/xxxxx、anxia.com/s/xxxxx 等
	urlShareRE  = regexp.MustCompile(`(?i)(?:https?://)?(?:www[.])?(?:115[.]com|115cdn[.]com|anxia[.]com)/(?:s|share)/([a-zA-Z0-9]{6,})`)
	queryCodeRE = regexp.MustCompile(`(?i)(?:[?&](?:password|pwd|receive_code|r)=)([a-zA-Z0-9]+)`)
	textShareRE = regexp.MustCompile(`(?i)(?:share[_-]?code|分享码|码)[=:：\s]*([a-zA-Z0-9]{5,})`)
	textPwdRE   = regexp.MustCompile(`(?i)(?:password|pwd|访问码|提取码)[=:：\s]*([a-zA-Z0-9]+)`)
	bareRE      = regexp.MustCompile(`^[a-zA-Z0-9]{6,32}$`)
	schemeRE    = regexp.MustCompile(`^([a-z0-9][a-z0-9+.-]*)://`)
)

func ParseShare(text string) (Share, error) {
	s, _ := url.QueryUnescape(strings.TrimSpace(text))
	if s == "" {
		return Share{}, &Error{Kind: KindInvalidShare, Message: "empty share link"}
	}
	// 剥离 115://、anxia:// 等自定义分享协议，便于按 URL / 裸码继续解析
	if m := schemeRE.FindStringSubmatch(s); m != nil {
		switch strings.ToLower(m[1]) {
		case "115", "anxia", "115pan", "hdhive", "radar":
			if rest := strings.TrimSpace(s[len(m[0]):]); rest != "" {
				s = rest
			}
		}
	}
	if m := urlShareRE.FindStringSubmatch(s); m != nil {
		pwd := ""
		if q := queryCodeRE.FindStringSubmatch(s); q != nil {
			pwd = q[1]
		}
		return Share{m[1], pwd}, nil
	}
	if m := textShareRE.FindStringSubmatch(s); m != nil {
		pwd := ""
		if q := textPwdRE.FindStringSubmatch(s); q != nil {
			pwd = q[1]
		}
		return Share{m[1], pwd}, nil
	}
	if bareRE.MatchString(s) {
		return Share{ShareCode: s}, nil
	}
	return Share{}, &Error{Kind: KindInvalidShare, Message: "unrecognized share link: " + redact(s)}
}

func New(cookie string, doer HTTPDoer, logger Logger) (*Client, error) {
	return NewWithBaseURL(defaultBaseURL, cookie, doer, logger)
}
func NewWithBaseURL(baseURL, cookie string, doer HTTPDoer, logger Logger) (*Client, error) {
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return nil, &Error{Kind: KindAuth, Message: "cookie is empty"}
	}
	if doer == nil {
		doer = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), cookie: cookie, http: doer, logger: logger, pageSize: 100, maxDepth: 4}, nil
}

func (c *Client) ListShare(ctx context.Context, share Share) ([]Item, error) {
	var out []Item
	seen := map[string]bool{}
	var walk func(string, int) error
	walk = func(cid string, depth int) error {
		if depth > c.maxDepth {
			return nil
		}
		if seen[cid] {
			return nil
		}
		seen[cid] = true
		offset := 0
		for {
			values := url.Values{"share_code": {share.ShareCode}, "receive_code": {share.ReceiveCode}, "cid": {cid}, "limit": {strconv.Itoa(c.pageSize)}, "offset": {strconv.Itoa(offset)}}
			payload, err := c.call(ctx, http.MethodGet, "/share/snap", values)
			if err != nil {
				return err
			}
			items := extractList(payload)
			for _, raw := range items {
				it := parseItem(raw)
				if it.IsDir {
					if it.ID != "" && it.ID != cid {
						if err := walk(it.ID, depth+1); err != nil {
							return err
						}
					}
				} else if it.ID != "" {
					out = append(out, it)
				}
			}
			if len(items) < c.pageSize {
				break
			}
			offset += c.pageSize
		}
		return nil
	}
	if err := walk("0", 0); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) RootFiles(ctx context.Context) ([]Item, error) {
	payload, err := c.call(ctx, http.MethodGet, "/files", url.Values{"cid": {"0"}, "limit": {"100"}, "offset": {"0"}})
	if err != nil {
		return nil, err
	}
	raws := extractList(payload)
	out := make([]Item, 0, len(raws))
	for _, r := range raws {
		out = append(out, parseItem(r))
	}
	return out, nil
}

func (c *Client) Receive(ctx context.Context, share Share, opts ReceiveOptions) (ReceiveResult, error) {
	strategy := opts.Strategy
	if strategy == "" {
		strategy = StrategyAuto
	}
	receiveID := ""
	receiveIDs := []string{}
	if strategy == StrategyAuto || strategy == StrategySingleDirectory || strategy == StrategyWholeShare {
		root, err := c.snapRoot(ctx, share)
		if err != nil {
			return ReceiveResult{}, err
		}
		if len(root) == 1 {
			it := parseItem(root[0])
			if it.IsDir {
				receiveID = it.ID
			}
		}
		if strategy == StrategySingleDirectory && receiveID == "" {
			return ReceiveResult{}, &Error{Kind: KindInvalidShare, Message: "share root is not a single directory"}
		}
		if receiveID == "" {
			for _, raw := range root {
				if id := parseItem(raw).ID; id != "" {
					receiveIDs = append(receiveIDs, id)
				}
			}
			if len(receiveIDs) == 0 {
				return ReceiveResult{}, &Error{Kind: KindInvalidShare, Message: "share root is empty"}
			}
		}
	}
	values := url.Values{"share_code": {share.ShareCode}, "receive_code": {share.ReceiveCode}}
	if receiveID != "" {
		values.Set("file_id", receiveID)
	} else if len(receiveIDs) > 0 {
		values.Set("file_id", strings.Join(receiveIDs, ","))
	}
	if opts.TargetCID != "" {
		values.Set("cid", opts.TargetCID)
	}
	payload, err := c.call(ctx, http.MethodPost, "/share/receive", values)
	if err != nil {
		if IsKind(err, KindAlreadyReceived) {
			return ReceiveResult{Already: true, ReceivedID: receiveID}, nil
		}
		return ReceiveResult{}, err
	}
	return ReceiveResult{ReceivedID: receiveID, Raw: payload}, nil
}

func (c *Client) snapRoot(ctx context.Context, share Share) ([]map[string]any, error) {
	var out []map[string]any
	for offset := 0; ; offset += c.pageSize {
		items, err := c.snapPage(ctx, share, "0", c.pageSize, offset)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(items) < c.pageSize {
			return out, nil
		}
	}
}

func (c *Client) snapPage(ctx context.Context, s Share, cid string, limit, offset int) ([]map[string]any, error) {
	p, err := c.call(ctx, http.MethodGet, "/share/snap", url.Values{"share_code": {s.ShareCode}, "receive_code": {s.ReceiveCode}, "cid": {cid}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}})
	if err != nil {
		return nil, err
	}
	return extractList(p), nil
}
func (c *Client) call(ctx context.Context, method, path string, values url.Values) (map[string]any, error) {
	var body io.Reader
	endpoint := c.baseURL + path
	if method == http.MethodGet {
		endpoint += "?" + values.Encode()
	} else {
		body = strings.NewReader(values.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", c.cookie)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	c.log(ctx, slog.LevelDebug, "115 request", "method", method, "path", path, "share_code", redact(values.Get("share_code")), "receive_code", redact(values.Get("receive_code")))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &Error{Kind: KindRemote, Message: "request failed"}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, &Error{Kind: KindProtocol, Message: err.Error()}
	}
	var payload map[string]any
	if err = json.Unmarshal(data, &payload); err != nil {
		return nil, &Error{Kind: KindProtocol, StatusCode: resp.StatusCode, Message: "invalid JSON response"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classify(resp.StatusCode, payload)
	}
	if state, ok := payload["state"].(bool); ok && !state {
		return nil, classify(resp.StatusCode, payload)
	}
	return payload, nil
}
func classify(status int, p map[string]any) *Error {
	errno := errnoString(p["errno"])
	msg := firstString(p, "error", "message", "msg")
	kind := KindRemote
	if status == http.StatusUnauthorized || status == http.StatusForbidden || strings.Contains(msg, "登录") {
		kind = KindAuth
	} else if status == http.StatusTooManyRequests {
		kind = KindRateLimit
	} else if errno == "4200045" || strings.Contains(msg, "已接收") || strings.Contains(msg, "重复") {
		kind = KindAlreadyReceived
	} else if strings.Contains(msg, "提取码") || strings.Contains(msg, "过期") || strings.Contains(msg, "失效") || strings.Contains(msg, "expired") {
		kind = KindInvalidShare
	}
	return &Error{Kind: kind, StatusCode: status, Errno: errno, Message: msg}
}
func errnoString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case json.Number:
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}
func (c *Client) log(ctx context.Context, l slog.Level, msg string, args ...any) {
	if c.logger != nil {
		c.logger.Log(ctx, l, msg, args...)
	}
}
func redact(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "***" + s[len(s)-2:]
}
func extractList(p map[string]any) []map[string]any {
	root := p
	if d, ok := p["data"].(map[string]any); ok {
		root = d
	}
	var a []any
	for _, k := range []string{"list", "data", "items"} {
		if x, ok := root[k].([]any); ok {
			a = x
			break
		}
	}
	out := make([]map[string]any, 0, len(a))
	for _, v := range a {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
func parseItem(m map[string]any) Item {
	name := firstString(m, "n", "name", "file_name")
	fc := fmt.Sprint(m["fc"])
	fid := firstString(m, "fid", "file_id")
	cid := firstString(m, "cid")
	isDir := fc == "0" || (fc == "<nil>" && fid == "")
	id := fid
	if isDir {
		id = cid
		if id == "" {
			id = fid
		}
	}
	size := int64Value(m["s"])
	if size == 0 {
		size = int64Value(m["file_size"])
	}
	return Item{ID: id, Name: name, PickCode: firstString(m, "pc", "pick_code", "pickcode"), Size: size, IsDir: isDir, IsVideo: isVideo(name) || size > 50*1024*1024, Raw: m}
}
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
func int64Value(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	return 0
}
func isVideo(n string) bool {
	n = strings.ToLower(n)
	for _, e := range []string{".mp4", ".mkv", ".ts", ".iso", ".avi", ".mov", ".wmv", ".flv", ".m2ts", ".rmvb", ".webm"} {
		if strings.HasSuffix(n, e) {
			return true
		}
	}
	return false
}
