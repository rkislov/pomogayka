package db

import (
	"context"
	"database/sql"
)

type Conn struct {
	SQL     *sql.DB
	Dialect Dialect
}

func (c *Conn) Rebind(query string) string {
	return Rebind(c.Dialect, query)
}

func (c *Conn) Exec(query string, args ...any) (sql.Result, error) {
	return c.SQL.Exec(c.Rebind(query), args...)
}

func (c *Conn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.SQL.ExecContext(ctx, c.Rebind(query), args...)
}

func (c *Conn) Query(query string, args ...any) (*sql.Rows, error) {
	return c.SQL.Query(c.Rebind(query), args...)
}

func (c *Conn) QueryRow(query string, args ...any) *sql.Row {
	return c.SQL.QueryRow(c.Rebind(query), args...)
}

func (c *Conn) Begin() (*Tx, error) {
	tx, err := c.SQL.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{SQL: tx, Dialect: c.Dialect}, nil
}

func (c *Conn) Close() error {
	return c.SQL.Close()
}

func (c *Conn) InsertIgnore(query string, args ...any) (sql.Result, error) {
	return c.Exec(InsertIgnoreSQL(c.Dialect, query), args...)
}

type Tx struct {
	SQL     *sql.Tx
	Dialect Dialect
}

func (t *Tx) Rebind(query string) string {
	return Rebind(t.Dialect, query)
}

func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.SQL.Exec(t.Rebind(query), args...)
}

func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.SQL.QueryRow(t.Rebind(query), args...)
}

func (t *Tx) Commit() error   { return t.SQL.Commit() }
func (t *Tx) Rollback() error { return t.SQL.Rollback() }
