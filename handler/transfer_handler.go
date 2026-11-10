package handler

import (
	"errors"
	"net/http"

	"wallet-service/models"
	"wallet-service/service"

	"github.com/gin-gonic/gin"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(s *service.TransferService) *TransferHandler {
	return &TransferHandler{service: s}
}

func (h *TransferHandler) CreateTransfer(c *gin.Context) {
	var req models.TransferRequest
	// request validation
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON request"})
		return
	}
    
	// invoking service logic
	result, err := h.service.CreateTransfer(c.Request.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrIdempotencyConflict):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case errors.Is(err, models.ErrIdempotencyPending):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case errors.Is(err, models.ErrInvalidRequest):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	c.JSON(result.HTTPStatus, result.Body)
}
