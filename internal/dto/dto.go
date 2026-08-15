package dto

// ManualCreateOrderRequest 手工建单
type ManualCreateOrderRequest struct {
	BuyerName     string           `json:"buyerName"`
	BuyerPhone    string           `json:"buyerPhone"`
	BuyerNick     string           `json:"buyerNick"`
	BuyerTel      string           `json:"buyerTel"`
	TotalAmount   float64          `json:"totalAmount"`
	PayAmount     float64          `json:"payAmount"`
	FreightAmount float64          `json:"freightAmount"`
	Remark        string           `json:"remark"`
	SellerRemark  string           `json:"sellerRemark"`
	ShipContent   string           `json:"shipContent"` // 发货内容（同步快递助手）
	SellerFlag    *int             `json:"sellerFlag"`
	Address       *AddressInput    `json:"address"`
	Items         []OrderItemInput `json:"items"`
	// SaveCustomer 保存收件人到客户中心
	SaveCustomer bool `json:"saveCustomer"`
	// SyncKDZS 同步创建快递助手手工单（默认 true）；账号取发货中心默认快递助手账号
	SyncKDZS *bool `json:"syncKdzs"`
	// PlatformOrderNo 可选外部订单编号
	PlatformOrderNo string `json:"platformOrderNo"`
	// ManualSourceID 手工订单来源（仅手工单；来自「手工订单来源」管理）
	ManualSourceID uint64 `json:"manualSourceId"`
	// CreateAction 对齐快递助手建单动作：
	// create_only=仅创建(待推单) | create_and_push=创建并推送(默认自营) | create_and_print=创建并打印
	CreateAction string `json:"createAction"`
	// PrintMode 仅 create_and_print：kdzs=快递助手打印 | carrier=自建物流账号打印（默认 carrier）
	PrintMode string `json:"printMode"`
}

// ManualBatchCreateRequest 批量手工建单（共享商品/备注，多个收件人）
type ManualBatchCreateRequest struct {
	Receivers      []ManualReceiverInput `json:"receivers"`
	Items          []OrderItemInput      `json:"items"`
	Remark         string                `json:"remark"`
	ShipContent    string                `json:"shipContent"`
	SellerFlag     *int                  `json:"sellerFlag"`
	SaveCustomer   bool                  `json:"saveCustomer"`
	SyncKDZS       *bool                 `json:"syncKdzs"`
	ManualSourceID uint64                `json:"manualSourceId"`
	CreateAction   string                `json:"createAction"`
	PrintMode      string                `json:"printMode"`
}

type ManualReceiverInput struct {
	BuyerName  string        `json:"buyerName"`
	BuyerPhone string        `json:"buyerPhone"`
	BuyerTel   string        `json:"buyerTel"`
	Address    *AddressInput `json:"address"`
}

type AddressInput struct {
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Province string `json:"province"`
	City     string `json:"city"`
	District string `json:"district"`
	Address  string `json:"address"`
	FullText string `json:"fullText"`
}

type OrderItemInput struct {
	SkuID          uint64  `json:"skuId"`
	SkuCode        string  `json:"skuCode"`
	PlatformSkuID  string  `json:"platformSkuId"`
	PlatformItemID string  `json:"platformItemId"`
	ProductName    string  `json:"productName"`
	SkuSpecs       string  `json:"skuSpecs"`
	PicURL         string  `json:"picUrl"`
	Quantity       int     `json:"quantity"`
	Price          float64 `json:"price"`
}

// IngestOrderRequest 外部模块推送/同步入库
type IngestOrderRequest struct {
	SourceChannel       string           `json:"sourceChannel" binding:"required"`
	Platform            string           `json:"platform"`
	PlatformOrderID     string           `json:"platformOrderId"`
	PlatformSysTid      string           `json:"platformSysTid"`
	ShopID              string           `json:"shopId"`
	ShopName            string           `json:"shopName"`
	ExternalRefID       string           `json:"externalRefId"`
	Status              string           `json:"status"`
	PlatformStatus      string           `json:"platformStatus"`
	BuyerNick           string           `json:"buyerNick"`
	BuyerName           string           `json:"buyerName"`
	BuyerPhone          string           `json:"buyerPhone"`
	TotalAmount         float64          `json:"totalAmount"`
	PayAmount           float64          `json:"payAmount"`
	FreightAmount       float64          `json:"freightAmount"`
	PayStatus           string           `json:"payStatus"`
	PayTime             string           `json:"payTime"`
	OrderTime           string           `json:"orderTime"`
	PlatformStatusText  string           `json:"platformStatusText"`
	EcommerceStatus     string           `json:"ecommerceStatus"`
	EcommerceStatusText string           `json:"ecommerceStatusText"`
	AfterSaleStatus     string           `json:"afterSaleStatus"`
	AfterSaleStatusText string           `json:"afterSaleStatusText"`
	AgentType           int              `json:"agentType"`
	Remark              string           `json:"remark"`
	SellerRemark        string           `json:"sellerRemark"`
	SellerFlag          *int             `json:"sellerFlag"` // 0灰 1红 2黄 3绿 4蓝 5紫；nil 表示不覆盖
	FenFaRemark         string           `json:"fenFaRemark"`
	PrinterRemark       string           `json:"printerRemark"`
	FactoryID           string           `json:"factoryId"`
	FactoryName         string           `json:"factoryName"`
	ExpressCompany      string           `json:"expressCompany"`
	ExpressCode         string           `json:"expressCode"`
	ExpressNo           string           `json:"expressNo"`
	ShippedAt           string           `json:"shippedAt"` // 平台真实发货时间
	Logistics           []LogisticsInput `json:"logistics"`
	RawPayload          string           `json:"rawPayload"`
	Address             *AddressInput    `json:"address"`
	Items               []OrderItemInput `json:"items"`
}

// LogisticsInput 同步入库的物流包裹
type LogisticsInput struct {
	ExpressCompany string `json:"expressCompany"`
	ExpressCode    string `json:"expressCode"`
	ExpressNo      string `json:"expressNo"`
	ShippedAt      string `json:"shippedAt"`
}

type AllocateRequest struct {
	AllocType       string `json:"allocType" binding:"required"` // self_ship | dropship | purchase_then_ship
	DropshipMode    string `json:"dropshipMode"`                 // 可选；代发时由绑定关系自动推断
	SupplierID      uint64 `json:"supplierId"`
	SupplierName    string `json:"supplierName"`
	FactoryID       string `json:"factoryId"`
	FactoryName     string `json:"factoryName"`
	PurchaseOrderID string `json:"purchaseOrderId"`
	Remark          string `json:"remark"`
}

type BatchDropshipRequest struct {
	OrderIDs     []uint64 `json:"orderIds" binding:"required,min=1"`
	SupplierID   uint64   `json:"supplierId" binding:"required"`
	SupplierName string   `json:"supplierName"`
}

// RelinkPurchaseOrderRequest 代发单合并后，把销售单上的采购单号批量改到目标单。
// toPoNo 为空表示清空关联（删除代发单后解绑）。
type RelinkPurchaseOrderRequest struct {
	FromPoNos []string `json:"fromPoNos" binding:"required,min=1"`
	ToPoNo    string   `json:"toPoNo"`
}

// UnlinkDropshipPORequest 供应链解绑销售单后回写：清空指定订单的采购单号；可选同步清履约分配。
type UnlinkDropshipPORequest struct {
	OrderIDs   []uint64 `json:"orderIds"`
	OrderNos   []string `json:"orderNos"`
	ClearAlloc bool     `json:"clearAlloc"` // true：恢复待分配；快递助手单会尽量先撤推单再标待推单
	Remark     string   `json:"remark"`
}

type ShipRequest struct {
	ExpressCompany string `json:"expressCompany"`
	ExpressNo      string `json:"expressNo"`
	Remark         string `json:"remark"`
	// 是否回传来源平台（电商→StoreSyncAgent 等）
	Callback bool `json:"callback"`
	// Items 本运单发货的销售行；空=剩余全部（无明细订单则整单发货）
	Items []ShipItemInput `json:"items"`
}

type ShipItemInput struct {
	OrderItemID uint64 `json:"orderItemId"`
	Qty         int    `json:"qty"`
}

// UnshipRequest 取消快递单后回退订单发货：按运单号清除商品发货明细并重算发货状态。
type UnshipRequest struct {
	ExpressNo string `json:"expressNo"`
	Remark    string `json:"remark"`
}

// UpdateRemarksRequest 订单详情手工维护备注。
type UpdateRemarksRequest struct {
	SellerRemark  string `json:"sellerRemark"`
	SellerFlag    *int   `json:"sellerFlag"` // 0灰 1红 2黄 3绿 4蓝 5紫；nil 不改旗帜
	FenFaRemark   string `json:"fenFaRemark"`
	PrinterRemark string `json:"printerRemark"`
	AllocRemark   string `json:"allocRemark"`
}

// UpdatePaymentRequest 自营中心回写付款状态（仅手工单生效）。
type UpdatePaymentRequest struct {
	PayStatus    string `json:"payStatus"`              // unpaid|partial|paid
	PayTime      string `json:"payTime"`                // 空字符串表示清空
	ClearPayTime bool   `json:"clearPayTime,omitempty"` // true 时清空 pay_time
}

type BindingRequest struct {
	SupplierID          uint64 `json:"supplierId" binding:"required"`
	SupplierCode        string `json:"supplierCode"`
	SupplierName        string `json:"supplierName" binding:"required"`
	SourceChannel       string `json:"sourceChannel"`
	ExternalFactoryID   string `json:"externalFactoryId" binding:"required"`
	ExternalFactoryName string `json:"externalFactoryName"`
	Platform            string `json:"platform"`
	Remark              string `json:"remark"`
}

type AllocSettingsRequest struct {
	Enabled  bool   `json:"enabled"`
	Strategy string `json:"strategy"`
}

type SkuSupplierRuleRequest struct {
	SkuCode      string `json:"skuCode" binding:"required"`
	SupplierID   uint64 `json:"supplierId" binding:"required"`
	SupplierCode string `json:"supplierCode"`
	SupplierName string `json:"supplierName" binding:"required"`
	Priority     int    `json:"priority"`
	Status       *int8  `json:"status"`
	Remark       string `json:"remark"`
}

type SyncKDZSRequest struct {
	Platform      string   `json:"platform"`
	ShopID        string   `json:"shopId"`
	TradeStatus   string   `json:"tradeStatus"`
	TradeStatuses []string `json:"tradeStatuses"`
	PageNo        int      `json:"pageNo"`
	PageSize      int      `json:"pageSize"`
	StartTime     string   `json:"startTime"`
	EndTime       string   `json:"endTime"`
	Tid           string   `json:"tid"` // 平台单号精确拉取（补单）
}

type SyncStoreRequest struct {
	Status string `json:"status"`
	Page   int    `json:"page"`
	Size   int    `json:"pageSize"`
}

// DecryptOrdersRequest 解密电商订单收件信息（经 StoreSyncAgent）
type DecryptOrdersRequest struct {
	OrderIDs []uint64 `json:"orderIds" binding:"required,min=1"`
}
