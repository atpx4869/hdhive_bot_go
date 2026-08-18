package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const defaultBaseURL = "https://api.themoviedb.org/3"

// HTTPDoer permits injecting an http.Client or test double.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	baseURL string
	token   string
	http    HTTPDoer
}

type SearchOptions struct {
	Language     string
	Page         int
	IncludeAdult bool
}

type SearchResult struct {
	ID            int64   `json:"id"`
	MediaType     string  `json:"media_type"`
	Title         string  `json:"title"`
	Name          string  `json:"name"`
	OriginalTitle string  `json:"original_title"`
	OriginalName  string  `json:"original_name"`
	Overview      string  `json:"overview"`
	ReleaseDate   string  `json:"release_date"`
	FirstAirDate  string  `json:"first_air_date"`
	PosterPath    string  `json:"poster_path"`
	VoteAverage   float64 `json:"vote_average"`
}

type SearchResponse struct {
	Page         int            `json:"page"`
	Results      []SearchResult `json:"results"`
	TotalPages   int            `json:"total_pages"`
	TotalResults int            `json:"total_results"`
}

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string { return fmt.Sprintf("tmdb HTTP %d: %s", e.StatusCode, e.Message) }

func New(token string, doer HTTPDoer) (*Client, error) {
	return NewWithBaseURL(defaultBaseURL, token, doer)
}

func NewWithBaseURL(baseURL, token string, doer HTTPDoer) (*Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("tmdb token is empty")
	}
	if doer == nil {
		doer = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: strings.TrimSpace(token), http: doer}, nil
}

func (c *Client) Search(ctx context.Context, query string, opts SearchOptions) (SearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchResponse{}, errors.New("tmdb search query is empty")
	}
	language := opts.Language
	if language == "" {
		language = "zh-CN"
	}
	page := opts.Page
	if page < 1 {
		page = 1
	}
	params := url.Values{
		"query":         {query},
		"language":      {language},
		"page":          {strconv.Itoa(page)},
		"include_adult": {strconv.FormatBool(opts.IncludeAdult)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/search/multi?"+params.Encode(), nil)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("build tmdb request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if isV3Key(c.token) {
		q := req.URL.Query()
		q.Set("api_key", c.token)
		req.URL.RawQuery = q.Encode()
	} else {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return SearchResponse{}, errors.New("tmdb request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return SearchResponse{}, fmt.Errorf("read tmdb response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			StatusMessage string `json:"status_message"`
		}
		_ = json.Unmarshal(body, &payload)
		msg := strings.TrimSpace(payload.StatusMessage)
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		return SearchResponse{}, &APIError{StatusCode: resp.StatusCode, Message: msg}
	}
	var result SearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return SearchResponse{}, fmt.Errorf("decode tmdb response: %w", err)
	}
	filtered := result.Results[:0]
	for _, item := range result.Results {
		if item.ID > 0 && (item.MediaType == "movie" || item.MediaType == "tv") {
			filtered = append(filtered, item)
		}
	}
	result.Results = filtered
	return result, nil
}

func isV3Key(token string) bool {
	if len(token) != 32 {
		return false
	}
	for _, r := range token {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}
