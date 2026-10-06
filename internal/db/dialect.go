package db

import (
	"fmt"
	"strings"
)

type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

func DetectDialect(databaseURL string) (Dialect, error) {
	u := strings.TrimSpace(strings.ToLower(databaseURL))
	switch {
	case u == "":
		return "", fmt.Errorf("DATABASE_URL is empty")
	case strings.HasPrefix(u, "postgres://"), strings.HasPrefix(u, "postgresql://"):
		return DialectPostgres, nil
	case strings.HasPrefix(u, "file:"), strings.HasPrefix(u, "sqlite:"), strings.Contains(u, ".db"):
		return DialectSQLite, nil
	default:
		return "", fmt.Errorf("unsupported DATABASE_URL dialect: %s", databaseURL)
	}
}

func Rebind(dialect Dialect, query string) string {
	if dialect != DialectPostgres {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(fmt.Sprintf("%d", n))
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

func InsertIgnoreSQL(dialect Dialect, insertSQL string) string {
	// Expects: INSERT INTO ... VALUES (...)
	sql := strings.TrimSpace(insertSQL)
	if dialect == DialectPostgres {
		if strings.HasPrefix(strings.ToUpper(sql), "INSERT OR IGNORE ") {
			sql = "INSERT " + strings.TrimSpace(sql[len("INSERT OR IGNORE "):])
		}
		if !strings.Contains(strings.ToUpper(sql), "ON CONFLICT") {
			sql += " ON CONFLICT DO NOTHING"
		}
		return sql
	}
	if strings.HasPrefix(strings.ToUpper(sql), "INSERT INTO ") {
		return "INSERT OR IGNORE " + sql[len("INSERT "):]
	}
	return sql
}
