package cluster

import (
	"bytes"
	"context"
	stdlog "log"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestDatabaseGORMConfigRedactsParameters(t *testing.T) {
	config := databaseGORMConfig()
	filter, ok := config.Logger.(gorm.ParamsFilter)
	if !ok {
		t.Fatal("database GORM logger does not filter parameters")
	}
	query := "INSERT INTO plugin_store_auth_key (key) VALUES (?)"
	filteredQuery, params := filter.ParamsFilter(context.Background(), query, bytes.Repeat([]byte{'K'}, 32))
	if filteredQuery != query || len(params) != 0 {
		t.Fatal("database GORM logger retained SQL parameters")
	}
	if _, okInfo := config.Logger.LogMode(gormlogger.Info).(gorm.ParamsFilter); !okInfo {
		t.Fatal("database GORM logger lost parameter filtering after LogMode")
	}
}

func TestParameterizedGORMLoggerRedactsSensitiveTraceValues(t *testing.T) {
	var output bytes.Buffer
	inner := gormlogger.New(stdlog.New(&output, "", 0), gormlogger.Config{
		SlowThreshold:        -time.Nanosecond,
		LogLevel:             gormlogger.Warn,
		Colorful:             false,
		ParameterizedQueries: false,
	})
	redacted := newParameterizedGORMLogger(inner).LogMode(gormlogger.Warn)
	db, errOpen := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: redacted})
	if errOpen != nil {
		t.Fatalf("open test database: %v", errOpen)
	}
	sqlDB, errDB := db.DB()
	if errDB != nil {
		t.Fatalf("get sql database: %v", errDB)
	}
	t.Cleanup(func() {
		if errClose := sqlDB.Close(); errClose != nil {
			t.Errorf("close sql database: %v", errClose)
		}
	})
	output.Reset()

	printableKey := bytes.Repeat([]byte{'K'}, 32)
	printableCiphertext := bytes.Repeat([]byte{'C'}, 64)
	if errExec := db.Exec("SELECT ?", printableKey).Error; errExec != nil {
		t.Fatalf("execute slow query: %v", errExec)
	}
	if errExec := db.Exec("SELECT * FROM missing_plugin_store_auth WHERE encrypted_credentials = ?", printableCiphertext).Error; errExec == nil {
		t.Fatal("sensitive SQL error query unexpectedly succeeded")
	}

	logs := output.Bytes()
	if !bytes.Contains(logs, []byte("SLOW SQL")) {
		t.Fatal("slow SQL trace was not emitted")
	}
	if !bytes.Contains(logs, []byte("missing_plugin_store_auth")) {
		t.Fatal("SQL error trace was not emitted")
	}
	if !bytes.Contains(logs, []byte("SELECT ?")) || !bytes.Contains(logs, []byte("encrypted_credentials = ?")) {
		t.Fatal("SQL traces did not retain parameter placeholders")
	}
	if bytes.Contains(logs, printableKey) {
		t.Fatal("slow SQL trace leaked the printable encryption key")
	}
	if bytes.Contains(logs, printableCiphertext) {
		t.Fatal("SQL error trace leaked printable encrypted credentials")
	}
}
