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
	if err := migrate(database); err != nil {
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

func seed(database *sql.DB, cfg config.Config) error {
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
			`INSERT INTO users (id, email, full_name, password_hash, role, is_active, created_at) VALUES (?, ?, ?, ?, ?, 1, ?)`,
			uuid.NewString(), strings.ToLower(cfg.AdminEmail), cfg.AdminFullName, string(hash), models.RoleAdmin, time.Now().UTC().Format(time.RFC3339),
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
		_ = database.QueryRow(`SELECT COUNT(*) FROM queues WHERE name = ?`, q.name).Scan(&exists)
		if exists == 0 {
			_, _ = database.Exec(
				`INSERT INTO queues (id, name, description, is_active) VALUES (?, ?, ?, 1)`,
				uuid.NewString(), q.name, q.desc,
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
		_ = database.QueryRow(`SELECT COUNT(*) FROM notification_templates WHERE event = ?`, t.event).Scan(&exists)
		if exists == 0 {
			_, _ = database.Exec(
				`INSERT INTO notification_templates (id, event, subject, body, is_active, updated_at) VALUES (?, ?, ?, ?, 1, ?)`,
				uuid.NewString(), t.event, t.subject, t.body, now,
			)
		}
	}
	return nil
}
