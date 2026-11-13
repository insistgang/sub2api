package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func manualAdminID(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Authentication required")
		return 0, false
	}
	role, _ := middleware.GetUserRoleFromContext(c)
	if role != service.RoleAdmin {
		response.Forbidden(c, "Admin access required")
		return 0, false
	}
	return subject.UserID, true
}
func (h *PaymentHandler) GetManualPayment(c *gin.Context) {
	if _, ok := manualAdminID(c); !ok {
		return
	}
	oid, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || oid <= 0 {
		response.BadRequest(c, "Invalid order ID")
		return
	}
	result, err := h.paymentService.AdminGetManualPayment(c.Request.Context(), oid)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
func (h *PaymentHandler) ApproveManualPayment(c *gin.Context) {
	uid, ok := manualAdminID(c)
	if !ok {
		return
	}
	oid, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || oid <= 0 {
		response.BadRequest(c, "Invalid order ID")
		return
	}
	var req service.ManualPaymentApproveRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	result, err := h.paymentService.ApproveManualPayment(c.Request.Context(), oid, uid, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
func (h *PaymentHandler) RejectManualPayment(c *gin.Context) {
	uid, ok := manualAdminID(c)
	if !ok {
		return
	}
	oid, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || oid <= 0 {
		response.BadRequest(c, "Invalid order ID")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err = c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	result, err := h.paymentService.RejectManualPayment(c.Request.Context(), oid, uid, req.Reason)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
