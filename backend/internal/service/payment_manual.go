package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

type ManualPaymentSubmitRequest struct {
	TransactionNo string `json:"transaction_no"`
	Note          string `json:"note"`
}
type ManualPaymentApproveRequest struct {
	TransactionNo   string  `json:"transaction_no"`
	ReceivedAmount  float64 `json:"received_amount"`
	ConfirmReceived bool    `json:"confirm_received"`
	Note            string  `json:"note"`
}
type ManualPaymentSubmission struct {
	TransactionNo string `json:"transaction_no"`
	Note          string `json:"note"`
	SubmittedAt   string `json:"submitted_at"`
}
type ManualPaymentOrder struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	Amount       float64   `json:"amount"`
	PayAmount    float64   `json:"pay_amount"`
	Currency     string    `json:"currency"`
	FeeRate      float64   `json:"fee_rate"`
	BonusAmount  float64   `json:"bonus_amount"`
	RefundAmount float64   `json:"refund_amount"`
	PaymentType  string    `json:"payment_type"`
	OutTradeNo   string    `json:"out_trade_no"`
	Status       string    `json:"status"`
	OrderType    string    `json:"order_type"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	IsManual     bool      `json:"is_manual"`
}
type ManualPaymentDetail struct {
	Order        ManualPaymentOrder       `json:"order"`
	QRCodeImg    string                   `json:"qr_code_img"`
	PayeeName    string                   `json:"payee_name"`
	Instructions string                   `json:"instructions"`
	Submission   *ManualPaymentSubmission `json:"submission,omitempty"`
	ReviewNote   string                   `json:"review_note"`
	ReviewedAt   string                   `json:"reviewed_at"`
}

func IsManualPaymentOrder(o *dbent.PaymentOrder) bool {
	return o != nil && (strings.EqualFold(psStringValue(o.ProviderKey), payment.TypeManual) || strings.EqualFold(psSnapshotStringValue(o.ProviderSnapshot["provider_key"]), payment.TypeManual))
}
func manualApproved(o *dbent.PaymentOrder) bool {
	return IsManualPaymentOrder(o) && o.PaidAt != nil && o.PaymentTradeNo != "" && psSnapshotStringValue(o.ProviderSnapshot["manual_verified_transaction_no"]) == o.PaymentTradeNo
}
func manualOrderError(o *dbent.PaymentOrder) error {
	if !IsManualPaymentOrder(o) || o.OrderType != payment.OrderTypeBalance {
		return infraerrors.BadRequest("NOT_MANUAL_ORDER", "order is not a manual balance recharge")
	}
	return nil
}
func manualPaymentDetail(o *dbent.PaymentOrder) *ManualPaymentDetail {
	d := &ManualPaymentDetail{Order: ManualPaymentOrder{ID: o.ID, UserID: o.UserID, Amount: o.Amount, PayAmount: o.PayAmount, Currency: PaymentOrderCurrency(o), FeeRate: o.FeeRate, BonusAmount: o.BonusAmount, RefundAmount: o.RefundAmount, PaymentType: o.PaymentType, OutTradeNo: o.OutTradeNo, Status: o.Status, OrderType: o.OrderType, CreatedAt: o.CreatedAt, ExpiresAt: o.ExpiresAt, IsManual: true}, QRCodeImg: psStringValue(o.QrCodeImg), PayeeName: psSnapshotStringValue(o.ProviderSnapshot["manual_payee_name"]), Instructions: psSnapshotStringValue(o.ProviderSnapshot["manual_instructions"]), ReviewNote: psSnapshotStringValue(o.ProviderSnapshot["manual_review_note"]), ReviewedAt: psSnapshotStringValue(o.ProviderSnapshot["manual_reviewed_at"])}
	if d.QRCodeImg == "" {
		d.QRCodeImg = psSnapshotStringValue(o.ProviderSnapshot["manual_qr_code_img"])
	}
	if at := psSnapshotStringValue(o.ProviderSnapshot["manual_submitted_at"]); at != "" {
		d.Submission = &ManualPaymentSubmission{TransactionNo: psSnapshotStringValue(o.ProviderSnapshot["manual_submitted_transaction_no"]), Note: psSnapshotStringValue(o.ProviderSnapshot["manual_submission_note"]), SubmittedAt: at}
	}
	return d
}
func (s *PaymentService) GetManualPayment(ctx context.Context, oid, uid int64) (*ManualPaymentDetail, error) {
	o, err := s.GetOrder(ctx, oid, uid)
	if err != nil {
		return nil, err
	}
	if err = manualOrderError(o); err != nil {
		return nil, err
	}
	return manualPaymentDetail(o), nil
}
func (s *PaymentService) AdminGetManualPayment(ctx context.Context, oid int64) (*ManualPaymentDetail, error) {
	o, err := s.GetOrderByID(ctx, oid)
	if err != nil {
		return nil, err
	}
	if err = manualOrderError(o); err != nil {
		return nil, err
	}
	return manualPaymentDetail(o), nil
}

func validateManualText(ref, note string) error {
	if len(ref) > 128 || len(note) > 2000 || strings.ContainsAny(ref, "\x00\r\n") || strings.ContainsRune(note, 0) {
		return infraerrors.BadRequest("INVALID_MANUAL_SUBMISSION", "transaction number or note is invalid")
	}
	return nil
}
func manualSnapshot(o *dbent.PaymentOrder) map[string]any {
	m := make(map[string]any, len(o.ProviderSnapshot)+5)
	for k, v := range o.ProviderSnapshot {
		m[k] = v
	}
	return m
}
func manualLock(ctx context.Context, client *dbent.Client, key string) error {
	if client.Driver().Dialect() != dialect.Postgres {
		return nil
	}
	_, err := client.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", key)
	return err
}
func manualLockedOrder(ctx context.Context, tx *dbent.Tx, oid int64) (*dbent.PaymentOrder, error) {
	q := tx.PaymentOrder.Query().Where(paymentorder.IDEQ(oid))
	if tx.Client().Driver().Dialect() == dialect.Postgres {
		q = q.ForUpdate()
	}
	o, err := q.Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	return o, err
}
func manualAudit(ctx context.Context, tx *dbent.Tx, oid int64, action string, uid int64, detail map[string]any) error {
	b, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(oid, 10)).SetAction(action).SetOperator(fmt.Sprintf("admin:%d", uid)).SetDetail(string(b)).Save(ctx)
	return err
}

func (s *PaymentService) SubmitManualPayment(ctx context.Context, oid, uid int64, req ManualPaymentSubmitRequest) (*ManualPaymentDetail, error) {
	req.TransactionNo = strings.TrimSpace(req.TransactionNo)
	req.Note = strings.TrimSpace(req.Note)
	if err := validateManualText(req.TransactionNo, req.Note); err != nil {
		return nil, err
	}
	maxPending := defaultMaxPendingOrders
	if s.configService != nil {
		cfg, err := s.configService.GetPaymentConfig(ctx)
		if err != nil {
			return nil, err
		}
		maxPending = cfg.MaxPendingOrders
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = manualLock(ctx, tx.Client(), fmt.Sprintf("payment-pending-user:%d", uid)); err != nil {
		return nil, err
	}
	o, err := manualLockedOrder(ctx, tx, oid)
	if err != nil {
		return nil, err
	}
	if o.UserID != uid {
		return nil, infraerrors.Forbidden("FORBIDDEN", "no permission for this order")
	}
	if err = manualOrderError(o); err != nil {
		return nil, err
	}
	if o.Status == OrderStatusPendingReview {
		return manualPaymentDetail(o), nil
	}
	if o.Status != OrderStatusPending && o.Status != OrderStatusExpired {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "order cannot be submitted for review")
	}
	if o.Status == OrderStatusExpired {
		if err = s.checkPendingLimit(ctx, tx, uid, maxPending); err != nil {
			return nil, err
		}
	}
	snapshot := manualSnapshot(o)
	snapshot["manual_submitted_transaction_no"] = req.TransactionNo
	snapshot["manual_submission_note"] = req.Note
	snapshot["manual_submitted_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	o, err = tx.PaymentOrder.UpdateOneID(oid).SetStatus(OrderStatusPendingReview).SetProviderSnapshot(snapshot).Save(ctx)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(map[string]any{"transaction_no": req.TransactionNo, "note": req.Note, "submitted_at": snapshot["manual_submitted_at"]})
	if _, err = tx.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(oid, 10)).SetAction("MANUAL_SUBMITTED").SetOperator(fmt.Sprintf("user:%d", uid)).SetDetail(string(b)).Save(ctx); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return manualPaymentDetail(o), nil
}

func validateManualApproval(o *dbent.PaymentOrder, req ManualPaymentApproveRequest) error {
	if !req.ConfirmReceived {
		return infraerrors.BadRequest("MANUAL_RECEIPT_CONFIRMATION_REQUIRED", "confirm receipt before approval")
	}
	if req.TransactionNo == "" {
		return infraerrors.BadRequest("MANUAL_TRANSACTION_REQUIRED", "actual transaction number is required")
	}
	if err := validateManualText(req.TransactionNo, req.Note); err != nil {
		return err
	}
	if math.IsNaN(req.ReceivedAmount) || math.IsInf(req.ReceivedAmount, 0) || req.ReceivedAmount <= 0 || !decimal.NewFromFloat(req.ReceivedAmount).Equal(decimal.NewFromFloat(o.PayAmount)) {
		return infraerrors.BadRequest("MANUAL_AMOUNT_MISMATCH", "received amount must exactly match the frozen payment amount")
	}
	return nil
}
func (s *PaymentService) ApproveManualPayment(ctx context.Context, oid, adminID int64, req ManualPaymentApproveRequest) (*ManualPaymentDetail, error) {
	req.TransactionNo = strings.TrimSpace(req.TransactionNo)
	req.Note = strings.TrimSpace(req.Note)
	// Validate mandatory confirmation before opening a transaction.
	if !req.ConfirmReceived {
		return nil, infraerrors.BadRequest("MANUAL_RECEIPT_CONFIRMATION_REQUIRED", "confirm receipt before approval")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	o, err := manualLockedOrder(ctx, tx, oid)
	if err != nil {
		return nil, err
	}
	if err = manualOrderError(o); err != nil {
		return nil, err
	}
	if err = validateManualApproval(o, req); err != nil {
		return nil, err
	}
	if o.Status != OrderStatusPendingReview {
		if !manualApproved(o) || o.PaymentTradeNo != req.TransactionNo || (o.Status != OrderStatusPaid && o.Status != OrderStatusRecharging && o.Status != OrderStatusFailed && o.Status != OrderStatusCompleted) {
			return nil, infraerrors.Conflict("INVALID_STATUS", "only pending review orders can be approved")
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
	} else {
		if err = manualLock(ctx, tx.Client(), "manual-payment-reference:"+o.PaymentType+":"+req.TransactionNo); err != nil {
			return nil, err
		}
		duplicate, queryErr := tx.PaymentOrder.Query().Where(paymentorder.IDNEQ(oid), paymentorder.PaymentTypeEQ(o.PaymentType), paymentorder.PaymentTradeNoEQ(req.TransactionNo), paymentorder.PaidAtNotNil()).All(ctx)
		if queryErr != nil {
			return nil, queryErr
		}
		for _, other := range duplicate {
			if IsManualPaymentOrder(other) {
				return nil, infraerrors.Conflict("MANUAL_TRANSACTION_REUSED", "transaction number has already been approved for another manual order")
			}
		}
		now := time.Now().UTC()
		snapshot := manualSnapshot(o)
		snapshot["manual_verified_transaction_no"] = req.TransactionNo
		snapshot["manual_received_amount"] = req.ReceivedAmount
		snapshot["manual_review_note"] = req.Note
		snapshot["manual_reviewed_at"] = now.Format(time.RFC3339Nano)
		snapshot["manual_reviewed_by"] = adminID
		o, err = tx.PaymentOrder.UpdateOneID(oid).SetStatus(OrderStatusPaid).SetPaidAt(now).SetPaymentTradeNo(req.TransactionNo).SetProviderSnapshot(snapshot).Save(ctx)
		if err != nil {
			return nil, err
		}
		if err = manualAudit(ctx, tx, oid, "MANUAL_APPROVED", adminID, map[string]any{"transaction_no": req.TransactionNo, "received_amount": req.ReceivedAmount, "credited_amount": o.Amount, "note": req.Note, "reviewed_at": snapshot["manual_reviewed_at"]}); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
	}
	if o.Status != OrderStatusCompleted {
		if err = s.executeFulfillment(ctx, oid); err != nil {
			// Another approval retry may already hold the existing fulfillment lease.
			// Return persisted progress for the identical approved receipt.
			if infraerrors.Reason(err) == "CONFLICT" {
				current, getErr := s.entClient.PaymentOrder.Get(ctx, oid)
				if getErr == nil && manualApproved(current) && current.PaymentTradeNo == req.TransactionNo && (current.Status == OrderStatusRecharging || current.Status == OrderStatusCompleted) {
					return manualPaymentDetail(current), nil
				}
			}
			return nil, err
		}
	}
	return s.AdminGetManualPayment(ctx, oid)
}
func (s *PaymentService) RejectManualPayment(ctx context.Context, oid, adminID int64, reason string) (*ManualPaymentDetail, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, infraerrors.BadRequest("MANUAL_REJECTION_REASON_REQUIRED", "rejection reason is required")
	}
	if err := validateManualText("", reason); err != nil {
		return nil, err
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	o, err := manualLockedOrder(ctx, tx, oid)
	if err != nil {
		return nil, err
	}
	if err = manualOrderError(o); err != nil {
		return nil, err
	}
	if o.Status != OrderStatusPendingReview {
		return nil, infraerrors.Conflict("INVALID_STATUS", "only pending review orders can be rejected")
	}
	now := time.Now().UTC()
	snapshot := manualSnapshot(o)
	snapshot["manual_review_note"] = reason
	snapshot["manual_reviewed_at"] = now.Format(time.RFC3339Nano)
	snapshot["manual_reviewed_by"] = adminID
	o, err = tx.PaymentOrder.UpdateOneID(oid).SetStatus(OrderStatusRejected).SetProviderSnapshot(snapshot).Save(ctx)
	if err != nil {
		return nil, err
	}
	if err = manualAudit(ctx, tx, oid, "MANUAL_REJECTED", adminID, map[string]any{"reason": reason, "reviewed_at": snapshot["manual_reviewed_at"]}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return manualPaymentDetail(o), nil
}
