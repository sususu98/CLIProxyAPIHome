package cluster

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/router-for-me/CLIProxyAPIHome/internal/node"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestOpenSQLite_AutoMigrateAndConfigRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, errOpenSQLite := OpenSQLite(ctx, filepath.Join(t.TempDir(), "home.db"))
	if errOpenSQLite != nil {
		t.Fatalf("OpenSQLite failed: %v", errOpenSQLite)
	}
	sqlDB, errDB := db.DB()
	if errDB != nil {
		t.Fatalf("get sql db: %v", errDB)
	}
	defer func() {
		if errClose := sqlDB.Close(); errClose != nil {
			t.Errorf("close sql db: %v", errClose)
		}
	}()

	if errMigrate := AutoMigrate(db); errMigrate != nil {
		t.Fatalf("AutoMigrate failed: %v", errMigrate)
	}

	repo := NewRepository(db)
	if errUpsertConfigValue := repo.UpsertConfigValue(ctx, "debug", true); errUpsertConfigValue != nil {
		t.Fatalf("UpsertConfigValue failed: %v", errUpsertConfigValue)
	}
	snapshot, errLoadConfigSnapshot := repo.LoadConfigSnapshot(ctx)
	if errLoadConfigSnapshot != nil {
		t.Fatalf("LoadConfigSnapshot failed: %v", errLoadConfigSnapshot)
	}

	var debug bool
	if errUnmarshal := json.Unmarshal(snapshot["debug"], &debug); errUnmarshal != nil {
		t.Fatalf("unmarshal debug: %v", errUnmarshal)
	}
	if !debug {
		t.Fatalf("debug = false, want true")
	}
}

func TestCPANodeSnapshotRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, errOpenSQLite := OpenSQLite(ctx, filepath.Join(t.TempDir(), "home.db"))
	if errOpenSQLite != nil {
		t.Fatalf("OpenSQLite failed: %v", errOpenSQLite)
	}
	sqlDB, errDB := db.DB()
	if errDB != nil {
		t.Fatalf("get sql db: %v", errDB)
	}
	defer func() {
		if errClose := sqlDB.Close(); errClose != nil {
			t.Errorf("close sql db: %v", errClose)
		}
	}()
	if errMigrate := AutoMigrate(db); errMigrate != nil {
		t.Fatalf("AutoMigrate failed: %v", errMigrate)
	}

	repo := NewRepository(db)
	connectedAt := time.Now().UTC().Add(-time.Minute)
	seenAt := time.Now().UTC()
	if errSnapshot := repo.ReplaceCPANodeSnapshot(ctx, "home-a", 8327, []node.Node{
		{NodeID: "node-1", IP: "10.0.0.5", ClientCount: 1, Connected: connectedAt},
	}, seenAt); errSnapshot != nil {
		t.Fatalf("ReplaceCPANodeSnapshot(home-a) failed: %v", errSnapshot)
	}
	if errSnapshot := repo.ReplaceCPANodeSnapshot(ctx, "home-b", 8327, []node.Node{
		{NodeID: "node-2", IP: "10.0.0.6", ClientCount: 2, Connected: connectedAt},
	}, seenAt); errSnapshot != nil {
		t.Fatalf("ReplaceCPANodeSnapshot(home-b) failed: %v", errSnapshot)
	}

	records, errList := repo.ListLiveCPANodes(ctx, seenAt.Add(-time.Second))
	if errList != nil {
		t.Fatalf("ListLiveCPANodes failed: %v", errList)
	}
	if len(records) != 2 {
		t.Fatalf("cpa records = %d, want 2", len(records))
	}
	if records[0].HomeIP != "home-a" || records[0].NodeID != "node-1" || records[0].ClientCount != 1 || records[0].ConnectedAt.IsZero() || records[0].LastSeenAt.IsZero() {
		t.Fatalf("first cpa record = %+v, want home-a/node-1 snapshot", records[0])
	}

	if errSnapshot := repo.ReplaceCPANodeSnapshot(ctx, "home-a", 8327, nil, seenAt.Add(time.Second)); errSnapshot != nil {
		t.Fatalf("ReplaceCPANodeSnapshot(home-a empty) failed: %v", errSnapshot)
	}
	records, errList = repo.ListLiveCPANodes(ctx, seenAt.Add(-time.Second))
	if errList != nil {
		t.Fatalf("ListLiveCPANodes after replace failed: %v", errList)
	}
	if len(records) != 1 || records[0].NodeID != "node-2" {
		t.Fatalf("cpa records after replace = %+v, want only node-2", records)
	}
}

func TestOpenSQLite_ConfiguresLocalConcurrency(t *testing.T) {
	t.Parallel()

	db, errOpenSQLite := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "home.db"))
	if errOpenSQLite != nil {
		t.Fatalf("OpenSQLite failed: %v", errOpenSQLite)
	}
	sqlDB, errDB := db.DB()
	if errDB != nil {
		t.Fatalf("get sql db: %v", errDB)
	}
	defer func() {
		if errClose := sqlDB.Close(); errClose != nil {
			t.Errorf("close sql db: %v", errClose)
		}
	}()

	if got := sqlDB.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}

	var journalMode string
	if errRaw := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; errRaw != nil {
		t.Fatalf("read journal_mode: %v", errRaw)
	}
	if got := strings.ToLower(strings.TrimSpace(journalMode)); got != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	var busyTimeout int
	if errRaw := db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error; errRaw != nil {
		t.Fatalf("read busy_timeout: %v", errRaw)
	}
	if busyTimeout < 5000 {
		t.Fatalf("busy_timeout = %d, want at least 5000", busyTimeout)
	}
}

func TestReplaceCPANodeSnapshotConcurrentSameHome(t *testing.T) {
	db, errOpenSQLite := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "home.db"))
	if errOpenSQLite != nil {
		t.Fatalf("OpenSQLite failed: %v", errOpenSQLite)
	}
	sqlDB, errDB := db.DB()
	if errDB != nil {
		t.Fatalf("get sql db: %v", errDB)
	}
	defer func() {
		if errClose := sqlDB.Close(); errClose != nil {
			t.Errorf("close sql db: %v", errClose)
		}
	}()
	if errMigrate := AutoMigrate(db); errMigrate != nil {
		t.Fatalf("AutoMigrate failed: %v", errMigrate)
	}

	repo := NewRepository(db)
	start := make(chan struct{})
	errCh := make(chan error, 16)
	now := time.Now().UTC()
	var wg sync.WaitGroup
	for i := 0; i < cap(errCh); i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errCh <- repo.ReplaceCPANodeSnapshot(context.Background(), "home-a", 8327, []node.Node{
				{NodeID: "cpa-a", IP: "10.0.0.1", Connected: now, ClientCount: idx + 1},
			}, now.Add(time.Duration(idx)*time.Millisecond))
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for errSnapshot := range errCh {
		if errSnapshot != nil {
			t.Fatalf("ReplaceCPANodeSnapshot() concurrent error = %v", errSnapshot)
		}
	}

	records, errRecords := repo.ListLiveCPANodes(context.Background(), now.Add(-time.Minute))
	if errRecords != nil {
		t.Fatalf("ListLiveCPANodes() error = %v", errRecords)
	}
	if len(records) != 1 || records[0].NodeID != "cpa-a" || records[0].HomeIP != "home-a" || records[0].HomePort != 8327 {
		t.Fatalf("records = %+v, want one final CPA snapshot", records)
	}
}

func TestJSONBGormDBDataTypeForMigrators(t *testing.T) {
	t.Parallel()

	pgDB, errOpenPostgres := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=127.0.0.1 user=cliproxy dbname=cliproxy_home sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{DisableAutomaticPing: true})
	if errOpenPostgres != nil {
		t.Fatalf("open postgres dry-run db: %v", errOpenPostgres)
	}
	sqliteDB, errOpenSQLite := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if errOpenSQLite != nil {
		t.Fatalf("open sqlite db: %v", errOpenSQLite)
	}
	sqlDB, errDB := sqliteDB.DB()
	if errDB != nil {
		t.Fatalf("get sqlite sql db: %v", errDB)
	}
	defer func() {
		if errClose := sqlDB.Close(); errClose != nil {
			t.Errorf("close sqlite sql db: %v", errClose)
		}
	}()

	jsonField := configRecordJSONBField(t)
	assertJSONBFullDataType(t, pgDB, jsonField, "jsonb")
	assertJSONBFullDataType(t, sqliteDB, jsonField, "text")
}

func configRecordJSONBField(t *testing.T) *schema.Field {
	t.Helper()

	parsedSchema, errParse := schema.Parse(&ConfigRecord{}, &sync.Map{}, schema.NamingStrategy{})
	if errParse != nil {
		t.Fatalf("parse ConfigRecord schema: %v", errParse)
	}
	jsonField := parsedSchema.LookUpField("Value")
	if jsonField == nil {
		t.Fatalf("ConfigRecord.Value field not found")
	}
	return jsonField
}

func assertJSONBFullDataType(t *testing.T, db *gorm.DB, field *schema.Field, want string) {
	t.Helper()

	expr := db.Migrator().FullDataTypeOf(field)
	got := strings.ToLower(strings.TrimSpace(expr.SQL))
	if !strings.Contains(got, want) {
		t.Fatalf("%s JSONB data type = %q, want %q", db.Dialector.Name(), got, want)
	}
}
