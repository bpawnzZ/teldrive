package tgstorage

import (
	"context"
	"testing"
	"time"

	"github.com/tgdrive/teldrive/internal/cache"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestPgEviction verifies that evicting a bot session clears both the cache
// entry and the underlying teldrive.kv row so a fresh key is minted on the
// next LoadSession.
func TestPgEviction(t *testing.T) {
	ctx := context.Background()

	// Use an in-memory SQLite DB with a `teldrive` schema attached so the
	// two-part `teldrive.kv` table name (used by the PostgreSQL backend)
	// resolves the same way for CREATE, INSERT and DELETE.
	db, err := gorm.Open(sqlite.Open("file:mem?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	if _, err := sqlDB.Exec("ATTACH DATABASE 'file:kv?mode=memory&cache=shared' AS teldrive"); err != nil {
		t.Fatalf("attach teldrive schema: %v", err)
	}
	const createTable = "CREATE TABLE teldrive.kv (`key` text, `value` bytea NOT NULL, `created_at` datetime, PRIMARY KEY (`key`))"
	if err := db.Exec(createTable).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}

	memCache := cache.NewMemoryCache(1024 * 1024)
	const botID = "123456789"

	// Seed a session row in "the DB" (same statement StoreSession issues).
	if err := db.Exec("INSERT INTO teldrive.kv (`key`, `value`, `created_at`) VALUES (?, ?, ?)",
		botID, []byte("stale session"), time.Now().UTC()).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}
	// Seed the matching cache entry (the load path uses cache.Key("session", key)).
	if err := memCache.Set(ctx, cache.Key("session", botID), []byte("stale session"), time.Hour); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	st := NewPostgresStorage(db, memCache, botID)
	if err := st.Evict(ctx); err != nil {
		t.Fatalf("evict: %v", err)
	}

	// DB row gone.
	var count int64
	if err := db.WithContext(ctx).Table("teldrive.kv").Where("key = ?", botID).Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected DB row evicted, got %d rows", count)
	}

	// Cache entry gone.
	var cached []byte
	err = memCache.Get(ctx, cache.Key("session", botID), &cached)
	if err == nil {
		t.Fatal("expected cache entry to be evicted, but it still exists")
	}
}

func TestMemoryEviction(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStorage()
	if err := st.StoreSession(ctx, []byte("data")); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := st.Evict(ctx); err != nil {
		t.Fatalf("evict: %v", err)
	}
	if _, err := st.LoadSession(ctx); err == nil {
		t.Fatal("expected session to be cleared after eviction")
	}
}
