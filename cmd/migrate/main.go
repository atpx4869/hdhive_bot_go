package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"

	appcrypto "github.com/atpx4869/hdhive_bot_go/internal/crypto"
	"github.com/atpx4869/hdhive_bot_go/internal/migrate"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

func main() {
	users := flag.String("users", "", "旧 telegram_users.json 路径")
	p115 := flag.String("p115", "", "旧 telegram_p115.json 路径")
	dsn := flag.String("dsn", "", "SQLite DSN；默认读取 SQLITE_DSN")
	key := flag.String("encryption-key", "", "base64 32 字节密钥；默认读取 ENCRYPTION_KEY")
	flag.Parse()
	if *users == "" && *p115 == "" {
		fmt.Fprintln(os.Stderr, "至少指定 --users 或 --p115")
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
	result, err := migrate.Import(context.Background(), db, *users, *p115)
	if err != nil {
		fail(err)
	}
	fmt.Printf("迁移完成：users=%d p115_accounts=%d\n", result.Users, result.Accounts)

	// 安全警告
	fmt.Println("\n⚠️  安全提示：")
	if *users != "" {
		fmt.Printf("  请安全删除旧用户文件: %s\n", *users)
	}
	if *p115 != "" {
		fmt.Printf("  请安全删除旧 115 配置文件: %s\n", *p115)
	}
	fmt.Println("  这些文件包含明文敏感数据，迁移后应立即删除。")
	fmt.Println("  建议使用: shred -u <file> 或手动删除并清空回收站。")
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
