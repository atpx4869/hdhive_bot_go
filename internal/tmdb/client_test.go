package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchAuthAndFiltering(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		wantAPIKey bool
	}{
		{name: "v3", token: "0123456789abcdef0123456789abcdef", wantAPIKey: true},
		{name: "v4", token: "long-read-access-token", wantAPIKey: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Path; got != "/search/multi" {
					t.Fatalf("path = %q", got)
				}
				if tt.wantAPIKey {
					if r.URL.Query().Get("api_key") != tt.token || r.Header.Get("Authorization") != "" {
						t.Fatalf("unexpected v3 auth")
					}
				} else if r.Header.Get("Authorization") != "Bearer "+tt.token || r.URL.Query().Get("api_key") != "" {
					t.Fatalf("unexpected v4 auth")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"page":1,"results":[{"id":1,"media_type":"movie","title":"A"},{"id":2,"media_type":"person"},{"id":0,"media_type":"tv"}]}`))
			}))
			defer srv.Close()

			client, err := NewWithBaseURL(srv.URL, tt.token, srv.Client())
			if err != nil {
				t.Fatal(err)
			}
			got, err := client.Search(context.Background(), " test ", SearchOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Results) != 1 || got.Results[0].Title != "A" {
				t.Fatalf("results = %#v", got.Results)
			}
		})
	}
}

func TestSearchAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status_message":"bad token"}`))
	}))
	defer srv.Close()
	client, _ := NewWithBaseURL(srv.URL, "v4-token", srv.Client())
	_, err := client.Search(context.Background(), "x", SearchOptions{})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %#v", err)
	}
}
