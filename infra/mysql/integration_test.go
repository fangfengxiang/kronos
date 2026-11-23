//go:build mysql_integration

package mysql_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/mysql"
	mysqldriver "github.com/go-sql-driver/mysql"
)

func TestPoolMatchesNativeSQL(t *testing.T) {
	dsn := os.Getenv("KRONOS_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Fatal("set KRONOS_MYSQL_TEST_DSN for an isolated MySQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	pool, err := mysql.Open(ctx, mysql.Config{DSN: dsn, MaxOpenConnections: 3, MaxIdleConnections: 2, ConnectionMaxLifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if got := pool.Stats().MaxOpenConnections; got != 3 {
		t.Fatalf("max open connections = %d", got)
	}
	// The MySQL driver also registers its database/sql driver name for native clients.
	native, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("native sql.Open rejected the MySQL DSN")
	}
	defer native.Close()
	var kronosConnection, nativeConnection uint64
	if err := pool.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&kronosConnection); err != nil {
		t.Fatal(err)
	}
	if err := native.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&nativeConnection); err != nil {
		t.Fatal(err)
	}
	if kronosConnection == nativeConnection {
		t.Fatal("independent pools share a connection")
	}
	for _, candidate := range []struct {
		name string
		pool *sql.DB
	}{{"Kronos", pool}, {"native", native}} {
		t.Run(candidate.name, func(t *testing.T) {
			checkTransactions(t, ctx, candidate.pool)
			queryContext, cancelQuery := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancelQuery()
			var completed int
			err := candidate.pool.QueryRowContext(queryContext, "SELECT SLEEP(3)").Scan(&completed)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("canceled SQL query returned %v", err)
			}
			if err := candidate.pool.PingContext(ctx); err != nil {
				t.Fatalf("pool did not recover after cancellation: %v", err)
			}
		})
	}
}

func TestOpenReturnsConnectionErrorBeforePublishingPool(t *testing.T) {
	configuredDSN := os.Getenv("KRONOS_MYSQL_TEST_DSN")
	if configuredDSN == "" {
		t.Fatal("set KRONOS_MYSQL_TEST_DSN for an isolated MySQL database")
	}
	driverConfig, err := mysqldriver.ParseDSN(configuredDSN)
	if err != nil {
		t.Fatal("MySQL test DSN is invalid")
	}
	driverConfig.Passwd = "kronos-invalid-test-password"
	dsn := driverConfig.FormatDSN()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	native, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("native sql.Open did not accept the test DSN")
	}
	defer native.Close()
	var denied *mysqldriver.MySQLError
	if err := native.PingContext(ctx); !errors.As(err, &denied) || denied.Number != 1045 {
		t.Fatalf("native PingContext did not report denied access: %v", err)
	}
	pool, err := mysql.Open(ctx, mysql.Config{DSN: dsn})
	if pool != nil {
		pool.Close()
		t.Fatal("Open returned a pool with invalid credentials")
	}
	if err != mysql.ErrUnavailable {
		t.Fatalf("Open with invalid credentials returned %v", err)
	}
}

func checkTransactions(t *testing.T, ctx context.Context, pool *sql.DB) {
	t.Helper()
	connection, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, `CREATE TEMPORARY TABLE mysql_pool_checks (id INT PRIMARY KEY, prefix VARCHAR(32)) DEFAULT CHARSET utf8mb4`); err != nil {
		t.Fatal(err)
	}
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "INSERT INTO mysql_pool_checks (id, prefix) VALUES (?, ?)", 1, "你好"); err != nil {
		t.Fatal(err)
	}
	var prefix string
	if err := transaction.QueryRowContext(ctx, "SELECT prefix FROM mysql_pool_checks WHERE id = ?", 1).Scan(&prefix); err != nil || prefix != "你好" {
		t.Fatalf("transaction read = %q, %v", prefix, err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := connection.QueryRowContext(ctx, "SELECT prefix FROM mysql_pool_checks WHERE id = ?", 1).Scan(&prefix); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("read after rollback = %v", err)
	}
	if _, err := connection.ExecContext(ctx, "INSERT INTO mysql_pool_checks (id, prefix) VALUES (?, ?)", 1, "你好"); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.ExecContext(ctx, "INSERT INTO mysql_pool_checks (id, prefix) VALUES (?, ?)", 1, "duplicate"); err == nil || !mysql.IsDuplicateKey(fmt.Errorf("insert test row: %w", err)) {
		t.Fatalf("duplicate insert returned %v", err)
	}
}

func ExampleOpen() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pool, err := mysql.Open(ctx, mysql.Config{
		DSN: os.Getenv("KRONOS_MYSQL_TEST_DSN"), MaxOpenConnections: 4, MaxIdleConnections: 2,
	})
	if err != nil {
		panic(err)
	}
	defer pool.Close()
	var value int
	if err := pool.QueryRowContext(ctx, "SELECT ?", 1).Scan(&value); err != nil {
		panic(err)
	}
	fmt.Println(value)
	// Output: 1
}
