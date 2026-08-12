package tgc

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gotd/td/tgerr"
)

func TestBotAuthLockPerBotIdentity(t *testing.T) {
	// The same bot ID must map to the same mutex so concurrent re-auth of the
	// same bot serializes, and different bot IDs map to different mutexes.
	lockA1 := botAuthLock("1000")
	lockA2 := botAuthLock("1000")
	lockB := botAuthLock("2000")

	if lockA1 != lockA2 {
		t.Fatal("same bot ID mapped to different locks; re-auth would not serialize per bot")
	}
	if lockA1 == lockB {
		t.Fatal("different bot IDs mapped to the same lock; re-auth would not be per-bot independent")
	}
}

func TestBotAuthLockSerializes(t *testing.T) {
	lock := botAuthLock("1000")

	var concurrent int32
	var maxConcurrent int32
	var wg sync.WaitGroup

	// Two goroutines should never hold the same per-bot lock concurrently.
	enter := func() {
		lock.Lock()
		defer lock.Unlock()
		if v := atomic.AddInt32(&concurrent, 1); v > 1 {
			atomic.StoreInt32(&maxConcurrent, v)
		}
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&concurrent, -1)
	}

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			enter()
		}()
	}
	wg.Wait()

	if atomic.LoadInt32(&maxConcurrent) > 1 {
		t.Fatal("per-bot lock was held concurrently by more than one goroutine")
	}
}

// TestIsKeyRevocationError exercises the revocation detector across the
// terminal error variants that must trigger a re-auth.
func TestIsKeyRevocationError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "authenticated", err: errors.New("some other error"), want: false},
		{name: "auth key unregistered", err: tgerr.New(401, "AUTH_KEY_UNREGISTERED"), want: true},
		{name: "session expired", err: tgerr.New(401, "SESSION_EXPIRED"), want: true},
		{name: "auth key duplicated", err: tgerr.New(401, "AUTH_KEY_DUPLICATED"), want: true},
		{name: "bare 401", err: tgerr.New(401, "OTHER"), want: true},
		{name: "non 401 rpc", err: tgerr.New(420, "FLOOD_WAIT_10"), want: false},
		{name: "wrapped revoked", err: errors.New("retry middleware skip: rpc error code 401: AUTH_KEY_UNREGISTERED"), want: true},
		{name: "wrapped other", err: errors.New("retry middleware skip: rpc error code 500: INTERNAL"), want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsKeyRevocationError(tc.err); got != tc.want {
				t.Fatalf("IsKeyRevocationError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
