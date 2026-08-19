package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	appcrypto "github.com/atpx4869/hdhive_bot_go/internal/crypto"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

func main() {
	filePath := flag.String("file", "", "JSON 文件路径")
	dsn := flag.String("dsn", "", "SQLite DSN；默认读取 SQLITE_DSN")
	key := flag.String("encryption-key", "", "base64 32 字节密钥；默认读取 ENCRYPTION_KEY")
	flag.Parse()

	if *filePath == "" {
		fmt.Fprintln(os.Stderr, "必须指定 --file 参数")
		os.Exit(2)
	}

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

	// 读取文件
	data, err := os.ReadFile(*filePath)
	if err != nil {
		fail(fmt.Errorf("读取文件失败：%w", err))
	}

	// 解析 JSON
	var importData map[string]any
	if err := json.Unmarshal(data, &importData); err != nil {
		fail(fmt.Errorf("JSON 解析失败：%w", err))
	}

	// 检查数据源
	source, _ := importData["source"].(string)
	fmt.Printf("数据源：%s\n", source)

	ctx := context.Background()
	imported := 0
	errors := []string{}

	// 导入用户数据
	if users, ok := importData["users"]; ok {
		switch u := users.(type) {
		case map[string]any: // Python 格式
			fmt.Printf("检测到 Python 格式用户数据\n")
			for uidStr, userData := range u {
				var uid int64
				fmt.Sscanf(uidStr, "%d", &uid)
				if uid <= 0 {
					continue
				}
				userMap, ok := userData.(map[string]any)
				if !ok {
					continue
				}
				authorized, _ := userMap["authorized"].(bool)
				note, _ := userMap["note"].(string)

				if err := db.SetUserAuthorization(ctx, uid, authorized); err != nil {
					errors = append(errors, fmt.Sprintf("用户 %d: %v", uid, err))
					continue
				}
				if note != "" {
					db.SetUserNote(ctx, uid, note)
				}
				imported++
				fmt.Printf("  导入用户 %d (授权: %v, 备注: %s)\n", uid, authorized, note)
			}
		case []any: // Go 格式
			fmt.Printf("检测到 Go 格式用户数据\n")
			for _, userData := range u {
				userMap, ok := userData.(map[string]any)
				if !ok {
					continue
				}
				uid, _ := userMap["id"].(float64)
				if uid <= 0 {
					continue
				}
				authorized, _ := userMap["authorized"].(bool)
				note, _ := userMap["note"].(string)

				if err := db.SetUserAuthorization(ctx, int64(uid), authorized); err != nil {
					errors = append(errors, fmt.Sprintf("用户 %d: %v", int64(uid), err))
					continue
				}
				if note != "" {
					db.SetUserNote(ctx, int64(uid), note)
				}
				imported++
				fmt.Printf("  导入用户 %d (授权: %v, 备注: %s)\n", int64(uid), authorized, note)
			}
		}
	}

	// 导入 115 配置
	if accounts, ok := importData["p115_accounts"]; ok {
		if accMap, ok := accounts.(map[string]any); ok {
			fmt.Printf("检测到 115 配置数据\n")
			for uidStr, accData := range accMap {
				var uid int64
				fmt.Sscanf(uidStr, "%d", &uid)
				if uid <= 0 {
					continue
				}
				accMap, ok := accData.(map[string]any)
				if !ok {
					continue
				}
				cookie, _ := accMap["cookie"].(string)
				targetCID, _ := accMap["target_cid"].(string)
				enabled, _ := accMap["enabled"].(bool)

				if cookie == "" {
					continue
				}

				// 检查是否是脱敏的 Cookie
				if strings.Contains(cookie, "=***") {
					fmt.Printf("  跳过用户 %d (Cookie 已脱敏)\n", uid)
					continue
				}

				if err := db.SetP115Config(ctx, uid, store.P115Config{
					Cookie:    cookie,
					TargetCID: targetCID,
					Enabled:   enabled,
				}); err != nil {
					errors = append(errors, fmt.Sprintf("115 配置 %d: %v", uid, err))
					continue
				}
				fmt.Printf("  导入 115 配置 %d (目标: %s, 启用: %v)\n", uid, targetCID, enabled)
			}
		}
	}

	// 总结
	fmt.Println("\n========================================")
	fmt.Printf("导入完成\n")
	fmt.Printf("• 导入用户：%d 个\n", imported)
	if len(errors) > 0 {
		fmt.Printf("• 错误：%d 个\n", len(errors))
		for _, e := range errors {
			fmt.Printf("  • %s\n", e)
		}
	}
	fmt.Println("========================================")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
