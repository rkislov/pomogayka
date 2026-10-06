package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"github.com/rkislov/pomogayka/internal/config"
	"github.com/rkislov/pomogayka/internal/models"
)

// MigrationSQL is optional override; otherwise Open loads dialect-specific file.
var MigrationSQL string

func Open(cfg config.Config) (*Conn, error) {
	dialect, err := DetectDialect(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	dsn := cfg.DatabaseURL
	driver := "sqlite"
	switch dialect {
	case DialectSQLite:
		if strings.HasPrefix(dsn, "file:") {
			path := strings.TrimPrefix(strings.Split(dsn, "?")[0], "file:")
			if dir := filepath.Dir(path); dir != "." && dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return nil, err
				}
			}
		}
		driver = "sqlite"
	case DialectPostgres:
		driver = "pgx"
	}

	raw, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	if dialect == DialectSQLite {
		raw.SetMaxOpenConns(1)
	} else {
		raw.SetMaxOpenConns(10)
		raw.SetMaxIdleConns(5)
	}
	raw.SetConnMaxLifetime(time.Hour)
	if err := raw.Ping(); err != nil {
		_ = raw.Close()
		return nil, err
	}

	conn := &Conn{SQL: raw, Dialect: dialect}
	if dialect == DialectSQLite {
		if _, err := conn.Exec(`PRAGMA foreign_keys = ON`); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	if err := applyMigrations(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := ensureSchema(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := seed(conn, cfg); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func applyMigrations(conn *Conn) error {
	sqlText := MigrationSQL
	if sqlText == "" {
		path := "migrations/sqlite/001_init.sql"
		if conn.Dialect == DialectPostgres {
			path = "migrations/postgres/001_init.sql"
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", path, err)
		}
		sqlText = string(b)
	}
	_, err := conn.SQL.Exec(sqlText) // no placeholders
	return err
}

func ensureSchema(conn *Conn) error {
	if conn.Dialect == DialectPostgres {
		return ensurePostgresSchema(conn)
	}
	return ensureSQLiteSchema(conn)
}

func ensurePostgresSchema(conn *Conn) error {
	_, err := ensureDefaultTenant(conn)
	return err
}

func ensureSQLiteSchema(conn *Conn) error {
	if err := ensureTenantsTable(conn); err != nil {
		return err
	}
	defaultTenantID, err := ensureDefaultTenant(conn)
	if err != nil {
		return err
	}
	if err := migrateLegacyUsers(conn, defaultTenantID); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "queues", "tenant_id", "TEXT"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "tickets", "tenant_id", "TEXT"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "notification_templates", "tenant_id", "TEXT"); err != nil {
		return err
	}
	_, _ = conn.Exec(`UPDATE queues SET tenant_id = ? WHERE tenant_id IS NULL OR tenant_id = ''`, defaultTenantID)
	_, _ = conn.Exec(`UPDATE tickets SET tenant_id = ? WHERE tenant_id IS NULL OR tenant_id = ''`, defaultTenantID)
	_, _ = conn.Exec(`UPDATE notification_templates SET tenant_id = ? WHERE tenant_id IS NULL OR tenant_id = ''`, defaultTenantID)
	_, _ = conn.Exec(`CREATE INDEX IF NOT EXISTS idx_tickets_tenant ON tickets(tenant_id)`)
	_, _ = conn.Exec(`CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id)`)
	_, _ = conn.Exec(`CREATE INDEX IF NOT EXISTS idx_users_manager ON users(manager_id)`)
	_, _ = conn.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_telegram_id ON users(telegram_id) WHERE telegram_id IS NOT NULL`)
	_, _ = conn.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_jabber_jid ON users(jabber_jid) WHERE jabber_jid IS NOT NULL AND jabber_jid != ''`)
	return nil
}

func ensureTenantsTable(conn *Conn) error {
	_, err := conn.Exec(`
CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL
)`)
	return err
}

func ensureDefaultTenant(conn *Conn) (string, error) {
	var id string
	err := conn.QueryRow(`SELECT id FROM tenants WHERE slug = 'default' LIMIT 1`).Scan(&id)
	if err == nil {
		return id, nil
	}
	id = uuid.NewString()
	_, err = conn.Exec(
		`INSERT INTO tenants (id, name, slug, is_active, created_at) VALUES (?, ?, 'default', 1, ?)`,
		id, "Организация по умолчанию", time.Now().UTC().Format(time.RFC3339),
	)
	return id, err
}

func addColumnIfMissing(conn *Conn, table, column, decl string) error {
	cols, err := tableColumns(conn, table)
	if err != nil {
		return err
	}
	if cols[column] {
		return nil
	}
	_, err = conn.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl))
	return err
}

func migrateLegacyUsers(conn *Conn, defaultTenantID string) error {
	cols, err := tableColumns(conn, "users")
	if err != nil {
		return err
	}
	needsRebuild := !cols["tenant_id"] || !cols["manager_id"] || !roleAllowsManager(conn)
	if !needsRebuild {
		_, _ = conn.Exec(`UPDATE users SET tenant_id = ? WHERE tenant_id IS NULL OR tenant_id = ''`, defaultTenantID)
		return nil
	}

	_, err = conn.Exec(`
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

	rows, err := conn.Query(`SELECT ` + selectCols + ` FROM users`)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil
		}
		if cols["tenant_id"] && cols["manager_id"] && roleAllowsManager(conn) {
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
		dest := []any{&id, &email, &fullName, &hash, &role, &active}
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
		var tg any
		if telegram.Valid {
			tg = telegram.Int64
		}
		_, err = conn.InsertIgnore(
			`INSERT INTO users_mt (id, tenant_id, email, full_name, password_hash, role, manager_id, is_active, telegram_id, jabber_jid, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?)`,
			id, defaultTenantID, email, fullName, hash, role, active, tg, jabber, created,
		)
		if err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	var oldCount, newCount int
	_ = conn.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&oldCount)
	_ = conn.QueryRow(`SELECT COUNT(*) FROM users_mt`).Scan(&newCount)
	if oldCount > 0 && newCount == 0 {
		return fmt.Errorf("users migration copied 0 rows")
	}
	if newCount > 0 || !cols["tenant_id"] {
		tx, err := conn.Begin()
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
	_, _ = conn.Exec(`DROP TABLE IF EXISTS users_mt`)
	return nil
}

func roleAllowsManager(conn *Conn) bool {
	var sqlText string
	err := conn.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='users'`).Scan(&sqlText)
	if err != nil {
		return false
	}
	return strings.Contains(sqlText, "'manager'")
}

func tableColumns(conn *Conn, table string) (map[string]bool, error) {
	rows, err := conn.Query(`PRAGMA table_info(` + table + `)`)
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

func seed(conn *Conn, cfg config.Config) error {
	tenantID, err := ensureDefaultTenant(conn)
	if err != nil {
		return err
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		_, err = conn.Exec(
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
		_ = conn.QueryRow(`SELECT COUNT(*) FROM queues WHERE tenant_id = ? AND name = ?`, tenantID, q.name).Scan(&exists)
		if exists == 0 {
			_, _ = conn.Exec(
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
		_ = conn.QueryRow(`SELECT COUNT(*) FROM notification_templates WHERE tenant_id = ? AND event = ?`, tenantID, t.event).Scan(&exists)
		if exists == 0 {
			_, _ = conn.Exec(
				`INSERT INTO notification_templates (id, tenant_id, event, subject, body, is_active, updated_at) VALUES (?, ?, ?, ?, ?, 1, ?)`,
				uuid.NewString(), tenantID, t.event, t.subject, t.body, now,
			)
		}
	}
	_, _ = conn.InsertIgnore(`INSERT INTO ticket_counters (name, value) VALUES (?, 0)`, "tickets:"+tenantID)
	return nil
}
