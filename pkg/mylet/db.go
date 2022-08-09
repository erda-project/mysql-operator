package mylet

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type Querier interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
}

func Open(dsn string, params ...string) (*sql.DB, error) {
	dsn += "?charset=utf8mb4&collation=utf8mb4_general_ci&multiStatements=true&maxAllowedPacket=134217728"
	for _, param := range params {
		dsn += "&" + param
	}
	return sql.Open("mysql", dsn)
}

const (
	Timeout1m = time.Minute
	Timeout5s = 5 * time.Second
)
