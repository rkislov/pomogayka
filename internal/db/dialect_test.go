package db

import "testing"

func TestRebindPostgres(t *testing.T) {
	got := Rebind(DialectPostgres, `SELECT a FROM t WHERE x = ? AND y = ?`)
	want := `SELECT a FROM t WHERE x = $1 AND y = $2`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestInsertIgnoreSQL(t *testing.T) {
	q := `INSERT INTO ticket_counters (name, value) VALUES (?, 0)`
	pg := InsertIgnoreSQL(DialectPostgres, q)
	if pg != q+` ON CONFLICT DO NOTHING` {
		t.Fatalf("postgres: %q", pg)
	}
	sq := InsertIgnoreSQL(DialectSQLite, q)
	if sq != `INSERT OR IGNORE INTO ticket_counters (name, value) VALUES (?, 0)` {
		t.Fatalf("sqlite: %q", sq)
	}
}

func TestDetectDialect(t *testing.T) {
	d, err := DetectDialect("postgres://u:p@localhost:5432/pomogayka?sslmode=disable")
	if err != nil || d != DialectPostgres {
		t.Fatalf("postgres detect: %v %v", d, err)
	}
	d, err = DetectDialect("file:data/pomogayka.db?_pragma=foreign_keys(ON)")
	if err != nil || d != DialectSQLite {
		t.Fatalf("sqlite detect: %v %v", d, err)
	}
}
