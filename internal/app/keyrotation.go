package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/atpx4869/hdhive_bot_go/internal/crypto"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

// RotateEncryptionKey 使用新密钥重新加密数据库中的敏感数据
func RotateEncryptionKey(ctx context.Context, db *store.Store, oldCryptor, newCryptor *crypto.Cipher, logger *slog.Logger) error {
	if db == nil || oldCryptor == nil || newCryptor == nil {
		return fmt.Errorf("store and both ciphers are required")
	}

	// 获取所有用户
	users, err := db.ListUsers(ctx, 10000, 0)
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}

	rotated := 0
	for _, user := range users {
		// 获取用户的 115 配置
		cfg, err := db.GetP115Config(ctx, user.ID)
		if err != nil {
			continue // 没有配置，跳过
		}

		// 解密旧的 cookie
		oldCookie, err := oldCryptor.Decrypt(user.ID, []byte(cfg.Cookie))
		if err != nil {
			logger.Warn("failed to decrypt cookie for user, skipping",
				"user_id", user.ID,
				"error", err,
			)
			continue
		}

		// 使用新密钥加密
		newCookie, err := newCryptor.Encrypt(user.ID, oldCookie)
		if err != nil {
			return fmt.Errorf("encrypt cookie for user %d: %w", user.ID, err)
		}

		// 更新配置
		cfg.Cookie = string(newCookie)
		if err := db.SetP115Config(ctx, user.ID, cfg); err != nil {
			return fmt.Errorf("update cookie for user %d: %w", user.ID, err)
		}

		rotated++
		logger.Info("rotated encryption for user", "user_id", user.ID)
	}

	// 注意：解锁记录的重新加密需要更复杂的逻辑
	// 当前实现只处理 115 Cookie

	logger.Info("encryption key rotation completed", "rotated_users", rotated)
	return nil
}

// ValidateKeyRotation 验证密钥轮换是否可行
func ValidateKeyRotation(ctx context.Context, db *store.Store, oldCryptor *crypto.Cipher) (int, error) {
	if db == nil || oldCryptor == nil {
		return 0, fmt.Errorf("store and cipher are required")
	}

	users, err := db.ListUsers(ctx, 10000, 0)
	if err != nil {
		return 0, fmt.Errorf("list users: %w", err)
	}

	decryptable := 0
	for _, user := range users {
		cfg, err := db.GetP115Config(ctx, user.ID)
		if err != nil {
			continue
		}

		// 尝试解密
		_, err = oldCryptor.Decrypt(user.ID, []byte(cfg.Cookie))
		if err == nil {
			decryptable++
		}
	}

	return decryptable, nil
}
