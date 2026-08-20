package main

import (
	"context"
	"fmt"
	"os"

	"github.com/atpx4869/hdhive_bot_go/internal/config"
	"github.com/atpx4869/hdhive_bot_go/internal/hdhive"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
	"github.com/atpx4869/hdhive_bot_go/internal/tmdb"

	appcrypto "github.com/atpx4869/hdhive_bot_go/internal/crypto"
)

func main() {
	fmt.Println("==========================================")
	fmt.Println("HDHive Bot Go - 配置检查工具")
	fmt.Println("==========================================")
	fmt.Println()

	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("❌ 配置加载失败: %v\n", err)
		os.Exit(1)
	}

	// 创建加密器
	crypt, err := appcrypto.New(cfg.EncryptionKey)
	if err != nil {
		fmt.Printf("❌ 加密密钥无效: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	allOk := true

	// 1. 检查 Telegram Bot
	fmt.Println("=== 1. Telegram Bot ===")
	if cfg.TelegramToken != "" {
		fmt.Printf("✅ Bot Token: 已设置 (%d 字符)\n", len(cfg.TelegramToken))
	} else {
		fmt.Println("❌ Bot Token: 未设置")
		allOk = false
	}

	if len(cfg.AdminUserIDs) > 0 {
		fmt.Printf("✅ 管理员 ID: %v\n", cfg.AdminUserIDs)
	} else {
		fmt.Println("❌ 管理员 ID: 未设置")
		allOk = false
	}

	// 2. 检查 TMDB
	fmt.Println("\n=== 2. TMDB ===")
	if cfg.TMDBToken != "" {
		fmt.Printf("✅ TMDB Token: 已设置 (%d 字符)\n", len(cfg.TMDBToken))
		
		// 测试 TMDB 连接
		tmdbClient, err := tmdb.New(cfg.TMDBToken, nil)
		if err != nil {
			fmt.Printf("❌ TMDB 客户端创建失败: %v\n", err)
			allOk = false
		} else {
			result, err := tmdbClient.Search(ctx, "测试", tmdb.SearchOptions{Page: 1})
			if err != nil {
				fmt.Printf("❌ TMDB 搜索测试失败: %v\n", err)
				allOk = false
			} else {
				fmt.Printf("✅ TMDB 搜索测试通过 (返回 %d 个结果)\n", len(result.Results))
			}
		}
	} else {
		fmt.Println("❌ TMDB Token: 未设置")
		allOk = false
	}

	// 3. 检查 HDHive
	fmt.Println("\n=== 3. HDHive ===")
	if cfg.HDHiveBaseURL != "" {
		fmt.Printf("✅ HDHive 基址: %s\n", cfg.HDHiveBaseURL)
	} else {
		fmt.Println("❌ HDHive 基址: 未设置")
		allOk = false
	}

	if cfg.HDHiveSecret != "" {
		fmt.Printf("✅ HDHive 密钥: 已设置 (%d 字符)\n", len(cfg.HDHiveSecret))
	} else {
		fmt.Println("❌ HDHive 密钥: 未设置")
		allOk = false
	}

	if cfg.HDHiveUserID != "" {
		fmt.Printf("✅ HDHive 用户 ID: %s\n", cfg.HDHiveUserID)
	} else {
		fmt.Println("❌ HDHive 用户 ID: 未设置")
		allOk = false
	}

	if cfg.HDHiveUserKey != "" {
		fmt.Printf("✅ HDHive 用户 Key: 已设置 (%d 字符)\n", len(cfg.HDHiveUserKey))
	} else {
		fmt.Println("❌ HDHive 用户 Key: 未设置")
		allOk = false
	}

	// 测试 HDHive 连接
	if cfg.HDHiveBaseURL != "" && cfg.HDHiveSecret != "" && cfg.HDHiveUserID != "" && cfg.HDHiveUserKey != "" {
		hdhiveClient, err := hdhive.New(cfg.HDHiveBaseURL, cfg.HDHiveSecret, cfg.HDHiveUserID, cfg.HDHiveUserKey, nil)
		if err != nil {
			fmt.Printf("❌ HDHive 客户端创建失败: %v\n", err)
			allOk = false
		} else {
			// 测试会话协商
			resources, err := hdhiveClient.Resources(ctx, "movie", 1)
			if err != nil {
				fmt.Printf("⚠️  HDHive 资源查询测试: %v (可能是正常的)\n", err)
			} else {
				fmt.Printf("✅ HDHive 连接测试通过 (返回 %d 个资源)\n", len(resources))
			}
		}
	}

	// 4. 检查数据库
	fmt.Println("\n=== 4. 数据库 ===")
	if cfg.DatabaseDSN != "" {
		fmt.Printf("✅ 数据库 DSN: %s\n", cfg.DatabaseDSN)
		
		db, err := store.Open(ctx, cfg.DatabaseDSN, crypt)
		if err != nil {
			fmt.Printf("❌ 数据库连接失败: %v\n", err)
			allOk = false
		} else {
			defer db.Close()
			fmt.Println("✅ 数据库连接成功")
			
			// 检查数据库大小
			size, err := db.GetDatabaseSize(ctx)
			if err == nil {
				fmt.Printf("   数据库大小: %d 字节\n", size)
			}
		}
	} else {
		fmt.Println("❌ 数据库 DSN: 未设置")
		allOk = false
	}

	// 5. 检查加密密钥
	fmt.Println("\n=== 5. 加密密钥 ===")
	if len(cfg.EncryptionKey) == 32 {
		fmt.Printf("✅ 加密密钥: 已设置 (32 字节)\n")
	} else {
		fmt.Printf("❌ 加密密钥: 无效长度 (%d 字节, 需要 32)\n", len(cfg.EncryptionKey))
		allOk = false
	}

	// 6. 检查 115 配置
	fmt.Println("\n=== 6. 115 配置 ===")
	fmt.Printf("   115 超时: %v\n", cfg.P115Timeout)
	if len(cfg.P115UserAgent) > 50 {
		fmt.Printf("   115 User-Agent: %s...\n", cfg.P115UserAgent[:50])
	} else {
		fmt.Printf("   115 User-Agent: %s\n", cfg.P115UserAgent)
	}
	fmt.Printf("   115 Endpoint: %s\n", cfg.P115Endpoint)

	// 7. 检查超时配置
	fmt.Println("\n=== 7. 超时配置 ===")
	fmt.Printf("   HTTP 超时: %v\n", cfg.HTTPTimeout)
	fmt.Printf("   TMDB 超时: %v\n", cfg.TMDBTimeout)
	fmt.Printf("   HDHive 超时: %v\n", cfg.HDHiveTimeout)
	fmt.Printf("   115 超时: %v\n", cfg.P115Timeout)
	fmt.Printf("   Session TTL: %v\n", cfg.SessionTTL)
	fmt.Printf("   Session 容量: %d\n", cfg.SessionCapacity)

	// 8. 检查代理配置
	fmt.Println("\n=== 8. 代理配置 ===")
	if cfg.HTTPProxyURL != "" {
		fmt.Printf("✅ HTTP 代理: %s\n", cfg.HTTPProxyURL)
	} else {
		fmt.Println("   HTTP 代理: 未配置 (直连)")
	}

	// 总结
	fmt.Println("\n==========================================")
	if allOk {
		fmt.Println("✅ 所有配置检查通过！")
		fmt.Println("\n可以启动 worker: go run ./cmd/worker")
	} else {
		fmt.Println("❌ 部分配置检查失败")
		fmt.Println("\n请检查环境变量配置：")
		fmt.Println("  TELEGRAM_BOT_TOKEN - Telegram Bot Token")
		fmt.Println("  TELEGRAM_ADMIN_USER_IDS - 管理员 Telegram ID")
		fmt.Println("  TMDB_TOKEN - TMDB API Token")
		fmt.Println("  HDHIVE_BASE_URL - HDHive 代理地址")
		fmt.Println("  HDHIVE_SECRET - HDHive 签名密钥")
		fmt.Println("  HDHIVE_USER_ID - HDHive 用户 ID")
		fmt.Println("  HDHIVE_USER_KEY - HDHive 访问密钥")
		fmt.Println("  SQLITE_DSN - 数据库路径")
		fmt.Println("  ENCRYPTION_KEY - 加密密钥 (base64 32字节)")
	}
	fmt.Println("==========================================")
}
