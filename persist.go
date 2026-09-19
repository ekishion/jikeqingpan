package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// 纯 Go SQLite 驱动（无 CGO），仅在 state_backend=sqlite 时实际使用。
	_ "modernc.org/sqlite"
)

// persistedLink 持久化的短链条目（目录短链与下载短链共用结构）。
type persistedLink struct {
	Token     string `json:"token"`
	Path      string `json:"path"`
	ExpiresAt int64  `json:"expires_at"` // unix nano
	CreatedAt int64  `json:"created_at"` // unix nano
	Uses      int    `json:"uses,omitempty"`
}

// persistedState 落盘状态快照。
type persistedState struct {
	Version    int             `json:"version"`
	DirLinks   []persistedLink `json:"dir_links,omitempty"`
	ShortLinks []persistedLink `json:"short_links,omitempty"`
}

// statePersistence 状态持久化后端。实现必须保证 Save 的原子性：
// 要么完整写入旧快照，要么完整写入新快照，不允许半份状态。
type statePersistence interface {
	Load() (*persistedState, error)
	Save(*persistedState) error
	Close() error
}

// newStatePersistence 按配置构造后端；state_backend=none 时返回 (nil, nil)。
func newStatePersistence(cfg *Config) (statePersistence, error) {
	switch cfg.StateBackend {
	case "sqlite":
		return newSQLitePersistence(cfg.StatePath)
	case "json":
		return newJSONPersistence(cfg.StatePath)
	default:
		return nil, nil
	}
}

// ===== JSON 后端 =====

type jsonPersistence struct {
	path string
}

func newJSONPersistence(path string) (*jsonPersistence, error) {
	// filepath.Dir("state.json") == "."，MkdirAll(".") 是无害 no-op
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return &jsonPersistence{path: path}, nil
}

func (p *jsonPersistence) Load() (*persistedState, error) {
	data, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return &persistedState{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var st persistedState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("状态文件损坏: %w", err)
	}
	return &st, nil
}

// Save 通过临时文件 + 原子重命名写入，崩溃时不会留下半份快照。
func (p *jsonPersistence) Save(st *persistedState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p.path)
}

func (p *jsonPersistence) Close() error { return nil }

// ===== SQLite 后端 =====

// sqlitePersistence 以 SQLite 为 durable KV：state 表每行存一段 JSON 快照
// （dir_links / short_links）。单写者场景，WAL + 单连接规避锁竞争。
// 语句全部内联字面量，值经 ? 占位符参数化传入，不存在动态拼接。
type sqlitePersistence struct {
	db *sql.DB
}

func newSQLitePersistence(path string) (*sqlitePersistence, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// 打开失败或库损坏时统一走上层 quarantine 重建，这里只保证连接可用。
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化 SQLite %q: %w", path, err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化 SQLite %q: %w", path, err)
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化 SQLite %q: %w", path, err)
	}
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS state (key TEXT PRIMARY KEY, value BLOB NOT NULL, updated_at INTEGER NOT NULL)"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqlitePersistence{db: db}, nil
}

func (p *sqlitePersistence) Load() (*persistedState, error) {
	st := &persistedState{Version: 1}

	var dirRaw []byte
	err := p.db.QueryRow("SELECT value FROM state WHERE key = ?", "dir_links").Scan(&dirRaw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// 首次启动，无历史状态
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(dirRaw, &st.DirLinks); err != nil {
			return nil, fmt.Errorf("状态行 dir_links 损坏: %w", err)
		}
	}

	var shortRaw []byte
	err = p.db.QueryRow("SELECT value FROM state WHERE key = ?", "short_links").Scan(&shortRaw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(shortRaw, &st.ShortLinks); err != nil {
			return nil, fmt.Errorf("状态行 short_links 损坏: %w", err)
		}
	}
	return st, nil
}

func (p *sqlitePersistence) Save(st *persistedState) error {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	dirRaw, err := json.Marshal(st.DirLinks)
	if err != nil {
		return err
	}
	shortRaw, err := json.Marshal(st.ShortLinks)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	if _, err := tx.Exec("INSERT OR REPLACE INTO state (key, value, updated_at) VALUES (?, ?, ?)", "dir_links", dirRaw, now); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT OR REPLACE INTO state (key, value, updated_at) VALUES (?, ?, ?)", "short_links", shortRaw, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *sqlitePersistence) Close() error { return p.db.Close() }
