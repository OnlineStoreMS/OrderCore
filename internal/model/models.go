package model

import "time"

// 订单来源
const (
	SourceKDZS   = "kdzs"    // 快递助手电商（StoreSyncAgent）
	SourceWXMall = "wx_mall" // 微信小程序商城（预留）
	SourceStore  = "store"   // 门店销售（StoreCore）
	SourceManual = "manual"  // 手工订单
)

// 统一订单状态
const (
	StatusPendingPayment = "pending_payment"
	StatusPendingShip    = "pending_ship"
	StatusAllocated      = "allocated"
	StatusPurchasing     = "purchasing"
	StatusShipped        = "shipped"
	StatusPartialShip    = "partial_ship"
	StatusCompleted      = "completed"
	StatusClosed         = "closed"
)

// 分配类型
const (
	AllocSelfShip         = "self_ship"          // 自营发货
	AllocDropship         = "dropship"           // 代发发货
	AllocPurchaseThenShip = "purchase_then_ship" // 采购发货
)

// 代发子类型
const (
	DropshipKDZSFactory  = "kdzs_factory"  // 快递助手厂家代发（推送即可，无需填单号）
	DropshipOSMSSupplier = "osms_supplier" // OSMS 供应商代发（线下沟通，手动填单号）
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
	Status           string     `gorm:"size:32;not null;index" json:"status"`
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
	PlatformStatus   string     `gorm:"size:64" json:"platformStatus"`
	Remark           string     `gorm:"size:512" json:"remark"`
	SellerRemark     string     `gorm:"size:512" json:"sellerRemark"`
	AllocRemark      string     `gorm:"size:512" json:"allocRemark"`
	AllocatedAt      *time.Time `json:"allocatedAt,omitempty"`
	ShippedAt        *time.Time `json:"shippedAt,omitempty"`
	RawPayload       string     `gorm:"type:text" json:"rawPayload,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`

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
