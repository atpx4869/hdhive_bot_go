package hdhive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionSigningResourcesAndUnlock(t *testing.T) {
	secret := "proxy-secret"
	clientNonce := "fixed-client-nonce"
	serverNonce := "server-nonce"
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/auth/session" {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["client_proof"] != proof(secret, "client", clientNonce) {
				t.Fatal("bad client proof")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"session_id": "sid", "server_nonce": serverNonce, "server_proof": proof(secret, "server", serverNonce), "expires_in": 3600}})
			return
		}
		bodyHash := sha256.Sum256(nil)
		if r.Method == http.MethodPost {
			bodyHash = sha256.Sum256([]byte(`{"slug":"abc"}`))
		}
		wantSig := sign(deriveKey(secret, clientNonce, serverNonce), r.Method, r.URL.EscapedPath(), "sid", r.Header.Get("X-Proxy-Sequence"), hex.EncodeToString(bodyHash[:]), "user-key")
		if r.Header.Get("X-Proxy-Signature") != wantSig {
			t.Fatalf("bad signature")
		}
		switch r.URL.Path {
		case "/api/v1/open/u1/resources/movie/550":
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"slug":"abc","pan_type":"115"}]}}`))
		case "/api/v1/open/u1/resources/unlock":
			_, _ = w.Write([]byte(`{"success":true,"data":{"full_url":"https://115.com/s/share?password=pwd"}}`))
		default:
			t.Fatalf("path=%s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c, err := New(srv.URL, secret, "u1", "user-key", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.nonce = func() (string, error) { return clientNonce, nil }
	items, err := c.Resources(context.Background(), "film", 550)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0]["website"] != "115" {
		t.Fatalf("items=%#v", items)
	}
	unlocked, err := c.Unlock(context.Background(), "hdhive://abc")
	if err != nil {
		t.Fatal(err)
	}
	if unlocked.URL == "" {
		t.Fatalf("unlock=%#v", unlocked)
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestUnlockBusinessFailure(t *testing.T) {
	secret := "s"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/auth/session" {
			_, _ = w.Write([]byte(`{"data":{"session_id":"x","server_nonce":"n","server_proof":"` + proof(secret, "server", "n") + `"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"error":"积分不足"}}`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, secret, "u", "k", srv.Client())
	c.nonce = func() (string, error) { return "c", nil }
	_, err := c.Unlock(context.Background(), "slug")
	apiErr, ok := err.(*APIError)
	if !ok || !apiErr.Business {
		t.Fatalf("err=%#v", err)
	}
}
