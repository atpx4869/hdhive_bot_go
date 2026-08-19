package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	appcrypto "github.com/atpx4869/hdhive_bot_go/internal/crypto"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

func main() {
	dsn := flag.String("dsn", "", "SQLite DSN；默认读取 SQLITE_DSN")
	key := flag.String("encryption-key", "", "base64 32 字节密钥；默认读取 ENCRYPTION_KEY")
	output := flag.String("output", "", "输出文件路径；默认输出到 stdout")
	flag.Parse()

	if *dsn == "" {
		*dsn = strings.TrimSpace(os.Getenv("SQLITE_DSN"))
	}
	if *key == "" {
		*key = strings.TrimSpace(os.Getenv("ENCRYPTION_KEY"))
	}
	if *dsn == "" || *key == "" {
		fail(fmt.Errorf("必须通过参数或环境变量提供 SQLite DSN 与 encryption key"))
	}

	decodedKey, err := base64.StdEncoding.Strict().DecodeString(*key)
	if err != nil || len(decodedKey) != 32 {
		fail(fmt.Errorf("encryption key 必须是 32 字节的 canonical base64"))
	}

	crypt, err := appcrypto.New(decodedKey)
	if err != nil {
		fail(err)
	}

	db, err := store.Open(context.Background(), *dsn, crypt)
	if err != nil {
		fail(err)
	}
	defer db.Close()

	ctx := context.Background()
	export := map[string]any{
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"version":     "1.0",
	}

	// 导出用户列表（脱敏）
	users, err := db.ListUsers(ctx, 10000, 0)
	if err != nil {
		fail(fmt.Errorf("导出用户失败：%w", err))
	}
	exportUsers := make([]map[string]any, 0, len(users))
	for _, u := range users {
		exportUsers = append(exportUsers, map[string]any{
			"id":         u.ID,
			"authorized": u.Authorized,
			"note":       u.Note,
			"created_at": u.CreatedAt.Format(time.RFC3339),
		})
	}
	export["users"] = exportUsers

	// 导出 115 配置（脱敏 Cookie）
	exportAccounts := make([]map[string]any, 0)
	for _, u := range users {
		cfg, err := db.GetP115Config(ctx, u.ID)
		if err != nil {
			continue
		}
		// 脱敏 Cookie
		maskedCookie := maskCookie(cfg.Cookie)
		exportAccounts = append(exportAccounts, map[string]any{
			"user_id":    u.ID,
			"cookie":     maskedCookie,
			"target_cid": cfg.TargetCID,
			"enabled":    cfg.Enabled,
		})
	}
	export["p115_accounts"] = exportAccounts

	// 导出活动日志统计
	logCount, oldest, newest, err := db.GetActivityLogsStats(ctx)
	if err == nil {
		export["activity_logs"] = map[string]any{
			"count":  logCount,
			"oldest": time.UnixMilli(oldest).UTC().Format(time.RFC3339),
			"newest": time.UnixMilli(newest).UTC().Format(time.RFC3339),
		}
	}

	// 数据库大小
	size, err := db.GetDatabaseSize(ctx)
	if err == nil {
		export["database_size_bytes"] = size
	}

	// 输出
	var out *os.File
	if *output == "" {
		out = os.Stdout
	} else {
		out, err = os.Create(*output)
		if err != nil {
			fail(fmt.Errorf("创建输出文件失败：%w", err))
		}
		defer out.Close()
	}

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(export); err != nil {
		fail(fmt.Errorf("编码 JSON 失败：%w", err))
	}

	if *output != "" {
		fmt.Printf("导出完成：%s\n", *output)
	}
}

func maskCookie(cookie string) string {
	if cookie == "" {
		return ""
	}
	parts := strings.Split(cookie, ";")
	masked := make([]string, 0, len(parts))
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			key := strings.TrimSpace(kv[0])
			// 只保留键名，值用 *** 替代
			masked = append(masked, key+"=***")
		}
	}
	return strings.Join(masked, "; ")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
