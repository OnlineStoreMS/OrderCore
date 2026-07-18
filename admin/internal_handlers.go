package admin

import (
	"net/http"
	"strconv"

	"ordercore/internal/dto"
	"ordercore/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

func internalTenantID(c *gin.Context) uint64 {
	if v := c.GetHeader("X-Tenant-Id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil && id > 0 {
			return id
		}
	}
	if q := c.Query("tenantId"); q != "" {
		if id, err := strconv.ParseUint(q, 10, 64); err == nil && id > 0 {
			return id
		}
	}
	return 1
}

// InternalIngest 供 MallCore 等服务间推送 wx_mall 订单（无 JWT）
func (h *Handlers) InternalIngest(c *gin.Context) {
	var req struct {
		dto.IngestOrderRequest
		TenantID uint64 `json:"tenantId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	tenantID := req.TenantID
	if tenantID == 0 {
		tenantID = internalTenantID(c)
	}
	o, created, err := h.orders.Ingest(tenantID, 0, req.IngestOrderRequest)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, gin.H{"order": o, "created": created})
}
