//go:build integration

package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/redeemcode"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type manualFailingRedeemRepository struct{ service.RedeemCodeRepository }

func (manualFailingRedeemRepository) Create(context.Context, *service.RedeemCode) error {
	return errors.New("injected redeem creation failure")
}

// This suite requires an explicitly supplied, disposable PostgreSQL database.
// It never migrates a database or uses the application's configured database.
func TestManualPostgresLifecycle(t *testing.T) {
	envFile := os.Getenv("MANUAL_PAYMENT_TEST_ENV_FILE")
	if envFile == "" {
		t.Skip("MANUAL_PAYMENT_TEST_ENV_FILE is not set; provide an isolated PostgreSQL database to run this suite")
	}
	b, err := os.ReadFile(envFile)
	require.NoError(t, err)
	var env map[string]string
	require.NoError(t, json.Unmarshal(b, &env))
	dsn := env["MANUAL_PAYMENT_TEST_DSN"]
	require.NotEmpty(t, dsn)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	var databaseName string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT current_database()").Scan(&databaseName))
	require.True(t, strings.HasPrefix(databaseName, "sub2api_manual_test_"), "refusing a database without the isolated-test prefix")
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	users := repository.NewUserRepository(client, db)
	redeems := repository.NewRedeemCodeRepository(client)
	settings := repository.NewSettingRepository(client)
	config := service.NewPaymentConfigService(client, settings, nil)
	require.NoError(t, settings.SetMultiple(ctx, map[string]string{service.SettingPaymentEnabled: "true", service.SettingMinRechargeAmount: "1", service.SettingMaxRechargeAmount: "1000", service.SettingMaxPendingOrders: "20", service.SettingBalanceRechargeMult: "0.142857142857", service.SettingRechargeFeeRate: "0", service.SettingPaymentVisibleMethodAlipaySource: "manual_alipay", service.SettingPaymentVisibleMethodAlipayEnabled: "true"}))
	_, err = config.CreateProviderInstance(ctx, service.CreateProviderInstanceRequest{ProviderKey: "manual", Name: "Isolated manual test", SupportedTypes: []string{"alipay"}, Enabled: true, Config: map[string]string{"currency": "CNY", "alipayQRCode": "https://example.com/isolated-test.png", "payeeName": "Test only"}})
	require.NoError(t, err)
	redeemSvc := service.NewRedeemService(redeems, users, nil, nil, nil, client, nil, nil)
	svc := service.NewPaymentService(client, payment.NewRegistry(), payment.NewDefaultLoadBalancer(client, nil), redeemSvc, nil, config, users, nil, nil)
	suffix := fmt.Sprint(time.Now().UnixNano())
	u, err := client.User.Create().SetEmail("manual-" + suffix + "@example.com").SetPasswordHash("test-only").SetUsername("Manual integration").Save(ctx)
	require.NoError(t, err)
	require.Zero(t, u.Balance)
	create := func() *service.CreateOrderResponse {
		r, e := svc.CreateOrder(ctx, service.CreateOrderRequest{UserID: u.ID, Amount: 7, PaymentType: "alipay", OrderType: "balance", ClientIP: "127.0.0.1", SrcHost: "example.com"})
		require.NoError(t, e)
		require.Equal(t, "manual_required", r.ResultType)
		require.True(t, r.IsManual)
		require.Equal(t, 7.0, r.PayAmount)
		require.Equal(t, 1.0, r.Amount)
		return r
	}
	balance := func() float64 { u, e := client.User.Get(ctx, u.ID); require.NoError(t, e); return u.Balance }
	submit := func(id int64) {
		_, e := svc.SubmitManualPayment(ctx, id, u.ID, service.ManualPaymentSubmitRequest{TransactionNo: "unverified-user-input"})
		require.NoError(t, e)
	}
	approve := func(id int64, ref string) error {
		_, e := svc.ApproveManualPayment(ctx, id, 100, service.ManualPaymentApproveRequest{TransactionNo: ref, ReceivedAmount: 7, ConfirmReceived: true})
		return e
	}
	first := create()
	_, err = svc.GetManualPayment(ctx, first.OrderID, u.ID+1)
	require.Error(t, err)
	_, err = svc.SubmitManualPayment(ctx, first.OrderID, u.ID+1, service.ManualPaymentSubmitRequest{})
	require.Error(t, err)
	submit(first.OrderID)
	require.Zero(t, balance())
	for _, req := range []service.ManualPaymentApproveRequest{{TransactionNo: "x", ReceivedAmount: 7}, {ReceivedAmount: 7, ConfirmReceived: true}, {TransactionNo: "x", ReceivedAmount: 7.001, ConfirmReceived: true}} {
		_, err = svc.ApproveManualPayment(ctx, first.OrderID, 100, req)
		require.Error(t, err)
	}
	require.NoError(t, svc.HandlePaymentNotification(ctx, &payment.PaymentNotification{OrderID: first.OutTradeNo, Amount: 7, Status: "failed"}, "manual"))
	require.Error(t, svc.HandlePaymentNotification(ctx, &payment.PaymentNotification{OrderID: first.OutTradeNo, TradeNo: "forged", Amount: 7, Status: "success"}, "manual"))
	require.Zero(t, balance())
	verified, err := svc.VerifyOrderByOutTradeNo(ctx, first.OutTradeNo, u.ID)
	require.NoError(t, err)
	require.Equal(t, "PENDING_REVIEW", verified.Status)
	require.NoError(t, settings.Set(ctx, service.SettingBalanceRechargeMult, "9"))
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); errCh <- approve(first.OrderID, "same-order-"+suffix) }()
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		require.NoError(t, e)
	}
	require.Equal(t, 1.0, balance())
	count, err := client.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(fmt.Sprint(first.OrderID)), paymentauditlog.ActionEQ("MANUAL_APPROVED")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	// Restore pricing for subsequent orders; the first approval used its frozen USD value.
	require.NoError(t, settings.Set(ctx, service.SettingBalanceRechargeMult, "0.142857142857"))
	a := create()
	c := create()
	submit(a.OrderID)
	submit(c.OrderID)
	errCh = make(chan error, 2)
	for _, id := range []int64{a.OrderID, c.OrderID} {
		wg.Add(1)
		go func(oid int64) { defer wg.Done(); errCh <- approve(oid, "shared-reference-"+suffix) }(id)
	}
	wg.Wait()
	close(errCh)
	successes := 0
	conflicts := 0
	for e := range errCh {
		if e == nil {
			successes++
		} else {
			require.Contains(t, e.Error(), "already been approved")
			conflicts++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	require.Equal(t, 2.0, balance())
	rejected := create()
	submit(rejected.OrderID)
	_, err = svc.RejectManualPayment(ctx, rejected.OrderID, 100, "not in recipient ledger")
	require.NoError(t, err)
	require.Error(t, svc.RetryFulfillment(ctx, rejected.OrderID))
	require.Error(t, approve(rejected.OrderID, "reject-"+suffix))
	require.Equal(t, 2.0, balance())
	// Simulate a process dying after its redeem transaction committed but before completion.
	_, err = client.PaymentOrder.UpdateOneID(first.OrderID).SetStatus("FAILED").ClearCompletedAt().Save(ctx)
	require.NoError(t, err)
	require.NoError(t, approve(first.OrderID, "same-order-"+suffix))
	require.Equal(t, 2.0, balance())
	persisted, err := client.PaymentOrder.Get(ctx, first.OrderID)
	require.NoError(t, err)
	used, err := client.RedeemCode.Query().Where(redeemcode.CodeEQ(persisted.RechargeCode), redeemcode.StatusEQ("used")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, used)
	// A failure after approval committed leaves a recoverable receipt and never credits early.
	recovery := create()
	submit(recovery.OrderID)
	failingRedeem := service.NewRedeemService(manualFailingRedeemRepository{redeems}, users, nil, nil, nil, client, nil, nil)
	failingSvc := service.NewPaymentService(client, payment.NewRegistry(), payment.NewDefaultLoadBalancer(client, nil), failingRedeem, nil, config, users, nil, nil)
	_, err = failingSvc.ApproveManualPayment(ctx, recovery.OrderID, 100, service.ManualPaymentApproveRequest{TransactionNo: "recovery-" + suffix, ReceivedAmount: 7, ConfirmReceived: true})
	require.Error(t, err)
	require.Equal(t, 2.0, balance())
	persisted, err = client.PaymentOrder.Get(ctx, recovery.OrderID)
	require.NoError(t, err)
	require.Equal(t, "FAILED", persisted.Status)
	require.NotNil(t, persisted.PaidAt)
	require.NoError(t, approve(recovery.OrderID, "recovery-"+suffix))
	require.Equal(t, 3.0, balance())
	require.NoError(t, approve(recovery.OrderID, "recovery-"+suffix))
	require.Equal(t, 3.0, balance())
	// Unpaid expired quotes may enter review; a pending review is never auto-expired.
	expired := create()
	_, err = client.PaymentOrder.UpdateOneID(expired.OrderID).SetStatus("EXPIRED").SetExpiresAt(time.Now().Add(-time.Hour)).Save(ctx)
	require.NoError(t, err)
	submit(expired.OrderID)
	_, err = svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	persisted, err = client.PaymentOrder.Get(ctx, expired.OrderID)
	require.NoError(t, err)
	require.Equal(t, "PENDING_REVIEW", persisted.Status)
	pendingCount, err := client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(u.ID), paymentorder.StatusIn("PENDING", "PENDING_REVIEW")).Count(ctx)
	require.NoError(t, err)
	require.Greater(t, pendingCount, 0)
}
