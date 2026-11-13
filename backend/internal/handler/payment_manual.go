package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *PaymentHandler) GetManualPayment(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	oid, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || oid <= 0 {
		response.BadRequest(c, "Invalid order ID")
		return
	}
	result, err := h.paymentService.GetManualPayment(c.Request.Context(), oid, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
func (h *PaymentHandler) SubmitManualPayment(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	oid, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || oid <= 0 {
		response.BadRequest(c, "Invalid order ID")
		return
	}
	var req service.ManualPaymentSubmitRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	result, err := h.paymentService.SubmitManualPayment(c.Request.Context(), oid, subject.UserID, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
