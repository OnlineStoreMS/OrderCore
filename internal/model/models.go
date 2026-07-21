package model

import "time"

// 订单类型（source_channel）：业务来源渠道
const (
	SourceKDZS   = "kdzs"    // 电商（StoreSyncAgent / 快递助手）
	SourceWXMall = "wx_mall" // 小程序（私域商城 MallCore）
	SourceStore  = "store"   // 门店（StoreCore）
	SourceXianyu = "xianyu"  // 闲鱼（后续）
	SourceManual = "manual"  // 手工订单
)

// KnownSourceChannels 工作台/筛选项展示的全部订单类型（含尚未接入的）
var KnownSourceChannels = []string{
	SourceKDZS,
	SourceWXMall,
	SourceStore,
	SourceXianyu,
	SourceManual,
}

// 履约状态（分配与履约进度；发货见 ShipStatus）
const (
	StatusPendingPayment = "pending_payment"
	StatusPendingAlloc   = "pending_alloc" // 待分配
	StatusAllocated      = "allocated"
	StatusPurchasing     = "purchasing"
	StatusCompleted      = "completed"
	StatusClosed         = "closed"

	// 以下为历史履约值，仅用于回填/读旧日志，不再写入
	StatusPendingShip = "pending_ship"
	StatusShipped     = "shipped"
	StatusPartialShip = "partial_ship"
)

// 发货状态
const (
	ShipWaitShip = "wait_ship" // 待发货
	ShipShipped  = "shipped"   // 已发货
)

// 分配类型
const (
	AllocSelfShip         = "self_ship"          // 自营发货
	AllocDropship         = "dropship"           // 代发发货
	AllocPurchaseThenShip = "purchase_then_ship" // 采购发货
)

// 代发子类型
const (
	DropshipKDZSFactory  = "kdzs_factory"  // 快递助手厂家代发（厂家侧发货，订单中心锁定填单号）
	DropshipOSMSSupplier = "osms_supplier" // OSMS 供应商代发（快递助手侧改自营，线下沟通后可填单号）
)

// 快递助手 agentType
const (
	AgentTypeSelf    = 1 // 自打单/自营
	AgentTypeFactory = 2 // 推厂家代发
)

// 快递助手交易状态
const (
	KDZSWaitAudit = "wait_audit" // 待推单
	KDZSWaitSend  = "wait_send"  // 待发货
)

// 物流回传状态
const (
	CallbackPending   = "pending"
	CallbackPushed    = "pushed"
	CallbackSucceeded = "succeeded"
	CallbackFailed    = "failed"
	CallbackSkipped   = "skipped"
)

// Order 统一销售订单
type Order struct {
	ID               uint64     `gorm:"primaryKey" json:"id"`
	TenantID         uint64     `gorm:"index;not null" json:"tenantId"`
	OrderNo          string     `gorm:"size:64;not null" json:"orderNo"`
	SourceChannel    string     `gorm:"size:32;not null;index" json:"sourceChannel"`
	Platform         string     `gorm:"size:32" json:"platform"` // 电商平台码：FXG/TB/...
	PlatformOrderID  string     `gorm:"size:128;index" json:"platformOrderId"`
	PlatformSysTid   string     `gorm:"size:128" json:"platformSysTid"`
	ShopID           string     `gorm:"size:64" json:"shopId"`
	ShopName         string     `gorm:"size:128" json:"shopName"`
	ExternalRefID    string     `gorm:"size:64;index" json:"externalRefId"` // StoreCore 销售单 ID 等
	Status           string     `gorm:"size:32;not null;index" json:"status"`         // 履约状态
	ShipStatus       string     `gorm:"size:32;not null;index;default:wait_ship" json:"shipStatus"` // 发货状态
	AllocType        string     `gorm:"size:32;index" json:"allocType"`
	DropshipMode     string     `gorm:"size:32" json:"dropshipMode"`
	SupplierID       uint64     `gorm:"index" json:"supplierId"`
	SupplierName     string     `gorm:"size:256" json:"supplierName"`
	FactoryID        string     `gorm:"size:64" json:"factoryId"`
	FactoryName      string     `gorm:"size:256" json:"factoryName"`
	PurchaseOrderID  string     `gorm:"size:64" json:"purchaseOrderId"`
	BuyerNick        string     `gorm:"size:128" json:"buyerNick"`
	BuyerName        string     `gorm:"size:128" json:"buyerName"`
	BuyerPhone       string     `gorm:"size:32" json:"buyerPhone"`
	TotalAmount      float64    `gorm:"type:decimal(12,2)" json:"totalAmount"`
	PayAmount        float64    `gorm:"type:decimal(12,2)" json:"payAmount"`
	FreightAmount    float64    `gorm:"type:decimal(12,2)" json:"freightAmount"`
	PayStatus        string     `gorm:"size:32" json:"payStatus"`
	PayTime          *time.Time `json:"payTime,omitempty"`
	OrderedAt        *time.Time `json:"orderedAt,omitempty"`
	PlatformStatus     string     `gorm:"size:64" json:"platformStatus"`         // 快递助手状态码 wait_audit/wait_send
	PlatformStatusText string     `gorm:"size:64" json:"platformStatusText"`     // 快递助手状态文案
	EcommerceStatus     string    `gorm:"size:64;index" json:"ecommerceStatus"`     // 电商平台订单状态码
	EcommerceStatusText string    `gorm:"size:64" json:"ecommerceStatusText"`       // 电商平台订单状态文案
	AfterSaleStatus     string    `gorm:"size:64" json:"afterSaleStatus"`           // 售后状态码
	AfterSaleStatusText string    `gorm:"size:64" json:"afterSaleStatusText"`       // 售后状态文案
	AgentType          int        `gorm:"default:0" json:"agentType"`            // 1自营 2厂家代发
	ShipEntryLocked    bool       `gorm:"default:false" json:"shipEntryLocked"`  // 锁定填单号发货入口
	ShipLockReason     string     `gorm:"size:256" json:"shipLockReason"`        // 锁定原因说明
	SkipAutoAlloc      bool       `gorm:"default:false" json:"skipAutoAlloc"`    // 撤回分配后跳过自营自动分配
	Remark             string     `gorm:"size:512" json:"remark"`
	SellerRemark       string     `gorm:"size:512" json:"sellerRemark"`
	AllocRemark        string     `gorm:"size:512" json:"allocRemark"`
	AllocatedAt        *time.Time `json:"allocatedAt,omitempty"`
	ShippedAt          *time.Time `json:"shippedAt,omitempty"`
	RawPayload         string     `gorm:"type:text" json:"rawPayload,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`

	Items      []OrderItem       `gorm:"foreignKey:OrderID" json:"items,omitempty"`
	Address    *OrderAddress     `gorm:"foreignKey:OrderID" json:"address,omitempty"`
	Shipments  []OrderShipment   `gorm:"foreignKey:OrderID" json:"shipments,omitempty"`
	StatusLogs []OrderStatusLog  `gorm:"foreignKey:OrderID" json:"statusLogs,omitempty"`
}

func (Order) TableName() string { return "orders" }

type OrderItem struct {
	ID              uint64  `gorm:"primaryKey" json:"id"`
	TenantID        uint64  `gorm:"index;not null" json:"tenantId"`
	OrderID         uint64  `gorm:"index;not null" json:"orderId"`
	LineNo          int     `gorm:"default:1" json:"lineNo"`
	SkuID           uint64  `gorm:"index" json:"skuId"`
	SkuCode         string  `gorm:"size:64" json:"skuCode"`
	PlatformSkuID   string  `gorm:"size:128" json:"platformSkuId"`
	PlatformItemID  string  `gorm:"size:128" json:"platformItemId"`
	ProductName     string  `gorm:"size:512" json:"productName"`
	SkuSpecs        string  `gorm:"size:256" json:"skuSpecs"`
	PicURL          string  `gorm:"size:512" json:"picUrl"`
	Quantity        int     `gorm:"not null" json:"quantity"`
	Price           float64 `gorm:"type:decimal(12,2)" json:"price"`
	TotalAmount     float64 `gorm:"type:decimal(12,2)" json:"totalAmount"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func (OrderItem) TableName() string { return "order_items" }

type OrderAddress struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	TenantID  uint64    `gorm:"index;not null" json:"tenantId"`
	OrderID   uint64    `gorm:"uniqueIndex;not null" json:"orderId"`
	Name      string    `gorm:"size:128" json:"name"`
	Phone     string    `gorm:"size:32" json:"phone"`
	Province  string    `gorm:"size:64" json:"province"`
	City      string    `gorm:"size:64" json:"city"`
	District  string    `gorm:"size:64" json:"district"`
	Address   string    `gorm:"size:512" json:"address"`
	FullText  string    `gorm:"size:1024" json:"fullText"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (OrderAddress) TableName() string { return "order_addresses" }

type OrderStatusLog struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	TenantID   uint64    `gorm:"index;not null" json:"tenantId"`
	OrderID    uint64    `gorm:"index;not null" json:"orderId"`
	FromStatus string    `gorm:"size:32" json:"fromStatus"`
	ToStatus   string    `gorm:"size:32;not null" json:"toStatus"`
	Action     string    `gorm:"size:64" json:"action"`
	Remark     string    `gorm:"size:512" json:"remark"`
	OperatorID uint64    `json:"operatorId"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (OrderStatusLog) TableName() string { return "order_status_logs" }

// OrderShipment 发货单
type OrderShipment struct {
	ID               uint64     `gorm:"primaryKey" json:"id"`
	TenantID         uint64     `gorm:"index;not null" json:"tenantId"`
	OrderID          uint64     `gorm:"index;not null" json:"orderId"`
	ShipmentNo       string     `gorm:"size:64;not null" json:"shipmentNo"`
	ExpressCompany   string     `gorm:"size:64" json:"expressCompany"`
	ExpressNo        string     `gorm:"size:128;index" json:"expressNo"`
	NeedTracking     bool       `gorm:"default:true" json:"needTracking"` // 厂家代发可为 false
	CallbackStatus   string     `gorm:"size:32;default:pending" json:"callbackStatus"`
	CallbackMessage  string     `gorm:"size:512" json:"callbackMessage"`
	CallbackAt       *time.Time `json:"callbackAt,omitempty"`
	Remark           string     `gorm:"size:512" json:"remark"`
	ShippedAt        *time.Time `json:"shippedAt,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

func (OrderShipment) TableName() string { return "order_shipments" }

// SupplierSourceBinding OSMS 供应商 ↔ 来源平台厂家绑定
type SupplierSourceBinding struct {
	ID                 uint64    `gorm:"primaryKey" json:"id"`
	TenantID           uint64    `gorm:"index;not null" json:"tenantId"`
	SupplierID         uint64    `gorm:"index;not null" json:"supplierId"`
	SupplierCode       string    `gorm:"size:64" json:"supplierCode"`
	SupplierName       string    `gorm:"size:256;not null" json:"supplierName"`
	SourceChannel      string    `gorm:"size:32;not null;index" json:"sourceChannel"` // kdzs
	ExternalFactoryID  string    `gorm:"size:64;not null" json:"externalFactoryId"`
	ExternalFactoryName string   `gorm:"size:256" json:"externalFactoryName"`
	Platform           string    `gorm:"size:32" json:"platform"`
	Remark             string    `gorm:"size:512" json:"remark"`
	Status             int8      `gorm:"default:1" json:"status"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

func (SupplierSourceBinding) TableName() string { return "supplier_source_bindings" }
