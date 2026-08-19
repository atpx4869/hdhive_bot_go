package hdhive

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	baseURL, secret, userID, userKey string
	http                             HTTPDoer
	now                              func() time.Time
	nonce                            func() (string, error)
	mu                               sync.Mutex
	sessionID                        string
	sessionKey                       []byte
	expiresAt                        time.Time
	sequence                         uint64
}

type APIError struct {
	StatusCode int
	Message    string
	Business   bool
}

func (e *APIError) Error() string { return fmt.Sprintf("hdhive HTTP %d: %s", e.StatusCode, e.Message) }

type Resource map[string]any

type UnlockResult struct {
	Raw                         map[string]any
	URL, ShareCode, ReceiveCode string
}

func New(baseURL, secret, userID, userKey string, doer HTTPDoer) (*Client, error) {
	if strings.TrimSpace(secret) == "" || strings.TrimSpace(userID) == "" {
		return nil, errors.New("hdhive secret and user ID are required")
	}
	if doer == nil {
		doer = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), secret: strings.TrimSpace(secret), userID: strings.TrimSpace(userID), userKey: strings.TrimSpace(userKey), http: doer, now: time.Now, nonce: randomNonce}, nil
}

func (c *Client) Resources(ctx context.Context, mediaType string, tmdbID int64) ([]Resource, error) {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType != "tv" {
		mediaType = "movie"
	}
	if tmdbID <= 0 {
		return nil, errors.New("tmdb ID must be positive")
	}
	payload, err := c.request(ctx, http.MethodGet, "/resources/"+mediaType+"/"+strconv.FormatInt(tmdbID, 10), nil)
	if err != nil {
		return nil, err
	}
	return normalizeResources(payload), nil
}

func (c *Client) Unlock(ctx context.Context, slug string) (UnlockResult, error) {
	slug = stripScheme(slug)
	if slug == "" {
		return UnlockResult{}, errors.New("unlock slug is empty")
	}
	payload, err := c.request(ctx, http.MethodPost, "/resources/unlock", map[string]any{"slug": slug})
	if err != nil {
		return UnlockResult{}, err
	}
	if failed(payload) {
		return UnlockResult{}, &APIError{StatusCode: http.StatusOK, Message: message(payload), Business: true}
	}
	result := UnlockResult{Raw: payload}
	digUnlock(payload, &result)
	if result.URL == "" && result.ShareCode == "" {
		return UnlockResult{}, errors.New("hdhive unlock succeeded without a usable link")
	}
	return result, nil
}

func (c *Client) request(ctx context.Context, method, path string, body any) (map[string]any, error) {
	if c.userKey == "" {
		return nil, errors.New("hdhive proxy user key is empty")
	}
	for attempt := 0; attempt < 2; attempt++ {
		payload, status, err := c.signedRequest(ctx, method, path, body)
		if err != nil {
			return nil, err
		}
		if status == http.StatusForbidden && attempt == 0 && authFailure(message(payload)) {
			c.mu.Lock()
			c.sessionID = ""
			c.sessionKey = nil
			c.expiresAt = time.Time{}
			c.mu.Unlock()
			continue
		}
		if status < 200 || status >= 300 {
			return nil, &APIError{StatusCode: status, Message: message(payload), Business: status >= 400 && status < 500}
		}
		return payload, nil
	}
	return nil, errors.New("hdhive authentication retry exhausted")
}

func (c *Client) signedRequest(ctx context.Context, method, path string, body any) (map[string]any, int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureSession(ctx); err != nil {
		return nil, 0, err
	}
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
	}
	fullPath := "/api/v1/open/" + url.PathEscape(c.userID) + ensureSlash(path)
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+fullPath, bytes.NewReader(raw))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.sequence++
	seq := strconv.FormatUint(c.sequence, 10)
	hash := sha256.Sum256(raw)
	hashHex := hex.EncodeToString(hash[:])
	sig := sign(c.sessionKey, method, req.URL.EscapedPath(), c.sessionID, seq, hashHex, c.userKey)
	req.Header.Set("X-Proxy-Session", c.sessionID)
	req.Header.Set("X-Proxy-Sequence", seq)
	req.Header.Set("X-Proxy-Body-SHA256", hashHex)
	req.Header.Set("X-Proxy-User-Key", c.userKey)
	req.Header.Set("X-Proxy-Signature", sig)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("hdhive request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, 0, err
	}
	var payload map[string]any
	if err = json.Unmarshal(data, &payload); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode hdhive response (HTTP %d): %w", resp.StatusCode, err)
	}
	return payload, resp.StatusCode, nil
}

func (c *Client) ensureSession(ctx context.Context) error {
	if c.sessionID != "" && c.now().Before(c.expiresAt.Add(-30*time.Second)) {
		return nil
	}
	clientNonce, err := c.nonce()
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"client_nonce": clientNonce, "client_proof": proof(c.secret, "client", clientNonce)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/auth/session", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("open hdhive session: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Message: "session negotiation failed"}
	}
	var outer map[string]any
	if json.Unmarshal(data, &outer) != nil {
		return errors.New("invalid hdhive session response")
	}
	d := asMap(outer["data"])
	if d == nil {
		d = outer
	}
	sid := stringValue(d["session_id"])
	serverNonce := stringValue(d["server_nonce"])
	serverProof := stringValue(d["server_proof"])
	if sid == "" || serverNonce == "" {
		return errors.New("hdhive session response is incomplete")
	}
	if !hmac.Equal([]byte(serverProof), []byte(proof(c.secret, "server", serverNonce))) {
		return errors.New("hdhive server proof validation failed")
	}
	expires := intValue(d["expires_in"], 3600)
	if expires < 60 {
		expires = 60
	}
	c.sessionID = sid
	c.sessionKey = deriveKey(c.secret, clientNonce, serverNonce)
	c.sequence = 0
	c.expiresAt = c.now().Add(time.Duration(expires) * time.Second)
	return nil
}

func proof(secret, label, nonce string) string {
	h := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(h, "hdhive-openproxy-proof\n%s\n%s", label, nonce)
	return hex.EncodeToString(h.Sum(nil))
}
func deriveKey(secret, clientNonce, serverNonce string) []byte {
	salt := []byte("hdhive-openproxy-session:" + clientNonce + ":" + serverNonce)
	prk := mac(salt, []byte(secret))
	return mac(prk, append([]byte("hdhive-openproxy-session-key"), 1))[:32]
}
func mac(key, msg []byte) []byte { h := hmac.New(sha256.New, key); h.Write(msg); return h.Sum(nil) }
func sign(key []byte, parts ...string) string {
	return hex.EncodeToString(mac(key, []byte(strings.Join(parts, "\n"))))
}
func randomNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func ensureSlash(s string) string {
	if strings.HasPrefix(s, "/") {
		return s
	}
	return "/" + s
}
func stripScheme(s string) string {
	s = strings.TrimSpace(s)
	l := strings.ToLower(s)
	for _, p := range []string{"hdhive://", "radar://"} {
		if strings.HasPrefix(l, p) {
			return s[len(p):]
		}
	}
	return s
}
func authFailure(s string) bool {
	return strings.Contains(s, "密钥") || strings.Contains(s, "签名") || strings.Contains(s, "缺少必要请求头")
}
func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
func intValue(v any, def int) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		i, e := strconv.Atoi(x)
		if e == nil {
			return i
		}
	}
	return def
}
func message(m map[string]any) string {
	for _, k := range []string{"message", "description", "error", "msg"} {
		if s := stringValue(m[k]); s != "" {
			return s
		}
	}
	if d := asMap(m["data"]); d != nil {
		return message(d)
	}
	return "request failed"
}
func failed(m map[string]any) bool {
	for _, x := range []map[string]any{m, asMap(m["data"])} {
		if x == nil {
			continue
		}
		if x["success"] == false || x["ok"] == false || stringValue(x["error"]) != "" {
			return true
		}
		if c, ok := x["code"]; ok {
			v := fmt.Sprint(c)
			if v != "0" && v != "200" && v != "<nil>" {
				return true
			}
		}
	}
	return false
}
func normalizeResources(v any) []Resource {
	var list []any
	switch x := v.(type) {
	case []any:
		list = x
	case map[string]any:
		for _, k := range []string{"data", "results", "items", "resources", "list"} {
			if a, ok := x[k].([]any); ok {
				list = a
				break
			}
			if d := asMap(x[k]); d != nil {
				for _, k2 := range []string{"list", "items", "results", "resources"} {
					if a, ok := d[k2].([]any); ok {
						list = a
						break
					}
				}
			}
		}
	}
	out := make([]Resource, 0, len(list))
	for _, v := range list {
		if m := asMap(v); m != nil {
			if _, ok := m["website"]; !ok {
				if p := stringValue(m["pan_type"]); p != "" {
					m["website"] = p
				} else {
					m["website"] = "其他"
				}
			}
			out = append(out, m)
		}
	}
	return out
}
func digUnlock(v any, r *UnlockResult) {
	digUnlockDepth(v, r, 0)
}

func digUnlockDepth(v any, r *UnlockResult, depth int) {
	if depth > 10 {
		return
	}
	switch x := v.(type) {
	case map[string]any:
		if r.ShareCode == "" {
			r.ShareCode = stringValue(x["share_code"])
		}
		if r.ReceiveCode == "" {
			for _, k := range []string{"access_code", "receive_code", "password"} {
				if s := stringValue(x[k]); s != "" {
					r.ReceiveCode = s
					break
				}
			}
		}
		for _, k := range []string{"full_url", "url", "share_url", "link", "real_115_url"} {
			if r.URL == "" {
				r.URL = stringValue(x[k])
			}
		}
		for _, y := range x {
			digUnlockDepth(y, r, depth+1)
		}
	case []any:
		for _, y := range x {
			digUnlockDepth(y, r, depth+1)
		}
	}
}
