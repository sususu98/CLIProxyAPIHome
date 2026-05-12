package cluster

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Open opens the resource.
func Open(ctx context.Context, cfg PGSQLConfig) (*gorm.DB, error) {
	// Keep validation before state changes so failures leave existing data intact.
	if ctx == nil {
		ctx = context.Background()
	}

	dsn, errDSN := cfg.DSN()
	if errDSN != nil {
		return nil, errDSN
	}

	db, errOpen := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if errOpen != nil {
		return nil, errOpen
	}

	sqlDB, errDB := db.DB()
	if errDB != nil {
		return nil, errDB
	}
	if errPing := sqlDB.PingContext(ctx); errPing != nil {
		if errClose := sqlDB.Close(); errClose != nil {
			return nil, fmt.Errorf("ping postgres: %w; close sql db: %v", errPing, errClose)
		}
		return nil, errPing
	}

	return db, nil
}

// ClientAddr handles a client addr.
func ClientAddr(ctx context.Context, db *gorm.DB) (string, error) {
	return clientAddr(ctx, db)
}

// AutoMigrate handles an auto migrate.
func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}
	return db.AutoMigrate(&AuthRecord{}, &ConfigRecord{}, &ClusterNodeRecord{}, &ClusterEventRecord{}, &UsageRecord{}, &OAuthSessionRecord{})
}

// DSN returns the PostgreSQL connection string.
func (c PGSQLConfig) DSN() (string, error) {
	if c.Password == "" && c.Passowrd != "" {
		c.Password = c.Passowrd
	}
	if c.SSLMode == "" {
		c.SSLMode = "disable"
	}
	if errValidate := c.Validate(); errValidate != nil {
		return "", errValidate
	}

	parts := []string{
		"host=" + quoteDSNValue(c.Host),
		"port=" + strconv.Itoa(c.Port),
		"user=" + quoteDSNValue(c.User),
		"password=" + quoteDSNValue(c.Password),
		"dbname=" + quoteDSNValue(c.Database),
		"sslmode=" + quoteDSNValue(c.SSLMode),
	}
	return strings.Join(parts, " "), nil
}

// clientAddr handles a client addr.
func clientAddr(ctx context.Context, db *gorm.DB) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return "", fmt.Errorf("database connection is nil")
	}

	var addr string
	if errScan := db.WithContext(ctx).Raw("SELECT inet_client_addr()").Scan(&addr).Error; errScan != nil {
		return "", errScan
	}
	if strings.TrimSpace(addr) == "" {
		return "", fmt.Errorf("postgres inet_client_addr() returned empty client address")
	}
	return addr, nil
}

// quoteDSNValue handles a quote dsn value.
func quoteDSNValue(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\r\n\\'") {
		return value
	}

	replacer := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + replacer.Replace(value) + "'"
}
