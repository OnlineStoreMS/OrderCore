package dto

// ManualCreateOrderRequest 手工建单
type ManualCreateOrderRequest struct {
	BuyerName     string             `json:"buyerName"`
	BuyerPhone    string             `json:"buyerPhone"`
	BuyerNick     string             `json:"buyerNick"`
	TotalAmount   float64            `json:"totalAmount"`
	PayAmount     float64            `json:"payAmount"`
	FreightAmount float64            `json:"freightAmount"`
	Remark        string             `json:"remark"`
	SellerRemark  string             `json:"sellerRemark"`
	Address       *AddressInput      `json:"address"`
	Items         []OrderItemInput   `json:"items"`
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
	SkuID       uint64  `json:"skuId"`
	SkuCode     string  `json:"skuCode"`
	ProductName string  `json:"productName"`
	SkuSpecs    string  `json:"skuSpecs"`
	PicURL      string  `json:"picUrl"`
	Quantity    int     `json:"quantity"`
	Price       float64 `json:"price"`
}

// IngestOrderRequest 外部模块推送/同步入库
type IngestOrderRequest struct {
	SourceChannel   string           `json:"sourceChannel" binding:"required"`
	Platform        string           `json:"platform"`
	PlatformOrderID string           `json:"platformOrderId"`
	PlatformSysTid  string           `json:"platformSysTid"`
	ShopID          string           `json:"shopId"`
	ShopName        string           `json:"shopName"`
	ExternalRefID   string           `json:"externalRefId"`
	Status          string           `json:"status"`
	PlatformStatus  string           `json:"platformStatus"`
	BuyerNick       string           `json:"buyerNick"`
	BuyerName       string           `json:"buyerName"`
	BuyerPhone      string           `json:"buyerPhone"`
	TotalAmount     float64          `json:"totalAmount"`
	PayAmount       float64          `json:"payAmount"`
	FreightAmount   float64          `json:"freightAmount"`
	PayStatus       string           `json:"payStatus"`
	PayTime         string           `json:"payTime"`
	OrderTime       string           `json:"orderTime"`
	PlatformStatusText string         `json:"platformStatusText"`
	Remark          string           `json:"remark"`
	SellerRemark    string           `json:"sellerRemark"`
	FactoryID       string           `json:"factoryId"`
	FactoryName     string           `json:"factoryName"`
	RawPayload      string           `json:"rawPayload"`
	Address         *AddressInput    `json:"address"`
	Items           []OrderItemInput `json:"items"`
}

type AllocateRequest struct {
	AllocType       string `json:"allocType" binding:"required"` // self_ship | dropship | purchase_then_ship
	DropshipMode    string `json:"dropshipMode"`                // kdzs_factory | osms_supplier
	SupplierID      uint64 `json:"supplierId"`
	SupplierName    string `json:"supplierName"`
	FactoryID       string `json:"factoryId"`
	FactoryName     string `json:"factoryName"`
	PurchaseOrderID string `json:"purchaseOrderId"`
	Remark          string `json:"remark"`
	// 电商厂家代发时是否立即推送快递助手
	PushKDZS bool `json:"pushKdzs"`
}

type ShipRequest struct {
	ExpressCompany string `json:"expressCompany"`
	ExpressNo      string `json:"expressNo"`
	Remark         string `json:"remark"`
	// 是否回传来源平台（电商→StoreSyncAgent 等）
	Callback bool `json:"callback"`
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

type SyncKDZSRequest struct {
	Platform     string   `json:"platform"`
	ShopID       string   `json:"shopId"`
	TradeStatus  string   `json:"tradeStatus"`
	TradeStatuses []string `json:"tradeStatuses"`
	PageNo       int      `json:"pageNo"`
	PageSize     int      `json:"pageSize"`
	StartTime    string   `json:"startTime"`
	EndTime      string   `json:"endTime"`
}

type SyncStoreRequest struct {
	Status string `json:"status"`
	Page   int    `json:"page"`
	Size   int    `json:"pageSize"`
}
