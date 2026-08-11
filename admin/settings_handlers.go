package admin

import (
	"net/http"
	"strconv"

	"ordercore/internal/model"
	"ordercore/internal/pkg/authcontext"
	"ordercore/internal/pkg/response"
	"ordercore/internal/service"

	"github.com/gin-gonic/gin"
)

type SettingsHandlers struct {
	settings *service.SettingsService
}

func NewSettingsHandlers(settings *service.SettingsService) *SettingsHandlers {
	return &SettingsHandlers{settings: settings}
}

func (h *SettingsHandlers) ListSyncJobs(c *gin.Context) {
	list, err := h.settings.ListSyncJobs(authcontext.TenantID(c))
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(c, list)
}

type updateSyncJobBody struct {
	Enabled         *bool   `json:"enabled"`
	IntervalMinutes *int    `json:"intervalMinutes"`
	ParamsJSON      *string `json:"paramsJson"`
	Name            *string `json:"name"`
}

func (h *SettingsHandlers) UpdateSyncJob(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var body updateSyncJobBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	job, err := h.settings.UpdateSyncJob(authcontext.TenantID(c), id, body.Enabled, body.IntervalMinutes, body.ParamsJSON, body.Name)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, job)
}

func (h *SettingsHandlers) RunSyncJob(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效 ID")
		return
	}
	stats, err := h.settings.RunSyncJob(c.Request.Context(), authcontext.TenantID(c), id, authcontext.BearerToken(c))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, stats)
}

func (h *SettingsHandlers) ListChannels(c *gin.Context) {
	list, err := h.settings.ListChannels(authcontext.TenantID(c))
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(c, list)
}

func (h *SettingsHandlers) CreateChannel(c *gin.Context) {
	var ch model.NotificationChannel
	if err := c.ShouldBindJSON(&ch); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	out, err := h.settings.CreateChannel(authcontext.TenantID(c), &ch)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Created(c, out)
}

func (h *SettingsHandlers) UpdateChannel(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var ch model.NotificationChannel
	if err := c.ShouldBindJSON(&ch); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	out, err := h.settings.UpdateChannel(authcontext.TenantID(c), id, &ch)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, out)
}

func (h *SettingsHandlers) DeleteChannel(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := h.settings.DeleteChannel(authcontext.TenantID(c), id); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *SettingsHandlers) TestChannel(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := h.settings.TestChannel(c.Request.Context(), authcontext.TenantID(c), id); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *SettingsHandlers) ListPushRules(c *gin.Context) {
	list, err := h.settings.ListPushRules(authcontext.TenantID(c))
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(c, list)
}

func (h *SettingsHandlers) CreatePushRule(c *gin.Context) {
	var rule model.PushRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	out, err := h.settings.CreatePushRule(authcontext.TenantID(c), &rule)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Created(c, out)
}

func (h *SettingsHandlers) UpdatePushRule(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var rule model.PushRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	out, err := h.settings.UpdatePushRule(authcontext.TenantID(c), id, &rule)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, out)
}

func (h *SettingsHandlers) DeletePushRule(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := h.settings.DeletePushRule(authcontext.TenantID(c), id); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *SettingsHandlers) PushOrder(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效 ID")
		return
	}
	event := c.DefaultQuery("event", model.PushEventManual)
	if err := h.settings.PushOrder(c.Request.Context(), authcontext.TenantID(c), id, event); err != nil {
		response.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *SettingsHandlers) ListPushLogs(c *gin.Context) {
	orderID, _ := strconv.ParseUint(c.Query("orderId"), 10, 64)
	list, err := h.settings.ListPushLogs(authcontext.TenantID(c), orderID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(c, list)
}
