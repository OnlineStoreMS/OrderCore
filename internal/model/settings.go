package model

import "time"

const (
	SyncJobKDZS  = "kdzs_orders"
	SyncJobStore = "store_orders"

	ChannelFeishuWebhook = "feishu_webhook"
	ChannelWecomWebhook  = "wecom_webhook"

	PushEventAllocated = "order_allocated"
	PushEventManual    = "manual_push"

	// AllocStrategyBindDropshipOnly 有 SKU→供应商绑定才自动代发，无绑定保持待分配
	AllocStrategyBindDropshipOnly = "bind_dropship_only"
)

// AllocSettings 租户级自动分配配置（每租户一行）
type AllocSettings struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	TenantID  uint64    `gorm:"uniqueIndex;not null" json:"tenantId"`
	Enabled   bool      `gorm:"default:false" json:"enabled"`
	Strategy  string    `gorm:"size:64;default:bind_dropship_only" json:"strategy"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (AllocSettings) TableName() string { return "alloc_settings" }

// SkuSupplierRule 订单 SKU 编码 → OSMS 供应商（自动代发匹配）
type SkuSupplierRule struct {
	ID           uint64    `gorm:"primaryKey" json:"id"`
	TenantID     uint64    `gorm:"index;not null" json:"tenantId"`
	SkuCode      string    `gorm:"size:128;not null;index" json:"skuCode"`
	SupplierID   uint64    `gorm:"index;not null" json:"supplierId"`
	SupplierCode string    `gorm:"size:64" json:"supplierCode"`
	SupplierName string    `gorm:"size:256;not null" json:"supplierName"`
	Priority     int       `gorm:"default:100" json:"priority"`
	Status       int8      `gorm:"default:1" json:"status"`
	Remark       string    `gorm:"size:512" json:"remark"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func (SkuSupplierRule) TableName() string { return "sku_supplier_rules" }

// SyncJob 定时同步任务配置
type SyncJob struct {
	ID              uint64     `gorm:"primaryKey" json:"id"`
	TenantID        uint64     `gorm:"uniqueIndex:uk_sync_job_tenant_type;not null" json:"tenantId"`
	JobType         string     `gorm:"size:32;uniqueIndex:uk_sync_job_tenant_type;not null" json:"jobType"`
	Name            string     `gorm:"size:128" json:"name"`
	Enabled         bool       `gorm:"default:false" json:"enabled"`
	IntervalMinutes int        `gorm:"default:15" json:"intervalMinutes"`
	ParamsJSON      string     `gorm:"type:text" json:"paramsJson"`
	LastRunAt       *time.Time `json:"lastRunAt,omitempty"`
	LastRunOK       bool       `json:"lastRunOk"`
	LastError       string     `gorm:"type:text" json:"lastError"`
	LastStatsJSON   string     `gorm:"type:text" json:"lastStatsJson"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

func (SyncJob) TableName() string { return "sync_jobs" }

// NotificationChannel 推送渠道（飞书/企微机器人等）
type NotificationChannel struct {
	ID          uint64    `gorm:"primaryKey" json:"id"`
	TenantID    uint64    `gorm:"index;not null" json:"tenantId"`
	Name        string    `gorm:"size:128;not null" json:"name"`
	ChannelType string    `gorm:"size:32;not null" json:"channelType"`
	WebhookURL  string    `gorm:"type:text" json:"webhookUrl"`
	Secret      string    `gorm:"size:256" json:"secret"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	Remark      string    `gorm:"size:512" json:"remark"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (NotificationChannel) TableName() string { return "notification_channels" }

// PushRule 供应商推送规则
type PushRule struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	TenantID   uint64    `gorm:"index;not null" json:"tenantId"`
	SupplierID uint64    `gorm:"index;not null" json:"supplierId"` // 0 = 租户默认
	Event      string    `gorm:"size:64;not null" json:"event"`
	ChannelID  uint64    `gorm:"index;not null" json:"channelId"`
	Enabled    bool      `gorm:"default:true" json:"enabled"`
	Remark     string    `gorm:"size:512" json:"remark"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (PushRule) TableName() string { return "push_rules" }

// PushLog 推送记录
type PushLog struct {
	ID           uint64     `gorm:"primaryKey" json:"id"`
	TenantID     uint64     `gorm:"index;not null" json:"tenantId"`
	OrderID      uint64     `gorm:"index;not null" json:"orderId"`
	SupplierID   uint64     `gorm:"index" json:"supplierId"`
	ChannelID    uint64     `gorm:"index" json:"channelId"`
	Event        string     `gorm:"size:64" json:"event"`
	ChannelType  string     `gorm:"size:32" json:"channelType"`
	Status       string     `gorm:"size:32" json:"status"` // succeeded | failed
	ErrorMessage string     `gorm:"type:text" json:"errorMessage"`
	PayloadBrief string     `gorm:"size:512" json:"payloadBrief"`
	SentAt       *time.Time `json:"sentAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

func (PushLog) TableName() string { return "push_logs" }
