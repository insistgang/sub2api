package repository

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// This optional check starts its own Redis with no TCP listener or persistence.
// It never accepts an address for an existing Redis instance.
func TestAdminConcurrencyAccountCapsWithDisposableRedis(t *testing.T) {
	binary := os.Getenv("SUB2API_TEST_REDIS_BINARY")
	if binary == "" {
		t.Skip("set SUB2API_TEST_REDIS_BINARY for the disposable real-Redis check")
	}
	dir, err := os.MkdirTemp("/tmp", "s2a-redis-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "redis.sock")
	cmd := exec.Command(binary, "--port", "0", "--unixsocket", socket, "--unixsocketperm", "700", "--save", "", "--appendonly", "no", "--dir", dir)
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	clientA := redis.NewClient(&redis.Options{Network: "unix", Addr: socket})
	clientB := redis.NewClient(&redis.Options{Network: "unix", Addr: socket})
	t.Cleanup(func() { _ = clientA.Close(); _ = clientB.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	require.Eventually(t, func() bool { return clientA.Ping(ctx).Err() == nil }, 5*time.Second, 20*time.Millisecond)
	cacheA := NewConcurrencyCache(clientA, 15, 900)
	cacheB := NewConcurrencyCache(clientB, 15, 900)
	acquired, err := cacheA.AcquireAccountSlot(ctx, 89101, 1, "other-process:active")
	require.NoError(t, err)
	require.True(t, acquired)
	svcB := service.ProvideConcurrencyService(cacheB, nil, nil)
	blocked, err := svcB.AcquireAccountSlot(ctx, 89101, 1)
	require.NoError(t, err)
	require.False(t, blocked.Acquired, "startup must retain another process's occupied account slot")
	require.NoError(t, cacheA.ReleaseAccountSlot(ctx, 89101, "other-process:active"))

	svcA := service.NewConcurrencyService(cacheA)
	var wg sync.WaitGroup
	results := make(chan *service.AcquireResult, 32)
	errors := make(chan error, 32)
	for i := 0; i < 32; i++ {
		svc := svcA
		if i%2 == 1 {
			svc = svcB
		}
		wg.Add(1)
		go func(s *service.ConcurrencyService) {
			defer wg.Done()
			result, acquireErr := s.AcquireAccountSlot(ctx, 89102, 3)
			if acquireErr != nil {
				errors <- acquireErr
				return
			}
			if result.Acquired {
				results <- result
			}
		}(svc)
	}
	wg.Wait()
	close(results)
	close(errors)
	for acquireErr := range errors {
		require.NoError(t, acquireErr)
	}
	require.Len(t, results, 3, "two application instances must share one account cap")
	for result := range results {
		result.ReleaseFunc()
	}
	live := cacheA.(service.LiveConcurrencyCache)
	acquired, err = live.AcquireLiveLease(ctx, 89103, 1, 89104, 0, 89105, "admin-live", false)
	require.NoError(t, err)
	require.True(t, acquired)
	blocked, err = svcB.AcquireAccountSlot(ctx, 89103, 1)
	require.NoError(t, err)
	require.False(t, blocked.Acquired, "admin's unbounded user leg must retain its account cap")
	require.NoError(t, live.ReleaseLiveLease(ctx, 89103, 89104, 89105, "admin-live"))
	admitted, err := svcB.AcquireAccountSlot(ctx, 89103, 1)
	require.NoError(t, err)
	require.True(t, admitted.Acquired)
	admitted.ReleaseFunc()
}
