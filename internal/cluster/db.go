package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/glebarez/sqlite"
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

// OpenSQLite opens a SQLite database.
func OpenSQLite(ctx context.Context, path string) (*gorm.DB, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	path = strings.TrimSpace(path)
	if path == "" {
		path = "home.db"
	}
	db, errOpen := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if errOpen != nil {
		return nil, errOpen
	}
	sqlDB, errDB := db.DB()
	if errDB != nil {
		return nil, errDB
	}
	if errPing := sqlDB.PingContext(ctx); errPing != nil {
		if errClose := sqlDB.Close(); errClose != nil {
			return nil, fmt.Errorf("ping sqlite: %w; close sql db: %v", errPing, errClose)
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
	if errMigrate := db.AutoMigrate(&AuthRecord{}, &ConfigRecord{}, &APIKeyRecord{}, &ChannelGroupRecord{}, &ChannelGroupDetailRecord{}, &ClusterNodeRecord{}, &ClusterEventRecord{}, &UsageRecord{}, &OAuthSessionRecord{}, &CertificateRecord{}); errMigrate != nil {
		return errMigrate
	}
	if errMigrate := migrateCertificateFingerprints(db); errMigrate != nil {
		return errMigrate
	}
	if errMigrate := migrateAPIKeyChannels(db); errMigrate != nil {
		return errMigrate
	}
	return migrateLegacyAPIKeys(db)
}

func migrateCertificateFingerprints(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}
	var records []CertificateRecord
	if errFind := db.
		Where("certificate_pem <> ? AND COALESCE(certificate_fingerprint, '') = ?", "", "").
		Find(&records).Error; errFind != nil {
		return errFind
	}
	for _, record := range records {
		fingerprint, errFingerprint := certificateFingerprintPEM([]byte(record.CertificatePEM))
		if errFingerprint != nil {
			return errFingerprint
		}
		if errUpdate := db.Model(&CertificateRecord{}).
			Where("id = ?", record.ID).
			Update("certificate_fingerprint", fingerprint).Error; errUpdate != nil {
			return errUpdate
		}
	}
	return nil
}

func migrateLegacyAPIKeys(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}

	record := ConfigRecord{}
	errFirst := db.Where("key = ?", configAPIKeysRootKey).First(&record).Error
	switch {
	case errors.Is(errFirst, gorm.ErrRecordNotFound):
		return nil
	case errFirst != nil:
		return errFirst
	}

	var apiKeys []string
	if len(record.Value) > 0 {
		if errUnmarshal := json.Unmarshal([]byte(record.Value), &apiKeys); errUnmarshal != nil {
			var rawList []any
			if errUnmarshalList := json.Unmarshal([]byte(record.Value), &rawList); errUnmarshalList != nil {
				return errUnmarshal
			}
			apiKeys = make([]string, 0, len(rawList))
			for _, item := range rawList {
				str, ok := item.(string)
				if !ok {
					continue
				}
				apiKeys = append(apiKeys, str)
			}
		}
	}
	apiKeys = normalizeAPIKeys(apiKeys)

	ctx := context.Background()
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if errReplace := replaceAPIKeysTx(ctx, tx, apiKeys); errReplace != nil {
			return errReplace
		}
		if errDelete := tx.Delete(&ConfigRecord{}, "key = ?", configAPIKeysRootKey).Error; errDelete != nil {
			return errDelete
		}
		return appendEvent(tx, "config", "migrate", configAPIKeysRootKey, 1)
	})
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
