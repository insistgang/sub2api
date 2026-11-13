package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const manualQRMaxBytes = 300 * 1024

// ValidateManualQRCode accepts a safe remote resource or a bounded, decoded raster.
// Remote URLs are never fetched by the server.
func ValidateManualQRCode(raw string) error {
	invalid := func() error {
		return infraerrors.BadRequest("INVALID_MANUAL_QR_CODE", "manual QR code must be an HTTPS image URL or a PNG/JPEG image up to 300 KiB")
	}
	if raw == "" || raw != strings.TrimSpace(raw) {
		return invalid()
	}
	if strings.HasPrefix(raw, "https://") {
		if len(raw) > 4096 {
			return invalid()
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || strings.ContainsAny(raw, "\x00\r\n") {
			return invalid()
		}
		return nil
	}
	format := ""
	encoded := ""
	for mime, f := range map[string]string{"data:image/png;base64,": "png", "data:image/jpeg;base64,": "jpeg"} {
		if strings.HasPrefix(raw, mime) {
			format = f
			encoded = strings.TrimPrefix(raw, mime)
		}
	}
	if format == "" || len(encoded) > base64.StdEncoding.EncodedLen(manualQRMaxBytes) {
		return invalid()
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > manualQRMaxBytes {
		return invalid()
	}
	cfg, actual, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || actual != format || cfg.Width < 16 || cfg.Height < 16 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 16*1024*1024 {
		return invalid()
	}
	if _, actual, err = image.Decode(bytes.NewReader(data)); err != nil || actual != format {
		return invalid()
	}
	return nil
}

func manualQRForType(config map[string]string, method string) string {
	switch method {
	case payment.TypeAlipay:
		return config["alipayQRCode"]
	case payment.TypeWxpay:
		return config["wxpayQRCode"]
	}
	return ""
}

// ValidateManualConfig checks every selected channel before it can be enabled.
func ValidateManualConfig(config map[string]string, supportedTypes string) error {
	if currency := strings.TrimSpace(config["currency"]); currency != "" && currency != "CNY" {
		return infraerrors.BadRequest("INVALID_MANUAL_CURRENCY", "manual payments require CNY")
	}
	for _, method := range strings.Split(supportedTypes, ",") {
		method = strings.TrimSpace(method)
		if method == "" {
			continue
		}
		if method != payment.TypeAlipay && method != payment.TypeWxpay {
			return infraerrors.BadRequest("INVALID_MANUAL_PAYMENT_TYPE", "manual payments support alipay and wxpay only")
		}
		if err := ValidateManualQRCode(manualQRForType(config, method)); err != nil {
			return err
		}
	}
	for _, key := range []string{"alipayQRCode", "wxpayQRCode"} {
		if config[key] != "" {
			if err := ValidateManualQRCode(config[key]); err != nil {
				return err
			}
		}
	}
	if len(config["payeeName"]) > 200 || len(config["instructions"]) > 4000 || strings.ContainsRune(config["payeeName"]+config["instructions"], 0) {
		return infraerrors.BadRequest("INVALID_MANUAL_CONFIG", "manual payee name or instructions are invalid")
	}
	return nil
}

type Manual struct{ config map[string]string }

func NewManual(config map[string]string) (*Manual, error) {
	if err := ValidateManualConfig(config, ""); err != nil {
		return nil, err
	}
	return &Manual{config: config}, nil
}
func (p *Manual) Name() string        { return "Manual QR payment" }
func (p *Manual) ProviderKey() string { return payment.TypeManual }
func (p *Manual) SupportedTypes() []payment.PaymentType {
	var methods []payment.PaymentType
	for _, m := range []string{payment.TypeAlipay, payment.TypeWxpay} {
		if manualQRForType(p.config, m) != "" {
			methods = append(methods, m)
		}
	}
	return methods
}
func (p *Manual) CreatePayment(_ context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	qr := manualQRForType(p.config, req.PaymentType)
	if err := ValidateManualQRCode(qr); err != nil {
		return nil, err
	}
	return &payment.CreatePaymentResponse{ResultType: payment.CreatePaymentResultManualRequired, QRCodeImg: qr, IsManual: true, Currency: "CNY"}, nil
}
func (p *Manual) QueryOrder(context.Context, string) (*payment.QueryOrderResponse, error) {
	return nil, fmt.Errorf("manual payment requires administrator review")
}
func (p *Manual) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	return nil, fmt.Errorf("manual payment has no notifications")
}
func (p *Manual) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	return nil, infraerrors.BadRequest("MANUAL_REFUND_UNSUPPORTED", "manual payments do not support automatic refunds")
}
