package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func manualTestPNG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	img.Set(0, 0, color.Black)
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}
func TestManualProviderCreatesOnlyManualResult(t *testing.T) {
	p, err := CreateProvider("manual", "1", map[string]string{"alipayQRCode": manualTestPNG(t), "currency": "CNY"})
	require.NoError(t, err)
	result, err := p.CreatePayment(context.Background(), payment.CreatePaymentRequest{PaymentType: "alipay", Amount: "7.00"})
	require.NoError(t, err)
	require.Equal(t, "manual_required", result.ResultType)
	require.True(t, result.IsManual)
	require.NotEmpty(t, result.QRCodeImg)
	require.Empty(t, result.PayURL)
	require.Empty(t, result.QRCode)
	_, err = p.QueryOrder(context.Background(), "x")
	require.Error(t, err)
	_, err = p.VerifyNotification(context.Background(), "", nil)
	require.Error(t, err)
	_, err = p.Refund(context.Background(), payment.RefundRequest{})
	require.Error(t, err)
	_, err = p.CreatePayment(context.Background(), payment.CreatePaymentRequest{PaymentType: "wxpay"})
	require.Error(t, err)
}
func TestManualQRCodeValidation(t *testing.T) {
	for _, v := range []string{"javascript:alert(1)", "http://example.com/a.png", "data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not a png")), "https://user:pass@example.com/a.png", "data:image/png;base64," + strings.Repeat("A", 500000)} {
		t.Run(v[:min(35, len(v))], func(t *testing.T) { require.Error(t, ValidateManualQRCode(v)) })
	}
	require.NoError(t, ValidateManualQRCode(manualTestPNG(t)))
	require.NoError(t, ValidateManualQRCode("https://example.com/a.png"))
	require.Error(t, ValidateManualConfig(map[string]string{"alipayQRCode": manualTestPNG(t)}, "alipay,wxpay"))
	require.Error(t, ValidateManualConfig(map[string]string{"currency": "USD", "alipayQRCode": manualTestPNG(t)}, "alipay"))
}

func TestManualQRCodeRasterBoundsAndMIME(t *testing.T) {
	for _, rect := range []image.Rectangle{image.Rect(0, 0, 1, 1), image.Rect(0, 0, 4097, 16)} {
		var b bytes.Buffer
		require.NoError(t, png.Encode(&b, image.NewRGBA(rect)))
		require.Error(t, ValidateManualQRCode("data:image/png;base64,"+base64.StdEncoding.EncodeToString(b.Bytes())))
	}
	var b bytes.Buffer
	require.NoError(t, jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 32, 32)), nil))
	encoded := base64.StdEncoding.EncodeToString(b.Bytes())
	require.NoError(t, ValidateManualQRCode("data:image/jpeg;base64,"+encoded))
	require.Error(t, ValidateManualQRCode("data:image/png;base64,"+encoded))
}
