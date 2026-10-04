package admin

import (
	"net/http"
	"strconv"
	"strings"

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
	o, created, err := h.orders.Ingest(c.Request.Context(), tenantID, 0, req.IngestOrderRequest, "")
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, gin.H{"order": o, "created": created})
}

func (h *Handlers) InternalFenFaRemarks(c *gin.Context) {
	got := strings.TrimSpace(c.GetHeader("X-Internal-Token"))
	want := strings.TrimSpace(h.internalToken)
	if want == "" || got == "" || got != want {
		response.Fail(c, http.StatusUnauthorized, "invalid internal token")
		return
	}
	var in struct {
		OrderNos []string `json:"orderNos"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, http.StatusBadRequest, "参数无效")
		return
	}
	if len(in.OrderNos) > 2000 {
		response.Fail(c, http.StatusBadRequest, "订单号过多")
		return
	}
	data, err := h.orders.FenFaRemarks(internalTenantID(c), in.OrderNos)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(c, data)
}
