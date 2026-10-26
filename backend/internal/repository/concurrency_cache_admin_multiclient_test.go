package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type sharedConcurrencyRedis struct {
	server  *miniredis.Miniredis
	clientA *redis.Client
	clientB *redis.Client
	cacheA  service.ConcurrencyCache
	cacheB  service.ConcurrencyCache
	svcA    *service.ConcurrencyService
	svcB    *service.ConcurrencyService
	liveA   service.LiveConcurrencyCache
	liveB   service.LiveConcurrencyCache
}

func newSharedConcurrencyRedis(t *testing.T) sharedConcurrencyRedis {
	t.Helper()
	server := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, clientA.Close()) })
	t.Cleanup(func() { require.NoError(t, clientB.Close()) })

	// Separate cache and service objects model independent app instances. Both
	// Redis clients use the same miniredis endpoint and the production namespace.
	cacheA := NewConcurrencyCache(clientA, 15, 900)
	cacheB := NewConcurrencyCache(clientB, 15, 900)
	liveA, ok := cacheA.(service.LiveConcurrencyCache)
	require.True(t, ok)
	liveB, ok := cacheB.(service.LiveConcurrencyCache)
	require.True(t, ok)
	return sharedConcurrencyRedis{
		server:  server,
		clientA: clientA,
		clientB: clientB,
		cacheA:  cacheA,
		cacheB:  cacheB,
		svcA:    service.NewConcurrencyService(cacheA),
		svcB:    service.NewConcurrencyService(cacheB),
		liveA:   liveA,
		liveB:   liveB,
	}
}

func TestProvideConcurrencyServicePreservesFreshSharedRedisSlotsAndQueues(t *testing.T) {
	env := newSharedConcurrencyRedis(t)
	ctx := context.Background()
	const accountID int64 = 762300
	const userID int64 = 88000
	const accountRequestID = "instance-a-account-slot"
	const userRequestID = "instance-a-user-slot"

	// Treat the one-time legacy wait-key migration as already complete so this
	// regression isolates stale process-prefix cleanup of active multi-instance
	// state rather than testing that migration's intended sweep.
	require.NoError(t, env.clientA.Set(ctx, legacyWaitSweepMarkerKey, "1", 0).Err())

	// Instance A uses a distinct request prefix, as it would in a separate app
	// process, while both app clients share one Redis server/namespace.
	accountAcquired, err := env.cacheA.AcquireAccountSlot(ctx, accountID, 1, accountRequestID)
	require.NoError(t, err)
	require.True(t, accountAcquired)
	userAcquired, err := env.cacheA.AcquireUserSlot(ctx, userID, 1, userRequestID)
	require.NoError(t, err)
	require.True(t, userAcquired)
	userQueued, err := env.cacheA.IncrementWaitCount(ctx, userID, 5)
	require.NoError(t, err)
	require.True(t, userQueued)
	accountQueued, err := env.cacheA.IncrementAccountWaitCount(ctx, accountID, 5)
	require.NoError(t, err)
	require.True(t, accountQueued)

	// Constructing instance B runs its normal startup cleanup against the same
	// Redis server. Fresh slots and queue counts from A must survive.
	instanceB := service.ProvideConcurrencyService(env.cacheB, nil, nil)
	accountCount, err := env.cacheB.GetAccountConcurrency(ctx, accountID)
	require.NoError(t, err)
	require.Equal(t, 1, accountCount, "startup must preserve another live instance's account slot")
	userCount, err := env.cacheB.GetUserConcurrency(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, 1, userCount, "startup must preserve another live instance's user slot")
	accountWaitCount, err := env.cacheB.GetAccountWaitingCount(ctx, accountID)
	require.NoError(t, err)
	require.Equal(t, 1, accountWaitCount, "startup must preserve another live instance's account wait count")
	userLoads, err := env.cacheB.GetUsersLoadBatch(ctx, []service.UserWithConcurrency{{ID: userID, MaxConcurrency: 1}})
	require.NoError(t, err)
	require.Equal(t, 1, userLoads[userID].WaitingCount, "startup must preserve another live instance's user wait count")

	blockedAccount, err := instanceB.AcquireAccountSlot(ctx, accountID, 1)
	require.NoError(t, err)
	require.False(t, blockedAccount.Acquired, "startup must not free shared account capacity")
	blockedUser, err := instanceB.AcquireUserSlot(ctx, userID, 1)
	require.NoError(t, err)
	require.False(t, blockedUser.Acquired, "startup must not free shared user capacity")

	// The replacement startup cleanup is expiration-based. Existing slot/wait
	// TTLs still restore capacity after abandoned work expires.
	env.server.FastForward(16 * time.Minute)
	instanceB = service.ProvideConcurrencyService(env.cacheB, nil, nil)
	accountCount, err = env.cacheB.GetAccountConcurrency(ctx, accountID)
	require.NoError(t, err)
	require.Zero(t, accountCount, "expired account slots should be recovered")
	userCount, err = env.cacheB.GetUserConcurrency(ctx, userID)
	require.NoError(t, err)
	require.Zero(t, userCount, "expired user slots should be recovered")
	accountWaitCount, err = env.cacheB.GetAccountWaitingCount(ctx, accountID)
	require.NoError(t, err)
	require.Zero(t, accountWaitCount, "expired account wait counts should be recovered")
	userLoads, err = env.cacheB.GetUsersLoadBatch(ctx, []service.UserWithConcurrency{{ID: userID, MaxConcurrency: 1}})
	require.NoError(t, err)
	require.Zero(t, userLoads[userID].WaitingCount, "expired user wait counts should be recovered")
	recoveredAccount, err := instanceB.AcquireAccountSlot(ctx, accountID, 1)
	require.NoError(t, err)
	require.True(t, recoveredAccount.Acquired, "account capacity should be reusable after its ordinary slot TTL")
	recoveredUser, err := instanceB.AcquireUserSlot(ctx, userID, 1)
	require.NoError(t, err)
	require.True(t, recoveredUser.Acquired, "user capacity should be reusable after its ordinary slot TTL")
	recoveredAccount.ReleaseFunc()
	recoveredUser.ReleaseFunc()

	require.NoError(t, env.cacheA.ReleaseAccountSlot(ctx, accountID, accountRequestID))
	require.NoError(t, env.cacheA.ReleaseUserSlot(ctx, userID, userRequestID))
}

func TestAccountSlotCapIsSharedAcrossInstancesAndGroups(t *testing.T) {
	env := newSharedConcurrencyRedis(t)
	ctx := context.Background()
	const accountID int64 = 762301
	const accountCap = 4
	const requests = 64

	// Each app instance stands for a caller that may have selected this same
	// globally identified account through a different group. Group IDs do not
	// enter the account-slot key; accountID is the shared authority.
	start := make(chan struct{})
	var wg sync.WaitGroup
	var acquired atomic.Int32
	var mu sync.Mutex
	releases := make([]func(), 0, accountCap)
	errs := make(chan error, requests)

	for i := 0; i < requests; i++ {
		serviceForInstance := env.svcA
		if i%2 == 1 {
			serviceForInstance = env.svcB
		}
		wg.Add(1)
		go func(svc *service.ConcurrencyService) {
			defer wg.Done()
			<-start
			result, err := svc.AcquireAccountSlot(ctx, accountID, accountCap)
			if err != nil {
				errs <- err
				return
			}
			if result.Acquired {
				acquired.Add(1)
				mu.Lock()
				releases = append(releases, result.ReleaseFunc)
				mu.Unlock()
			}
		}(serviceForInstance)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, accountCap, acquired.Load(), "parallel app instances must not oversubscribe one account")
	require.Len(t, releases, accountCap)

	countA, err := env.cacheA.GetAccountConcurrency(ctx, accountID)
	require.NoError(t, err)
	countB, err := env.cacheB.GetAccountConcurrency(ctx, accountID)
	require.NoError(t, err)
	require.Equal(t, accountCap, countA)
	require.Equal(t, accountCap, countB, "independent clients must observe the same shared account slots")

	blocked, err := env.svcB.AcquireAccountSlot(ctx, accountID, accountCap)
	require.NoError(t, err)
	require.False(t, blocked.Acquired, "a caller from another group cannot preempt occupied slots")

	// Removing an unrelated/non-owner request ID must not release any slot.
	require.NoError(t, env.cacheB.ReleaseAccountSlot(ctx, accountID, "not-the-owner-request"))
	blocked, err = env.svcA.AcquireAccountSlot(ctx, accountID, accountCap)
	require.NoError(t, err)
	require.False(t, blocked.Acquired, "only the holder's release may restore capacity")

	// Releasing one actual owner slot restores exactly one admission across the
	// other app instance.
	releases[0]()
	replacement, err := env.svcB.AcquireAccountSlot(ctx, accountID, accountCap)
	require.NoError(t, err)
	require.True(t, replacement.Acquired)
	countA, err = env.cacheA.GetAccountConcurrency(ctx, accountID)
	require.NoError(t, err)
	require.Equal(t, accountCap, countA)

	replacement.ReleaseFunc()
	for _, release := range releases[1:] {
		release()
	}
	countB, err = env.cacheB.GetAccountConcurrency(ctx, accountID)
	require.NoError(t, err)
	require.Zero(t, countB, "owner releases must restore all capacity")
}

func TestLiveLeaseAdminUserExemptionRetainsSharedAccountCap(t *testing.T) {
	env := newSharedConcurrencyRedis(t)
	ctx := context.Background()
	const accountID int64 = 762302
	const userID int64 = 88001
	const apiKeyID int64 = 99001

	// An admin's internal userMax=0 exemption cannot override the account cap
	// or displace another request's ordinary account slot.
	ordinary, err := env.svcA.AcquireAccountSlot(ctx, accountID, 1)
	require.NoError(t, err)
	require.True(t, ordinary.Acquired)
	adminLive, err := env.liveB.AcquireLiveLease(ctx, accountID, 1, userID, 0, apiKeyID, "admin-live-blocked", false)
	require.NoError(t, err)
	require.False(t, adminLive, "userMax=0 disables only the user-side limit, not account occupancy")
	ordinary.ReleaseFunc()

	adminLive, err = env.liveB.AcquireLiveLease(ctx, accountID, 1, userID, 0, apiKeyID, "admin-live-allowed", false)
	require.NoError(t, err)
	require.True(t, adminLive)
	accountCount, err := env.cacheA.GetAccountConcurrency(ctx, accountID)
	require.NoError(t, err)
	require.Equal(t, 1, accountCount, "admin live lease occupies one shared account slot")
	ordinary, err = env.svcA.AcquireAccountSlot(ctx, accountID, 1)
	require.NoError(t, err)
	require.False(t, ordinary.Acquired, "ordinary requests remain blocked by the live account lease")
	require.NoError(t, env.liveA.ReleaseLiveLease(ctx, accountID, userID, apiKeyID, "admin-live-allowed"))
	ordinary, err = env.svcB.AcquireAccountSlot(ctx, accountID, 1)
	require.NoError(t, err)
	require.True(t, ordinary.Acquired, "releasing the live lease restores shared account capacity")
	ordinary.ReleaseFunc()

	// Model the actual Live handoff: its admission already owns one ordinary
	// account/user slot, then a live lease replaces those slots atomically. The
	// zero user limit must not relax the account cap after the ordinary slots are
	// released.
	const handoffAccountID int64 = 762304
	const handoffUserID int64 = 88003
	const handoffAPIKeyID int64 = 99003
	handoffAccount, err := env.svcA.AcquireAccountSlot(ctx, handoffAccountID, 1)
	require.NoError(t, err)
	require.True(t, handoffAccount.Acquired)
	handoffUser, err := env.svcA.AcquireUserSlot(ctx, handoffUserID, 1)
	require.NoError(t, err)
	require.True(t, handoffUser.Acquired)
	handoffLeaseID := "admin-live-handoff"
	handoffLive, err := env.liveB.AcquireLiveLease(ctx, handoffAccountID, 1, handoffUserID, 0, handoffAPIKeyID, handoffLeaseID, true)
	require.NoError(t, err)
	require.True(t, handoffLive)
	handoffBlocked, err := env.svcB.AcquireAccountSlot(ctx, handoffAccountID, 1)
	require.NoError(t, err)
	require.False(t, handoffBlocked.Acquired, "a live handoff cannot admit a third request while its ordinary owner slot is held")
	handoffAccount.ReleaseFunc()
	handoffUser.ReleaseFunc()

	accountCount, err = env.cacheA.GetAccountConcurrency(ctx, handoffAccountID)
	require.NoError(t, err)
	require.Equal(t, 1, accountCount, "live handoff must leave only its account slot counted")
	userCount, err := env.cacheB.GetUserConcurrency(ctx, handoffUserID)
	require.NoError(t, err)
	require.Equal(t, 1, userCount, "live handoff must leave only its live user lease counted")
	handoffAccount, err = env.svcB.AcquireAccountSlot(ctx, handoffAccountID, 1)
	require.NoError(t, err)
	require.False(t, handoffAccount.Acquired, "live admin lease must not exceed the account cap")
	require.NoError(t, env.liveA.ReleaseLiveLease(ctx, handoffAccountID, handoffUserID, handoffAPIKeyID, handoffLeaseID))

	// A positive userMax continues to constrain a live lease and regular user
	// slots across the two app instances.
	const limitedUserID int64 = 88002
	const otherAccountID int64 = 762303
	userSlot, err := env.svcA.AcquireUserSlot(ctx, limitedUserID, 1)
	require.NoError(t, err)
	require.True(t, userSlot.Acquired)
	limitedLive, err := env.liveB.AcquireLiveLease(ctx, otherAccountID, 1, limitedUserID, 1, apiKeyID+1, "positive-user-limit-blocked", false)
	require.NoError(t, err)
	require.False(t, limitedLive, "positive userMax still counts ordinary user occupancy")
	userSlot.ReleaseFunc()

	limitedLive, err = env.liveB.AcquireLiveLease(ctx, otherAccountID, 1, limitedUserID, 1, apiKeyID+1, "positive-user-limit-allowed", false)
	require.NoError(t, err)
	require.True(t, limitedLive)
	userSlot, err = env.svcA.AcquireUserSlot(ctx, limitedUserID, 1)
	require.NoError(t, err)
	require.False(t, userSlot.Acquired, "positive userMax counts live leases against ordinary user slots")
	require.NoError(t, env.liveA.ReleaseLiveLease(ctx, otherAccountID, limitedUserID, apiKeyID+1, "positive-user-limit-allowed"))
	userSlot, err = env.svcB.AcquireUserSlot(ctx, limitedUserID, 1)
	require.NoError(t, err)
	require.True(t, userSlot.Acquired, "releasing the live lease restores user capacity")
	userSlot.ReleaseFunc()
}
