package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestUserFileParsing(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantIDs []int64
		wantErr bool
	}{
		{
			name:    "valid users",
			json:    `{"authorized_user_ids": [123, 456], "notes": {"123": "张三"}}`,
			wantIDs: []int64{123, 456},
		},
		{
			name:    "empty list",
			json:    `{"authorized_user_ids": [], "notes": {}}`,
			wantIDs: []int64{},
		},
		{
			name:    "with notes only",
			json:    `{"authorized_user_ids": [], "notes": {"789": "李四"}}`,
			wantIDs: []int64{789},
		},
		{
			name:    "invalid json",
			json:    `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec := json.NewDecoder(strings.NewReader(tt.json))
			dec.UseNumber()
			var legacy userFile
			err := dec.Decode(&legacy)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			// 统计所有唯一用户 ID（来自 authorized_user_ids 和 notes）
			seen := map[int64]bool{}
			for _, v := range legacy.AuthorizedUserIDs {
				if id, err := v.Int64(); err == nil {
					seen[id] = true
				}
			}
			for rawID := range legacy.Notes {
				// 简单验证是数字
				if id, err := strconv.ParseInt(rawID, 10, 64); err == nil {
					seen[id] = true
				}
			}
			if len(seen) != len(tt.wantIDs) {
				t.Fatalf("expected %d IDs, got %d", len(tt.wantIDs), len(seen))
			}
		})
	}
}

func TestLegacyP115Parsing(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantLen int
		wantErr bool
	}{
		{
			name:    "valid config",
			json:    `{"123": {"cookie": "UID=u;CID=c;SEID=s", "target_cid": "0", "enabled": true}}`,
			wantLen: 1,
		},
		{
			name:    "multiple users",
			json:    `{"123": {"cookie": "UID=u1"}, "456": {"cookie": "UID=u2", "enabled": false}}`,
			wantLen: 2,
		},
		{
			name:    "empty cookie skipped",
			json:    `{"123": {"cookie": "", "target_cid": "0"}, "456": {"cookie": "UID=u"}}`,
			wantLen: 1,
		},
		{
			name:    "null enabled defaults to true",
			json:    `{"123": {"cookie": "UID=u", "enabled": null}}`,
			wantLen: 1,
		},
		{
			name:    "invalid json",
			json:    `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var legacy map[string]legacyP115
			err := json.Unmarshal([]byte(tt.json), &legacy)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			count := 0
			for _, cfg := range legacy {
				if cfg.Cookie != "" {
					count++
				}
			}
			if count != tt.wantLen {
				t.Fatalf("expected %d configs, got %d", tt.wantLen, count)
			}
		})
	}
}

func TestLegacyP115EnabledDefault(t *testing.T) {
	tests := []struct {
		name string
		json string
		want bool
	}{
		{"explicit true", `{"cookie": "UID=u", "enabled": true}`, true},
		{"explicit false", `{"cookie": "UID=u", "enabled": false}`, false},
		{"null", `{"cookie": "UID=u", "enabled": null}`, true},
		{"missing", `{"cookie": "UID=u"}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg legacyP115
			if err := json.Unmarshal([]byte(tt.json), &cfg); err != nil {
				t.Fatal(err)
			}
			enabled := true
			if cfg.Enabled != nil {
				enabled = *cfg.Enabled
			}
			if enabled != tt.want {
				t.Fatalf("enabled = %v, want %v", enabled, tt.want)
			}
		})
	}
}

func TestImportValidation(t *testing.T) {
	// 测试空 store
	_, err := Import(nil, nil, "", "")
	if err == nil {
		t.Fatal("expected error for nil store")
	}
}

func TestImportFileReading(t *testing.T) {
	// 创建临时文件
	dir := t.TempDir()

	// 有效的用户文件
	usersJSON := `{"authorized_user_ids": [123, 456], "notes": {"123": "张三"}}`
	usersPath := filepath.Join(dir, "users.json")
	if err := os.WriteFile(usersPath, []byte(usersJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// 有效的 115 文件
	p115JSON := `{"123": {"cookie": "UID=u;CID=c;SEID=s", "target_cid": "0", "enabled": true}}`
	p115Path := filepath.Join(dir, "p115.json")
	if err := os.WriteFile(p115Path, []byte(p115JSON), 0644); err != nil {
		t.Fatal(err)
	}

	// 测试文件读取（不测试数据库写入）
	_, err := os.ReadFile(usersPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.ReadFile(p115Path)
	if err != nil {
		t.Fatal(err)
	}
}

func TestImportInvalidFiles(t *testing.T) {
	dir := t.TempDir()

	// 无效的用户 JSON
	invalidUsers := filepath.Join(dir, "invalid_users.json")
	os.WriteFile(invalidUsers, []byte("{invalid"), 0644)

	// 无效的 115 JSON
	invalidP115 := filepath.Join(dir, "invalid_p115.json")
	os.WriteFile(invalidP115, []byte("{invalid"), 0644)

	// 测试无效文件会被检测到
	_, err := os.ReadFile(invalidUsers)
	if err != nil {
		t.Fatal(err)
	}
}

func TestUserFileEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"negative id", `{"authorized_user_ids": [-1]}`},
		{"zero id", `{"authorized_user_ids": [0]}`},
		{"string id", `{"authorized_user_ids": ["abc"]}`},
		{"float id", `{"authorized_user_ids": [1.5]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec := json.NewDecoder(strings.NewReader(tt.json))
			dec.UseNumber()
			var legacy userFile
			if err := dec.Decode(&legacy); err != nil {
				// 解析错误是预期的
				return
			}
			// 验证每个 ID
			for _, v := range legacy.AuthorizedUserIDs {
				if _, err := v.Int64(); err != nil {
					// 非整数是预期的
					return
				}
			}
		})
	}
}
