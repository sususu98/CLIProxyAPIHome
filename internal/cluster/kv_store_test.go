package cluster

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestKVRepository(t *testing.T) (*Repository, context.Context) {
	t.Helper()

	ctx := context.Background()
	db, errOpenSQLite := OpenSQLite(ctx, filepath.Join(t.TempDir(), "home.db"))
	if errOpenSQLite != nil {
		t.Fatalf("OpenSQLite() error = %v", errOpenSQLite)
	}
	sqlDB, errDB := db.DB()
	if errDB != nil {
		t.Fatalf("get sqlite db: %v", errDB)
	}
	t.Cleanup(func() {
		if errClose := sqlDB.Close(); errClose != nil {
			t.Errorf("close sqlite db: %v", errClose)
		}
	})
	if errMigrate := AutoMigrate(db); errMigrate != nil {
		t.Fatalf("AutoMigrate() error = %v", errMigrate)
	}

	return NewRepository(db), ctx
}

func TestKVSetGetPreservesBinaryBytesAndCopies(t *testing.T) {
	t.Parallel()

	repo, ctx := newTestKVRepository(t)
	value := []byte{0, 1, 2, 255}
	written, errSet := repo.KVSet(ctx, "binary", value, 0, KVSetModeAlways)
	if errSet != nil {
		t.Fatalf("KVSet() error = %v", errSet)
	}
	if !written {
		t.Fatalf("KVSet() written = false, want true")
	}
	value[1] = 9

	got, found, errGet := repo.KVGet(ctx, "binary")
	if errGet != nil {
		t.Fatalf("KVGet() error = %v", errGet)
	}
	if !found {
		t.Fatalf("KVGet() found = false, want true")
	}
	want := []byte{0, 1, 2, 255}
	if !bytes.Equal(got, want) {
		t.Fatalf("KVGet() = %v, want %v", got, want)
	}
	got[2] = 8

	gotAgain, foundAgain, errGetAgain := repo.KVGet(ctx, "binary")
	if errGetAgain != nil {
		t.Fatalf("KVGet() again error = %v", errGetAgain)
	}
	if !foundAgain || !bytes.Equal(gotAgain, want) {
		t.Fatalf("KVGet() again = %v, %v, want %v, true", gotAgain, foundAgain, want)
	}
}

func TestKVSetModes(t *testing.T) {
	t.Parallel()

	repo, ctx := newTestKVRepository(t)
	if written, errSet := repo.KVSet(ctx, "mode", []byte("first"), 0, KVSetModeAlways); errSet != nil || !written {
		t.Fatalf("KVSet(always create) = %v, %v, want true, nil", written, errSet)
	}
	if written, errSet := repo.KVSet(ctx, "mode", []byte("second"), 0, KVSetModeNX); errSet != nil || written {
		t.Fatalf("KVSet(nx existing) = %v, %v, want false, nil", written, errSet)
	}
	got, found, errGet := repo.KVGet(ctx, "mode")
	if errGet != nil {
		t.Fatalf("KVGet() error = %v", errGet)
	}
	if !found || string(got) != "first" {
		t.Fatalf("KVGet() = %q, %v, want first, true", got, found)
	}
	if written, errSet := repo.KVSet(ctx, "mode", []byte("third"), 0, KVSetModeXX); errSet != nil || !written {
		t.Fatalf("KVSet(xx existing) = %v, %v, want true, nil", written, errSet)
	}
	if written, errSet := repo.KVSet(ctx, "missing", []byte("value"), 0, KVSetModeXX); errSet != nil || written {
		t.Fatalf("KVSet(xx missing) = %v, %v, want false, nil", written, errSet)
	}
	if written, errSet := repo.KVSet(ctx, "created-by-nx", []byte("value"), 0, KVSetModeNX); errSet != nil || !written {
		t.Fatalf("KVSet(nx missing) = %v, %v, want true, nil", written, errSet)
	}
}

func TestKVExpiredRowsAreMissingAndLazyDeleteKeepsActiveRows(t *testing.T) {
	t.Parallel()

	repo, ctx := newTestKVRepository(t)
	past := time.Now().UTC().Add(-time.Minute)
	future := time.Now().UTC().Add(time.Hour)
	if errCreate := repo.db.Create(&KVRecord{Key: "expired", Value: []byte("old"), ExpiresAt: &past}).Error; errCreate != nil {
		t.Fatalf("create expired record: %v", errCreate)
	}
	if errCreate := repo.db.Create(&KVRecord{Key: "active", Value: []byte("new"), ExpiresAt: &future}).Error; errCreate != nil {
		t.Fatalf("create active record: %v", errCreate)
	}

	got, found, errGet := repo.KVGet(ctx, "expired")
	if errGet != nil {
		t.Fatalf("KVGet(expired) error = %v", errGet)
	}
	if found || got != nil {
		t.Fatalf("KVGet(expired) = %v, %v, want nil, false", got, found)
	}
	ttl, errTTL := repo.KVTTL(ctx, "active")
	if errTTL != nil {
		t.Fatalf("KVTTL(active) error = %v", errTTL)
	}
	if ttl <= 0 {
		t.Fatalf("KVTTL(active) = %d, want positive", ttl)
	}
	gotActive, foundActive, errGetActive := repo.KVGet(ctx, "active")
	if errGetActive != nil {
		t.Fatalf("KVGet(active) error = %v", errGetActive)
	}
	if !foundActive || string(gotActive) != "new" {
		t.Fatalf("KVGet(active) = %q, %v, want new, true", gotActive, foundActive)
	}
}

func TestLazyDeleteExpiredKVDoesNotDeleteConcurrentNewWrite(t *testing.T) {
	t.Parallel()

	repo, ctx := newTestKVRepository(t)
	past := time.Now().UTC().Add(-time.Minute)
	future := time.Now().UTC().Add(time.Hour)
	if errCreate := repo.db.Create(&KVRecord{Key: "race", Value: []byte("old"), ExpiresAt: &past}).Error; errCreate != nil {
		t.Fatalf("create expired record: %v", errCreate)
	}
	if errUpdate := repo.db.Model(&KVRecord{}).Where("key = ?", "race").Updates(map[string]any{
		"value":      []byte("new"),
		"expires_at": &future,
	}).Error; errUpdate != nil {
		t.Fatalf("simulate concurrent write: %v", errUpdate)
	}

	if errDelete := lazyDeleteExpiredKV(ctx, repo.db, "race", &past); errDelete != nil {
		t.Fatalf("lazyDeleteExpiredKV() error = %v", errDelete)
	}
	got, found, errGet := repo.KVGet(ctx, "race")
	if errGet != nil {
		t.Fatalf("KVGet(race) error = %v", errGet)
	}
	if !found || string(got) != "new" {
		t.Fatalf("KVGet(race) = %q, %v, want new, true", got, found)
	}
}

func TestKVDelCountsOnlyActiveKeys(t *testing.T) {
	t.Parallel()

	repo, ctx := newTestKVRepository(t)
	past := time.Now().UTC().Add(-time.Minute)
	if written, errSet := repo.KVSet(ctx, "active", []byte("value"), 0, KVSetModeAlways); errSet != nil || !written {
		t.Fatalf("KVSet(active) = %v, %v, want true, nil", written, errSet)
	}
	if written, errSet := repo.KVSet(ctx, "no-ttl", []byte("value"), 0, KVSetModeAlways); errSet != nil || !written {
		t.Fatalf("KVSet(no-ttl) = %v, %v, want true, nil", written, errSet)
	}
	if errCreate := repo.db.Create(&KVRecord{Key: "expired", Value: []byte("old"), ExpiresAt: &past}).Error; errCreate != nil {
		t.Fatalf("create expired record: %v", errCreate)
	}

	deleted, errDel := repo.KVDel(ctx, []string{"active", "expired", "missing", "no-ttl"})
	if errDel != nil {
		t.Fatalf("KVDel() error = %v", errDel)
	}
	if deleted != 2 {
		t.Fatalf("KVDel() = %d, want 2", deleted)
	}
}

func TestKVExpireAndTTL(t *testing.T) {
	t.Parallel()

	repo, ctx := newTestKVRepository(t)
	past := time.Now().UTC().Add(-time.Minute)
	if ok, errExpire := repo.KVExpire(ctx, "missing", time.Minute); errExpire != nil || ok {
		t.Fatalf("KVExpire(missing) = %v, %v, want false, nil", ok, errExpire)
	}
	if errCreate := repo.db.Create(&KVRecord{Key: "expired", Value: []byte("old"), ExpiresAt: &past}).Error; errCreate != nil {
		t.Fatalf("create expired record: %v", errCreate)
	}
	if ok, errExpire := repo.KVExpire(ctx, "expired", time.Minute); errExpire != nil || ok {
		t.Fatalf("KVExpire(expired) = %v, %v, want false, nil", ok, errExpire)
	}
	if written, errSet := repo.KVSet(ctx, "ttl", []byte("value"), 0, KVSetModeAlways); errSet != nil || !written {
		t.Fatalf("KVSet(ttl) = %v, %v, want true, nil", written, errSet)
	}
	if ttl, errTTL := repo.KVTTL(ctx, "missing"); errTTL != nil || ttl != -2 {
		t.Fatalf("KVTTL(missing) = %d, %v, want -2, nil", ttl, errTTL)
	}
	if ttl, errTTL := repo.KVTTL(ctx, "ttl"); errTTL != nil || ttl != -1 {
		t.Fatalf("KVTTL(no ttl) = %d, %v, want -1, nil", ttl, errTTL)
	}
	if ok, errExpire := repo.KVExpire(ctx, "ttl", time.Minute); errExpire != nil || !ok {
		t.Fatalf("KVExpire(active) = %v, %v, want true, nil", ok, errExpire)
	}
	if ttl, errTTL := repo.KVTTL(ctx, "ttl"); errTTL != nil || ttl <= 0 {
		t.Fatalf("KVTTL(active) = %d, %v, want positive, nil", ttl, errTTL)
	}
}

func TestKVIncrBy(t *testing.T) {
	t.Parallel()

	repo, ctx := newTestKVRepository(t)
	got, errIncr := repo.KVIncrBy(ctx, "counter", 5)
	if errIncr != nil {
		t.Fatalf("KVIncrBy(missing) error = %v", errIncr)
	}
	if got != 5 {
		t.Fatalf("KVIncrBy(missing) = %d, want 5", got)
	}
	got, errIncr = repo.KVIncrBy(ctx, "counter", -2)
	if errIncr != nil {
		t.Fatalf("KVIncrBy(existing) error = %v", errIncr)
	}
	if got != 3 {
		t.Fatalf("KVIncrBy(existing) = %d, want 3", got)
	}
	if written, errSet := repo.KVSet(ctx, "bad", []byte("not-int"), 0, KVSetModeAlways); errSet != nil || !written {
		t.Fatalf("KVSet(bad) = %v, %v, want true, nil", written, errSet)
	}
	if _, errIncrBad := repo.KVIncrBy(ctx, "bad", 1); errIncrBad == nil {
		t.Fatalf("KVIncrBy(non integer) error = nil, want error")
	}
}

func TestKVMGetMaintainsOrder(t *testing.T) {
	t.Parallel()

	repo, ctx := newTestKVRepository(t)
	if written, errSet := repo.KVSet(ctx, "first", []byte("1"), 0, KVSetModeAlways); errSet != nil || !written {
		t.Fatalf("KVSet(first) = %v, %v, want true, nil", written, errSet)
	}
	if written, errSet := repo.KVSet(ctx, "third", []byte("3"), 0, KVSetModeAlways); errSet != nil || !written {
		t.Fatalf("KVSet(third) = %v, %v, want true, nil", written, errSet)
	}

	results, errMGet := repo.KVMGet(ctx, []string{"first", "missing", "third"})
	if errMGet != nil {
		t.Fatalf("KVMGet() error = %v", errMGet)
	}
	if len(results) != 3 {
		t.Fatalf("KVMGet() len = %d, want 3", len(results))
	}
	if !results[0].Found || string(results[0].Value) != "1" {
		t.Fatalf("KVMGet()[0] = %#v, want found 1", results[0])
	}
	if results[1].Found || results[1].Value != nil {
		t.Fatalf("KVMGet()[1] = %#v, want missing", results[1])
	}
	if !results[2].Found || string(results[2].Value) != "3" {
		t.Fatalf("KVMGet()[2] = %#v, want found 3", results[2])
	}
}

func TestKVMSetRejectsInvalidKeyAtomically(t *testing.T) {
	t.Parallel()

	repo, ctx := newTestKVRepository(t)
	errMSet := repo.KVMSet(ctx, map[string][]byte{
		"valid": []byte("value"),
		"   ":   []byte("bad"),
	})
	if errMSet == nil {
		t.Fatalf("KVMSet() error = nil, want error")
	}
	got, found, errGet := repo.KVGet(ctx, "valid")
	if errGet != nil {
		t.Fatalf("KVGet(valid) error = %v", errGet)
	}
	if found || got != nil {
		t.Fatalf("KVGet(valid) = %v, %v, want nil, false", got, found)
	}
	if errMSetOK := repo.KVMSet(ctx, map[string][]byte{
		"first":  []byte("1"),
		"second": []byte("2"),
	}); errMSetOK != nil {
		t.Fatalf("KVMSet(valid) error = %v", errMSetOK)
	}
	results, errMGet := repo.KVMGet(ctx, []string{"second", "first"})
	if errMGet != nil {
		t.Fatalf("KVMGet() error = %v", errMGet)
	}
	if !results[0].Found || string(results[0].Value) != "2" || !results[1].Found || string(results[1].Value) != "1" {
		t.Fatalf("KVMGet() = %#v, want second then first", results)
	}
}

func TestKVPurgeExpired(t *testing.T) {
	t.Parallel()

	repo, ctx := newTestKVRepository(t)
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	for _, key := range []string{"expired-1", "expired-2"} {
		if errCreate := repo.db.Create(&KVRecord{Key: key, Value: []byte("old"), ExpiresAt: &past}).Error; errCreate != nil {
			t.Fatalf("create %s: %v", key, errCreate)
		}
	}
	if errCreate := repo.db.Create(&KVRecord{Key: "active", Value: []byte("new"), ExpiresAt: &future}).Error; errCreate != nil {
		t.Fatalf("create active: %v", errCreate)
	}

	deleted, errPurge := repo.KVPurgeExpired(ctx, now, 1)
	if errPurge != nil {
		t.Fatalf("KVPurgeExpired(limit 1) error = %v", errPurge)
	}
	if deleted != 1 {
		t.Fatalf("KVPurgeExpired(limit 1) = %d, want 1", deleted)
	}
	deleted, errPurge = repo.KVPurgeExpired(ctx, now, 100)
	if errPurge != nil {
		t.Fatalf("KVPurgeExpired(limit 100) error = %v", errPurge)
	}
	if deleted != 1 {
		t.Fatalf("KVPurgeExpired(limit 100) = %d, want 1", deleted)
	}
	got, found, errGet := repo.KVGet(ctx, "active")
	if errGet != nil {
		t.Fatalf("KVGet(active) error = %v", errGet)
	}
	if !found || string(got) != "new" {
		t.Fatalf("KVGet(active) = %q, %v, want new, true", got, found)
	}
}
