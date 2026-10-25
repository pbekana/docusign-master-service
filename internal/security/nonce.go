// Package security provides HMAC request verification, nonce replay prevention,
// and AES-256-GCM encryption helpers.
package security

import (
	"context"
	"sync"
	"time"
)

// nonceEntry records when a nonce was first accepted.
type nonceEntry struct {
	expiry time.Time
}

// NonceStore prevents replay attacks by tracking recently seen nonces.
// It is safe for concurrent use.
type NonceStore struct {
	mu      sync.Map
	cleanup chan struct{}
}

// NewNonceStore creates a NonceStore and starts a background goroutine that
// purges expired entries every 60 seconds. The goroutine stops when ctx is
// cancelled — call this with a derived context in tests to avoid leaks.
func NewNonceStore(ctx context.Context) *NonceStore {
	ns := &NonceStore{}

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ns.purgeExpired()
			}
		}
	}()

	return ns
}

// Use marks the nonce as seen if it has not been used before and has not expired
// (expiry is in the future). Returns true when the nonce is accepted (first use,
// not expired), false otherwise.
func (ns *NonceStore) Use(nonce string, expiry time.Time) bool {
	if time.Now().After(expiry) {
		// Nonce window has already passed; reject.
		return false
	}

	// LoadOrStore atomically inserts if not present.
	_, loaded := ns.mu.LoadOrStore(nonce, nonceEntry{expiry: expiry})
	// loaded == true means the nonce already existed → replay.
	return !loaded
}

// purgeExpired deletes entries whose expiry has passed more than once (2× tolerance
// is ensured by the caller setting expiry = now + 2*tolerance when storing).
func (ns *NonceStore) purgeExpired() {
	now := time.Now()
	ns.mu.Range(func(key, value any) bool {
		entry, ok := value.(nonceEntry)
		if ok && now.After(entry.expiry) {
			ns.mu.Delete(key)
		}
		return true
	})
}
