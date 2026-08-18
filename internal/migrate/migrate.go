package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

type Result struct{ Users, Accounts int }

type userFile struct {
	AuthorizedUserIDs []json.Number     `json:"authorized_user_ids"`
	Notes             map[string]string `json:"notes"`
}
type legacyP115 struct {
	Cookie    string `json:"cookie"`
	TargetCID string `json:"target_cid"`
	Enabled   *bool  `json:"enabled"`
}

func Import(ctx context.Context, db *store.Store, usersPath, p115Path string) (Result, error) {
	if db == nil {
		return Result{}, errors.New("store is required")
	}
	var result Result
	if usersPath != "" {
		data, err := os.ReadFile(usersPath)
		if err != nil {
			return result, fmt.Errorf("read users JSON: %w", err)
		}
		dec := json.NewDecoder(strings.NewReader(string(data)))
		dec.UseNumber()
		var legacy userFile
		if err := dec.Decode(&legacy); err != nil {
			return result, fmt.Errorf("decode users JSON: %w", err)
		}
		seen := map[int64]bool{}
		for _, value := range legacy.AuthorizedUserIDs {
			id, err := strconv.ParseInt(string(value), 10, 64)
			if err != nil || id <= 0 {
				return result, fmt.Errorf("invalid authorized user ID %q", value)
			}
			if err := db.SetUserAuthorization(ctx, id, true); err != nil {
				return result, err
			}
			seen[id] = true
			if note := strings.TrimSpace(legacy.Notes[strconv.FormatInt(id, 10)]); note != "" {
				if err := db.SetUserNote(ctx, id, note); err != nil {
					return result, err
				}
			}
		}
		for rawID, note := range legacy.Notes {
			id, err := strconv.ParseInt(rawID, 10, 64)
			if err != nil || id <= 0 {
				return result, fmt.Errorf("invalid note user ID %q", rawID)
			}
			if !seen[id] {
				if err := db.SetUserAuthorization(ctx, id, false); err != nil {
					return result, err
				}
			}
			if err := db.SetUserNote(ctx, id, strings.TrimSpace(note)); err != nil {
				return result, err
			}
			seen[id] = true
		}
		result.Users = len(seen)
	}
	if p115Path != "" {
		data, err := os.ReadFile(p115Path)
		if err != nil {
			return result, fmt.Errorf("read 115 JSON: %w", err)
		}
		var legacy map[string]legacyP115
		if err := json.Unmarshal(data, &legacy); err != nil {
			return result, fmt.Errorf("decode 115 JSON: %w", err)
		}
		for rawID, cfg := range legacy {
			id, err := strconv.ParseInt(rawID, 10, 64)
			if err != nil || id <= 0 {
				return result, fmt.Errorf("invalid 115 user ID %q", rawID)
			}
			cfg.Cookie = strings.TrimSpace(cfg.Cookie)
			if cfg.Cookie == "" {
				continue
			}
			enabled := true
			if cfg.Enabled != nil {
				enabled = *cfg.Enabled
			}
			target := strings.TrimSpace(cfg.TargetCID)
			if target == "" {
				target = "0"
			}
			if err := db.SetP115Config(ctx, id, store.P115Config{Cookie: cfg.Cookie, TargetCID: target, Enabled: enabled}); err != nil {
				return result, err
			}
			result.Accounts++
		}
	}
	return result, nil
}
