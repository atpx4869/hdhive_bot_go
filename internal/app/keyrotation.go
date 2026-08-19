package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/atpx4869/hdhive_bot_go/internal/crypto"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

// RotateEncryptionKey re-encrypts all sensitive data (115 cookies and unlock results)
// with a new encryption key.
func RotateEncryptionKey(ctx context.Context, db *store.Store, oldCryptor, newCryptor *crypto.Cipher, logger *slog.Logger) error {
	if db == nil || oldCryptor == nil || newCryptor == nil {
		return fmt.Errorf("store and both ciphers are required")
	}

	users, err := db.ListUsers(ctx, 10000, 0)
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}

	rotatedCookies := 0
	rotatedUnlocks := 0

	for _, user := range users {
		// Rotate 115 cookie
		cfg, err := db.GetP115Config(ctx, user.ID)
		if err == nil {
			oldCookie, err := oldCryptor.Decrypt(user.ID, []byte(cfg.Cookie))
			if err != nil {
				logger.Warn("failed to decrypt cookie for user, skipping",
					"user_id", user.ID, "error", err)
			} else {
				newCookie, err := newCryptor.Encrypt(user.ID, oldCookie)
				if err != nil {
					return fmt.Errorf("encrypt cookie for user %d: %w", user.ID, err)
				}
				cfg.Cookie = string(newCookie)
				if err := db.SetP115Config(ctx, user.ID, cfg); err != nil {
					return fmt.Errorf("update cookie for user %d: %w", user.ID, err)
				}
				rotatedCookies++
				logger.Info("rotated cookie encryption", "user_id", user.ID)
			}
		}

		// Rotate unlock records
		rotated, err := rotateUnlockRecords(ctx, db, user.ID, oldCryptor, newCryptor, logger)
		if err != nil {
			logger.Warn("failed to rotate unlock records for user",
				"user_id", user.ID, "error", err)
			continue
		}
		rotatedUnlocks += rotated
	}

	logger.Info("encryption key rotation completed",
		"rotated_cookies", rotatedCookies,
		"rotated_unlocks", rotatedUnlocks)
	return nil
}

// rotateUnlockRecords re-encrypts all success unlock results for a single user.
func rotateUnlockRecords(ctx context.Context, db *store.Store, userID int64, oldCryptor, newCryptor *crypto.Cipher, logger *slog.Logger) (int, error) {
	// Get all unlock records by querying known patterns
	// We need to iterate all records for this user
	records, err := db.ListUnlockRecordsByUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("list unlock records: %w", err)
	}

	rotated := 0
	for _, record := range records {
		if record.Status != "success" || len(record.Result) == 0 {
			continue
		}

		// Decrypt with old key
		plaintext, err := oldCryptor.Decrypt(userID, record.Result)
		if err != nil {
			logger.Warn("failed to decrypt unlock result, skipping",
				"user_id", userID,
				"resource_id", record.ResourceID,
				"error", err)
			continue
		}

		// Encrypt with new key
		newEncrypted, err := newCryptor.Encrypt(userID, plaintext)
		if err != nil {
			return rotated, fmt.Errorf("encrypt unlock result for user %d resource %s: %w",
				userID, record.ResourceID, err)
		}

		// Update record using raw method to avoid double encryption
		// (SetUnlockRecord would re-encrypt with the store's current cryptor)
		if err := db.SetUnlockRecordRaw(ctx, userID, record.ResourceID, record.Status, newEncrypted); err != nil {
			return rotated, fmt.Errorf("update unlock record: %w", err)
		}
		rotated++
	}

	return rotated, nil
}

// ValidateKeyRotation checks that all encrypted data can be decrypted with the old key.
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
		// Check 115 cookie
		cfg, err := db.GetP115Config(ctx, user.ID)
		if err == nil {
			if _, err := oldCryptor.Decrypt(user.ID, []byte(cfg.Cookie)); err == nil {
				decryptable++
			}
		}

		// Check unlock records
		records, err := db.ListUnlockRecordsByUser(ctx, user.ID)
		if err == nil {
			for _, record := range records {
				if record.Status == "success" && len(record.Result) > 0 {
					if _, err := oldCryptor.Decrypt(user.ID, record.Result); err == nil {
						decryptable++
					}
				}
			}
		}
	}

	return decryptable, nil
}
