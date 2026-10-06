package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	pomogayka "github.com/rkislov/pomogayka"
	"github.com/rkislov/pomogayka/internal/db"
)

func main() {
	_ = godotenv.Load()

	sqliteURL := flag.String("sqlite", env("SQLITE_DATABASE_URL", "file:data/pomogayka.db?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"), "source SQLite DATABASE_URL")
	postgresURL := flag.String("postgres", env("POSTGRES_DATABASE_URL", env("DATABASE_URL", "")), "target PostgreSQL DATABASE_URL")
	dryRun := flag.Bool("dry-run", false, "only validate connections and counts")
	flag.Parse()

	if *postgresURL == "" || !strings.HasPrefix(strings.ToLower(*postgresURL), "postgres") {
		log.Fatal("provide PostgreSQL URL via -postgres or POSTGRES_DATABASE_URL / DATABASE_URL")
	}
	srcDialect, err := db.DetectDialect(*sqliteURL)
	if err != nil || srcDialect != db.DialectSQLite {
		log.Fatalf("source must be SQLite URL, got %q", *sqliteURL)
	}
	dstDialect, err := db.DetectDialect(*postgresURL)
	if err != nil || dstDialect != db.DialectPostgres {
		log.Fatalf("target must be PostgreSQL URL, got %q", *postgresURL)
	}

	src, err := sql.Open("sqlite", *sqliteURL)
	if err != nil {
		log.Fatal(err)
	}
	defer src.Close()
	if _, err := src.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		log.Fatal(err)
	}
	if err := src.Ping(); err != nil {
		log.Fatal("sqlite ping: ", err)
	}

	dstSQL, err := sql.Open("pgx", *postgresURL)
	if err != nil {
		log.Fatal(err)
	}
	defer dstSQL.Close()
	if err := dstSQL.Ping(); err != nil {
		log.Fatal("postgres ping: ", err)
	}
	dst := &db.Conn{SQL: dstSQL, Dialect: db.DialectPostgres}

	schema, err := pomogayka.Content.ReadFile("migrations/postgres/001_init.sql")
	if err != nil {
		// fallback to filesystem
		schema, err = os.ReadFile("migrations/postgres/001_init.sql")
		if err != nil {
			log.Fatal(err)
		}
	}
	if _, err := dst.SQL.Exec(string(schema)); err != nil {
		log.Fatal("apply postgres schema: ", err)
	}

	tables := []string{"tenants", "users", "queues", "tickets", "comments", "notification_templates", "ticket_counters"}
	log.Println("source counts:")
	for _, table := range tables {
		n, err := count(src, table)
		if err != nil {
			log.Fatalf("count %s: %v", table, err)
		}
		log.Printf("  %-24s %d", table, n)
	}

	if *dryRun {
		log.Println("dry-run complete, no data copied")
		return
	}

	// Clear destination tables for idempotent reload (optional safety).
	if err := truncatePostgres(dst); err != nil {
		log.Fatal("truncate postgres: ", err)
	}

	if err := copyTable(src, dst, "tenants",
		[]string{"id", "name", "slug", "is_active", "created_at"},
		`SELECT id, name, slug, is_active, created_at FROM tenants`); err != nil {
		log.Fatal(err)
	}

	// users: first without manager_id, then update manager links
	if err := copyUsers(src, dst); err != nil {
		log.Fatal(err)
	}

	if err := copyTable(src, dst, "queues",
		[]string{"id", "tenant_id", "name", "description", "is_active"},
		`SELECT id, tenant_id, name, description, is_active FROM queues`); err != nil {
		log.Fatal(err)
	}
	if err := copyTable(src, dst, "tickets",
		[]string{"id", "tenant_id", "number", "title", "description", "status", "priority", "author_id", "assignee_id", "queue_id", "created_at", "updated_at", "closed_at"},
		`SELECT id, tenant_id, number, title, description, status, priority, author_id, assignee_id, queue_id, created_at, updated_at, closed_at FROM tickets`); err != nil {
		log.Fatal(err)
	}
	if err := copyTable(src, dst, "comments",
		[]string{"id", "ticket_id", "author_id", "body", "is_internal", "created_at"},
		`SELECT id, ticket_id, author_id, body, is_internal, created_at FROM comments`); err != nil {
		log.Fatal(err)
	}
	if err := copyTable(src, dst, "notification_templates",
		[]string{"id", "tenant_id", "event", "subject", "body", "is_active", "updated_at"},
		`SELECT id, tenant_id, event, subject, body, is_active, updated_at FROM notification_templates`); err != nil {
		log.Fatal(err)
	}
	if err := copyTable(src, dst, "ticket_counters",
		[]string{"name", "value"},
		`SELECT name, value FROM ticket_counters`); err != nil {
		log.Fatal(err)
	}

	log.Println("migration completed successfully")
	log.Println("target counts:")
	for _, table := range tables {
		n, err := count(dst.SQL, table)
		if err != nil {
			log.Fatalf("count %s: %v", table, err)
		}
		log.Printf("  %-24s %d", table, n)
	}
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func count(dbase interface {
	QueryRow(query string, args ...any) *sql.Row
}, table string) (int, error) {
	var n int
	err := dbase.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n)
	return n, err
}

func truncatePostgres(dst *db.Conn) error {
	_, err := dst.SQL.Exec(`
TRUNCATE TABLE
  comments,
  tickets,
  notification_templates,
  queues,
  users,
  tenants,
  ticket_counters
RESTART IDENTITY CASCADE`)
	return err
}

func copyTable(src *sql.DB, dst *db.Conn, table string, columns []string, selectSQL string) error {
	rows, err := src.Query(selectSQL)
	if err != nil {
		return fmt.Errorf("select %s: %w", table, err)
	}
	defer rows.Close()

	placeholders := make([]string, len(columns))
	for i := range columns {
		placeholders[i] = "?"
	}
	insertSQL := fmt.Sprintf(
		`INSERT INTO %s (%s) VALUES (%s)`,
		table,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	tx, err := dst.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	n := 0
	cols := make([]any, len(columns))
	ptrs := make([]any, len(columns))
	for i := range cols {
		ptrs[i] = &cols[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("scan %s: %w", table, err)
		}
		args := make([]any, len(cols))
		copy(args, cols)
		if _, err := tx.Exec(insertSQL, args...); err != nil {
			return fmt.Errorf("insert %s: %w", table, err)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("copied %s: %d rows", table, n)
	return nil
}

func copyUsers(src *sql.DB, dst *db.Conn) error {
	// Detect optional columns for older SQLite dumps.
	cols, err := sqliteColumns(src, "users")
	if err != nil {
		return err
	}
	selectSQL := `SELECT id, tenant_id, email, full_name, password_hash, role, manager_id, is_active, telegram_id, jabber_jid, created_at FROM users`
	if !cols["tenant_id"] {
		return fmt.Errorf("sqlite users table has no tenant_id; upgrade/run app on SQLite once before migrating")
	}
	_ = selectSQL

	rows, err := src.Query(`SELECT id, tenant_id, email, full_name, password_hash, role, COALESCE(manager_id,''), is_active, telegram_id, COALESCE(jabber_jid,''), created_at FROM users`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type userRow struct {
		id, tenantID, email, fullName, hash, role, managerID, jabber, created string
		active                                                                int
		telegram                                                              sql.NullInt64
	}
	var all []userRow
	for rows.Next() {
		var u userRow
		if err := rows.Scan(&u.id, &u.tenantID, &u.email, &u.fullName, &u.hash, &u.role, &u.managerID, &u.active, &u.telegram, &u.jabber, &u.created); err != nil {
			return err
		}
		all = append(all, u)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	tx, err := dst.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, u := range all {
		var tg any
		if u.telegram.Valid {
			tg = u.telegram.Int64
		}
		_, err := tx.Exec(
			`INSERT INTO users (id, tenant_id, email, full_name, password_hash, role, manager_id, is_active, telegram_id, jabber_jid, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?)`,
			u.id, u.tenantID, u.email, u.fullName, u.hash, u.role, u.active, tg, nullIfEmpty(u.jabber), u.created,
		)
		if err != nil {
			return fmt.Errorf("insert user %s: %w", u.email, err)
		}
	}
	for _, u := range all {
		if u.managerID == "" {
			continue
		}
		if _, err := tx.Exec(`UPDATE users SET manager_id = ? WHERE id = ?`, u.managerID, u.id); err != nil {
			return fmt.Errorf("update manager for %s: %w", u.email, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("copied users: %d rows", len(all))
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func sqliteColumns(src *sql.DB, table string) (map[string]bool, error) {
	rows, err := src.Query(`PRAGMA table_info(` + table + `)`)
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

