package cluster

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestHydrateAuthListRuntimesInheritsGeminiVirtualProxy(t *testing.T) {
	t.Parallel()

	parent := &coreauth.Auth{
		ID:       "parent-auth",
		Provider: "gemini-cli",
		Prefix:   "team-a",
		ProxyURL: "http://parent-proxy.example:8080",
		Attributes: map[string]string{
			"gemini_virtual_primary": "true",
			"virtual_children":       "project-a,project-b",
		},
		Metadata: map[string]any{
			"type":       "gemini",
			"email":      "user@example.com",
			"project_id": "project-a,project-b",
		},
	}
	child := &coreauth.Auth{
		ID:       "child-auth",
		Provider: "gemini-cli",
		ProxyURL: "http://stale-proxy.example:8080",
		Attributes: map[string]string{
			"gemini_virtual_parent":  parent.ID,
			"gemini_virtual_project": "project-a",
			"runtime_only":           "true",
		},
		Metadata: map[string]any{
			"type":       "gemini",
			"virtual":    true,
			"project_id": "project-a",
		},
	}

	hydrateAuthListRuntimes([]*coreauth.Auth{child, parent})

	if child.ProxyURL != parent.ProxyURL {
		t.Fatalf("child proxy URL = %q, want %q", child.ProxyURL, parent.ProxyURL)
	}
	if child.Prefix != parent.Prefix {
		t.Fatalf("child prefix = %q, want %q", child.Prefix, parent.Prefix)
	}
}

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
