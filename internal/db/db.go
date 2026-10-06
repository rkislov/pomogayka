package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"github.com/rkislov/pomogayka/internal/config"
	"github.com/rkislov/pomogayka/internal/models"
)

var MigrationSQL string

func Open(cfg config.Config) (*sql.DB, error) {
	dsn := cfg.DatabaseURL
	if strings.HasPrefix(dsn, "file:") {
		path := strings.TrimPrefix(strings.Split(dsn, "?")[0], "file:")
		if dir := filepath.Dir(path); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
	}
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetConnMaxLifetime(time.Hour)
	if err := database.Ping(); err != nil {
		return nil, err
	}
	if _, err := database.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return nil, err
	}
	if err := migrate(database); err != nil {
		return nil, err
	}
	if err := ensureSchema(database); err != nil {
		return nil, err
	}
	if err := seed(database, cfg); err != nil {
		return nil, err
	}
	return database, nil
}

func migrate(database *sql.DB) error {
	sqlText := MigrationSQL
	if sqlText == "" {
		b, err := os.ReadFile("migrations/001_init.sql")
		if err != nil {
			return fmt.Errorf("read migration: %w", err)
		}
		sqlText = string(b)
	}
	_, err := database.Exec(sqlText)
	return err
}

func ensureSchema(database *sql.DB) error {
	if err := ensureTenantsTable(database); err != nil {
		return err
	}
	defaultTenantID, err := ensureDefaultTenant(database)
	if err != nil {
		return err
	}
	if err := migrateLegacyUsers(database, defaultTenantID); err != nil {
		return err
	}
	if err := addColumnIfMissing(database, "queues", "tenant_id", "TEXT"); err != nil {
		return err
	}
	if err := addColumnIfMissing(database, "tickets", "tenant_id", "TEXT"); err != nil {
		return err
	}
	if err := addColumnIfMissing(database, "notification_templates", "tenant_id", "TEXT"); err != nil {
		return err
	}
	_, _ = database.Exec(`UPDATE queues SET tenant_id = ? WHERE tenant_id IS NULL OR tenant_id = ''`, defaultTenantID)
	_, _ = database.Exec(`UPDATE tickets SET tenant_id = ? WHERE tenant_id IS NULL OR tenant_id = ''`, defaultTenantID)
	_, _ = database.Exec(`UPDATE notification_templates SET tenant_id = ? WHERE tenant_id IS NULL OR tenant_id = ''`, defaultTenantID)
	_, _ = database.Exec(`CREATE INDEX IF NOT EXISTS idx_tickets_tenant ON tickets(tenant_id)`)
	_, _ = database.Exec(`CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id)`)
	_, _ = database.Exec(`CREATE INDEX IF NOT EXISTS idx_users_manager ON users(manager_id)`)
	_, _ = database.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_telegram_id ON users(telegram_id) WHERE telegram_id IS NOT NULL`)
	_, _ = database.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_jabber_jid ON users(jabber_jid) WHERE jabber_jid IS NOT NULL AND jabber_jid != ''`)
	return nil
}

func ensureTenantsTable(database *sql.DB) error {
	_, err := database.Exec(`
CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL
)`)
	return err
}

func ensureDefaultTenant(database *sql.DB) (string, error) {
	var id string
	err := database.QueryRow(`SELECT id FROM tenants WHERE slug = 'default' LIMIT 1`).Scan(&id)
	if err == nil {
		return id, nil
	}
	id = uuid.NewString()
	_, err = database.Exec(
		`INSERT INTO tenants (id, name, slug, is_active, created_at) VALUES (?, ?, 'default', 1, ?)`,
		id, "Организация по умолчанию", time.Now().UTC().Format(time.RFC3339),
	)
	return id, err
}

func addColumnIfMissing(database *sql.DB, table, column, decl string) error {
	cols, err := tableColumns(database, table)
	if err != nil {
		return err
	}
	if cols[column] {
		return nil
	}
	_, err = database.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl))
	return err
}

func migrateLegacyUsers(database *sql.DB, defaultTenantID string) error {
	cols, err := tableColumns(database, "users")
	if err != nil {
		return err
	}
	needsRebuild := !cols["tenant_id"] || !cols["manager_id"] || !roleAllowsManager(database)
	if !needsRebuild {
		_, _ = database.Exec(`UPDATE users SET tenant_id = ? WHERE tenant_id IS NULL OR tenant_id = ''`, defaultTenantID)
		return nil
	}

	_, err = database.Exec(`
CREATE TABLE IF NOT EXISTS users_mt (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    full_name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('client', 'agent', 'manager', 'admin')),
    manager_id TEXT,
    is_active INTEGER NOT NULL DEFAULT 1,
    telegram_id INTEGER,
    jabber_jid TEXT,
    created_at TEXT NOT NULL
)`)
	if err != nil {
		return err
	}

	hasTenant := cols["tenant_id"]
	hasManager := cols["manager_id"]
	hasTelegram := cols["telegram_id"]
	hasJabber := cols["jabber_jid"]

	selectCols := "id, email, full_name, password_hash, role, is_active, created_at"
	if hasTelegram {
		selectCols = "id, email, full_name, password_hash, role, is_active, telegram_id, created_at"
	}
	if hasJabber {
		if hasTelegram {
			selectCols = "id, email, full_name, password_hash, role, is_active, telegram_id, COALESCE(jabber_jid,''), created_at"
		} else {
			selectCols = "id, email, full_name, password_hash, role, is_active, COALESCE(jabber_jid,''), created_at"
		}
	}

	rows, err := database.Query(`SELECT ` + selectCols + ` FROM users`)
	if err != nil {
		// empty/new DB path — users already from 001_init
		if strings.Contains(err.Error(), "no such table") {
			return nil
		}
		// If users already is users_mt shape from fresh migrate, skip
		if cols["tenant_id"] && cols["manager_id"] && roleAllowsManager(database) {
			return nil
		}
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, email, fullName, hash, role, created string
		var active int
		var telegram sql.NullInt64
		var jabber string
		var dest []any
		dest = []any{&id, &email, &fullName, &hash, &role, &active}
		if hasTelegram {
			dest = append(dest, &telegram)
		}
		if hasJabber {
			dest = append(dest, &jabber)
		}
		dest = append(dest, &created)
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		tenantID := defaultTenantID
		managerID := any(nil)
		if hasTenant {
			// will be overwritten below if we selected tenant — legacy path without tenant uses default
		}
		_ = hasManager
		var tg any
		if telegram.Valid {
			tg = telegram.Int64
		}
		_, err = database.Exec(
			`INSERT OR IGNORE INTO users_mt (id, tenant_id, email, full_name, password_hash, role, manager_id, is_active, telegram_id, jabber_jid, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, tenantID, email, fullName, hash, role, managerID, active, tg, jabber, created,
		)
		if err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// If users already has new schema (fresh install), users_mt may be empty duplicate — detect row counts
	var oldCount, newCount int
	_ = database.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&oldCount)
	_ = database.QueryRow(`SELECT COUNT(*) FROM users_mt`).Scan(&newCount)
	if oldCount > 0 && newCount == 0 {
		return fmt.Errorf("users migration copied 0 rows")
	}
	if newCount > 0 || !cols["tenant_id"] {
		tx, err := database.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE users`); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`ALTER TABLE users_mt RENAME TO users`); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	_, _ = database.Exec(`DROP TABLE IF EXISTS users_mt`)
	return nil
}

func roleAllowsManager(database *sql.DB) bool {
	// Probe by checking sqlite master SQL for users table.
	var sqlText string
	err := database.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='users'`).Scan(&sqlText)
	if err != nil {
		return false
	}
	return strings.Contains(sqlText, "'manager'")
}

func tableColumns(database *sql.DB, table string) (map[string]bool, error) {
	rows, err := database.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func seed(database *sql.DB, cfg config.Config) error {
	tenantID, err := ensureDefaultTenant(database)
	if err != nil {
		return err
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		_, err = database.Exec(
			`INSERT INTO users (id, tenant_id, email, full_name, password_hash, role, manager_id, is_active, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, NULL, 1, ?)`,
			uuid.NewString(), tenantID, strings.ToLower(cfg.AdminEmail), cfg.AdminFullName, string(hash), models.RoleAdmin, time.Now().UTC().Format(time.RFC3339),
		)
		if err != nil {
			return err
		}
	}

	queues := []struct{ name, desc string }{
		{"IT-поддержка", "Общие обращения в службу поддержки"},
		{"Сеть", "Сетевой доступ, VPN, firewall"},
		{"Рабочие места", "ПК, периферия, ПО"},
	}
	for _, q := range queues {
		var exists int
		_ = database.QueryRow(`SELECT COUNT(*) FROM queues WHERE tenant_id = ? AND name = ?`, tenantID, q.name).Scan(&exists)
		if exists == 0 {
			_, _ = database.Exec(
				`INSERT INTO queues (id, tenant_id, name, description, is_active) VALUES (?, ?, ?, ?, 1)`,
				uuid.NewString(), tenantID, q.name, q.desc,
			)
		}
	}

	templates := []struct{ event, subject, body string }{
		{"ticket_created", "Помогайка: заявка #{{ticket.number}} создана", "Заявка #{{ticket.number}} {{ticket.title}} создана.\n\nТекст обращения:\n{{ticket.description}}\n\nСтатус: {{ticket.status}}\nОчередь: {{ticket.queue}}"},
		{"status_changed", "Помогайка: статус заявки #{{ticket.number}} изменён", "Статус заявки #{{ticket.number}} изменён на {{ticket.status}}."},
		{"comment_public", "Помогайка: новый ответ по заявке #{{ticket.number}}", "По заявке #{{ticket.number}} добавлен ответ.\n\n{{comment.text}}"},
		{"assigned", "Помогайка: заявка #{{ticket.number}} назначена", "Заявка #{{ticket.number}} назначена на {{assignee.full_name}}."},
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, t := range templates {
		var exists int
		_ = database.QueryRow(`SELECT COUNT(*) FROM notification_templates WHERE tenant_id = ? AND event = ?`, tenantID, t.event).Scan(&exists)
		if exists == 0 {
			_, _ = database.Exec(
				`INSERT INTO notification_templates (id, tenant_id, event, subject, body, is_active, updated_at) VALUES (?, ?, ?, ?, ?, 1, ?)`,
				uuid.NewString(), tenantID, t.event, t.subject, t.body, now,
			)
		}
	}
	_, _ = database.Exec(`INSERT OR IGNORE INTO ticket_counters (name, value) VALUES (?, 0)`, "tickets:"+tenantID)
	return nil
}
