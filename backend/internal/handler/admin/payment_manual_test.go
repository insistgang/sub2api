//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestManualOrderListOmitsCollectionImage(t *testing.T) {
	key := "manual"
	img := "data:image/png;base64,private"
	o := &dbent.PaymentOrder{ID: 1, ProviderKey: &key, QrCodeImg: &img, ProviderSnapshot: map[string]any{"privateKey": "secret"}}
	b, err := json.Marshal(sanitizeAdminPaymentOrdersForResponse([]*dbent.PaymentOrder{o}))
	require.NoError(t, err)
	require.Contains(t, string(b), `"is_manual":true`)
	require.NotContains(t, string(b), "private")
	require.NotContains(t, string(b), "snapshot")
}

func TestManualAdminActionsRejectOrdinaryUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &PaymentHandler{}
	for _, action := range []gin.HandlerFunc{h.GetManualPayment, h.ApproveManualPayment, h.RejectManualPayment} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/manual", strings.NewReader(`{"confirm_received":true}`))
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 10})
		c.Set(string(middleware.ContextKeyUserRole), "user")
		action(c)
		require.Equal(t, http.StatusForbidden, recorder.Code)
	}
}

func TestManualAdminActionsRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &PaymentHandler{}
	for _, action := range []gin.HandlerFunc{h.GetManualPayment, h.ApproveManualPayment, h.RejectManualPayment} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/manual", strings.NewReader(`{}`))
		action(c)
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
	}
}
