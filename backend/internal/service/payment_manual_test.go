//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestManualProviderRoutingAndSnapshot(t *testing.T) {
	require.NoError(t, validateProviderRequest("manual", "Personal collection", "alipay,wxpay"))
	for _, m := range []string{"alipay", "wxpay"} {
		key, ok := VisibleMethodProviderKeyForSource(m, "manual_"+m)
		require.True(t, ok)
		require.Equal(t, "manual", key)
	}
	require.Equal(t, []string{"alipay", "wxpay"}, enabledVisibleMethodsForProvider("manual", "alipay,wxpay"))
	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{ProviderKey: "manual", InstanceID: "1", Config: map[string]string{"alipayQRCode": "https://example.com/alipay.png", "wxpayQRCode": "https://example.com/wx.png", "payeeName": "Payee", "instructions": "Check amount", "secretKey": "never"}}, CreateOrderRequest{PaymentType: "alipay"})
	require.Equal(t, "https://example.com/alipay.png", snapshot["manual_qr_code_img"])
	require.Equal(t, "Payee", snapshot["manual_payee_name"])
	require.NotContains(t, snapshot, "secretKey")
}
func createManualTestOrder(t *testing.T, client *dbent.Client, status string, uid int64) *dbent.PaymentOrder {
	t.Helper()
	ctx := context.Background()
	if uid == 0 {
		u, err := client.User.Create().SetEmail(t.Name() + "@example.com").SetPasswordHash("hash").Save(ctx)
		require.NoError(t, err)
		uid = u.ID
	}
	o, err := client.PaymentOrder.Create().SetUserID(uid).SetUserEmail("u@example.com").SetUserName("U").SetAmount(1).SetPayAmount(7).SetRechargeCode("PAY-" + t.Name()).SetOutTradeNo("order-" + t.Name()).SetPaymentType("alipay").SetPaymentTradeNo("").SetOrderType("balance").SetStatus(status).SetExpiresAt(time.Now().Add(-time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("example.com").SetProviderKey("manual").SetProviderSnapshot(map[string]any{"provider_key": "manual", "currency": "CNY", "manual_qr_code_img": "https://example.com/a.png", "manual_payee_name": "Payee", "privateKey": "must-not-leak"}).Save(ctx)
	require.NoError(t, err)
	return o
}
func TestManualSubmitOwnsOrderAndDoesNotCredit(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	o := createManualTestOrder(t, client, "PENDING", 0)
	svc := &PaymentService{entClient: client}
	_, err := svc.SubmitManualPayment(ctx, o.ID, o.UserID+1, ManualPaymentSubmitRequest{})
	require.Error(t, err)
	detail, err := svc.SubmitManualPayment(ctx, o.ID, o.UserID, ManualPaymentSubmitRequest{TransactionNo: "user-ref", Note: "paid"})
	require.NoError(t, err)
	require.Equal(t, "PENDING_REVIEW", detail.Order.Status)
	require.Equal(t, "user-ref", detail.Submission.TransactionNo)
	current, err := client.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.Nil(t, current.PaidAt)
	require.Empty(t, current.PaymentTradeNo)
	u, err := client.User.Get(ctx, o.UserID)
	require.NoError(t, err)
	require.Zero(t, u.Balance)
	detail, err = svc.SubmitManualPayment(ctx, o.ID, o.UserID, ManualPaymentSubmitRequest{TransactionNo: "overwrite"})
	require.NoError(t, err)
	require.Equal(t, "user-ref", detail.Submission.TransactionNo)
	raw, err := json.Marshal(detail)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "privateKey")
	require.NotContains(t, string(raw), "provider_snapshot")
}
func TestManualApprovalAndRejectValidation(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	o := createManualTestOrder(t, client, "PENDING", 0)
	svc := &PaymentService{entClient: client}
	_, err := svc.SubmitManualPayment(ctx, o.ID, o.UserID, ManualPaymentSubmitRequest{})
	require.NoError(t, err)
	for _, req := range []ManualPaymentApproveRequest{{TransactionNo: "x", ReceivedAmount: 7}, {ReceivedAmount: 7, ConfirmReceived: true}, {TransactionNo: "x", ReceivedAmount: 7.001, ConfirmReceived: true}, {TransactionNo: "x", ReceivedAmount: 8, ConfirmReceived: true}} {
		_, err := svc.ApproveManualPayment(ctx, o.ID, 10, req)
		require.Error(t, err)
	}
	detail, err := svc.RejectManualPayment(ctx, o.ID, 10, "not received")
	require.NoError(t, err)
	require.Equal(t, "REJECTED", detail.Order.Status)
	require.Error(t, svc.RetryFulfillment(ctx, o.ID))
	_, err = svc.SubmitManualPayment(ctx, o.ID, o.UserID, ManualPaymentSubmitRequest{})
	require.Error(t, err)
	require.Error(t, svc.confirmPayment(ctx, o.ID, "forged", 7, "manual", nil))
	current, err := client.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, "REJECTED", current.Status)
}
func TestManualProviderEnablementAndCheckoutMarker(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{entClient: client, settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentVisibleMethodAlipaySource: "manual_alipay", SettingPaymentVisibleMethodAlipayEnabled: "true"}}}
	_, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{ProviderKey: "manual", Name: "Personal", SupportedTypes: []string{"alipay", "wxpay"}, Config: map[string]string{"alipayQRCode": "https://example.com/a.png"}, Enabled: true})
	require.Error(t, err)
	inst, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{ProviderKey: "manual", Name: "Personal", SupportedTypes: []string{"alipay"}, Config: map[string]string{"alipayQRCode": "https://example.com/a.png"}, Enabled: true, RefundEnabled: true, AllowUserRefund: true})
	require.NoError(t, err)
	require.False(t, inst.RefundEnabled)
	require.False(t, inst.AllowUserRefund)
	limits, err := svc.GetAvailableMethodLimits(ctx)
	require.NoError(t, err)
	require.True(t, limits.Methods["alipay"].IsManual)
	invalid := true
	_, err = svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{SupportedTypes: []string{"wxpay"}, Enabled: &invalid})
	require.Error(t, err)
}
func TestManualPendingReviewCapAndExpiry(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	o := createManualTestOrder(t, client, "PENDING_REVIEW", 0)
	svc := &PaymentService{entClient: client}
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	require.Error(t, svc.checkPendingLimit(ctx, tx, o.UserID, 1))
	require.NoError(t, tx.Rollback())
	expiredCount, err := svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Zero(t, expiredCount)
	_, err = client.PaymentOrder.UpdateOneID(o.ID).SetStatus("EXPIRED").Save(ctx)
	require.NoError(t, err)
	detail, err := svc.SubmitManualPayment(ctx, o.ID, o.UserID, ManualPaymentSubmitRequest{})
	require.NoError(t, err)
	require.Equal(t, "PENDING_REVIEW", detail.Order.Status)
}
func TestManualApproveRetryCreditsFrozenAmountOnce(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	o := createManualTestOrder(t, client, "PENDING_REVIEW", 0)
	repo := &paymentFulfillmentRedeemRepo{}
	credited := 0.0
	users := &mockUserRepo{getByIDUser: &User{ID: o.UserID}}
	users.updateBalanceFn = func(_ context.Context, _ int64, amount float64) error { credited += amount; return nil }
	svc := &PaymentService{entClient: client, redeemService: NewRedeemService(repo, users, nil, nil, nil, client, nil, nil), userRepo: users}
	req := ManualPaymentApproveRequest{TransactionNo: "actual-ref", ReceivedAmount: 7, ConfirmReceived: true}
	d, err := svc.ApproveManualPayment(ctx, o.ID, 10, req)
	require.NoError(t, err)
	require.Equal(t, "COMPLETED", d.Order.Status)
	require.Equal(t, 1.0, credited)
	_, err = svc.ApproveManualPayment(ctx, o.ID, 10, req)
	require.NoError(t, err)
	require.Equal(t, 1.0, credited)
	req.TransactionNo = "different-ref"
	_, err = svc.ApproveManualPayment(ctx, o.ID, 10, req)
	require.Error(t, err)
	require.Equal(t, 1.0, credited)
}
func TestManualCannotActivateInvalidStoredQRCode(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	_, err := client.PaymentProviderInstance.Create().SetProviderKey("manual").SetName("Invalid legacy").SetConfig(`{"alipayQRCode":"javascript:bad"}`).SetSupportedTypes("alipay").SetEnabled(true).Save(ctx)
	require.NoError(t, err)
	svc := &PaymentConfigService{entClient: client}
	limits, err := svc.GetAvailableMethodLimits(ctx)
	require.NoError(t, err)
	require.NotContains(t, limits.Methods, "alipay")
}
func TestManualRefundEntrypointsRefuseEvenWithRefundFlags(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	o := createManualTestOrder(t, client, "COMPLETED", 0)
	svc := &PaymentService{entClient: client}
	_, err := svc.validateRefundRequest(ctx, o.ID, o.UserID)
	require.Error(t, err)
	_, _, err = svc.PrepareRefund(ctx, o.ID, 1, "refund", true, true)
	require.Error(t, err)
	_, err = svc.ExecuteRefund(ctx, &RefundPlan{OrderID: o.ID, Order: o})
	require.Error(t, err)
	current, err := client.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, "COMPLETED", current.Status)
}

func TestManualRequiresMatchingVisibleSource(t *testing.T) {
	instances := []*dbent.PaymentProviderInstance{{ProviderKey: "manual", SupportedTypes: "alipay", Enabled: true}}
	repo := &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentVisibleMethodAlipaySource: "official_alipay"}}
	svc := &PaymentConfigService{settingRepo: repo}
	_, err := svc.resolveVisibleMethodProviderKey(context.Background(), "alipay", instances)
	require.Error(t, err)
	repo.values[SettingPaymentVisibleMethodAlipaySource] = ""
	_, err = svc.resolveVisibleMethodProviderKey(context.Background(), "alipay", instances)
	require.Error(t, err)
	repo.values[SettingPaymentVisibleMethodAlipaySource] = "manual_alipay"
	_, err = svc.resolveVisibleMethodProviderKey(context.Background(), "alipay", instances)
	require.Error(t, err)
	repo.values[SettingPaymentVisibleMethodAlipayEnabled] = "true"
	key, err := svc.resolveVisibleMethodProviderKey(context.Background(), "alipay", instances)
	require.NoError(t, err)
	require.Equal(t, "manual", key)
}

func TestManualReconcile(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	var uid int64
	for i := 0; i < pendingPaymentReconcileLimit+1; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			o := createManualTestOrder(t, client, "PENDING", uid)
			uid = o.UserID
			_, err := client.PaymentOrder.UpdateOneID(o.ID).SetExpiresAt(time.Now().Add(time.Hour)).Save(ctx)
			require.NoError(t, err)
		})
	}
	auto := createManualTestOrder(t, client, "PENDING", uid)
	_, err := client.PaymentOrder.UpdateOneID(auto.ID).ClearProviderKey().ClearProviderSnapshot().SetExpiresAt(time.Now().Add(time.Hour)).Save(ctx)
	require.NoError(t, err)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{key: payment.TypeAlipay, resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}}
	registry.Register(provider)
	svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true}
	_, err = svc.ReconcilePendingPaymentOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, provider.queryCalls, "manual orders must not consume the automatic reconciliation batch")
}

func TestManualCreateOrderRejectsZeroRoundedCredit(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().SetEmail("zero-credit@example.com").SetPasswordHash("hash").SetUsername("zero-credit").Save(ctx)
	require.NoError(t, err)
	settings := &paymentConfigSettingRepoStub{values: map[string]string{
		SettingPaymentEnabled:                    "true",
		SettingMinRechargeAmount:                 "0.01",
		SettingMaxRechargeAmount:                 "1000",
		SettingBalanceRechargeMult:               "0.01",
		SettingPaymentVisibleMethodAlipaySource:  VisibleMethodSourceManualAlipay,
		SettingPaymentVisibleMethodAlipayEnabled: "true",
	}}
	config := NewPaymentConfigService(client, settings, nil)
	_, err = config.CreateProviderInstance(ctx, CreateProviderInstanceRequest{ProviderKey: payment.TypeManual, Name: "Manual zero credit test", SupportedTypes: []string{payment.TypeAlipay}, Enabled: true, Config: map[string]string{"currency": "CNY", "alipayQRCode": "https://example.com/collection.png"}})
	require.NoError(t, err)
	cfg, err := config.GetPaymentConfig(ctx)
	require.NoError(t, err)
	quote := quoteRechargeBonus(cfg, 0.01, "CNY")
	require.Equal(t, 0.01, quote.PayBase, "the payable quote remains positive")
	require.Zero(t, quote.Credited, "the USD wallet credit rounds to zero")
	users := &mockUserRepo{getByIDUser: &User{ID: user.ID, Email: user.Email, Username: user.Username, Status: payment.EntityStatusActive}}
	svc := NewPaymentService(client, payment.NewRegistry(), payment.NewDefaultLoadBalancer(client, nil), nil, nil, config, users, nil, nil)
	result, err := svc.CreateOrder(ctx, CreateOrderRequest{UserID: user.ID, Amount: 0.01, PaymentType: payment.TypeAlipay, OrderType: payment.OrderTypeBalance, ClientIP: "127.0.0.1", SrcHost: "example.com"})
	require.ErrorContains(t, err, "credited balance must be positive after rounding")
	require.Nil(t, result)
	count, err := client.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count, "an invalid manual quote must be rejected before order persistence")
}
