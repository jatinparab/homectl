package manager

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Disabled     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Session struct {
	ID         int64
	UserID     int64
	TokenHash  string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastUsedAt time.Time
}

type AgentConfigRecord struct {
	AgentName  string
	Version    int64
	Nonce      []byte
	Ciphertext []byte
	KeyID      string
	UpdatedAt  time.Time
}

type UserRepository interface {
	CountUsers(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, username, passwordHash string) (User, error)
	GetByUsername(ctx context.Context, username string) (User, error)
	GetByID(ctx context.Context, id int64) (User, error)
}

type SessionRepository interface {
	CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) (Session, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error)
	DeleteSessionByTokenHash(ctx context.Context, tokenHash string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error
	TouchSession(ctx context.Context, id int64, lastUsedAt time.Time) error
}

type AgentConfigRepository interface {
	UpsertAgentConfig(ctx context.Context, agentName string, nonce, ciphertext []byte, keyID string) (AgentConfigRecord, error)
	GetAgentConfig(ctx context.Context, agentName string) (AgentConfigRecord, error)
}

var ErrNotFound = errors.New("not found")

type Store struct {
	db           *sql.DB
	Users        UserRepository
	Sessions     SessionRepository
	AgentConfigs AgentConfigRepository
}

func OpenStore(stateDir string) (*Store, error) {
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(stateDir, "homectl.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrateStore(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	repo := &sqliteRepo{db: db}
	return &Store{
		db:           db,
		Users:        repo,
		Sessions:     repo,
		AgentConfigs: repo,
	}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func migrateStore(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			disabled INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_used_at TEXT NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,
		`CREATE TABLE IF NOT EXISTS agent_configs (
			agent_name TEXT PRIMARY KEY,
			version INTEGER NOT NULL,
			nonce BLOB NOT NULL,
			ciphertext BLOB NOT NULL,
			key_id TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, ?)`, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

type sqliteRepo struct {
	db *sql.DB
}

func (r *sqliteRepo) CountUsers(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *sqliteRepo) CreateUser(ctx context.Context, username, passwordHash string) (User, error) {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `INSERT INTO users(username, password_hash, disabled, created_at, updated_at) VALUES(?, ?, 0, ?, ?)`, username, passwordHash, formatDBTime(now), formatDBTime(now))
	if err != nil {
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return User{ID: id, Username: username, PasswordHash: passwordHash, CreatedAt: now, UpdatedAt: now}, nil
}

func (r *sqliteRepo) GetByUsername(ctx context.Context, username string) (User, error) {
	return scanUser(r.db.QueryRowContext(ctx, `SELECT id, username, password_hash, disabled, created_at, updated_at FROM users WHERE username = ?`, username))
}

func (r *sqliteRepo) GetByID(ctx context.Context, id int64) (User, error) {
	return scanUser(r.db.QueryRowContext(ctx, `SELECT id, username, password_hash, disabled, created_at, updated_at FROM users WHERE id = ?`, id))
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (User, error) {
	var u User
	var disabled int
	var created, updated string
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &disabled, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	u.Disabled = disabled != 0
	u.CreatedAt = parseDBTime(created)
	u.UpdatedAt = parseDBTime(updated)
	return u, nil
}

func (r *sqliteRepo) CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) (Session, error) {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `INSERT INTO sessions(user_id, token_hash, expires_at, created_at, last_used_at) VALUES(?, ?, ?, ?, ?)`,
		userID, tokenHash, formatDBTime(expiresAt), formatDBTime(now), formatDBTime(now))
	if err != nil {
		return Session{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Session{}, err
	}
	return Session{ID: id, UserID: userID, TokenHash: tokenHash, ExpiresAt: expiresAt, CreatedAt: now, LastUsedAt: now}, nil
}

func (r *sqliteRepo) GetSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	var s Session
	var expires, created, lastUsed string
	err := r.db.QueryRowContext(ctx, `SELECT id, user_id, token_hash, expires_at, created_at, last_used_at FROM sessions WHERE token_hash = ?`, tokenHash).
		Scan(&s.ID, &s.UserID, &s.TokenHash, &expires, &created, &lastUsed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrNotFound
		}
		return Session{}, err
	}
	s.ExpiresAt = parseDBTime(expires)
	s.CreatedAt = parseDBTime(created)
	s.LastUsedAt = parseDBTime(lastUsed)
	return s, nil
}

func (r *sqliteRepo) DeleteSessionByTokenHash(ctx context.Context, tokenHash string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (r *sqliteRepo) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, formatDBTime(now))
	return err
}

func (r *sqliteRepo) TouchSession(ctx context.Context, id int64, lastUsedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE sessions SET last_used_at = ? WHERE id = ?`, formatDBTime(lastUsedAt), id)
	return err
}

func (r *sqliteRepo) UpsertAgentConfig(ctx context.Context, agentName string, nonce, ciphertext []byte, keyID string) (AgentConfigRecord, error) {
	now := time.Now().UTC()
	var version int64
	err := r.db.QueryRowContext(ctx, `SELECT version FROM agent_configs WHERE agent_name = ?`, agentName).Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AgentConfigRecord{}, err
	}
	version++
	_, err = r.db.ExecContext(ctx, `INSERT INTO agent_configs(agent_name, version, nonce, ciphertext, key_id, updated_at)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_name) DO UPDATE SET version = excluded.version, nonce = excluded.nonce, ciphertext = excluded.ciphertext, key_id = excluded.key_id, updated_at = excluded.updated_at`,
		agentName, version, nonce, ciphertext, keyID, formatDBTime(now))
	if err != nil {
		return AgentConfigRecord{}, err
	}
	return AgentConfigRecord{AgentName: agentName, Version: version, Nonce: nonce, Ciphertext: ciphertext, KeyID: keyID, UpdatedAt: now}, nil
}

func (r *sqliteRepo) GetAgentConfig(ctx context.Context, agentName string) (AgentConfigRecord, error) {
	var rec AgentConfigRecord
	var updated string
	err := r.db.QueryRowContext(ctx, `SELECT agent_name, version, nonce, ciphertext, key_id, updated_at FROM agent_configs WHERE agent_name = ?`, agentName).
		Scan(&rec.AgentName, &rec.Version, &rec.Nonce, &rec.Ciphertext, &rec.KeyID, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AgentConfigRecord{}, ErrNotFound
		}
		return AgentConfigRecord{}, err
	}
	rec.UpdatedAt = parseDBTime(updated)
	return rec, nil
}

func formatDBTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseDBTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func normalizeUsername(username string) string {
	return stringsToLowerTrim(username)
}

func stringsToLowerTrim(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
