package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"ordercore/internal/dto"
	"ordercore/internal/integration/supplycore"
	"ordercore/internal/pkg/authcontext"
	"ordercore/internal/pkg/response"
	"ordercore/internal/repo"
	"ordercore/internal/service"

	"github.com/gin-gonic/gin"
)

type Handlers struct {
	orders *service.OrderService
	supply *supplycore.Client
}

func NewHandlers(orders *service.OrderService, supply *supplycore.Client) *Handlers {
	return &Handlers{orders: orders, supply: supply}
}

func (h *Handlers) Dashboard(c *gin.Context) {
	data, err := h.orders.Dashboard(authcontext.TenantID(c))
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(c, data)
}

func (h *Handlers) ListOrders(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	list, total, err := h.orders.List(authcontext.TenantID(c), repo.OrderListQuery{
		SourceChannel:  c.Query("sourceChannel"),
		Status:         c.Query("status"),
		AllocType:      c.Query("allocType"),
		Keyword:        c.Query("keyword"),
		Platform:       c.Query("platform"),
		OrderedAtStart: parseQueryTime(c.Query("orderedAtStart")),
		OrderedAtEnd:   parseQueryTime(c.Query("orderedAtEnd")),
		PayTimeStart:   parseQueryTime(c.Query("payTimeStart")),
		PayTimeEnd:     parseQueryTime(c.Query("payTimeEnd")),
		Page:           page,
		PageSize:       pageSize,
	})
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(c, response.PageResult(list, total, page, pageSize))
}

func (h *Handlers) GetOrder(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效 ID")
		return
	}
	o, err := h.orders.Get(authcontext.TenantID(c), id)
	if err != nil {
		response.Fail(c, http.StatusNotFound, "订单不存在")
		return
	}
	response.OK(c, o)
}

func (h *Handlers) CreateManual(c *gin.Context) {
	var req dto.ManualCreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	o, err := h.orders.CreateManual(authcontext.TenantID(c), authcontext.UserID(c), req)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Created(c, o)
}

func (h *Handlers) Ingest(c *gin.Context) {
	var req dto.IngestOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	o, created, err := h.orders.Ingest(authcontext.TenantID(c), authcontext.UserID(c), req)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, gin.H{"order": o, "created": created})
}

func (h *Handlers) Allocate(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var req dto.AllocateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	o, err := h.orders.Allocate(c.Request.Context(), authcontext.TenantID(c), authcontext.UserID(c), id, req, authcontext.BearerToken(c))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, o)
}

func (h *Handlers) Ship(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var req dto.ShipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	o, err := h.orders.Ship(c.Request.Context(), authcontext.TenantID(c), authcontext.UserID(c), id, req, authcontext.BearerToken(c))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, o)
}

func (h *Handlers) SyncKDZS(c *gin.Context) {
	var req dto.SyncKDZSRequest
	_ = c.ShouldBindJSON(&req)
	stats, err := h.orders.SyncFromKDZS(c.Request.Context(), authcontext.TenantID(c), authcontext.UserID(c), req, authcontext.BearerToken(c))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, stats)
}

func (h *Handlers) SyncStore(c *gin.Context) {
	var req dto.SyncStoreRequest
	_ = c.ShouldBindJSON(&req)
	stats, err := h.orders.SyncFromStore(c.Request.Context(), authcontext.TenantID(c), authcontext.UserID(c), req, authcontext.BearerToken(c))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, stats)
}

func (h *Handlers) ListFactories(c *gin.Context) {
	pageNo, _ := strconv.Atoi(c.DefaultQuery("pageNo", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "50"))
	result, err := h.orders.ListKDZSFactories(c.Request.Context(), authcontext.BearerToken(c), c.Query("platform"), pageNo, pageSize)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, result)
}

func (h *Handlers) ListSuppliers(c *gin.Context) {
	if h.supply == nil {
		response.Fail(c, http.StatusBadRequest, "SupplyCore 未配置")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "50"))
	list, total, err := h.supply.ListSuppliers(c.Request.Context(), authcontext.BearerToken(c), c.Query("keyword"), page, pageSize)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, response.PageResult(list, total, page, pageSize))
}

func (h *Handlers) ListBindings(c *gin.Context) {
	list, err := h.orders.ListBindings(authcontext.TenantID(c))
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(c, list)
}

func (h *Handlers) CreateBinding(c *gin.Context) {
	var req dto.BindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	b, err := h.orders.CreateBinding(authcontext.TenantID(c), req)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Created(c, b)
}

func (h *Handlers) UpdateBinding(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var req dto.BindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	b, err := h.orders.UpdateBinding(authcontext.TenantID(c), id, req)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, b)
}

func (h *Handlers) DeleteBinding(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := h.orders.DeleteBinding(authcontext.TenantID(c), id); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func parseID(s string) (uint64, error) {
	return strconv.ParseUint(s, 10, 64)
}

func parseQueryTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return &t
		}
	}
	return nil
}
