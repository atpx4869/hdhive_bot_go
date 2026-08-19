package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

// Cryptor encrypts/decrypts account configuration bound to a user ID.
type Cryptor interface {
	Encrypt(userID int64, plaintext []byte) ([]byte, error)
	Decrypt(userID int64, ciphertext []byte) ([]byte, error)
}

type Store struct {
	db     *sql.DB
	crypt  Cryptor
	nowUTC func() time.Time
}

type User struct {
	ID         int64
	Authorized bool
	Note       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type P115Config struct {
	Cookie    string `json:"cookie"`
	TargetCID string `json:"target_cid"`
	Enabled   bool   `json:"enabled"`
}

type UnlockRecord struct {
	UserID     int64
	ResourceID string
	Status     string
	Result     []byte
	UpdatedAt  time.Time
}

type ActivityLog struct {
	ID            int64
	UserID        int64
	Action        string
	Detail        string
	Status        string
	MediaTitle    string
	ResourceTitle string
	ErrorCode     string
	CreatedAt     time.Time
}

type ActivityQuery struct {
	UserID *int64
	Action string
	Status string
	Since  *time.Time
	Until  *time.Time
	Limit  int
	Offset int
}

// Open opens SQLite, verifies connectivity, and applies schema migrations.
func Open(ctx context.Context, dsn string, crypt Cryptor) (*Store, error) {
	if dsn == "" {
		return nil, errors.New("dsn is required")
	}
	if crypt == nil {
		return nil, errors.New("cryptor is required")
	}
	// 检查 SQLite 文件权限
	if err := checkFilePermissions(dsn); err != nil {
		slog.Warn("sqlite file permission warning", "error", err)
	}
	db, err := sql.Open("sqlite", withForeignKeys(dsn))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	var foreignKeys int
	if err := db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		db.Close()
		if err != nil {
			return nil, fmt.Errorf("verify sqlite foreign keys: %w", err)
		}
		return nil, errors.New("sqlite foreign keys could not be enabled")
	}

	s := &Store{db: db, crypt: crypt, nowUTC: func() time.Time { return time.Now().UTC() }}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func withForeignKeys(dsn string) string {
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + "_pragma=foreign_keys(1)"
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY,
    authorized INTEGER NOT NULL DEFAULT 0 CHECK (authorized IN (0, 1)),
    note TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS p115_accounts (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    encrypted_config BLOB NOT NULL,
    target_cid TEXT NOT NULL DEFAULT '0',
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS unlock_records (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    resource_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('in_flight', 'success', 'rejected', 'unknown')),
    encrypted_result BLOB,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, resource_id)
);
CREATE TABLE IF NOT EXISTS activity_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    action TEXT NOT NULL CHECK (length(action) > 0),
    detail TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_activity_logs_user_created ON activity_logs(user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_activity_logs_action_created ON activity_logs(action, created_at DESC, id DESC);
`
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlite migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate sqlite schema: %w", err)
	}
	// Existing databases created by older releases need these additive columns.
	for _, statement := range []string{
		`ALTER TABLE p115_accounts ADD COLUMN target_cid TEXT NOT NULL DEFAULT '0'`,
		`ALTER TABLE p115_accounts ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1))`,
		`ALTER TABLE activity_logs ADD COLUMN status TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE activity_logs ADD COLUMN media_title TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE activity_logs ADD COLUMN resource_title TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE activity_logs ADD COLUMN error_code TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return fmt.Errorf("migrate sqlite schema: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite migration: %w", err)
	}
	return nil
}

func (s *Store) ensureUser(ctx context.Context, userID int64) error {
	if userID <= 0 {
		return errors.New("userID must be positive")
	}
	now := s.nowUTC().UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO users(id, created_at, updated_at) VALUES(?, ?, ?) ON CONFLICT(id) DO NOTHING`, userID, now, now)
	return err
}

func (s *Store) GetUser(ctx context.Context, userID int64) (User, error) {
	var user User
	var authorized int
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx, `SELECT id, authorized, note, created_at, updated_at FROM users WHERE id = ?`, userID).
		Scan(&user.ID, &authorized, &user.Note, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	user.Authorized = authorized == 1
	user.CreatedAt = time.UnixMilli(createdAt).UTC()
	user.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	return user, nil
}

func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]User, error) {
	if limit < 0 || limit > 500 || offset < 0 {
		return nil, errors.New("limit must be between 0 and 500 and offset must be non-negative")
	}
	if limit == 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, authorized, note, created_at, updated_at FROM users ORDER BY id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		var user User
		var authorized int
		var createdAt, updatedAt int64
		if err := rows.Scan(&user.ID, &authorized, &user.Note, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		user.Authorized = authorized == 1
		user.CreatedAt, user.UpdatedAt = time.UnixMilli(createdAt).UTC(), time.UnixMilli(updatedAt).UTC()
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

func (s *Store) SetUserAuthorization(ctx context.Context, userID int64, authorized bool) error {
	if err := s.ensureUser(ctx, userID); err != nil {
		return fmt.Errorf("ensure user: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET authorized = ?, updated_at = ? WHERE id = ?`, authorized, s.nowUTC().UnixMilli(), userID)
	if err != nil {
		return fmt.Errorf("set user authorization: %w", err)
	}
	return nil
}

func (s *Store) SetUserNote(ctx context.Context, userID int64, note string) error {
	if err := s.ensureUser(ctx, userID); err != nil {
		return fmt.Errorf("ensure user: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET note = ?, updated_at = ? WHERE id = ?`, note, s.nowUTC().UnixMilli(), userID)
	if err != nil {
		return fmt.Errorf("set user note: %w", err)
	}
	return nil
}

func (s *Store) SetP115Config(ctx context.Context, userID int64, cfg P115Config) error {
	if cfg.Cookie == "" {
		return errors.New("p115 cookie is required")
	}
	if cfg.TargetCID == "" {
		cfg.TargetCID = "0"
	}
	plaintext, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal p115 config: %w", err)
	}
	encrypted, err := s.crypt.Encrypt(userID, plaintext)
	if err != nil {
		return fmt.Errorf("encrypt p115 config: %w", err)
	}
	if err := s.ensureUser(ctx, userID); err != nil {
		return fmt.Errorf("ensure user: %w", err)
	}
	now := s.nowUTC().UnixMilli()
	_, err = s.db.ExecContext(ctx, `
INSERT INTO p115_accounts(user_id, encrypted_config, target_cid, enabled, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id) DO UPDATE SET encrypted_config = excluded.encrypted_config, target_cid = excluded.target_cid, enabled = excluded.enabled, updated_at = excluded.updated_at`, userID, encrypted, cfg.TargetCID, cfg.Enabled, now, now)
	if err != nil {
		return fmt.Errorf("set p115 config: %w", err)
	}
	return nil
}

func (s *Store) GetP115Config(ctx context.Context, userID int64) (P115Config, error) {
	var encrypted []byte
	var targetCID string
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT encrypted_config, target_cid, enabled FROM p115_accounts WHERE user_id = ?`, userID).Scan(&encrypted, &targetCID, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return P115Config{}, ErrNotFound
	}
	if err != nil {
		return P115Config{}, fmt.Errorf("get p115 config: %w", err)
	}
	plaintext, err := s.crypt.Decrypt(userID, encrypted)
	if err != nil {
		return P115Config{}, fmt.Errorf("decrypt p115 config: %w", err)
	}
	var cfg P115Config
	if err := json.Unmarshal(plaintext, &cfg); err != nil {
		return P115Config{}, fmt.Errorf("decode p115 config: %w", err)
	}
	if cfg.Cookie == "" {
		return P115Config{}, errors.New("stored p115 config is invalid")
	}
	cfg.TargetCID = targetCID
	cfg.Enabled = enabled == 1
	return cfg, nil
}

func (s *Store) DisableP115Config(ctx context.Context, userID int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE p115_accounts SET enabled = 0, updated_at = ? WHERE user_id = ? AND enabled = 1`, s.nowUTC().UnixMilli(), userID)
	if err != nil {
		return fmt.Errorf("disable p115 config: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("p115 rows affected: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteP115Config(ctx context.Context, userID int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM p115_accounts WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete p115 config: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("p115 rows affected: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ClaimUnlock(ctx context.Context, userID int64, resourceID string) (bool, error) {
	if userID <= 0 || strings.TrimSpace(resourceID) == "" {
		return false, errors.New("unlock user and resource are required")
	}
	if err := s.ensureUser(ctx, userID); err != nil {
		return false, err
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO unlock_records(user_id, resource_id, status, updated_at) VALUES(?, ?, 'in_flight', ?) ON CONFLICT(user_id, resource_id) DO NOTHING`, userID, resourceID, s.nowUTC().UnixMilli())
	if err != nil {
		return false, fmt.Errorf("claim unlock: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim unlock rows: %w", err)
	}
	return rows == 1, nil
}

func (s *Store) GetUnlockRecord(ctx context.Context, userID int64, resourceID string) (UnlockRecord, error) {
	var record UnlockRecord
	var encrypted []byte
	var updated int64
	err := s.db.QueryRowContext(ctx, `SELECT status, encrypted_result, updated_at FROM unlock_records WHERE user_id = ? AND resource_id = ?`, userID, resourceID).Scan(&record.Status, &encrypted, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return UnlockRecord{}, ErrNotFound
	}
	if err != nil {
		return UnlockRecord{}, fmt.Errorf("get unlock record: %w", err)
	}
	record.UserID, record.ResourceID, record.UpdatedAt = userID, resourceID, time.UnixMilli(updated).UTC()
	if len(encrypted) > 0 {
		plaintext, err := s.crypt.Decrypt(userID, encrypted)
		if err != nil {
			return UnlockRecord{}, fmt.Errorf("decrypt unlock result: %w", err)
		}
		record.Result = plaintext
	}
	return record, nil
}

func (s *Store) ResetUnlockRecord(ctx context.Context, userID int64, resourceID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM unlock_records WHERE user_id = ? AND resource_id = ? AND status = 'unknown'`, userID, resourceID)
	if err != nil {
		return fmt.Errorf("reset unlock record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reset unlock rows affected: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetUnlockRecord(ctx context.Context, record UnlockRecord) error {
	if record.UserID <= 0 || strings.TrimSpace(record.ResourceID) == "" {
		return errors.New("unlock user and resource are required")
	}
	if err := s.ensureUser(ctx, record.UserID); err != nil {
		return err
	}
	var encrypted []byte
	var err error
	if len(record.Result) > 0 {
		encrypted, err = s.crypt.Encrypt(record.UserID, record.Result)
		if err != nil {
			return fmt.Errorf("encrypt unlock result: %w", err)
		}
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO unlock_records(user_id, resource_id, status, encrypted_result, updated_at) VALUES(?, ?, ?, ?, ?) ON CONFLICT(user_id, resource_id) DO UPDATE SET status=excluded.status, encrypted_result=excluded.encrypted_result, updated_at=excluded.updated_at`, record.UserID, record.ResourceID, record.Status, encrypted, s.nowUTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("set unlock record: %w", err)
	}
	return nil
}

func (s *Store) AddActivityLog(ctx context.Context, userID int64, action, detail string, opts ...ActivityLogOption) (ActivityLog, error) {
	if userID <= 0 {
		return ActivityLog{}, errors.New("userID must be positive")
	}
	if action == "" {
		return ActivityLog{}, errors.New("action is required")
	}
	opt := ActivityLogOptions{}
	for _, o := range opts {
		o(&opt)
	}
	now := s.nowUTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO activity_logs(user_id, action, detail, status, media_title, resource_title, error_code, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, action, detail, opt.Status, opt.MediaTitle, opt.ResourceTitle, opt.ErrorCode, now.UnixMilli())
	if err != nil {
		return ActivityLog{}, fmt.Errorf("add activity log: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return ActivityLog{}, fmt.Errorf("activity log ID: %w", err)
	}
	return ActivityLog{
		ID:            id,
		UserID:        userID,
		Action:        action,
		Detail:        detail,
		Status:        opt.Status,
		MediaTitle:    opt.MediaTitle,
		ResourceTitle: opt.ResourceTitle,
		ErrorCode:     opt.ErrorCode,
		CreatedAt:     now,
	}, nil
}

// ActivityLogOptions 活动日志选项
type ActivityLogOptions struct {
	Status        string
	MediaTitle    string
	ResourceTitle string
	ErrorCode     string
}

// ActivityLogOption 活动日志选项函数
type ActivityLogOption func(*ActivityLogOptions)

// WithStatus 设置状态
func WithStatus(status string) ActivityLogOption {
	return func(o *ActivityLogOptions) { o.Status = status }
}

// WithMediaTitle 设置媒体标题
func WithMediaTitle(title string) ActivityLogOption {
	return func(o *ActivityLogOptions) { o.MediaTitle = title }
}

// WithResourceTitle 设置资源标题
func WithResourceTitle(title string) ActivityLogOption {
	return func(o *ActivityLogOptions) { o.ResourceTitle = title }
}

// WithErrorCode 设置错误码
func WithErrorCode(code string) ActivityLogOption {
	return func(o *ActivityLogOptions) { o.ErrorCode = code }
}

func (s *Store) QueryActivityLogs(ctx context.Context, q ActivityQuery) ([]ActivityLog, error) {
	if q.Limit < 0 || q.Limit > 500 || q.Offset < 0 {
		return nil, errors.New("limit must be between 0 and 500 and offset must be non-negative")
	}
	limit := q.Limit
	if limit == 0 {
		limit = 100
	}
	query := `SELECT id, user_id, action, detail, status, media_title, resource_title, error_code, created_at FROM activity_logs WHERE 1=1`
	args := make([]any, 0, 8)
	if q.UserID != nil {
		query += ` AND user_id = ?`
		args = append(args, *q.UserID)
	}
	if q.Action != "" {
		query += ` AND action = ?`
		args = append(args, q.Action)
	}
	if q.Status != "" {
		query += ` AND status = ?`
		args = append(args, q.Status)
	}
	if q.Since != nil {
		query += ` AND created_at >= ?`
		args = append(args, q.Since.UTC().UnixMilli())
	}
	if q.Until != nil {
		query += ` AND created_at <= ?`
		args = append(args, q.Until.UTC().UnixMilli())
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, q.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query activity logs: %w", err)
	}
	defer rows.Close()

	logs := make([]ActivityLog, 0)
	for rows.Next() {
		var log ActivityLog
		var createdAt int64
		if err := rows.Scan(&log.ID, &log.UserID, &log.Action, &log.Detail, &log.Status, &log.MediaTitle, &log.ResourceTitle, &log.ErrorCode, &createdAt); err != nil {
			return nil, fmt.Errorf("scan activity log: %w", err)
		}
		log.CreatedAt = time.UnixMilli(createdAt).UTC()
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate activity logs: %w", err)
	}
	return logs, nil
}

// checkFilePermissions checks SQLite file permissions and warns if too open.
func checkFilePermissions(dsn string) error {
	// 只在 Linux/macOS 上检查权限
	if runtime.GOOS == "windows" {
		return nil
	}
	// 从 DSN 中提取文件路径
	dbPath := extractDBPath(dsn)
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	// 检查文件是否存在
	info, err := os.Stat(dbPath)
	if os.IsNotExist(err) {
		return nil // 文件不存在，稍后会创建
	}
	if err != nil {
		return fmt.Errorf("stat sqlite file: %w", err)
	}
	// 检查权限
	mode := info.Mode().Perm()
	if mode&0077 != 0 {
		return fmt.Errorf("sqlite file %s has permissions %o (recommended: 0600)", dbPath, mode)
	}
	return nil
}

// extractDBPath extracts the file path from a SQLite DSN.
func extractDBPath(dsn string) string {
	// 处理 file: 前缀
	if strings.HasPrefix(dsn, "file:") {
		dsn = strings.TrimPrefix(dsn, "file:")
		// 移除查询参数
		if idx := strings.Index(dsn, "?"); idx >= 0 {
			dsn = dsn[:idx]
		}
	}
	// 处理相对路径
	if dsn != "" && !filepath.IsAbs(dsn) {
		abs, err := filepath.Abs(dsn)
		if err == nil {
			return abs
		}
	}
	return dsn
}
