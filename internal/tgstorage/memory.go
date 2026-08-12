package tgstorage

import (
	"context"
	"sync"

	"github.com/gotd/td/session"
)

var _ Storage = (*MemoryStorage)(nil)

// MemoryStorage implements session storage using in-memory storage
// This is suitable for development/testing only - data is lost on restart
type MemoryStorage struct {
	mu      sync.Mutex
	storage *session.StorageMemory
}

// NewMemoryStorage creates a new in-memory session storage
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		storage: new(session.StorageMemory),
	}
}

// LoadSession retrieves session data from memory
func (s *MemoryStorage) LoadSession(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.storage.LoadSession(ctx)
}

// StoreSession saves session data to memory
func (s *MemoryStorage) StoreSession(ctx context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.storage.StoreSession(ctx, data)
}

// Evict removes the session from memory so a fresh key is minted next time.
func (s *MemoryStorage) Evict(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storage = new(session.StorageMemory)
	return nil
}

// Type returns the storage type
func (s *MemoryStorage) Type() string {
	return "memory"
}

// Close is a no-op for memory storage
func (s *MemoryStorage) Close() error {
	return nil
}
