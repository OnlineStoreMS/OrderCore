package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ordercore/internal/dto"
	"ordercore/internal/integration/customercore"
	"ordercore/internal/integration/productcore"
	"ordercore/internal/integration/selfcore"
	"ordercore/internal/integration/shippingcore"
	"ordercore/internal/integration/storecore"
	"ordercore/internal/integration/storesync"
	"ordercore/internal/integration/supplycore"
	"ordercore/internal/model"
	"ordercore/internal/repo"

	"gorm.io/gorm"
)

type ctxKey int

const deferDropshipPOKey ctxKey = 1

type deferredDropshipBatch struct {
	mu     sync.Mutex
	orders map[uint64]*model.Order
}

func withDeferDropshipPO(ctx context.Context) (context.Context, *deferredDropshipBatch) {
	b := &deferredDropshipBatch{orders: map[uint64]*model.Order{}}
	return context.WithValue(ctx, deferDropshipPOKey, b), b
}

func deferredDropshipFromCtx(ctx context.Context) *deferredDropshipBatch {
	v, _ := ctx.Value(deferDropshipPOKey).(*deferredDropshipBatch)
	return v
}

func (b *deferredDropshipBatch) add(o *model.Order) {
	if b == nil || o == nil || o.ID == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.orders[o.ID] = o
}

func (b *deferredDropshipBatch) take() []*model.Order {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*model.Order, 0, len(b.orders))
	for _, o := range b.orders {
		out = append(out, o)
	}
	b.orders = map[uint64]*model.Order{}
	return out
}

type OrderService struct {
	repos        *repo.Repos
	storeSync    *storesync.Client
	storeCore    *storecore.Client
	supply       *supplycore.Client
	selfCore     *selfcore.Client
	product      *productcore.Client
	customerCore *customercore.Client
	shipping     *shippingcore.Client
	onAllocated  func(tenantID, orderID uint64)
}

func NewOrderService(repos *repo.Repos, storeSync *storesync.Client, storeCore *storecore.Client, supply *supplycore.Client, selfCore *selfcore.Client, product *productcore.Client, customer *customercore.Client, shipping *shippingcore.Client) *OrderService {
	return &OrderService{repos: repos, storeSync: storeSync, storeCore: storeCore, supply: supply, selfCore: selfCore, product: product, customerCore: customer, shipping: shipping}
}

func (s *OrderService) SetOnAllocated(fn func(tenantID, orderID uint64)) {
	s.onAllocated = fn
}

func (s *OrderService) Dashboard(tenantID uint64, start, end time.Time, timeType string) (map[string]any, error) {
	start, end, err := repo.NormalizeDashboardRange(start, end)
	if err != nil {
		return nil, err
	}
	timeType = repo.NormalizeTrendTimeType(timeType)
	cards, err := s.repos.DashboardCards(tenantID, start, end, timeType)
	if err != nil {
		return nil, err
	}
	byStatus, err := s.repos.CountByStatus(tenantID)
	if err != nil {
		return nil, err
	}
	bySource, err := s.repos.CountBySource(tenantID)
	if err != nil {
		return nil, err
	}
	trend, err := s.repos.DailyOrderTrend(tenantID, start, end, timeType)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"cards":    cards,
		"byStatus": byStatus,
		"bySource": bySource,
		"trend":    trend,
		"timeType": timeType,
	}, nil
}

func (s *OrderService) List(tenantID uint64, q repo.OrderListQuery) ([]model.Order, int64, error) {
	return s.repos.ListOrders(tenantID, q)
}

func (s *OrderService) Get(tenantID, id uint64) (*model.Order, error) {
	return s.repos.GetOrder(tenantID, id)
}

func (s *OrderService) CreateManual(ctx context.Context, tenantID, operatorID uint64, req dto.ManualCreateOrderRequest, bearerToken string) (*model.Order, error) {
	// 对齐快递助手：允许无商品建单（仅发货内容 / 收件人）
	normalizeManualAddress(&req)
	if strings.TrimSpace(req.BuyerPhone) == "" && strings.TrimSpace(req.BuyerTel) == "" {
		return nil, fmt.Errorf("收件人手机或固话至少填一项")
	}
	if req.Address == nil || strings.TrimSpace(req.Address.Province) == "" || strings.TrimSpace(req.Address.City) == "" ||
		strings.TrimSpace(req.Address.District) == "" || strings.TrimSpace(req.Address.Address) == "" {
		return nil, fmt.Errorf("请填写完整收件地址（省/市/区/详细地址）")
	}
	if strings.TrimSpace(req.BuyerName) == "" {
		req.BuyerName = req.Address.Name
	}
	if strings.TrimSpace(req.BuyerPhone) == "" {
		req.BuyerPhone = req.Address.Phone
	}

	createAction := normalizeManualCreateAction(req.CreateAction)
	printMode := normalizeManualPrintMode(req.PrintMode, createAction)
	syncKDZS := true
	if req.SyncKDZS != nil {
		syncKDZS = *req.SyncKDZS
	}
	// 自建物流打印：强制不同步快递助手（即使开关打开）
	if createAction == "create_and_print" && printMode == "carrier" {
		syncKDZS = false
	}
	// 快递助手打印：必须同步（用发货中心默认账号）
	if createAction == "create_and_print" && printMode == "kdzs" {
		syncKDZS = true
	}
	// 创建并推送且打开同步：走快递助手自营推单
	handType := "2"
	if syncKDZS && (createAction == "create_and_push" || (createAction == "create_and_print" && printMode == "kdzs")) {
		handType = "1"
	} else if syncKDZS {
		handType = "2"
	}

	if req.SaveCustomer {
		if err := s.saveManualCustomer(tenantID, req); err != nil {
			log.Printf("[CreateManual] save customer failed: %v", err)
			return nil, fmt.Errorf("保存客户失败: %w", err)
		}
	}

	orderNo, err := s.repos.NextOrderNo(tenantID)
	if err != nil {
		return nil, err
	}
	o := &model.Order{
		TenantID:           tenantID,
		OrderNo:            orderNo,
		SourceChannel:      model.SourceManual,
		Platform:           "DFHAND",
		Status:             model.StatusPendingAlloc,
		ShipStatus:         model.ShipWaitShip,
		BuyerName:          req.BuyerName,
		BuyerPhone:         req.BuyerPhone,
		BuyerNick:          req.BuyerNick,
		TotalAmount:        req.TotalAmount,
		PayAmount:          req.PayAmount,
		FreightAmount:      req.FreightAmount,
		PayStatus:          "unpaid",
		Remark:             req.Remark,
		SellerRemark:       req.SellerRemark,
		ShipContent:        strings.TrimSpace(req.ShipContent),
		PlatformStatus:     model.KDZSWaitAudit,
		PlatformStatusText: "待推单",
		AgentType:          model.AgentTypeSelf,
	}
	if req.SellerFlag != nil {
		o.SellerFlag = *req.SellerFlag
	}
	if req.PlatformOrderNo != "" {
		o.PlatformOrderID = strings.TrimSpace(req.PlatformOrderNo)
	}
	now := time.Now()
	// 手工单付款时间由自营中心有付款记录后回写，创建时不填
	o.PayTime = nil
	o.OrderedAt = &now
	for i, it := range req.Items {
		qty := it.Quantity
		if qty <= 0 {
			qty = 1
		}
		amt := it.Price * float64(qty)
		o.Items = append(o.Items, model.OrderItem{
			TenantID:       tenantID,
			LineNo:         i + 1,
			SkuID:          it.SkuID,
			SkuCode:        it.SkuCode,
			PlatformSkuID:  it.PlatformSkuID,
			PlatformItemID: it.PlatformItemID,
			ProductName:    it.ProductName,
			SkuSpecs:       it.SkuSpecs,
			PicURL:         it.PicURL,
			Quantity:       qty,
			Price:          it.Price,
			TotalAmount:    amt,
		})
		if req.TotalAmount == 0 {
			o.TotalAmount += amt
		}
	}
	if o.PayAmount == 0 {
		o.PayAmount = o.TotalAmount
	}
	o.Address = mapAddress(tenantID, 0, req.Address)

	err = s.repos.Transaction(func(tx *repo.Repos) error {
		if err := tx.CreateOrder(o); err != nil {
			return err
		}
		return tx.AddStatusLog(&model.OrderStatusLog{
			TenantID:   tenantID,
			OrderID:    o.ID,
			ToStatus:   o.Status,
			Action:     "create_manual",
			Remark:     fmt.Sprintf("手工建单 action=%s print=%s sync=%v type=%s", createAction, printMode, syncKDZS, handType),
			OperatorID: operatorID,
		})
	})
	if err != nil {
		return nil, err
	}

	if syncKDZS && s.storeSync != nil {
		kdzsAcc, accErr := s.resolveShippingDefaultKdzsAccount(ctx, bearerToken)
		if accErr != nil {
			_ = s.repos.AddStatusLog(&model.OrderStatusLog{
				TenantID: tenantID, OrderID: o.ID, ToStatus: o.Status,
				Action: "sync_kdzs_failed", Remark: "解析发货中心默认账号失败: " + accErr.Error(), OperatorID: operatorID,
			})
			return nil, fmt.Errorf("本地订单已创建(%s)，但获取发货中心默认快递助手账号失败: %w", o.OrderNo, accErr)
		}
		kdzsRes, syncErr := s.syncManualToKDZS(ctx, bearerToken, req, o, handType, kdzsAcc.Code)
		if syncErr != nil {
			log.Printf("[CreateManual] sync kdzs failed order=%s: %v", o.OrderNo, syncErr)
			_ = s.repos.AddStatusLog(&model.OrderStatusLog{
				TenantID:   tenantID,
				OrderID:    o.ID,
				ToStatus:   o.Status,
				Action:     "sync_kdzs_failed",
				Remark:     "同步快递助手失败: " + syncErr.Error(),
				OperatorID: operatorID,
			})
			return nil, fmt.Errorf("本地订单已创建(%s)，但同步快递助手失败: %w", o.OrderNo, syncErr)
		}
		if kdzsRes != nil {
			updates := map[string]any{}
			if kdzsRes.Tid != "" {
				updates["platform_order_id"] = kdzsRes.Tid
			}
			if kdzsRes.SysTid != "" {
				updates["platform_sys_tid"] = kdzsRes.SysTid
			}
			if len(updates) > 0 {
				_ = s.repos.UpdateOrderFields(tenantID, o.ID, updates)
			}
			_ = s.repos.AddStatusLog(&model.OrderStatusLog{
				TenantID:   tenantID,
				OrderID:    o.ID,
				ToStatus:   o.Status,
				Action:     "sync_kdzs",
				Remark:     fmt.Sprintf("已同步快递助手(发货中心默认:%s/%s) action=%s type=%s tid=%s sysTid=%s", kdzsAcc.Code, kdzsRes.AccountName, createAction, handType, kdzsRes.Tid, kdzsRes.SysTid),
				OperatorID: operatorID,
			})
		}
	}

	// 创建并推送 / 创建并打印：默认自营发货（建自营单）
	if createAction == "create_and_push" || createAction == "create_and_print" {
		if syncKDZS {
			// 快递助手已推自营；本地先记待发货，分配时跳过二次 self_print
			_ = s.repos.UpdateOrderFields(tenantID, o.ID, map[string]any{
				"platform_status":      model.KDZSWaitSend,
				"platform_status_text": "待发货",
				"agent_type":           model.AgentTypeSelf,
			})
		}
		allocated, aerr := s.Allocate(ctx, tenantID, operatorID, o.ID, dto.AllocateRequest{
			AllocType: model.AllocSelfShip,
			Remark:    "手工建单" + manualCreateActionLabel(createAction) + "（默认自营）",
		}, bearerToken)
		if aerr != nil {
			log.Printf("[CreateManual] auto self_ship allocate failed order=%s: %v", o.OrderNo, aerr)
			return nil, fmt.Errorf("订单已创建(%s)，但自营分配失败: %w", o.OrderNo, aerr)
		}
		o = allocated
		if createAction == "create_and_print" {
			printRemark := "创建并打印：已分配自营，跳转发货中心（快递助手）继续打单发货"
			if printMode == "carrier" {
				printRemark = "创建并打印：已分配自营，跳转发货中心（自建物流）继续打单发货；未同步快递助手"
			}
			_ = s.repos.AddStatusLog(&model.OrderStatusLog{
				TenantID:   tenantID,
				OrderID:    o.ID,
				ToStatus:   o.Status,
				Action:     "print_handoff",
				Remark:     printRemark,
				OperatorID: operatorID,
			})
		}
	}
	return s.repos.GetOrder(tenantID, o.ID)
}

func (s *OrderService) CreateManualBatch(ctx context.Context, tenantID, operatorID uint64, req dto.ManualBatchCreateRequest, bearerToken string) ([]*model.Order, error) {
	if len(req.Receivers) == 0 {
		return nil, fmt.Errorf("收件人列表不能为空")
	}
	out := make([]*model.Order, 0, len(req.Receivers))
	for _, r := range req.Receivers {
		single := dto.ManualCreateOrderRequest{
			BuyerName:    r.BuyerName,
			BuyerPhone:   r.BuyerPhone,
			BuyerTel:     r.BuyerTel,
			Remark:       req.Remark,
			ShipContent:  req.ShipContent,
			SellerFlag:   req.SellerFlag,
			Address:      r.Address,
			Items:        req.Items,
			SaveCustomer: req.SaveCustomer,
			SyncKDZS:     req.SyncKDZS,
			CreateAction: req.CreateAction,
			PrintMode:    req.PrintMode,
		}
		o, err := s.CreateManual(ctx, tenantID, operatorID, single, bearerToken)
		if err != nil {
			return out, err
		}
		out = append(out, o)
	}
	return out, nil
}

func (s *OrderService) ParseManualAddress(ctx context.Context, bearerToken string, raw string, batch bool) (json.RawMessage, error) {
	if s.storeSync == nil {
		return nil, fmt.Errorf("storesyncagent 未配置")
	}
	return s.storeSync.ParseHandAddress(ctx, bearerToken, storesync.ParseAddressRequest{
		RawAddress: raw,
		Batch:      batch,
	})
}

func (s *OrderService) SearchManualPIMProducts(ctx context.Context, bearerToken, keyword string, page, pageSize int) (json.RawMessage, error) {
	if s.product == nil {
		return nil, fmt.Errorf("productcore 未配置")
	}
	return s.product.SuperSearchRaw(ctx, bearerToken, keyword, page, pageSize)
}

func (s *OrderService) SearchManualShopProducts(ctx context.Context, bearerToken string, q storesync.ShopProductQuery) (*storesync.ShopProductListResult, error) {
	if s.storeSync == nil {
		return nil, fmt.Errorf("storesyncagent 未配置")
	}
	// 手工建单选商品：按规格编码优先，无结果再按规格名称
	kw := strings.TrimSpace(q.SkuOuterID)
	if kw == "" {
		kw = strings.TrimSpace(q.SpuPropertiesName)
	}
	if kw == "" {
		kw = strings.TrimSpace(q.Title)
	}
	if kw != "" {
		byCode := q
		byCode.Title = ""
		byCode.SkuOuterID = kw
		byCode.SpuPropertiesName = ""
		res, err := s.storeSync.ListProducts(ctx, bearerToken, byCode)
		if err != nil {
			return nil, err
		}
		if res != nil && len(res.Items) > 0 {
			return res, nil
		}
		byName := q
		byName.Title = ""
		byName.SkuOuterID = ""
		byName.SpuPropertiesName = kw
		return s.storeSync.ListProducts(ctx, bearerToken, byName)
	}
	return s.storeSync.ListProducts(ctx, bearerToken, q)
}

func (s *OrderService) LookupManualCustomer(tenantID uint64, phone string) (map[string]interface{}, error) {
	if s.customerCore == nil {
		return nil, fmt.Errorf("customercore 未配置")
	}
	out, err := s.customerCore.GetByPhone(tenantID, phone)
	if err != nil {
		// 未找到客户视为空结果，方便前端静默处理
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "不存在") {
			return nil, nil
		}
		return nil, err
	}
	return out, nil
}

func (s *OrderService) ListManualCustomerAddresses(tenantID, customerID uint64) ([]customercore.AddressItem, error) {
	if s.customerCore == nil {
		return nil, fmt.Errorf("customercore 未配置")
	}
	return s.customerCore.ListAddresses(tenantID, customerID)
}

func (s *OrderService) SearchManualRecipients(tenantID uint64, keyword string, page, pageSize int) (*customercore.RecipientSearchResult, error) {
	if s.customerCore == nil {
		return nil, fmt.Errorf("customercore 未配置")
	}
	return s.customerCore.SearchRecipients(tenantID, keyword, page, pageSize)
}

func normalizeManualAddress(req *dto.ManualCreateOrderRequest) {
	if req.Address == nil {
		return
	}
	if req.Address.Name == "" {
		req.Address.Name = req.BuyerName
	}
	if req.Address.Phone == "" {
		req.Address.Phone = req.BuyerPhone
	}
	if req.Address.Address == "" && req.Address.FullText != "" && req.Address.Province == "" {
		// 仅有全文时留给一键填充；此处不强制拆分
		req.Address.Address = req.Address.FullText
	}
}

func (s *OrderService) saveManualCustomer(tenantID uint64, req dto.ManualCreateOrderRequest) error {
	if s.customerCore == nil {
		return fmt.Errorf("customercore 未配置")
	}
	phone := strings.TrimSpace(req.BuyerPhone)
	if phone == "" {
		return fmt.Errorf("保存客户需要手机号")
	}
	in := customercore.UpsertByPhoneInput{
		TenantID:    tenantID,
		Phone:       phone,
		DisplayName: strings.TrimSpace(req.BuyerName),
		Source:      "manual_order",
	}
	if req.Address != nil {
		def := int8(1)
		in.Address = &customercore.AddressInput{
			ContactName: firstNonEmpty(req.Address.Name, req.BuyerName),
			Phone:       firstNonEmpty(req.Address.Phone, phone),
			Province:    req.Address.Province,
			City:        req.Address.City,
			District:    req.Address.District,
			Detail:      firstNonEmpty(req.Address.Address, req.Address.FullText),
			Label:       "收货地址",
			IsDefault:   &def,
		}
	}
	_, err := s.customerCore.UpsertByPhone(in)
	return err
}

func normalizeManualCreateAction(action string) string {
	switch strings.TrimSpace(action) {
	case "create_and_push", "push", "1":
		return "create_and_push"
	case "create_and_print", "print", "3":
		return "create_and_print"
	default:
		return "create_only"
	}
}

func normalizeManualPrintMode(mode, createAction string) string {
	if createAction != "create_and_print" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "carrier", "sf", "self", "local":
		return "carrier"
	default:
		return "kdzs"
	}
}

func manualCreateActionLabel(action string) string {
	switch action {
	case "create_and_push":
		return "创建并推送"
	case "create_and_print":
		return "创建并打印"
	default:
		return "仅创建"
	}
}

func (s *OrderService) resolveShippingDefaultKdzsAccount(ctx context.Context, bearerToken string) (*shippingcore.KdzsAccountDetail, error) {
	if s.shipping == nil || !s.shipping.Enabled() {
		return nil, fmt.Errorf("shippingcore 未配置")
	}
	return s.shipping.DefaultKdzsAccount(ctx, bearerToken)
}

func (s *OrderService) syncManualToKDZS(ctx context.Context, bearerToken string, req dto.ManualCreateOrderRequest, o *model.Order, handType, accountID string) (*storesync.CreateHandOrderResult, error) {
	addr := req.Address
	skus := make([]storesync.HandOrderSku, 0, len(req.Items))
	for _, it := range req.Items {
		qty := it.Quantity
		if qty <= 0 {
			qty = 1
		}
		skus = append(skus, storesync.HandOrderSku{
			ItemID:   it.PlatformItemID,
			ItemName: it.ProductName,
			ItemPic:  it.PicURL,
			SkuID:    it.PlatformSkuID,
			SkuCode:  it.SkuCode,
			SkuName:  it.SkuSpecs,
			SkuPic:   it.PicURL,
			Num:      strconv.Itoa(qty),
			SkuSpec:  it.SkuSpecs,
			PicPath:  it.PicURL,
			OuterID:  it.SkuCode,
		})
	}
	flag := o.SellerFlag
	if req.SellerFlag != nil {
		flag = *req.SellerFlag
	}
	// 对齐快递助手：有商品时建单不写发货内容（打印面单时再按规格填充）；无商品时才写入手填发货内容
	sendInfo := ""
	if len(skus) == 0 {
		sendInfo = strings.TrimSpace(req.ShipContent)
	}
	if strings.TrimSpace(handType) == "" {
		handType = "2"
	}
	return s.storeSync.CreateHandOrder(ctx, bearerToken, storesync.CreateHandOrderRequest{
		Recipient:      firstNonEmpty(req.BuyerName, addr.Name),
		Phone:          firstNonEmpty(req.BuyerPhone, addr.Phone),
		Tel:            req.BuyerTel,
		Province:       addr.Province,
		City:           addr.City,
		County:         addr.District,
		ReceiveAddress: firstNonEmpty(addr.Address, addr.FullText),
		SaveRecipient:  false, // 客户中心由 OrderCore 负责
		SkuList:        skus,
		Remark:         req.Remark,
		SellerFlag:     &flag,
		SendInfo:       sendInfo,
		OrderCode:      firstNonEmpty(req.PlatformOrderNo, o.OrderNo),
		Type:           handType,
		AccountID:      strings.TrimSpace(accountID),
	})
}

func (s *OrderService) Ingest(ctx context.Context, tenantID, operatorID uint64, req dto.IngestOrderRequest, bearerToken string) (*model.Order, bool, error) {
	channel := strings.TrimSpace(req.SourceChannel)
	if channel == "" {
		return nil, false, fmt.Errorf("sourceChannel 必填")
	}
	s.enrichItemsWithProductSKU(ctx, bearerToken, req.Items)

	var existing *model.Order
	var err error
	if req.PlatformOrderID != "" {
		existing, err = s.repos.FindBySourcePlatform(tenantID, channel, req.PlatformOrderID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			existing = nil
		}
	}
	if existing == nil && req.ExternalRefID != "" {
		existing, err = s.repos.FindByExternalRef(tenantID, channel, req.ExternalRefID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			existing = nil
		}
	}

	hint := deriveKDZSIngest(channel, req)
	status := hint.Status
	shipStatus := hint.ShipStatus
	platformStatus := coalesceStr(hint.PlatformStatus, req.PlatformStatus)
	platformStatusText := coalesceStr(hint.PlatformStatusText, req.PlatformStatusText)
	if existing != nil {
		fromStatus := existing.Status
		terminalPre := existing.Status == model.StatusCompleted || existing.Status == model.StatusClosed
		needDetachPO := !terminalPre && (hint.ClearAlloc || status == model.StatusClosed) &&
			(existing.AllocType == model.AllocDropship || strings.TrimSpace(existing.PurchaseOrderID) != "")
		if needDetachPO && strings.TrimSpace(bearerToken) != "" {
			if err := s.cancelLinkedDropshipPOs(ctx, tenantID, existing.ID, existing.PurchaseOrderID, bearerToken); err != nil {
				return nil, false, fmt.Errorf("同步代发单撤回失败: %w", err)
			}
		}
		needCancelSelf := !terminalPre && (hint.ClearAlloc || status == model.StatusClosed) &&
			(existing.AllocType == model.AllocSelfShip || strings.TrimSpace(existing.SelfOrderNo) != "")
		if needCancelSelf && strings.TrimSpace(bearerToken) != "" {
			if err := s.cancelLinkedSelfOrders(ctx, existing.ID, bearerToken); err != nil {
				return nil, false, fmt.Errorf("同步自营单取消失败: %w", err)
			}
		}
		err = s.repos.Transaction(func(tx *repo.Repos) error {
			fields := map[string]any{
				"platform":               req.Platform,
				"platform_sys_tid":       req.PlatformSysTid,
				"shop_id":                req.ShopID,
				"shop_name":              req.ShopName,
				"buyer_nick":             req.BuyerNick,
				"buyer_name":             req.BuyerName,
				"buyer_phone":            req.BuyerPhone,
				"total_amount":           req.TotalAmount,
				"pay_amount":             req.PayAmount,
				"freight_amount":         req.FreightAmount,
				"pay_status":             req.PayStatus,
				"platform_status":        platformStatus,
				"platform_status_text":   platformStatusText,
				"ecommerce_status":       req.EcommerceStatus,
				"ecommerce_status_text":  req.EcommerceStatusText,
				"after_sale_status":      req.AfterSaleStatus,
				"after_sale_status_text": req.AfterSaleStatusText,
				"agent_type":             hint.AgentType,
				"ship_entry_locked":      hint.ShipEntryLocked,
				"ship_lock_reason":       hint.ShipLockReason,
				"remark":                 req.Remark,
				"seller_remark":          req.SellerRemark,
				"fen_fa_remark":          req.FenFaRemark,
				"printer_remark":         req.PrinterRemark,
				"raw_payload":            req.RawPayload,
			}
			if req.SellerFlag != nil {
				fields["seller_flag"] = *req.SellerFlag
			}
			// 平台备注为空时不覆盖本地手工填写
			if strings.TrimSpace(req.SellerRemark) == "" {
				delete(fields, "seller_remark")
			}
			if strings.TrimSpace(req.FenFaRemark) == "" {
				delete(fields, "fen_fa_remark")
			}
			if strings.TrimSpace(req.PrinterRemark) == "" {
				delete(fields, "printer_remark")
			}
			if strings.TrimSpace(req.Remark) == "" {
				delete(fields, "remark")
			}
			// 已解密明文不被同步脱敏覆盖，避免重复解密
			keepPlainReceiver := orderHasPlainReceiver(existing) && ingestReceiverMasked(req)
			if keepPlainReceiver {
				delete(fields, "buyer_name")
				delete(fields, "buyer_phone")
			}
			if t := parseTime(req.PayTime); t != nil {
				fields["pay_time"] = t
			}
			if t := parseTime(req.OrderTime); t != nil {
				fields["ordered_at"] = t
			}

			statusChanged := false
			terminal := existing.Status == model.StatusCompleted || existing.Status == model.StatusClosed
			// 退款成功/交易关闭：同步关闭并清空分配
			if !terminal && status == model.StatusClosed {
				fields["status"] = status
				fields["alloc_type"] = ""
				fields["dropship_mode"] = ""
				fields["supplier_id"] = 0
				fields["supplier_name"] = ""
				fields["factory_id"] = ""
				fields["factory_name"] = ""
				fields["purchase_order_id"] = ""
				fields["self_order_no"] = ""
				fields["alloc_remark"] = ""
				fields["allocated_at"] = nil
				// 关闭且未真实发货：不要继续占「待发货」队列
				if existing.ShipStatus != model.ShipShipped {
					fields["ship_status"] = ""
				}
				statusChanged = fromStatus != status
			} else if !terminal && hint.ApplySyncAlloc {
				// 撤回分配后跳过「规则引擎自营自动分配」；
				// 但快递助手已进入待发货/已发货/完成时，仍以快递助手为准回写（否则会出现助手侧已自营、中心仍待分配）。
				ps := strings.ToLower(strings.TrimSpace(hint.PlatformStatus))
				kdzsDecided := ps == model.KDZSWaitSend || ps == "shipped" || ps == "completed"
				skipSelfAuto := hint.AgentType == model.AgentTypeSelf && existing.SkipAutoAlloc && !kdzsDecided &&
					hint.ShipStatus != model.ShipShipped && hint.Status != model.StatusCompleted
				// OSMS 线下代发会对快递助手走 self_print；同步常残留厂家字段，若按「厂家代发」回写会
				// 重新锁定填单号，导致发货中心回传失败（需再撤分配）。本地已是 OSMS 代发时一律保留。
				osmsRestoreSID, osmsRestoreSName := uint64(0), ""
				preserveOSMSDropship := false
				if existing.AllocType == model.AllocDropship &&
					existing.DropshipMode == model.DropshipOSMSSupplier &&
					existing.SupplierID > 0 {
					preserveOSMSDropship = true
					osmsRestoreSID, osmsRestoreSName = existing.SupplierID, existing.SupplierName
				} else if hint.AllocType == model.AllocSelfShip || hint.AgentType == model.AgentTypeFactory {
					// 自营镜像或厂家噪点：仍可按代发采购单号恢复 OSMS 供应商
					if poNo := strings.TrimSpace(existing.PurchaseOrderID); poNo != "" {
						if sid, sname, ok := s.lookupDropshipPOSupplier(ctx, bearerToken, poNo); ok {
							// 有厂家绑定的供应商不应被「恢复成 OSMS」——那是真厂家代发
							if b, berr := s.repos.FindBindingBySupplier(tenantID, sid, model.SourceKDZS); berr != nil || b.ExternalFactoryID == "" {
								preserveOSMSDropship = true
								osmsRestoreSID, osmsRestoreSName = sid, sname
							}
						}
					}
				}
				// 本地已发货：不再用同步改写履约分配（避免已发 OSMS 单被盖成 kdzs_factory）
				localShipped := existing.ShipStatus == model.ShipShipped
				if localShipped && existing.AllocType != "" {
					preserveOSMSDropship = preserveOSMSDropship ||
						(existing.AllocType == model.AllocDropship && existing.DropshipMode == model.DropshipOSMSSupplier)
					if existing.AllocType == model.AllocDropship && existing.DropshipMode == model.DropshipOSMSSupplier && existing.SupplierID > 0 {
						osmsRestoreSID, osmsRestoreSName = existing.SupplierID, existing.SupplierName
						preserveOSMSDropship = true
					}
				}
				if !skipSelfAuto && !preserveOSMSDropship && !localShipped {
					fields["alloc_type"] = hint.AllocType
					fields["dropship_mode"] = hint.DropshipMode
					fields["factory_id"] = req.FactoryID
					fields["factory_name"] = req.FactoryName
					if hint.AllocType == model.AllocSelfShip {
						fields["supplier_id"] = 0
						fields["supplier_name"] = ""
					} else if hint.AllocType == model.AllocDropship && hint.DropshipMode == model.DropshipKDZSFactory {
						if sid, sname := s.resolveBoundSupplier(tenantID, req.FactoryID, req.FactoryName); sid > 0 {
							fields["supplier_id"] = sid
							fields["supplier_name"] = sname
						}
					}
					fields["status"] = status
					fields["skip_auto_alloc"] = false
					statusChanged = fromStatus != status
					if existing.AllocatedAt == nil {
						now := time.Now()
						fields["allocated_at"] = &now
					}
				} else {
					if !preserveOSMSDropship {
						fields["factory_id"] = req.FactoryID
						fields["factory_name"] = req.FactoryName
					}
					if preserveOSMSDropship {
						fields["alloc_type"] = model.AllocDropship
						fields["dropship_mode"] = model.DropshipOSMSSupplier
						fields["supplier_id"] = osmsRestoreSID
						fields["supplier_name"] = osmsRestoreSName
						fields["agent_type"] = model.AgentTypeSelf
						fields["status"] = status
						fields["skip_auto_alloc"] = false
						// 覆盖顶部从 hint 写入的厂家锁定，保证发货中心可回传
						if localShipped || shipStatus == model.ShipShipped || ps == "shipped" || ps == "completed" {
							fields["ship_entry_locked"] = true
							if ps == "completed" {
								fields["ship_lock_reason"] = "快递助手交易完成"
							} else {
								fields["ship_lock_reason"] = "快递助手已发货"
							}
						} else {
							locked, reason := computeShipLock(channel, platformStatus, model.AgentTypeSelf, model.DropshipOSMSSupplier)
							fields["ship_entry_locked"] = locked
							fields["ship_lock_reason"] = reason
						}
						hint.LogRemark = fmt.Sprintf("同步保留OSMS代发→%s", osmsRestoreSName)
						if status != fromStatus ||
							existing.AllocType != model.AllocDropship ||
							existing.DropshipMode != model.DropshipOSMSSupplier ||
							existing.SupplierID != osmsRestoreSID ||
							existing.AgentType != model.AgentTypeSelf {
							statusChanged = true
						}
						if existing.AllocatedAt == nil {
							now := time.Now()
							fields["allocated_at"] = &now
						}
						log.Printf("[ordercore] preserve OSMS dropship order=%s po=%s supplier=%d %s (kdzsAgent=%d)",
							existing.OrderNo, existing.PurchaseOrderID, osmsRestoreSID, osmsRestoreSName, hint.AgentType)
					} else if localShipped {
						// 已发货且非 OSMS：只跟平台态/锁定说明，不动履约分配
						fields["status"] = status
						if hint.ShipEntryLocked {
							fields["ship_entry_locked"] = true
							fields["ship_lock_reason"] = hint.ShipLockReason
						}
					}
				}
			} else if !terminal && hint.ClearAlloc {
				// 快递助手回到待推单（撤单等）：清空订单中心分配，恢复待分配
				fields["alloc_type"] = ""
				fields["dropship_mode"] = ""
				fields["factory_id"] = ""
				fields["factory_name"] = ""
				fields["supplier_id"] = 0
				fields["supplier_name"] = ""
				fields["purchase_order_id"] = ""
				fields["self_order_no"] = ""
				fields["alloc_remark"] = ""
				fields["allocated_at"] = nil
				fields["skip_auto_alloc"] = false
				fields["agent_type"] = hint.AgentType
				fields["status"] = status
				statusChanged = fromStatus != status
			} else if !terminal && existing.AllocType == "" {
				fields["factory_id"] = req.FactoryID
				fields["factory_name"] = req.FactoryName
				fields["status"] = status
				statusChanged = fromStatus != status
			} else if !terminal {
				// 已有履约分配：保留履约状态，仅刷新厂家/平台镜像字段
				fields["factory_id"] = req.FactoryID
				fields["factory_name"] = req.FactoryName
				// 厂家代发缺供应商时，按绑定关系补全
				if existing.AllocType == model.AllocDropship && existing.DropshipMode == model.DropshipKDZSFactory && existing.SupplierID == 0 {
					fid := strings.TrimSpace(req.FactoryID)
					if fid == "" {
						fid = existing.FactoryID
					}
					fname := strings.TrimSpace(req.FactoryName)
					if fname == "" {
						fname = existing.FactoryName
					}
					if sid, sname := s.resolveBoundSupplier(tenantID, fid, fname); sid > 0 {
						fields["supplier_id"] = sid
						fields["supplier_name"] = sname
					}
				}
			}

			// 发货状态独立更新（关闭单不写入待发货）
			closingNow := status == model.StatusClosed
			if shipStatus != "" && existing.Status != model.StatusClosed && !closingNow {
				fields["ship_status"] = shipStatus
				if shipStatus == model.ShipShipped {
					if t := parseTime(req.ShippedAt); t != nil {
						fields["shipped_at"] = t
					} else if existing.ShippedAt == nil {
						now := time.Now()
						fields["shipped_at"] = &now
					}
				}
			}

			if err := tx.UpdateOrderFields(tenantID, existing.ID, fields); err != nil {
				return err
			}
			items := mapItems(tenantID, existing.ID, req.Items)
			preserveOSMSSKUFields(existing.Items, items)
			if err := tx.ReplaceItems(tenantID, existing.ID, items); err != nil {
				return err
			}
			if req.Address != nil && !keepPlainReceiver {
				addr := mapAddress(tenantID, existing.ID, req.Address)
				if err := tx.UpsertAddress(addr); err != nil {
					return err
				}
			}
			if err := syncIngestLogistics(tx, tenantID, existing.ID, req); err != nil {
				return err
			}
			if statusChanged {
				return tx.AddStatusLog(&model.OrderStatusLog{
					TenantID:   tenantID,
					OrderID:    existing.ID,
					FromStatus: fromStatus,
					ToStatus:   status,
					Action:     "ingest_update",
					Remark:     hint.LogRemark,
					OperatorID: operatorID,
				})
			}
			return nil
		})
		if err != nil {
			return nil, false, err
		}
		o, err := s.repos.GetOrder(tenantID, existing.ID)
		if err != nil {
			return nil, false, err
		}
		hadDropshipAlloc := existing.AllocType == model.AllocDropship && existing.SupplierID > 0
		s.TryAutoAllocateBySKU(ctx, tenantID, operatorID, o, bearerToken)
		o, _ = s.repos.GetOrder(tenantID, existing.ID)
		o = s.clearStalePurchaseOrderRef(ctx, tenantID, o, bearerToken)
		// 本轮新分配：自动建单。
		// 例外：仍待发货且缺代发单的开放订单也补建（含代发单被删除后留下的脏关联已清理的情况）。
		needPO := needsDropshipPO(o) && (!hadDropshipAlloc ||
			(o.ShipStatus == model.ShipWaitShip && (o.Status == model.StatusAllocated || o.Status == model.StatusPendingShip)))
		if needPO {
			s.queueOrCreateDropshipPO(ctx, tenantID, o, bearerToken)
			o, _ = s.repos.GetOrder(tenantID, existing.ID)
		}
		s.autoSyncDropshipLogistics(ctx, o, req, bearerToken)
		// 合单发货：分发备注只保留在第一单，其余清空（快递助手常复制到每单）
		o = s.dedupeMergeShipFenFa(ctx, tenantID, o)
		// 分发备注变更或合单去重后，补写未付款代发采购小计
		if o != nil && strings.TrimSpace(o.PurchaseOrderID) != "" {
			newFen := strings.TrimSpace(req.FenFaRemark)
			oldFen := strings.TrimSpace(existing.FenFaRemark)
			curFen := strings.TrimSpace(o.FenFaRemark)
			if newFen != oldFen || curFen != oldFen || ingestHasLogistics(req) {
				s.syncLinkedPOPurchasePrices(ctx, o, bearerToken)
			}
		}
		return o, false, nil
	}

	if shipStatus == "" {
		shipStatus = model.ShipWaitShip
	}
	var o *model.Order
	for attempt := 0; attempt < 5; attempt++ {
		orderNo, nerr := s.repos.NextOrderNo(tenantID)
		if nerr != nil {
			return nil, false, nerr
		}
		o = &model.Order{
			TenantID:            tenantID,
			OrderNo:             orderNo,
			SourceChannel:       channel,
			Platform:            req.Platform,
			PlatformOrderID:     req.PlatformOrderID,
			PlatformSysTid:      req.PlatformSysTid,
			ShopID:              req.ShopID,
			ShopName:            req.ShopName,
			ExternalRefID:       req.ExternalRefID,
			Status:              status,
			ShipStatus:          shipStatus,
			AllocType:           hint.AllocType,
			DropshipMode:        hint.DropshipMode,
			BuyerNick:           req.BuyerNick,
			BuyerName:           req.BuyerName,
			BuyerPhone:          req.BuyerPhone,
			TotalAmount:         req.TotalAmount,
			PayAmount:           req.PayAmount,
			FreightAmount:       req.FreightAmount,
			PayStatus:           req.PayStatus,
			PlatformStatus:      platformStatus,
			PlatformStatusText:  platformStatusText,
			EcommerceStatus:     req.EcommerceStatus,
			EcommerceStatusText: req.EcommerceStatusText,
			AfterSaleStatus:     req.AfterSaleStatus,
			AfterSaleStatusText: req.AfterSaleStatusText,
			AgentType:           hint.AgentType,
			ShipEntryLocked:     hint.ShipEntryLocked,
			ShipLockReason:      hint.ShipLockReason,
			Remark:              req.Remark,
			SellerRemark:        req.SellerRemark,
			FenFaRemark:         req.FenFaRemark,
			PrinterRemark:       req.PrinterRemark,
			FactoryID:           req.FactoryID,
			FactoryName:         req.FactoryName,
			RawPayload:          req.RawPayload,
		}
		if req.SellerFlag != nil {
			o.SellerFlag = *req.SellerFlag
		}
		if hint.ApplySyncAlloc {
			now := time.Now()
			o.AllocatedAt = &now
			if hint.AllocType == model.AllocDropship && hint.DropshipMode == model.DropshipKDZSFactory {
				if sid, sname := s.resolveBoundSupplier(tenantID, req.FactoryID, req.FactoryName); sid > 0 {
					o.SupplierID = sid
					o.SupplierName = sname
				}
			}
		}
		if shipStatus == model.ShipShipped || status == model.StatusCompleted {
			if t := parseTime(req.ShippedAt); t != nil {
				o.ShippedAt = t
			} else {
				now := time.Now()
				o.ShippedAt = &now
			}
		}
		if t := parseTime(req.PayTime); t != nil {
			o.PayTime = t
		}
		if t := parseTime(req.OrderTime); t != nil {
			o.OrderedAt = t
		}
		o.Items = mapItems(tenantID, 0, req.Items)
		if req.Address != nil {
			o.Address = mapAddress(tenantID, 0, req.Address)
		}
		err = s.repos.Transaction(func(tx *repo.Repos) error {
			if err := tx.CreateOrder(o); err != nil {
				return err
			}
			if err := syncIngestLogistics(tx, tenantID, o.ID, req); err != nil {
				return err
			}
			return tx.AddStatusLog(&model.OrderStatusLog{
				TenantID:   tenantID,
				OrderID:    o.ID,
				ToStatus:   o.Status,
				Action:     "ingest",
				Remark:     hint.LogRemark,
				OperatorID: operatorID,
			})
		})
		if err == nil {
			break
		}
		// 并发发号偶发撞号：换下一个序号重试
		if isUniqueViolation(err) && attempt < 4 {
			log.Printf("[ordercore] ingest order_no conflict %s, retry %d", orderNo, attempt+1)
			continue
		}
		return nil, false, err
	}
	if err != nil {
		return nil, false, err
	}
	out, err := s.repos.GetOrder(tenantID, o.ID)
	if err != nil {
		return nil, false, err
	}
	s.TryAutoAllocateBySKU(ctx, tenantID, operatorID, out, bearerToken)
	out, _ = s.repos.GetOrder(tenantID, o.ID)
	// 新单：本轮已代发分配且无采购单号 → 可自动建单（同步批次内合并）
	if needsDropshipPO(out) {
		s.queueOrCreateDropshipPO(ctx, tenantID, out, bearerToken)
		out, _ = s.repos.GetOrder(tenantID, o.ID)
	}
	s.autoSyncDropshipLogistics(ctx, out, req, bearerToken)
	out = s.dedupeMergeShipFenFa(ctx, tenantID, out)
	return out, true, nil
}

func (s *OrderService) Allocate(ctx context.Context, tenantID, operatorID uint64, orderID uint64, req dto.AllocateRequest, bearerToken string) (*model.Order, error) {
	o, err := s.repos.GetOrder(tenantID, orderID)
	if err != nil {
		return nil, err
	}
	if o.Status == model.StatusCompleted || o.Status == model.StatusClosed {
		return nil, fmt.Errorf("当前状态不可分配")
	}
	if o.ShipStatus == model.ShipShipped {
		return nil, fmt.Errorf("订单已发货，不可再分配")
	}
	if o.SourceChannel == model.SourceKDZS && o.AgentType == model.AgentTypeFactory {
		return nil, fmt.Errorf("快递助手已推厂家代发，无需在订单中心再分配")
	}
	if blocked, reason := ecommerceBlocksFulfillment(o.EcommerceStatus, o.EcommerceStatusText, o.AfterSaleStatus, o.AfterSaleStatusText); blocked {
		return nil, fmt.Errorf("%s", reason)
	}

	allocType := strings.TrimSpace(req.AllocType)
	supplierID := req.SupplierID
	supplierName := req.SupplierName
	factoryID := req.FactoryID
	factoryName := req.FactoryName
	dropshipMode := ""
	agentType := model.AgentTypeSelf
	kdzsAction := "" // self_print | push_factory | ""
	purchaseOrderID := strings.TrimSpace(req.PurchaseOrderID)
	syncKDZS := shouldSyncKDZSAgent(o)

	switch allocType {
	case model.AllocSelfShip, model.AllocPurchaseThenShip:
		dropshipMode = ""
		if syncKDZS {
			kdzsAction = "self_print"
			agentType = model.AgentTypeSelf
		}
	case model.AllocDropship:
		if supplierID == 0 {
			return nil, fmt.Errorf("代发发货请选择 OSMS 供应商")
		}
		if supplierName == "" {
			if b, err := s.repos.FindBindingBySupplier(tenantID, supplierID, model.SourceKDZS); err == nil {
				supplierName = b.SupplierName
			}
		}
		// 有厂家绑定 → 推快递助手厂家；无绑定 → 快递助手改自营，线下给供应商代发
		if b, err := s.repos.FindBindingBySupplier(tenantID, supplierID, model.SourceKDZS); err == nil && b.ExternalFactoryID != "" {
			dropshipMode = model.DropshipKDZSFactory
			factoryID = b.ExternalFactoryID
			factoryName = b.ExternalFactoryName
			supplierName = b.SupplierName
			agentType = model.AgentTypeFactory
			if syncKDZS {
				kdzsAction = "push_factory"
			}
		} else {
			dropshipMode = model.DropshipOSMSSupplier
			factoryID = ""
			factoryName = ""
			agentType = model.AgentTypeSelf
			if syncKDZS {
				kdzsAction = "self_print"
			}
		}
	default:
		return nil, fmt.Errorf("无效的分配类型")
	}

	// 代发：同步批次延后合并建单；手工/接口分配始终建代发单。
	// 「自动建代发单」开关仅约束同步自动分配（queueOrCreateDropshipPO），不阻塞手工改分配。
	// 若请求已带 purchaseOrderId（批量合并代发），则复用该单号不再新建。
	var createdPOID uint64
	var selfOrderNo string
	if allocType == model.AllocDropship {
		if purchaseOrderID != "" {
			// 外部已建合并代发单
		} else if deferredDropshipFromCtx(ctx) != nil {
			// 同步批次：延后到 flush 按供应商合并建单
		} else {
			poNo, poID, created, err := s.ensureDropshipPurchaseOrder(ctx, o, supplierID, supplierName, bearerToken)
			if err != nil {
				return nil, err
			}
			purchaseOrderID = poNo
			if created {
				createdPOID = poID
				if dropshipMode == model.DropshipKDZSFactory {
					if _, serr := s.supply.SubmitPurchaseOrder(ctx, bearerToken, poID); serr != nil {
						_ = s.rollbackDropshipPurchaseOrder(ctx, bearerToken, poID)
						return nil, fmt.Errorf("代发单自动提交失败: %w", serr)
					}
				}
			}
		}
	}
	if allocType == model.AllocSelfShip {
		soNo, err := s.ensureSelfOrder(ctx, o, bearerToken)
		if err != nil {
			return nil, err
		}
		selfOrderNo = soNo
	}

	nextStatus := model.StatusAllocated
	if allocType == model.AllocPurchaseThenShip {
		nextStatus = model.StatusPurchasing
	}
	locked, lockReason := computeShipLock(o.SourceChannel, o.PlatformStatus, agentType, dropshipMode)

	if kdzsAction != "" {
		needKDZS := true
		// 仅「待发货且已是自营」可跳过；待推单必须调 self_print，否则快递助手仍停在待推单
		if kdzsAction == "self_print" && o.AgentType == model.AgentTypeSelf &&
			o.PlatformStatus == model.KDZSWaitSend {
			needKDZS = false
		}
		if kdzsAction == "push_factory" && o.AgentType == model.AgentTypeFactory &&
			o.FactoryID != "" && o.FactoryID == factoryID {
			needKDZS = false
		}
		if needKDZS {
			if err := s.setKDZSAgentType(ctx, o, kdzsAction, factoryID, bearerToken); err != nil {
				if createdPOID > 0 {
					_ = s.rollbackDropshipPurchaseOrder(ctx, bearerToken, createdPOID)
				}
				return nil, fmt.Errorf("同步快递助手失败: %w", err)
			}
		} else {
			kdzsAction = kdzsAction + "(skip)"
		}
	}

	now := time.Now()
	from := o.Status
	err = s.repos.Transaction(func(tx *repo.Repos) error {
		fields := map[string]any{
			"alloc_type":        allocType,
			"dropship_mode":     dropshipMode,
			"supplier_id":       supplierID,
			"supplier_name":     supplierName,
			"factory_id":        factoryID,
			"factory_name":      factoryName,
			"purchase_order_id": purchaseOrderID,
			"self_order_no":     selfOrderNo,
			"alloc_remark":      req.Remark,
			"status":            nextStatus,
			"ship_status":       model.ShipWaitShip,
			"allocated_at":      now,
			"agent_type":        agentType,
			"ship_entry_locked": locked,
			"ship_lock_reason":  lockReason,
			"skip_auto_alloc":   false,
		}
		if kdzsAction == "push_factory" {
			fields["platform_status"] = model.KDZSWaitSend
			fields["platform_status_text"] = "待发货"
		} else if kdzsAction == "self_print" && o.PlatformStatus == model.KDZSWaitAudit {
			// 推单后快递助手侧通常进入待发货；先乐观更新，下次同步校正
			fields["platform_status"] = model.KDZSWaitSend
			fields["platform_status_text"] = "待发货"
			locked2, reason2 := computeShipLock(o.SourceChannel, model.KDZSWaitSend, agentType, dropshipMode)
			fields["ship_entry_locked"] = locked2
			fields["ship_lock_reason"] = reason2
		}
		return tx.TransitionOrder(tenantID, orderID, fields, &model.OrderStatusLog{
			FromStatus: from,
			ToStatus:   nextStatus,
			Action:     "allocate",
			Remark:     fmt.Sprintf("%s/%s kdzs=%s po=%s %s", allocType, dropshipMode, kdzsAction, purchaseOrderID, req.Remark),
			OperatorID: operatorID,
		})
	})
	if err != nil {
		if createdPOID > 0 {
			_ = s.rollbackDropshipPurchaseOrder(ctx, bearerToken, createdPOID)
		}
		return nil, err
	}
	out, err := s.repos.GetOrder(tenantID, orderID)
	if err != nil {
		return nil, err
	}
	// 记忆模式：人工代发成功后记住订单 SKU→供应商（自动分配不写入）
	if allocType == model.AllocDropship && supplierID > 0 && strings.TrimSpace(req.Remark) != autoAllocRemark {
		s.rememberSkuSupplierBindings(tenantID, out.Items, supplierID, "", supplierName)
	}
	if s.onAllocated != nil && out != nil && out.SupplierID > 0 {
		s.onAllocated(tenantID, out.ID)
	}
	return out, nil
}

// ensureDropshipPurchaseOrder 按 refSoId 复用未取消的代发单，否则新建草稿。
// 返回 poNo、poID、是否本轮新建。
func (s *OrderService) ensureDropshipPurchaseOrder(ctx context.Context, o *model.Order, supplierID uint64, supplierName, bearerToken string) (poNo string, poID uint64, created bool, err error) {
	if s.supply == nil {
		return "", 0, false, fmt.Errorf("SupplyCore 未配置，无法创建代发采购单")
	}
	if len(o.Items) == 0 {
		return "", 0, false, fmt.Errorf("订单无明细，无法创建代发采购单")
	}

	existing, _, listErr := s.supply.ListPurchaseOrders(ctx, bearerToken, o.ID, "dropship", 1, 20)
	if listErr == nil {
		for _, it := range existing {
			if it.Status == "cancelled" {
				continue
			}
			if it.PayStatus == "paid" || it.PayStatus == "partial" {
				return it.PoNo, it.ID, false, nil
			}
			if it.Status == "draft" || it.Status == "ordered" || it.Status == "paid" ||
				it.Status == "partial_shipped" || it.Status == "in_transit" || it.Status == "partial_received" || it.Status == "completed" {
				return it.PoNo, it.ID, false, nil
			}
		}
	}

	items := s.mapOrderToPOLines(ctx, bearerToken, o, s.loadSupplierPOFlags(ctx, bearerToken, supplierID, nil).syncFrom)
	if len(items) == 0 {
		return "", 0, false, fmt.Errorf("订单无明细，无法创建代发采购单")
	}
	saleTotal := 0.0
	for _, line := range items {
		saleTotal += line.SaleAmount
	}
	remark := fmt.Sprintf("OMS代发 %s", o.OrderNo)
	if supplierName != "" {
		remark = remark + " → " + supplierName
	}
	po, err := s.supply.CreatePurchaseOrder(ctx, bearerToken, supplycore.PurchaseOrderInput{
		SupplierID:      supplierID,
		FulfillmentType: "dropship",
		RefSoID:         o.ID,
		RefTraceID:      o.OrderNo,
		SaleAmount:      roundMoney(saleTotal),
		Remark:          remark,
		Items:           items,
	})
	if err != nil {
		return "", 0, false, fmt.Errorf("创建 SupplyCore 代发单失败: %w", err)
	}
	return po.PoNo, po.ID, true, nil
}

// ensureSelfOrder 按 refSoId 复用未取消的自营单，否则新建。
func (s *OrderService) ensureSelfOrder(ctx context.Context, o *model.Order, bearerToken string) (soNo string, err error) {
	if s.selfCore == nil || !s.selfCore.Enabled() {
		return "", fmt.Errorf("SelfCore 未配置，无法创建自营单")
	}
	if len(o.Items) == 0 {
		return "", fmt.Errorf("订单无明细，无法创建自营单")
	}
	existing, listErr := s.selfCore.ListByRefSoID(ctx, bearerToken, o.ID)
	if listErr == nil {
		for _, it := range existing {
			if it.Status == "cancelled" {
				continue
			}
			if it.SoNo != "" {
				return it.SoNo, nil
			}
		}
	}
	items := s.mapOrderToSelfLines(o)
	if len(items) == 0 {
		return "", fmt.Errorf("订单无明细，无法创建自营单")
	}
	saleTotal := 0.0
	for _, line := range items {
		saleTotal += line.SaleAmount
	}
	addr := ""
	buyerPhone := o.BuyerPhone
	buyerName := o.BuyerName
	if o.Address != nil {
		if o.Address.FullText != "" {
			addr = o.Address.FullText
		} else {
			addr = strings.TrimSpace(strings.Join([]string{
				o.Address.Name, o.Address.Phone, o.Address.Province, o.Address.City, o.Address.District, o.Address.Address,
			}, " "))
		}
		if buyerName == "" {
			buyerName = o.Address.Name
		}
		if buyerPhone == "" {
			buyerPhone = o.Address.Phone
		}
	}
	orderedAt := ""
	if o.OrderedAt != nil {
		orderedAt = o.OrderedAt.Format("2006-01-02 15:04:05")
	}
	payStatus := strings.TrimSpace(o.PayStatus)
	paidAt := ""
	if o.PayTime != nil {
		paidAt = o.PayTime.Format("2006-01-02 15:04:05")
	}
	// 电商订单默认已付款（与快递助手入库一致）
	if payStatus == "" && o.SourceChannel == model.SourceKDZS {
		payStatus = "paid"
	}
	created, err := s.selfCore.CreateSelfOrder(ctx, bearerToken, selfcore.SelfOrderInput{
		RefSoID:       o.ID,
		RefTraceID:    o.OrderNo,
		SaleAmount:    roundMoney(saleTotal),
		BuyerName:     buyerName,
		BuyerPhone:    buyerPhone,
		Address:       addr,
		Remark:        fmt.Sprintf("OMS自营 %s", o.OrderNo),
		SourceChannel: o.SourceChannel,
		Platform:      o.Platform,
		ShopName:      o.ShopName,
		BuyerRemark:   o.Remark,
		SellerRemark:  o.SellerRemark,
		FenFaRemark:   o.FenFaRemark,
		PrinterRemark: o.PrinterRemark,
		OrderedAt:     orderedAt,
		PayStatus:     payStatus,
		PaidAt:        paidAt,
		Items:         items,
	})
	if err != nil {
		return "", fmt.Errorf("创建 SelfCore 自营单失败: %w", err)
	}
	return created.SoNo, nil
}

func (s *OrderService) mapOrderToSelfLines(o *model.Order) []selfcore.SelfOrderItemInput {
	if o == nil || len(o.Items) == 0 {
		return nil
	}
	pay := o.PayAmount
	if pay <= 0 {
		pay = o.TotalAmount
	}
	weights := make([]float64, len(o.Items))
	var sumW float64
	for i, it := range o.Items {
		w := it.TotalAmount
		if w <= 0 {
			qty := it.Quantity
			if qty <= 0 {
				qty = 1
			}
			w = it.Price * float64(qty)
		}
		if w <= 0 {
			w = 1
		}
		weights[i] = w
		sumW += w
	}
	out := make([]selfcore.SelfOrderItemInput, 0, len(o.Items))
	allocated := 0.0
	for i, it := range o.Items {
		qty := it.Quantity
		if qty <= 0 {
			qty = 1
		}
		var saleAmt float64
		if i == len(o.Items)-1 {
			saleAmt = roundMoney(pay - allocated)
		} else if sumW > 0 {
			saleAmt = roundMoney(pay * weights[i] / sumW)
			allocated += saleAmt
		}
		unit := 0.0
		if qty > 0 {
			unit = roundMoney(saleAmt / float64(qty))
		}
		out = append(out, selfcore.SelfOrderItemInput{
			PimSkuID:      it.SkuID,
			SkuCode:       it.SkuCode,
			ProductName:   it.ProductName,
			SkuSpecs:      it.SkuSpecs,
			PicURL:        it.PicURL,
			Qty:           qty,
			SaleUnitPrice: unit,
			SaleAmount:    saleAmt,
			RefSoID:       o.ID,
			RefOrderNo:    o.OrderNo,
			Remark:        strings.TrimSpace(strings.Join([]string{o.Remark, o.SellerRemark}, " ")),
		})
	}
	return out
}

// mapOrderToPOLines 将销售单明细转为采购行；明细「订单金额」= 订单实付按行分摊。
// syncFrom 非空时，从对应备注解析采购小计并按数量反推单价。
func (s *OrderService) mapOrderToPOLines(ctx context.Context, bearerToken string, o *model.Order, syncFrom string) []supplycore.PurchaseOrderItemInput {
	if o == nil || len(o.Items) == 0 {
		return nil
	}
	pay := o.PayAmount
	if pay <= 0 {
		pay = o.TotalAmount
	}
	weights := make([]float64, len(o.Items))
	var sumW float64
	for i, it := range o.Items {
		w := it.TotalAmount
		if w <= 0 {
			qty := it.Quantity
			if qty <= 0 {
				qty = 1
			}
			w = it.Price * float64(qty)
		}
		if w <= 0 {
			w = 1
		}
		weights[i] = w
		sumW += w
	}

	purchaseTotal, hasPurchase := parseRemarkPurchaseAmount(orderRemarkBySyncSource(o, syncFrom))
	totalQty := 0
	for _, it := range o.Items {
		q := it.Quantity
		if q <= 0 {
			q = 1
		}
		totalQty += q
	}
	if totalQty <= 0 {
		totalQty = len(o.Items)
	}

	out := make([]supplycore.PurchaseOrderItemInput, 0, len(o.Items))
	var allocated float64
	var purchaseAllocated float64
	for i, it := range o.Items {
		qty := it.Quantity
		if qty <= 0 {
			qty = 1
		}
		skuCode := strings.TrimSpace(it.SkuCode)
		skuID := it.SkuID
		if skuID == 0 && skuCode != "" && s.product != nil {
			if id, rerr := s.product.ResolveSkuIDByCode(ctx, bearerToken, skuCode); rerr == nil && id > 0 {
				skuID = id
			}
		}
		var saleAmt float64
		if i == len(o.Items)-1 {
			saleAmt = roundMoney(pay - allocated)
		} else if sumW > 0 {
			saleAmt = roundMoney(pay * weights[i] / sumW)
			allocated += saleAmt
		}
		if saleAmt < 0 {
			saleAmt = 0
		}
		saleUnit := 0.0
		if qty > 0 {
			saleUnit = roundMoney(saleAmt / float64(qty))
		}
		unitPrice := 0.0
		if hasPurchase {
			var lineAmt float64
			if i == len(o.Items)-1 {
				lineAmt = roundMoney(purchaseTotal - purchaseAllocated)
			} else {
				lineAmt = roundMoney(purchaseTotal * float64(qty) / float64(totalQty))
				purchaseAllocated += lineAmt
			}
			if lineAmt < 0 {
				lineAmt = 0
			}
			unitPrice = roundMoney(lineAmt / float64(qty))
		}
		parts := make([]string, 0, 4)
		parts = append(parts, "OMS单号："+o.OrderNo)
		if buyer := strings.TrimSpace(o.Remark); buyer != "" {
			parts = append(parts, "买家留言："+buyer)
		}
		if seller := strings.TrimSpace(o.SellerRemark); seller != "" {
			parts = append(parts, "卖家备注："+seller)
		}
		if fenfa := strings.TrimSpace(o.FenFaRemark); fenfa != "" {
			parts = append(parts, "分发备注："+fenfa)
		}
		if printer := strings.TrimSpace(o.PrinterRemark); printer != "" {
			parts = append(parts, "打单备注："+printer)
		}
		remark := strings.Join(parts, "；")
		out = append(out, supplycore.PurchaseOrderItemInput{
			SkuID:         skuID,
			ProductName:   it.ProductName,
			SkuCode:       skuCode,
			SkuSpecs:      it.SkuSpecs,
			PicURL:        it.PicURL,
			Qty:           qty,
			SaleUnitPrice: saleUnit,
			SaleAmount:    saleAmt,
			UnitPrice:     unitPrice,
			RefSoID:       o.ID,
			RefOrderNo:    o.OrderNo,
			Remark:        remark,
		})
	}
	return out
}

func orderRemarkBySyncSource(o *model.Order, source string) string {
	if o == nil {
		return ""
	}
	switch strings.TrimSpace(source) {
	case "fen_fa_remark":
		return o.FenFaRemark
	case "alloc_remark":
		return o.AllocRemark
	case "seller_remark":
		return o.SellerRemark
	case "printer_remark":
		return o.PrinterRemark
	default:
		return ""
	}
}

var remarkPurchaseAmountRe = regexp.MustCompile(`\d+(?:\.\d+)?`)

func parseRemarkPurchaseAmount(raw string) (float64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil && v >= 0 {
		return roundMoney(v), true
	}
	trimmed := strings.TrimSpace(strings.TrimRight(s, "元块￥$ "))
	if v, err := strconv.ParseFloat(trimmed, 64); err == nil && v >= 0 {
		return roundMoney(v), true
	}
	matches := remarkPurchaseAmountRe.FindAllString(s, -1)
	if len(matches) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(matches[len(matches)-1], 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return roundMoney(v), true
}

// BatchAllocateDropship 批量代发：同一供应商合并为一张 SupplyCore 代发采购单（多行明细），再逐单分配。
func (s *OrderService) BatchAllocateDropship(ctx context.Context, tenantID, operatorID uint64, orderIDs []uint64, supplierID uint64, supplierName, bearerToken string) (map[string]any, error) {
	if supplierID == 0 {
		return nil, fmt.Errorf("请选择供应商")
	}
	if len(orderIDs) == 0 {
		return nil, fmt.Errorf("请选择订单")
	}
	if s.supply == nil {
		return nil, fmt.Errorf("SupplyCore 未配置")
	}
	if supplierName == "" {
		if b, err := s.repos.FindBindingBySupplier(tenantID, supplierID, model.SourceKDZS); err == nil {
			supplierName = b.SupplierName
		}
	}

	orders := make([]*model.Order, 0, len(orderIDs))
	seen := map[uint64]struct{}{}
	for _, id := range orderIDs {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		o, err := s.repos.GetOrder(tenantID, id)
		if err != nil {
			return nil, fmt.Errorf("订单 %d 不存在", id)
		}
		if o.Status != model.StatusPendingAlloc && o.Status != model.StatusPendingShip {
			return nil, fmt.Errorf("%s 当前状态不可代发分配", o.OrderNo)
		}
		if len(o.Items) == 0 {
			return nil, fmt.Errorf("%s 无商品明细", o.OrderNo)
		}
		orders = append(orders, o)
	}
	if len(orders) == 0 {
		return nil, fmt.Errorf("请选择有效订单")
	}

	remark := fmt.Sprintf("OMS批量代发 %d 单 → %s", len(orders), supplierName)
	po, saleTotal, lineCount, err := s.createMergedDropshipPO(ctx, orders, supplierID, supplierName, remark, bearerToken)
	if err != nil {
		return nil, err
	}
	// 有厂家绑定（快递助手厂家代发）则自动提交
	if b, berr := s.repos.FindBindingBySupplier(tenantID, supplierID, model.SourceKDZS); berr == nil && b.ExternalFactoryID != "" {
		if _, serr := s.supply.SubmitPurchaseOrder(ctx, bearerToken, po.ID); serr != nil {
			_ = s.rollbackDropshipPurchaseOrder(ctx, bearerToken, po.ID)
			return nil, fmt.Errorf("代发单自动提交失败: %w", serr)
		}
	}

	ok := 0
	errs := make([]string, 0)
	for _, o := range orders {
		_, aerr := s.Allocate(ctx, tenantID, operatorID, o.ID, dto.AllocateRequest{
			AllocType:       model.AllocDropship,
			SupplierID:      supplierID,
			SupplierName:    supplierName,
			PurchaseOrderID: po.PoNo,
		}, bearerToken)
		if aerr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", o.OrderNo, aerr))
			continue
		}
		ok++
	}
	if ok == 0 {
		_ = s.rollbackDropshipPurchaseOrder(ctx, bearerToken, po.ID)
		return nil, fmt.Errorf("批量代发全部失败: %s", strings.Join(errs, "; "))
	}
	return map[string]any{
		"poNo":        po.PoNo,
		"poId":        po.ID,
		"saleAmount":  roundMoney(saleTotal),
		"totalAmount": roundMoney(saleTotal),
		"orderCount":  len(orders),
		"lineCount":   lineCount,
		"success":     ok,
		"failed":      len(errs),
		"errors":      errs,
	}, nil
}

func needsDropshipPO(o *model.Order) bool {
	if o == nil || o.AllocType != model.AllocDropship || o.SupplierID == 0 {
		return false
	}
	if o.Status == model.StatusClosed {
		return false
	}
	if strings.TrimSpace(o.PurchaseOrderID) != "" {
		return false
	}
	return len(o.Items) > 0
}

// clearStalePurchaseOrderRef 若销售单挂着已删除的代发单号，清空以便同步可重建。
func (s *OrderService) clearStalePurchaseOrderRef(ctx context.Context, tenantID uint64, o *model.Order, bearerToken string) *model.Order {
	if o == nil || s.supply == nil || strings.TrimSpace(bearerToken) == "" {
		return o
	}
	poNo := strings.TrimSpace(o.PurchaseOrderID)
	if poNo == "" {
		return o
	}
	list, _, err := s.supply.ListPurchaseOrdersEx(ctx, bearerToken, 0, "dropship", poNo, 1, 20)
	if err != nil {
		// 下游短暂失败时不误清，避免重复建单
		return o
	}
	for _, it := range list {
		if strings.TrimSpace(it.PoNo) == poNo {
			return o
		}
	}
	if err := s.repos.UpdateOrderFields(tenantID, o.ID, map[string]any{"purchase_order_id": ""}); err != nil {
		log.Printf("[ordercore] clear stale po ref order=%s po=%s: %v", o.OrderNo, poNo, err)
		return o
	}
	o.PurchaseOrderID = ""
	log.Printf("[ordercore] cleared stale purchase_order_id=%s order=%s (PO missing in SupplyCore)", poNo, o.OrderNo)
	return o
}

func (s *OrderService) ensureSyncDropshipPOForOrder(ctx context.Context, tenantID uint64, o *model.Order, bearerToken string) {
	if !needsDropshipPO(o) || s.supply == nil || strings.TrimSpace(bearerToken) == "" {
		return
	}
	if !s.supplierAutoCreateDropshipPO(ctx, bearerToken, o.SupplierID) {
		return
	}
	if err := s.createAndBindDropshipPOs(ctx, tenantID, []*model.Order{o}, bearerToken, false); err != nil {
		log.Printf("[ordercore] auto dropship PO order=%s: %v", o.OrderNo, err)
	}
}

// queueOrCreateDropshipPO 同步批次加入延后合并队列；非同步立即按供应商开关建单。
func (s *OrderService) queueOrCreateDropshipPO(ctx context.Context, tenantID uint64, o *model.Order, bearerToken string) {
	if !needsDropshipPO(o) {
		return
	}
	if batch := deferredDropshipFromCtx(ctx); batch != nil {
		batch.add(o)
		return
	}
	s.ensureSyncDropshipPOForOrder(ctx, tenantID, o, bearerToken)
}

func (s *OrderService) flushDeferredDropshipPOs(ctx context.Context, tenantID uint64, bearerToken string, batch *deferredDropshipBatch) {
	orders := batch.take()
	if len(orders) == 0 {
		return
	}
	fresh := make([]*model.Order, 0, len(orders))
	for _, o := range orders {
		cur, err := s.repos.GetOrder(tenantID, o.ID)
		if err != nil || !needsDropshipPO(cur) {
			continue
		}
		fresh = append(fresh, cur)
	}
	if len(fresh) == 0 {
		return
	}
	if err := s.createAndBindDropshipPOs(ctx, tenantID, fresh, bearerToken, false); err != nil {
		log.Printf("[ordercore] flush deferred dropship PO n=%d: %v", len(fresh), err)
	}
}

type supplierPOFlags struct {
	autoCreate bool
	syncFrom   string
	loaded     bool
}

func (s *OrderService) loadSupplierPOFlags(ctx context.Context, bearerToken string, supplierID uint64, cache map[uint64]*supplierPOFlags) *supplierPOFlags {
	if supplierID == 0 {
		return &supplierPOFlags{}
	}
	if cache != nil {
		if v, ok := cache[supplierID]; ok {
			return v
		}
	}
	flags := &supplierPOFlags{loaded: true}
	if s.supply != nil && strings.TrimSpace(bearerToken) != "" {
		if sup, err := s.supply.GetSupplier(ctx, bearerToken, supplierID); err == nil && sup != nil {
			flags.autoCreate = sup.AutoCreateDropshipPO
			flags.syncFrom = strings.TrimSpace(sup.SyncPurchasePriceFrom)
		}
	}
	if cache != nil {
		cache[supplierID] = flags
	}
	return flags
}

func (s *OrderService) supplierAutoCreateDropshipPO(ctx context.Context, bearerToken string, supplierID uint64) bool {
	return s.loadSupplierPOFlags(ctx, bearerToken, supplierID, nil).autoCreate
}

// BackfillDropshipPOs 运维补建：对已分配缺代发单的订单合并建单（不看「自动建代发单」开关）。
func (s *OrderService) BackfillDropshipPOs(ctx context.Context, tenantID uint64, orders []*model.Order, bearerToken string) error {
	return s.createAndBindDropshipPOs(ctx, tenantID, orders, bearerToken, true)
}

// createAndBindDropshipPOs 同供应商本批合并为一张代发单。
// force=false 时仅对开启「自动建代发单」的供应商建单（同步自动分配）；force=true 用于运维补建。
func (s *OrderService) createAndBindDropshipPOs(ctx context.Context, tenantID uint64, orders []*model.Order, bearerToken string, force bool) error {
	if len(orders) == 0 {
		return nil
	}
	bySupplier := map[uint64][]*model.Order{}
	names := map[uint64]string{}
	for _, o := range orders {
		if !needsDropshipPO(o) {
			continue
		}
		bySupplier[o.SupplierID] = append(bySupplier[o.SupplierID], o)
		if o.SupplierName != "" {
			names[o.SupplierID] = o.SupplierName
		}
	}
	flagCache := map[uint64]*supplierPOFlags{}
	var firstErr error
	for supplierID, group := range bySupplier {
		if len(group) == 0 {
			continue
		}
		if !force && !s.loadSupplierPOFlags(ctx, bearerToken, supplierID, flagCache).autoCreate {
			continue
		}
		supplierName := names[supplierID]
		if supplierName == "" {
			if b, err := s.repos.FindBindingBySupplier(tenantID, supplierID, model.SourceKDZS); err == nil {
				supplierName = b.SupplierName
			}
		}
		remark := fmt.Sprintf("OMS同步代发 %d 单 → %s", len(group), supplierName)
		if force {
			remark = fmt.Sprintf("OMS补建代发 %d 单 → %s", len(group), supplierName)
		}
		if len(group) == 1 {
			if force {
				remark = fmt.Sprintf("OMS补建代发 → %s", supplierName)
			} else {
				remark = fmt.Sprintf("OMS同步代发 → %s", supplierName)
			}
		}
		po, _, _, err := s.createMergedDropshipPO(ctx, group, supplierID, supplierName, remark, bearerToken)
		if err != nil {
			log.Printf("[ordercore] create dropship PO supplier=%d n=%d: %v", supplierID, len(group), err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if isKDZSFactoryGroup(group) {
			if _, serr := s.supply.SubmitPurchaseOrder(ctx, bearerToken, po.ID); serr != nil {
				log.Printf("[ordercore] auto-submit dropship PO %s: %v", po.PoNo, serr)
			}
		}
		for _, o := range group {
			if err := s.repos.UpdateOrderFields(tenantID, o.ID, map[string]any{
				"purchase_order_id": po.PoNo,
			}); err != nil {
				log.Printf("[ordercore] bind dropship PO order=%s po=%s: %v", o.OrderNo, po.PoNo, err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			o.PurchaseOrderID = po.PoNo
			actionRemark := fmt.Sprintf("同步自动创建代发单 po=%s → %s", po.PoNo, supplierName)
			if force {
				actionRemark = fmt.Sprintf("补建代发单 po=%s → %s", po.PoNo, supplierName)
			}
			_ = s.repos.AddStatusLog(&model.OrderStatusLog{
				TenantID:   tenantID,
				OrderID:    o.ID,
				FromStatus: o.Status,
				ToStatus:   o.Status,
				Action:     "auto_dropship_po",
				Remark:     actionRemark,
			})
		}
	}
	return firstErr
}

func isKDZSFactoryGroup(orders []*model.Order) bool {
	for _, o := range orders {
		if o != nil && o.DropshipMode == model.DropshipKDZSFactory {
			return true
		}
	}
	return false
}

func (s *OrderService) createMergedDropshipPO(ctx context.Context, orders []*model.Order, supplierID uint64, supplierName, remark, bearerToken string) (*supplycore.PurchaseOrderDetail, float64, int, error) {
	if s.supply == nil {
		return nil, 0, 0, fmt.Errorf("SupplyCore 未配置")
	}
	if len(orders) == 0 {
		return nil, 0, 0, fmt.Errorf("无有效订单")
	}
	syncFrom := s.loadSupplierPOFlags(ctx, bearerToken, supplierID, nil).syncFrom
	items := make([]supplycore.PurchaseOrderItemInput, 0)
	traceParts := make([]string, 0, len(orders))
	var saleTotal float64
	// 多单合并建单时不按各单分发备注直接写单价（合单备注常复制到每单，会翻倍）；
	// 建单后统一 SyncPurchasePrices，有运单号时整包只计一次。
	lineSyncFrom := syncFrom
	if len(orders) > 1 {
		lineSyncFrom = ""
	}
	for _, o := range orders {
		traceParts = append(traceParts, o.OrderNo)
		lines := s.mapOrderToPOLines(ctx, bearerToken, o, lineSyncFrom)
		items = append(items, lines...)
		for _, line := range lines {
			saleTotal += line.SaleAmount
		}
	}
	if len(items) == 0 {
		return nil, 0, 0, fmt.Errorf("无代发明细")
	}
	if remark == "" {
		remark = fmt.Sprintf("OMS代发 %d 单 → %s", len(orders), supplierName)
	}
	po, err := s.supply.CreatePurchaseOrder(ctx, bearerToken, supplycore.PurchaseOrderInput{
		SupplierID:      supplierID,
		FulfillmentType: "dropship",
		RefSoID:         orders[0].ID,
		RefTraceID:      strings.Join(traceParts, ","),
		SaleAmount:      roundMoney(saleTotal),
		Remark:          remark,
		Items:           items,
	})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("创建合并代发单失败: %w", err)
	}
	// 建单后再拉一次备注同步（分发备注可能刚写入 / 建单时 flags 偶发未读到）
	if syncFrom != "" && po != nil && po.ID > 0 {
		if serr := s.supply.SyncPurchasePrices(ctx, bearerToken, po.ID); serr != nil {
			log.Printf("[ordercore] sync purchase prices after create po=%s: %v", po.PoNo, serr)
		}
	}
	return po, saleTotal, len(items), nil
}

func (s *OrderService) RelinkPurchaseOrder(ctx context.Context, tenantID uint64, fromPoNos []string, toPoNo string) (int64, error) {
	return s.repos.RelinkPurchaseOrderIDs(tenantID, fromPoNos, strings.TrimSpace(toPoNo))
}

// UnlinkDropshipPO 供应链侧解绑后回写：清空采购单号；clearAlloc 时恢复待分配（不调用快递助手）。
func (s *OrderService) UnlinkDropshipPO(ctx context.Context, tenantID, operatorID uint64, req dto.UnlinkDropshipPORequest) (int, error) {
	ids := make([]uint64, 0, len(req.OrderIDs)+len(req.OrderNos))
	seen := map[uint64]struct{}{}
	for _, id := range req.OrderIDs {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, no := range req.OrderNos {
		no = strings.TrimSpace(no)
		if no == "" {
			continue
		}
		o, err := s.repos.FindByOrderNo(tenantID, no)
		if err != nil || o == nil {
			continue
		}
		if _, ok := seen[o.ID]; ok {
			continue
		}
		seen[o.ID] = struct{}{}
		ids = append(ids, o.ID)
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("请提供 orderIds 或 orderNos")
	}
	remark := strings.TrimSpace(req.Remark)
	if remark == "" {
		remark = "供应链解绑代发单"
	}
	updated := 0
	for _, id := range ids {
		o, err := s.repos.GetOrder(tenantID, id)
		if err != nil || o == nil {
			continue
		}
		if o.Status == model.StatusCompleted || o.Status == model.StatusClosed {
			// 终态只清采购单号，不清分配
			if strings.TrimSpace(o.PurchaseOrderID) == "" {
				continue
			}
			if err := s.repos.UpdateOrderFields(tenantID, id, map[string]any{"purchase_order_id": ""}); err != nil {
				return updated, err
			}
			_ = s.repos.AddStatusLog(&model.OrderStatusLog{
				TenantID: tenantID, OrderID: id, FromStatus: o.Status, ToStatus: o.Status,
				Action: "unlink_dropship_po", Remark: remark + "（终态仅清采购单号）", OperatorID: operatorID,
			})
			updated++
			continue
		}
		fields := map[string]any{"purchase_order_id": ""}
		toStatus := o.Status
		if req.ClearAlloc && o.ShipStatus != model.ShipShipped {
			fields["alloc_type"] = ""
			fields["dropship_mode"] = ""
			fields["supplier_id"] = 0
			fields["supplier_name"] = ""
			fields["factory_id"] = ""
			fields["factory_name"] = ""
			fields["alloc_remark"] = ""
			fields["allocated_at"] = nil
			fields["status"] = model.StatusPendingAlloc
			fields["ship_status"] = model.ShipWaitShip
			fields["agent_type"] = model.AgentTypeSelf
			fields["skip_auto_alloc"] = true
			if o.SourceChannel == model.SourceKDZS {
				fields["platform_status"] = model.KDZSWaitAudit
				fields["platform_status_text"] = "待推单"
				fields["ship_entry_locked"] = true
				fields["ship_lock_reason"] = "快递助手待推单，请先分配；仅自营待发货可填单号"
			} else {
				fields["ship_entry_locked"] = false
				fields["ship_lock_reason"] = ""
			}
			toStatus = model.StatusPendingAlloc
		}
		from := o.Status
		if err := s.repos.Transaction(func(tx *repo.Repos) error {
			return tx.TransitionOrder(tenantID, id, fields, &model.OrderStatusLog{
				FromStatus: from,
				ToStatus:   toStatus,
				Action:     "unlink_dropship_po",
				Remark:     remark,
				OperatorID: operatorID,
			})
		}); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func (s *OrderService) rollbackDropshipPurchaseOrder(ctx context.Context, bearerToken string, poID uint64) error {
	if s.supply == nil || poID == 0 {
		return nil
	}
	if err := s.supply.DeletePurchaseOrder(ctx, bearerToken, poID); err == nil {
		return nil
	}
	_, err := s.supply.CancelPurchaseOrder(ctx, bearerToken, poID)
	return err
}

// cancelLinkedDropshipPOs 撤回分配时同步代发采购单：
// - 有关联代发单号时，先将该销售单明细标为已撤回（划线痕迹 + 备注）
// - 仍有其它销售单挂在同一代发单：仅 detach，不整单取消
// - 本单为最后关联方：detach 会在全部明细撤回后取消整单；再兜底 cancel/delete
// - 代发单在 SupplyCore 已不存在：视为已同步，不阻断撤回
func (s *OrderService) cancelLinkedDropshipPOs(ctx context.Context, tenantID, orderID uint64, purchaseOrderID, bearerToken string) error {
	if s.supply == nil {
		return nil
	}
	poNo := strings.TrimSpace(purchaseOrderID)
	orderNo := ""
	if o, err := s.repos.GetOrder(tenantID, orderID); err == nil && o != nil {
		orderNo = o.OrderNo
	}

	var others int64
	if poNo != "" {
		var err error
		others, err = s.repos.CountByPurchaseOrderID(tenantID, poNo, orderID)
		if err != nil {
			return err
		}
		// 无论是否最后一单，都先标记本单明细为已撤回，避免「最后一单只取消头、明细不划线」
		_, derr := s.supply.DetachSalesOrder(ctx, bearerToken, poNo, orderNo, orderID, "撤回分配")
		if derr != nil {
			if isSupplyNotFound(derr) {
				log.Printf("[ordercore] detach dropship PO missing po=%s order=%s: %v (skip)", poNo, orderNo, derr)
				return nil
			}
			return fmt.Errorf("同步代发单撤回失败: %w", derr)
		}
		if others > 0 {
			return nil
		}
	}

	seen := map[uint64]struct{}{}
	list, _, err := s.supply.ListPurchaseOrders(ctx, bearerToken, orderID, "dropship", 1, 50)
	if err != nil && poNo == "" {
		return nil
	}
	if err == nil {
		for _, it := range list {
			seen[it.ID] = struct{}{}
			if err := s.cancelOneDropshipPO(ctx, bearerToken, it); err != nil {
				return err
			}
		}
	}
	if poNo != "" {
		byNo, _, err := s.supply.ListPurchaseOrdersEx(ctx, bearerToken, 0, "dropship", poNo, 1, 10)
		if err == nil {
			for _, it := range byNo {
				if it.PoNo != poNo {
					continue
				}
				if _, ok := seen[it.ID]; ok {
					continue
				}
				if err := s.cancelOneDropshipPO(ctx, bearerToken, it); err != nil {
					return err
				}
			}
		} else if isSupplyNotFound(err) {
			return nil
		}
	}
	return nil
}

func isSupplyNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "记录不存在") ||
		strings.Contains(msg, `"code":404`) ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "http 404")
}

func (s *OrderService) cancelOneDropshipPO(ctx context.Context, bearerToken string, it supplycore.PurchaseOrderListItem) error {
	if it.Status == "cancelled" {
		return nil
	}
	// 已付款/履约：销售单已在 DetachSalesOrder 解绑划线即可，不可整单取消（也不阻断撤回分配）
	if it.PayStatus == "paid" || it.PayStatus == "partial" || it.Status == "paid" ||
		it.Status == "partial_shipped" || it.Status == "shipped" || it.Status == "in_transit" ||
		it.Status == "partial_received" || it.Status == "completed" {
		log.Printf("[ordercore] skip cancel dropship PO %s status=%s pay=%s (already detached line)", it.PoNo, it.Status, it.PayStatus)
		return nil
	}
	if it.Status == "draft" || it.Status == "ordered" {
		if _, err := s.supply.CancelPurchaseOrder(ctx, bearerToken, it.ID); err != nil {
			if it.Status == "draft" {
				if delErr := s.supply.DeletePurchaseOrder(ctx, bearerToken, it.ID); delErr != nil {
					return fmt.Errorf("取消代发单 %s 失败: %w", it.PoNo, err)
				}
				return nil
			}
			return fmt.Errorf("取消代发单 %s 失败: %w", it.PoNo, err)
		}
	}
	return nil
}

// RevokeAllocate 撤回分配：快递助手侧先撤单/退审到待推单，再清空 OMS 履约分配。
func (s *OrderService) RevokeAllocate(ctx context.Context, tenantID, operatorID, orderID uint64, bearerToken string) (*model.Order, error) {
	o, err := s.repos.GetOrder(tenantID, orderID)
	if err != nil {
		return nil, err
	}
	if o.Status == model.StatusCompleted || o.Status == model.StatusClosed {
		return nil, fmt.Errorf("当前状态不可撤回分配")
	}
	if o.ShipStatus == model.ShipShipped {
		return nil, fmt.Errorf("订单已发货，不可撤回分配")
	}
	if o.AllocType == "" && o.Status == model.StatusPendingAlloc {
		return nil, fmt.Errorf("订单尚未分配")
	}

	// 先处理关联代发采购单（已付款则禁止撤回）
	if o.AllocType == model.AllocDropship || strings.TrimSpace(o.PurchaseOrderID) != "" {
		if err := s.cancelLinkedDropshipPOs(ctx, tenantID, o.ID, o.PurchaseOrderID, bearerToken); err != nil {
			return nil, err
		}
	}
	// 自营分配：同步取消 SelfCore 本地自营单
	if o.AllocType == model.AllocSelfShip || strings.TrimSpace(o.SelfOrderNo) != "" {
		if err := s.cancelLinkedSelfOrders(ctx, o.ID, bearerToken); err != nil {
			return nil, err
		}
	}

	kdzsRemark := ""
	if shouldSyncKDZSAgent(o) {
		// 待发货（自营/厂家）需先调快递助手撤单；已在待推单则跳过
		needCancel := o.PlatformStatus == model.KDZSWaitSend ||
			o.AllocType == model.AllocSelfShip ||
			o.DropshipMode == model.DropshipKDZSFactory ||
			o.DropshipMode == model.DropshipOSMSSupplier ||
			o.AgentType == model.AgentTypeFactory
		if needCancel && o.PlatformStatus != model.KDZSWaitAudit {
			if err := s.cancelKDZSPush(ctx, o, bearerToken); err != nil {
				return nil, fmt.Errorf("同步快递助手撤单失败: %w", err)
			}
			kdzsRemark = "kdzs=cancel_push"
		} else if o.PlatformStatus == model.KDZSWaitAudit {
			kdzsRemark = "kdzs=cancel_push(skip:wait_audit)"
		}
	}

	from := o.Status
	locked, lockReason := false, ""
	if o.SourceChannel == model.SourceKDZS {
		locked = true
		lockReason = "快递助手待推单，请先分配；仅自营待发货可填单号"
	}
	err = s.repos.Transaction(func(tx *repo.Repos) error {
		fields := map[string]any{
			"alloc_type":        "",
			"dropship_mode":     "",
			"supplier_id":       0,
			"supplier_name":     "",
			"factory_id":        "",
			"factory_name":      "",
			"purchase_order_id": "",
			"self_order_no":     "",
			"alloc_remark":      "",
			"allocated_at":      nil,
			"status":            model.StatusPendingAlloc,
			"ship_status":       model.ShipWaitShip,
			"agent_type":        model.AgentTypeSelf,
			"ship_entry_locked": locked,
			"ship_lock_reason":  lockReason,
			"skip_auto_alloc":   true,
		}
		if shouldSyncKDZSAgent(o) {
			fields["platform_status"] = model.KDZSWaitAudit
			fields["platform_status_text"] = "待推单"
		}
		remark := "撤回分配"
		if kdzsRemark != "" {
			remark = remark + " " + kdzsRemark
		}
		return tx.TransitionOrder(tenantID, orderID, fields, &model.OrderStatusLog{
			FromStatus: from,
			ToStatus:   model.StatusPendingAlloc,
			Action:     "revoke_allocate",
			Remark:     remark,
			OperatorID: operatorID,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repos.GetOrder(tenantID, orderID)
}

// cancelLinkedSelfOrders 撤回分配时同步取消 SelfCore 自营单（按销售单 refSoId）。
// 自营单已不存在视为已同步；已发货等不可取消状态会阻断撤回。
func (s *OrderService) cancelLinkedSelfOrders(ctx context.Context, orderID uint64, bearerToken string) error {
	if s.selfCore == nil || !s.selfCore.Enabled() {
		return nil
	}
	_, err := s.selfCore.CancelByRefSoID(ctx, bearerToken, orderID, "撤回分配")
	if err != nil {
		if isSupplyNotFound(err) {
			log.Printf("[ordercore] cancel self order missing orderID=%d: %v (skip)", orderID, err)
			return nil
		}
		return fmt.Errorf("同步自营单取消失败: %w", err)
	}
	return nil
}

// kdzsAgentSysTid 快递助手推单/撤单接口使用的 sysTid。
// 手工单（DFHAND）建单 SuccessList/SuccessRealList 与电商含义不一致，setTradeAgentType 认的是平台单号（platform_order_id）。
func kdzsAgentSysTid(o *model.Order) (sysTid, tid string) {
	if o == nil {
		return "", ""
	}
	sysTid = strings.TrimSpace(o.PlatformSysTid)
	tid = strings.TrimSpace(o.PlatformOrderID)
	if strings.EqualFold(strings.TrimSpace(o.Platform), "DFHAND") || o.SourceChannel == model.SourceManual {
		if tid != "" {
			return tid, sysTid
		}
	}
	if sysTid == "" {
		sysTid = tid
	}
	return sysTid, tid
}

func (s *OrderService) cancelKDZSPush(ctx context.Context, o *model.Order, token string) error {
	if s.storeSync == nil {
		return fmt.Errorf("StoreSyncAgent 未配置")
	}
	sysTid, _ := kdzsAgentSysTid(o)
	if sysTid == "" {
		return fmt.Errorf("缺少平台系统单号，无法同步快递助手")
	}
	tradeStatus := o.PlatformStatus
	if tradeStatus == "" || tradeStatus == model.KDZSWaitAudit {
		tradeStatus = model.KDZSWaitSend
	}
	platform := o.Platform
	if platform == "" {
		platform = "FXG"
	}
	return s.storeSync.CancelOrderPush(ctx, token, storesync.CancelPushRequest{
		Platform:    platform,
		TradeStatus: tradeStatus,
		SysTids:     []string{sysTid},
	})
}

func (s *OrderService) setKDZSAgentType(ctx context.Context, o *model.Order, action, factoryID, token string) error {
	if s.storeSync == nil {
		return fmt.Errorf("StoreSyncAgent 未配置")
	}
	sysTid, tid := kdzsAgentSysTid(o)
	if sysTid == "" {
		return fmt.Errorf("缺少平台系统单号，无法同步快递助手")
	}
	tradeStatus := o.PlatformStatus
	if tradeStatus == "" {
		tradeStatus = model.KDZSWaitAudit
	}
	req := storesync.SetAgentTypeRequest{
		Platform:    o.Platform,
		TradeStatus: tradeStatus,
		Action:      action,
		FactoryID:   factoryID,
		SysTids:     []string{sysTid},
	}
	if tid != "" && tid != sysTid {
		req.Tids = []string{tid}
	}
	return s.storeSync.SetOrderAgentType(ctx, token, req)
}

func (s *OrderService) UpdateRemarks(ctx context.Context, tenantID, operatorID, orderID uint64, req dto.UpdateRemarksRequest, bearerToken string) (*model.Order, error) {
	o, err := s.repos.GetOrder(tenantID, orderID)
	if err != nil {
		return nil, err
	}
	if o.Status == model.StatusClosed {
		return nil, fmt.Errorf("订单已关闭，不可修改备注")
	}
	seller := strings.TrimSpace(req.SellerRemark)
	fenFa := strings.TrimSpace(req.FenFaRemark)
	printer := strings.TrimSpace(req.PrinterRemark)
	alloc := strings.TrimSpace(req.AllocRemark)
	oldSeller := strings.TrimSpace(o.SellerRemark)
	oldPrinter := strings.TrimSpace(o.PrinterRemark)
	oldFlag := o.SellerFlag
	newFlag := oldFlag
	flagChanged := false
	if req.SellerFlag != nil {
		newFlag = *req.SellerFlag
		if newFlag < 0 {
			newFlag = 0
		}
		if newFlag > 5 {
			newFlag = 5
		}
		flagChanged = newFlag != oldFlag
	}
	sellerChanged := seller != oldSeller || flagChanged

	// 快递助手订单：卖家备注/旗帜 / 打单备注变更时先写回，再落库
	if o.SourceChannel == model.SourceKDZS && s.storeSync != nil && strings.TrimSpace(bearerToken) != "" {
		sysTid := strings.TrimSpace(o.PlatformSysTid)
		if sysTid == "" {
			sysTid = strings.TrimSpace(o.PlatformOrderID)
		}
		platform := strings.TrimSpace(o.Platform)
		if sysTid != "" && platform != "" {
			tradeStatus := strings.TrimSpace(o.PlatformStatus)
			if sellerChanged {
				flagPtr := &newFlag
				if err := s.storeSync.UpdateTradeRemark(ctx, bearerToken, storesync.UpdateTradeRemarkRequest{
					Platform:    platform,
					TradeStatus: tradeStatus,
					SysTids:     []string{sysTid},
					MemoType:    "sellerMemo",
					Remark:      seller,
					SellerFlag:  flagPtr,
				}); err != nil {
					return nil, fmt.Errorf("写回快递助手卖家备注失败: %w", err)
				}
			}
			if printer != oldPrinter {
				if printer == "" {
					return nil, fmt.Errorf("打单备注不能为空（快递助手侧限制）")
				}
				if err := s.storeSync.UpdateTradeRemark(ctx, bearerToken, storesync.UpdateTradeRemarkRequest{
					Platform:    platform,
					TradeStatus: tradeStatus,
					SysTids:     []string{sysTid},
					MemoType:    "printerMemo",
					Remark:      printer,
				}); err != nil {
					return nil, fmt.Errorf("写回快递助手打单备注失败: %w", err)
				}
			}
		}
	}

	logRemark := "更新卖家/分发/打单/分配备注"
	if o.SourceChannel == model.SourceKDZS && (sellerChanged || printer != oldPrinter) {
		logRemark = "更新备注并写回快递助手"
	}
	fields := map[string]any{
		"seller_remark":  seller,
		"fen_fa_remark":  fenFa,
		"printer_remark": printer,
		"alloc_remark":   alloc,
	}
	if req.SellerFlag != nil {
		fields["seller_flag"] = newFlag
	}
	err = s.repos.Transaction(func(tx *repo.Repos) error {
		if err := tx.UpdateOrderFields(tenantID, orderID, fields); err != nil {
			return err
		}
		return tx.AddStatusLog(&model.OrderStatusLog{
			TenantID:   tenantID,
			OrderID:    orderID,
			FromStatus: o.Status,
			ToStatus:   o.Status,
			Action:     "update_remarks",
			Remark:     logRemark,
			OperatorID: operatorID,
		})
	})
	if err != nil {
		return nil, err
	}
	out, gerr := s.repos.GetOrder(tenantID, orderID)
	if gerr != nil {
		return nil, gerr
	}
	if s.supply != nil && strings.TrimSpace(bearerToken) != "" && out != nil {
		s.syncLinkedPOPurchasePrices(ctx, out, bearerToken)
	}
	return out, nil
}

// UpdatePaymentFromSelf 自营中心回写付款状态；仅手工单更新 pay_time/pay_status，其它渠道忽略。
func (s *OrderService) UpdatePaymentFromSelf(ctx context.Context, tenantID, orderID uint64, req dto.UpdatePaymentRequest) (*model.Order, error) {
	o, err := s.repos.GetOrder(tenantID, orderID)
	if err != nil {
		return nil, err
	}
	if o.SourceChannel != model.SourceManual {
		return o, nil
	}
	payStatus := strings.TrimSpace(req.PayStatus)
	if payStatus == "" {
		payStatus = "unpaid"
	}
	fields := map[string]any{
		"pay_status": payStatus,
	}
	if req.ClearPayTime || strings.TrimSpace(req.PayTime) == "" {
		fields["pay_time"] = nil
	} else if t := parseTime(req.PayTime); t != nil {
		fields["pay_time"] = t
	}
	if err := s.repos.UpdateOrderFields(tenantID, orderID, fields); err != nil {
		return nil, err
	}
	_ = s.repos.AddStatusLog(&model.OrderStatusLog{
		TenantID: tenantID,
		OrderID:  orderID,
		ToStatus: o.Status,
		Action:   "sync_payment_from_self",
		Remark:   fmt.Sprintf("自营付款回写 payStatus=%s", payStatus),
	})
	return s.repos.GetOrder(tenantID, orderID)
}

func (s *OrderService) syncLinkedPOPurchasePrices(ctx context.Context, o *model.Order, bearerToken string) {
	if s.supply == nil || o == nil || strings.TrimSpace(bearerToken) == "" {
		return
	}
	poNo := strings.TrimSpace(o.PurchaseOrderID)
	seen := map[uint64]struct{}{}
	collect := func(list []supplycore.PurchaseOrderListItem) {
		for _, it := range list {
			if it.ID == 0 {
				continue
			}
			if it.Status == "cancelled" || it.Status == "completed" {
				continue
			}
			if it.PayStatus == "paid" || it.PayStatus == "partial" {
				continue
			}
			if _, ok := seen[it.ID]; ok {
				continue
			}
			seen[it.ID] = struct{}{}
			if serr := s.supply.SyncPurchasePrices(ctx, bearerToken, it.ID); serr != nil {
				log.Printf("[ordercore] sync purchase prices po=%s order=%s: %v", it.PoNo, o.OrderNo, serr)
			}
		}
	}
	// 合并代发单时单头 refSoId 只挂首单，优先按采购单号；再按本销售单 id 兜底
	if poNo != "" {
		list, _, err := s.supply.ListPurchaseOrdersEx(ctx, bearerToken, 0, "dropship", poNo, 1, 20)
		if err != nil {
			log.Printf("[ordercore] list PO by no for price sync order=%s po=%s: %v", o.OrderNo, poNo, err)
		} else {
			collect(list)
		}
	}
	if o.ID > 0 {
		list, _, err := s.supply.ListPurchaseOrdersEx(ctx, bearerToken, o.ID, "dropship", "", 1, 20)
		if err != nil {
			log.Printf("[ordercore] list PO by so for price sync order=%s: %v", o.OrderNo, err)
		} else {
			collect(list)
		}
	}
}

func (s *OrderService) Ship(ctx context.Context, tenantID, operatorID, orderID uint64, req dto.ShipRequest, bearerToken string) (*model.Order, error) {
	o, err := s.repos.GetOrder(tenantID, orderID)
	if err != nil {
		return nil, err
	}
	if o.Status == model.StatusClosed {
		return nil, fmt.Errorf("订单已关闭")
	}
	// 本地已标发货但快递助手仍待发货且回传失败：允许换正确单号重试
	retryFailedCallback := false
	if o.ShipStatus == model.ShipShipped {
		if o.SourceChannel == model.SourceKDZS &&
			strings.EqualFold(strings.TrimSpace(o.PlatformStatus), model.KDZSWaitSend) &&
			orderLatestCallbackFailed(o) {
			retryFailedCallback = true
		} else {
			return nil, fmt.Errorf("订单已发货")
		}
	}
	if o.ShipEntryLocked && !retryFailedCallback {
		reason := o.ShipLockReason
		if reason == "" {
			reason = "当前订单已锁定填单号发货"
		}
		return nil, fmt.Errorf("%s", reason)
	}
	if blocked, reason := ecommerceBlocksFulfillment(o.EcommerceStatus, o.EcommerceStatusText, o.AfterSaleStatus, o.AfterSaleStatusText); blocked {
		return nil, fmt.Errorf("%s", reason)
	}
	if o.AllocType == model.AllocDropship && o.DropshipMode == model.DropshipKDZSFactory {
		return nil, fmt.Errorf("快递助手厂家代发由厂家发货，无需手工填单号")
	}
	if o.SourceChannel == model.SourceKDZS && o.PlatformStatus != model.KDZSWaitSend {
		return nil, fmt.Errorf("仅快递助手「待发货」且自营单可填单号回传")
	}
	if o.AllocType == "" {
		return nil, fmt.Errorf("请先完成分配再发货")
	}
	if strings.TrimSpace(req.ExpressNo) == "" {
		return nil, fmt.Errorf("物流单号不能为空")
	}

	shipNo, err := s.repos.NextShipmentNo(tenantID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	expressNo := strings.TrimSpace(req.ExpressNo)
	sh := &model.OrderShipment{
		TenantID:       tenantID,
		OrderID:        orderID,
		ShipmentNo:     shipNo,
		ExpressCompany: req.ExpressCompany,
		ExpressNo:      expressNo,
		NeedTracking:   true,
		CallbackStatus: model.CallbackPending,
		Remark:         req.Remark,
		ShippedAt:      &now,
	}

	doCallback := req.Callback
	if !doCallback {
		// 默认：电商/小程序订单自动回传
		doCallback = o.SourceChannel == model.SourceKDZS || o.SourceChannel == model.SourceWXMall
	}

	if doCallback {
		msg, cbErr := s.callbackSource(ctx, o, sh, bearerToken)
		if cbErr != nil {
			sh.CallbackStatus = model.CallbackFailed
			sh.CallbackMessage = truncate(cbErr.Error(), 500)
			// 记失败流水，但不把订单标成已发货，避免前端误报成功且无法重试
			if cerr := s.repos.CreateShipment(sh); cerr != nil {
				log.Printf("[ordercore] persist failed ship callback order=%s: %v", o.OrderNo, cerr)
			}
			_ = s.repos.AddStatusLog(&model.OrderStatusLog{
				TenantID:   tenantID,
				OrderID:    orderID,
				FromStatus: o.Status,
				ToStatus:   o.Status,
				Action:     "ship_callback_failed",
				Remark:     truncate(fmt.Sprintf("回传失败 %s %s: %s", req.ExpressCompany, expressNo, cbErr.Error()), 500),
				OperatorID: operatorID,
			})
			// 若此前误标已发货，回退为待发货以便继续回传
			if retryFailedCallback || o.ShipStatus == model.ShipShipped {
				_ = s.repos.UpdateOrderFields(tenantID, orderID, map[string]any{
					"ship_status": model.ShipWaitShip,
					"shipped_at":  nil,
				})
			}
			return nil, fmt.Errorf("回传快递助手失败: %w", cbErr)
		}
		sh.CallbackStatus = model.CallbackSucceeded
		sh.CallbackMessage = truncate(msg, 500)
		sh.CallbackAt = &now
	} else {
		sh.CallbackStatus = model.CallbackSkipped
		sh.CallbackMessage = "未回传来源平台"
	}

	from := o.Status
	fields := map[string]any{
		"ship_status": model.ShipShipped,
		"shipped_at":  now,
	}
	// 回传成功时同步平台态，避免本地已发货而 platform_status 仍停在待发货
	if doCallback && sh.CallbackStatus == model.CallbackSucceeded && o.SourceChannel == model.SourceKDZS {
		fields["platform_status"] = "shipped"
		fields["platform_status_text"] = "已发货"
		fields["ship_entry_locked"] = true
		fields["ship_lock_reason"] = "快递助手已发货"
	}
	err = s.repos.Transaction(func(tx *repo.Repos) error {
		if err := tx.CreateShipment(sh); err != nil {
			return err
		}
		return tx.TransitionOrder(tenantID, orderID, fields, &model.OrderStatusLog{
			FromStatus: from,
			ToStatus:   from, // 履约状态不变
			Action:     "ship",
			Remark:     fmt.Sprintf("发货状态→已发货 %s %s", req.ExpressCompany, expressNo),
			OperatorID: operatorID,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repos.GetOrder(tenantID, orderID)
}

func orderLatestCallbackFailed(o *model.Order) bool {
	if o == nil || len(o.Shipments) == 0 {
		return false
	}
	latest := o.Shipments[0]
	for i := 1; i < len(o.Shipments); i++ {
		if o.Shipments[i].ID > latest.ID {
			latest = o.Shipments[i]
		}
	}
	return latest.CallbackStatus == model.CallbackFailed
}

func (s *OrderService) callbackSource(ctx context.Context, o *model.Order, sh *model.OrderShipment, token string) (string, error) {
	switch o.SourceChannel {
	case model.SourceKDZS:
		if s.storeSync == nil {
			return "", fmt.Errorf("StoreSyncAgent 未配置")
		}
		// 与请求上下文解耦：前端/网关超时取消时，仍尽量把快递助手发货跑完并落库
		cbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 150*time.Second)
		defer cancel()
		res, err := s.storeSync.ShipCallback(cbCtx, token, storesync.ShipCallbackRequest{
			Platform:       o.Platform,
			ShopID:         o.ShopID,
			PlatformTid:    o.PlatformOrderID,
			PlatformSysTid: o.PlatformSysTid,
			ExpressCompany: sh.ExpressCompany,
			ExpressNo:      sh.ExpressNo,
			OrderNo:        o.OrderNo,
			Remark:         sh.Remark,
		})
		if err != nil {
			return "", err
		}
		if res != nil && res.Message != "" {
			return res.Message, nil
		}
		return "已回传 StoreSyncAgent", nil
	case model.SourceWXMall:
		// 预留：微信小程序商城后台接入后实现
		return "", fmt.Errorf("微信小程序商城物流回传接口待开发")
	case model.SourceStore, model.SourceManual:
		return "门店/手工订单无需平台回传", nil
	default:
		return "", fmt.Errorf("未知来源渠道: %s", o.SourceChannel)
	}
}

func expandKDZSTradeStatuses(statuses []string) []string {
	discrete := []string{model.KDZSWaitAudit, model.KDZSWaitSend, "shipped", "completed"}
	out := make([]string, 0, len(statuses)+5)
	seen := map[string]struct{}{}
	wantAllCatchup := false
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			return
		}
		if s == "all" {
			wantAllCatchup = true
			for _, d := range discrete {
				if _, ok := seen[d]; ok {
					continue
				}
				seen[d] = struct{}{}
				out = append(out, d)
			}
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range statuses {
		add(s)
	}
	// 退款/取消单会离开 wait_send 等 Tab，末尾补 ALL 兜底刷新（已见过的单号仍跳过）
	if wantAllCatchup {
		if _, ok := seen["all"]; !ok {
			out = append(out, "all")
		}
	}
	return out
}

func (s *OrderService) SyncFromKDZS(ctx context.Context, tenantID, operatorID uint64, req dto.SyncKDZSRequest, token string) (map[string]int, error) {
	if s.storeSync == nil {
		return nil, fmt.Errorf("StoreSyncAgent 未配置")
	}
	ctx, batch := withDeferDropshipPO(ctx)
	if tid := strings.TrimSpace(req.Tid); tid != "" {
		stats, err := s.syncKDZSByTid(ctx, tenantID, operatorID, tid, req.Platform, token)
		if err == nil {
			s.flushDeferredDropshipPOs(ctx, tenantID, token, batch)
		}
		return stats, err
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}
	statuses := make([]string, 0, 1)
	if len(req.TradeStatuses) > 0 {
		statuses = append(statuses, req.TradeStatuses...)
	} else if strings.TrimSpace(req.TradeStatus) != "" {
		statuses = append(statuses, strings.TrimSpace(req.TradeStatus))
	} else {
		// 按快递助手 Tab 分别拉，末尾 all 兜底退款/取消
		statuses = []string{model.KDZSWaitAudit, model.KDZSWaitSend, "shipped", "completed", "all"}
	}
	statuses = expandKDZSTradeStatuses(statuses)

	startTime, endTime := strings.TrimSpace(req.StartTime), strings.TrimSpace(req.EndTime)
	if startTime == "" || endTime == "" {
		// 「全部」未带时间窗时默认近 30 天，与快递助手列表一致
		now := time.Now()
		endTime = now.Format("2006-01-02") + " 23:59:59"
		startTime = now.AddDate(0, 0, -29).Truncate(24*time.Hour).Format("2006-01-02") + " 00:00:00"
	}

	// 未指定平台：按已授权电商店铺覆盖全部平台（抖店/淘宝等）
	platforms := []string{}
	if p := strings.TrimSpace(req.Platform); p != "" {
		platforms = []string{p}
	} else {
		plats, err := s.storeSync.ListEcommercePlatforms(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("获取电商平台列表失败: %w", err)
		}
		platforms = plats
		if len(platforms) == 0 {
			platforms = []string{"FXG"}
		}
	}

	created, updated, fetched, reportedTotal := 0, 0, 0, 0
	seen := map[string]struct{}{}

	for pi, platform := range platforms {
		for si, status := range statuses {
			if pi > 0 || si > 0 {
				if err := sleepKDZSGap(ctx); err != nil {
					return syncKDZSStats(created, updated, fetched, reportedTotal), err
				}
			}
			// 无页数上限：按接口返回的 total 一直翻到取完
			for page := 1; ; page++ {
				if page > 1 {
					if err := sleepKDZSGap(ctx); err != nil {
						return syncKDZSStats(created, updated, fetched, reportedTotal), err
					}
				}
				result, err := s.storeSync.ListOrders(ctx, token, storesync.OrderQuery{
					Platform:      platform,
					ShopID:        req.ShopID,
					TradeStatus:   status,
					PageNo:        page,
					PageSize:      pageSize,
					StartDateTime: startTime,
					EndDateTime:   endTime,
				})
				if err != nil {
					return syncKDZSStats(created, updated, fetched, reportedTotal), err
				}
				fetched += len(result.Items)
				if page == 1 {
					reportedTotal += result.Total
				}
				for _, t := range result.Items {
					ingest := mapTradeToIngest(t)
					if status != "all" {
						ingest.PlatformStatus = status
						ingest.PlatformStatusText = kdzsPlatformStatusText(status)
					}
					key := ingest.PlatformOrderID
					if key == "" {
						key = ingest.PlatformSysTid
					}
					if key != "" {
						if _, ok := seen[key]; ok {
							continue
						}
						seen[key] = struct{}{}
					}
					_, isNew, err := s.Ingest(ctx, tenantID, operatorID, ingest, token)
					if err != nil {
						// 单笔失败不中断整次定时同步，避免一条撞号拖垮全量任务
						log.Printf("[ordercore] sync kdzs ingest fail platform=%s tid=%s: %v", platform, key, err)
						continue
					}
					if isNew {
						created++
					} else {
						updated++
					}
				}
				if len(result.Items) == 0 {
					break
				}
				if result.Total > 0 && page*pageSize >= result.Total {
					break
				}
				// total 异常为 0 时：本页不足一页即视为结束
				if result.Total <= 0 && len(result.Items) < pageSize {
					break
				}
			}
		}
	}
	s.flushDeferredDropshipPOs(ctx, tenantID, token, batch)
	return syncKDZSStats(created, updated, fetched, reportedTotal), nil
}

func syncKDZSStats(created, updated, fetched, total int) map[string]int {
	return map[string]int{"created": created, "updated": updated, "fetched": fetched, "total": total}
}

func sleepKDZSGap(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(3500 * time.Millisecond):
		return nil
	}
}

func (s *OrderService) syncKDZSByTid(ctx context.Context, tenantID, operatorID uint64, tid, platform, token string) (map[string]int, error) {
	// 按列表态探测：tid 回查常用 ALL/电商态，会把「待推单」误成「待发货」
	probeStatuses := []string{model.KDZSWaitAudit, model.KDZSWaitSend, "shipped", "completed", ""}
	var (
		result        *storesync.OrderListResult
		err           error
		matchedStatus string
	)
	for i, st := range probeStatuses {
		if i > 0 {
			if err := sleepKDZSGap(ctx); err != nil {
				return nil, err
			}
		}
		result, err = s.storeSync.ListOrders(ctx, token, storesync.OrderQuery{
			Platform:    platform,
			Tid:         tid,
			TradeStatus: st,
			PageNo:      1,
			PageSize:    5,
		})
		if err != nil {
			if strings.Contains(err.Error(), "过于频繁") || strings.Contains(err.Error(), "811") {
				_ = sleepKDZSGap(ctx)
				continue
			}
			return nil, err
		}
		if result != nil && len(result.Items) > 0 {
			matchedStatus = st
			break
		}
	}
	if result == nil || len(result.Items) == 0 {
		return map[string]int{"created": 0, "updated": 0, "fetched": 0, "total": 0}, fmt.Errorf("快递助手未找到平台单号 %s", tid)
	}
	created, updated := 0, 0
	for _, t := range result.Items {
		ingest := mapTradeToIngest(t)
		if matchedStatus != "" {
			ingest.PlatformStatus = matchedStatus
			ingest.PlatformStatusText = kdzsPlatformStatusText(matchedStatus)
		} else {
			norm, text := normalizeKDZSPlatformStatus(ingest.PlatformStatus, ingest.PlatformStatusText)
			if norm != "" {
				ingest.PlatformStatus = norm
				ingest.PlatformStatusText = text
			}
		}
		_, isNew, err := s.Ingest(ctx, tenantID, operatorID, ingest, token)
		if err != nil {
			return nil, err
		}
		if isNew {
			created++
		} else {
			updated++
		}
	}
	return map[string]int{"created": created, "updated": updated, "fetched": len(result.Items), "total": result.Total}, nil
}

// EnsureKDZSOrderByPlatformID 本地没有该平台单号时，尝试从快递助手补拉。
func (s *OrderService) EnsureKDZSOrderByPlatformID(ctx context.Context, tenantID, operatorID uint64, platformOrderID, token string) error {
	platformOrderID = strings.TrimSpace(platformOrderID)
	if platformOrderID == "" || s.storeSync == nil || strings.TrimSpace(token) == "" {
		return nil
	}
	if existing, err := s.repos.FindBySourcePlatform(tenantID, model.SourceKDZS, platformOrderID); err == nil && existing != nil {
		return nil
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	_, err := s.SyncFromKDZS(ctx, tenantID, operatorID, dto.SyncKDZSRequest{Tid: platformOrderID}, token)
	return err
}

// RefreshOpenKDZSOrders 按平台单号回查快递助手，刷新未完结订单的状态/售后等信息。
func (s *OrderService) RefreshOpenKDZSOrders(ctx context.Context, tenantID, operatorID uint64, token string, limit int) (int, error) {
	if s.storeSync == nil {
		return 0, fmt.Errorf("StoreSyncAgent 未配置")
	}
	orders, err := s.repos.ListOpenKDZSOrders(tenantID, limit)
	if err != nil {
		return 0, err
	}
	refreshed := 0
	var lastErr error
	for i, o := range orders {
		if i > 0 {
			select {
			case <-ctx.Done():
				return refreshed, ctx.Err()
			case <-time.After(3500 * time.Millisecond):
			}
		}
		tid := o.PlatformOrderID
		if tid == "" {
			tid = o.PlatformSysTid
		}
		if tid == "" {
			continue
		}
		platform := o.Platform
		if platform == "" {
			platform = "FXG"
		}
		result, err := s.storeSync.ListOrders(ctx, token, storesync.OrderQuery{
			Platform: platform,
			Tid:      tid,
			PageNo:   1,
			PageSize: 5,
		})
		if err != nil {
			lastErr = err
			// 限流时再等一轮后继续，避免整批刷挂
			if strings.Contains(err.Error(), "过于频繁") || strings.Contains(err.Error(), "811") {
				select {
				case <-ctx.Done():
					return refreshed, ctx.Err()
				case <-time.After(3500 * time.Millisecond):
				}
			}
			continue
		}
		if result == nil || len(result.Items) == 0 {
			continue
		}
		ingest := mapTradeToIngest(result.Items[0])
		normStatus, normText := normalizeKDZSPlatformStatus(ingest.PlatformStatus, ingest.PlatformStatusText)
		// 取消/退款完成：必须以回查实态为准，不能沿用库内 wait_send
		if closed, _ := ecommerceShouldClose(ingest.EcommerceStatus, ingest.EcommerceStatusText, ingest.AfterSaleStatus); closed ||
			normStatus == "order_cancelled" || strings.Contains(normStatus, "cancel") {
			if normStatus != "" {
				ingest.PlatformStatus = normStatus
				ingest.PlatformStatusText = coalesceStr(normText, kdzsPlatformStatusText(normStatus))
			}
		} else if normStatus == "shipped" || normStatus == "completed" {
			ingest.PlatformStatus = normStatus
			ingest.PlatformStatusText = normText
		} else if o.PlatformStatus == model.KDZSWaitAudit || o.PlatformStatus == model.KDZSWaitSend ||
			o.PlatformStatus == "shipped" || o.PlatformStatus == "completed" {
			ingest.PlatformStatus = o.PlatformStatus
			ingest.PlatformStatusText = o.PlatformStatusText
		} else {
			ingest.PlatformStatus = normStatus
			ingest.PlatformStatusText = normText
		}
		if _, _, err := s.Ingest(ctx, tenantID, operatorID, ingest, token); err != nil {
			lastErr = err
			continue
		}
		refreshed++
	}
	return refreshed, lastErr
}

func (s *OrderService) SyncFromStore(ctx context.Context, tenantID, operatorID uint64, req dto.SyncStoreRequest, token string) (map[string]int, error) {
	if s.storeCore == nil {
		return nil, fmt.Errorf("StoreCore 未配置")
	}
	page := req.Page
	if page <= 0 {
		page = 1
	}
	size := req.Size
	if size <= 0 {
		size = 50
	}
	result, err := s.storeCore.ListSalesOrders(ctx, token, page, size, req.Status)
	if err != nil {
		return nil, err
	}
	created, updated := 0, 0
	for _, so := range result.List {
		ingest := mapStoreSalesToIngest(so)
		_, isNew, err := s.Ingest(ctx, tenantID, operatorID, ingest, token)
		if err != nil {
			return nil, err
		}
		if isNew {
			created++
		} else {
			updated++
		}
	}
	return map[string]int{"created": created, "updated": updated, "fetched": len(result.List), "total": int(result.Total)}, nil
}

func (s *OrderService) ListKDZSFactories(ctx context.Context, token, platform string, pageNo, pageSize int) (*storesync.FactoryListResult, error) {
	if s.storeSync == nil {
		return nil, fmt.Errorf("StoreSyncAgent 未配置")
	}
	return s.storeSync.ListFactories(ctx, token, platform, pageNo, pageSize)
}

// DecryptOrders 调用 StoreSyncAgent 解密收件信息，并回写订单地址。
func (s *OrderService) DecryptOrders(ctx context.Context, tenantID uint64, orderIDs []uint64, token string) ([]model.Order, error) {
	if s.storeSync == nil {
		return nil, fmt.Errorf("StoreSyncAgent 未配置")
	}
	if len(orderIDs) == 0 {
		return nil, fmt.Errorf("orderIds 必填")
	}
	seen := map[uint64]struct{}{}
	out := make([]model.Order, 0, len(orderIDs))
	var firstErr error
	for _, id := range orderIDs {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		o, err := s.decryptOneOrder(ctx, tenantID, id, token)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("订单 %d: %w", id, err)
			}
			continue
		}
		out = append(out, *o)
	}
	if len(out) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("没有可解密的电商订单")
	}
	return out, nil
}

func (s *OrderService) decryptOneOrder(ctx context.Context, tenantID, orderID uint64, token string) (*model.Order, error) {
	o, err := s.repos.GetOrder(tenantID, orderID)
	if err != nil {
		return nil, err
	}
	// 库内已是明文则直接返回，避免重复调快递助手解密
	if orderHasPlainReceiver(o) {
		return o, nil
	}
	if o.SourceChannel != model.SourceKDZS {
		return nil, fmt.Errorf("仅支持电商（快递助手）订单")
	}
	platform := strings.TrimSpace(o.Platform)
	sysTid := strings.TrimSpace(o.PlatformSysTid)
	if platform == "" || sysTid == "" {
		return nil, fmt.Errorf("缺少平台或系统单号")
	}

	item, err := s.decryptKDZSReceiver(ctx, token, platform, o.PlatformStatus, sysTid)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(item.ReceiverName)
	mobile := strings.TrimSpace(item.ReceiverMobile)
	addrDetail := strings.TrimSpace(item.ReceiverAddress)
	full := strings.TrimSpace(item.FormattedReceiver)
	if full == "" {
		full = strings.TrimSpace(strings.Join([]string{name, mobile, addrDetail}, " "))
	}
	if full == "" {
		return nil, fmt.Errorf("解密结果为空")
	}

	err = s.repos.Transaction(func(tx *repo.Repos) error {
		if err := tx.UpdateOrderFields(tenantID, orderID, map[string]any{
			"buyer_name":  name,
			"buyer_phone": mobile,
		}); err != nil {
			return err
		}
		return tx.UpsertAddress(&model.OrderAddress{
			TenantID: tenantID,
			OrderID:  orderID,
			Name:     name,
			Phone:    mobile,
			Address:  addrDetail,
			FullText: full,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repos.GetOrder(tenantID, orderID)
}

func (s *OrderService) decryptKDZSReceiver(ctx context.Context, token, platform, preferredStatus, sysTid string) (*storesync.DecryptOrderItem, error) {
	var lastErr error
	for _, st := range decryptTradeStatuses(preferredStatus) {
		res, err := s.storeSync.DecryptOrders(ctx, token, storesync.DecryptOrdersRequest{
			Platform:    platform,
			TradeStatus: st,
			SysTids:     []string{sysTid},
		})
		if err != nil {
			lastErr = err
			continue
		}
		if res == nil || len(res.Items) == 0 {
			lastErr = fmt.Errorf("解密无结果")
			continue
		}
		item := res.Items[0]
		if strings.TrimSpace(item.FormattedReceiver) == "" &&
			strings.TrimSpace(item.ReceiverName) == "" &&
			strings.TrimSpace(item.ReceiverMobile) == "" {
			lastErr = fmt.Errorf("解密结果为空")
			continue
		}
		return &item, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("解密失败")
}

func decryptTradeStatuses(preferred string) []string {
	pref := strings.ToLower(strings.TrimSpace(preferred))
	base := []string{"wait_send", "wait_audit", "shipped", "completed"}
	out := make([]string, 0, len(base)+1)
	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(pref)
	for _, s := range base {
		add(s)
	}
	return out
}

// ---- bindings ----

func (s *OrderService) ListBindings(tenantID uint64) ([]model.SupplierSourceBinding, error) {
	return s.repos.ListBindings(tenantID)
}

func (s *OrderService) CreateBinding(tenantID uint64, req dto.BindingRequest) (*model.SupplierSourceBinding, error) {
	channel := req.SourceChannel
	if channel == "" {
		channel = model.SourceKDZS
	}
	b := &model.SupplierSourceBinding{
		TenantID:            tenantID,
		SupplierID:          req.SupplierID,
		SupplierCode:        req.SupplierCode,
		SupplierName:        req.SupplierName,
		SourceChannel:       channel,
		ExternalFactoryID:   req.ExternalFactoryID,
		ExternalFactoryName: req.ExternalFactoryName,
		Platform:            req.Platform,
		Remark:              req.Remark,
		Status:              1,
	}
	if err := s.repos.CreateBinding(b); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *OrderService) UpdateBinding(tenantID, id uint64, req dto.BindingRequest) (*model.SupplierSourceBinding, error) {
	b, err := s.repos.GetBinding(tenantID, id)
	if err != nil {
		return nil, err
	}
	if req.SupplierID > 0 {
		b.SupplierID = req.SupplierID
	}
	if req.SupplierCode != "" {
		b.SupplierCode = req.SupplierCode
	}
	if req.SupplierName != "" {
		b.SupplierName = req.SupplierName
	}
	if req.SourceChannel != "" {
		b.SourceChannel = req.SourceChannel
	}
	if req.ExternalFactoryID != "" {
		b.ExternalFactoryID = req.ExternalFactoryID
	}
	if req.ExternalFactoryName != "" {
		b.ExternalFactoryName = req.ExternalFactoryName
	}
	if req.Platform != "" {
		b.Platform = req.Platform
	}
	b.Remark = req.Remark
	if err := s.repos.UpdateBinding(b); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *OrderService) DeleteBinding(tenantID, id uint64) error {
	return s.repos.DeleteBinding(tenantID, id)
}

// resolveBoundSupplier 按快递助手厂家 ID（或名称）查找已启用的供应商绑定。
func (s *OrderService) resolveBoundSupplier(tenantID uint64, factoryID, factoryName string) (uint64, string) {
	factoryID = strings.TrimSpace(factoryID)
	factoryName = strings.TrimSpace(factoryName)
	if factoryID != "" {
		if b, err := s.repos.FindBindingByFactory(tenantID, model.SourceKDZS, factoryID); err == nil && b != nil && b.SupplierID > 0 {
			name := strings.TrimSpace(b.SupplierName)
			if name == "" {
				name = b.ExternalFactoryName
			}
			return b.SupplierID, name
		}
	}
	if factoryName != "" {
		if b, err := s.repos.FindBindingByFactoryName(tenantID, model.SourceKDZS, factoryName); err == nil && b != nil && b.SupplierID > 0 {
			name := strings.TrimSpace(b.SupplierName)
			if name == "" {
				name = b.ExternalFactoryName
			}
			return b.SupplierID, name
		}
	}
	return 0, ""
}

// lookupDropshipPOSupplier 按代发采购单号查供应商（用于同步时恢复被 self_print 冲掉的 OSMS 代发）。
func (s *OrderService) lookupDropshipPOSupplier(ctx context.Context, bearerToken, poNo string) (uint64, string, bool) {
	poNo = strings.TrimSpace(poNo)
	if poNo == "" || s.supply == nil || strings.TrimSpace(bearerToken) == "" {
		return 0, "", false
	}
	list, _, err := s.supply.ListPurchaseOrdersEx(ctx, bearerToken, 0, "dropship", poNo, 1, 20)
	if err != nil {
		return 0, "", false
	}
	for _, it := range list {
		if strings.TrimSpace(it.PoNo) != poNo {
			continue
		}
		if it.FulfillmentType != "" && it.FulfillmentType != "dropship" {
			continue
		}
		if it.SupplierID == 0 {
			continue
		}
		name := strings.TrimSpace(it.SupplierName)
		if name == "" {
			if detail, gerr := s.supply.GetPurchaseOrder(ctx, bearerToken, it.ID); gerr == nil && detail != nil {
				name = strings.TrimSpace(detail.SupplierName)
			}
		}
		return it.SupplierID, name, true
	}
	return 0, "", false
}

// ---- helpers ----

func mapAddress(tenantID, orderID uint64, in *dto.AddressInput) *model.OrderAddress {
	if in == nil {
		return nil
	}
	full := in.FullText
	if full == "" {
		full = strings.TrimSpace(strings.Join([]string{in.Province, in.City, in.District, in.Address}, " "))
	}
	return &model.OrderAddress{
		TenantID: tenantID,
		OrderID:  orderID,
		Name:     in.Name,
		Phone:    in.Phone,
		Province: in.Province,
		City:     in.City,
		District: in.District,
		Address:  in.Address,
		FullText: full,
	}
}

func looksMaskedText(s string) bool {
	return strings.Contains(s, "*") || strings.Contains(s, "＊")
}

// orderHasPlainReceiver 订单库内收件信息已是明文（曾解密持久化）。
func orderHasPlainReceiver(o *model.Order) bool {
	if o == nil {
		return false
	}
	name := strings.TrimSpace(o.BuyerName)
	phone := strings.TrimSpace(o.BuyerPhone)
	full, detail := "", ""
	if o.Address != nil {
		if n := strings.TrimSpace(o.Address.Name); n != "" {
			name = n
		}
		if p := strings.TrimSpace(o.Address.Phone); p != "" {
			phone = p
		}
		full = strings.TrimSpace(o.Address.FullText)
		detail = strings.TrimSpace(o.Address.Address)
	}
	joined := strings.TrimSpace(strings.Join([]string{name, phone, full, detail}, " "))
	if joined == "" {
		return false
	}
	return !looksMaskedText(name) && !looksMaskedText(phone) && !looksMaskedText(full) && !looksMaskedText(detail)
}

func ingestReceiverMasked(req dto.IngestOrderRequest) bool {
	if looksMaskedText(req.BuyerName) || looksMaskedText(req.BuyerPhone) {
		return true
	}
	if req.Address == nil {
		return false
	}
	a := req.Address
	return looksMaskedText(a.Name) || looksMaskedText(a.Phone) ||
		looksMaskedText(a.FullText) || looksMaskedText(a.Address)
}

// preserveOSMSSKUFields 同步更新明细时保留 OSMS 侧已维护的 skuId/商家编码（不从快递助手 outerId 覆盖）。
func preserveOSMSSKUFields(oldItems, newItems []model.OrderItem) {
	if len(oldItems) == 0 || len(newItems) == 0 {
		return
	}
	byLine := map[int]model.OrderItem{}
	for _, it := range oldItems {
		byLine[it.LineNo] = it
	}
	for i := range newItems {
		old, ok := byLine[newItems[i].LineNo]
		if !ok && i < len(oldItems) {
			old = oldItems[i]
			ok = true
		}
		if !ok {
			continue
		}
		if newItems[i].SkuID == 0 && old.SkuID > 0 {
			newItems[i].SkuID = old.SkuID
		}
		if strings.TrimSpace(newItems[i].SkuCode) == "" && strings.TrimSpace(old.SkuCode) != "" {
			newItems[i].SkuCode = old.SkuCode
		}
	}
}

// enrichItemsWithProductSKU 用商家编码匹配 ProductCore SKU；失败不阻断同步。
func (s *OrderService) enrichItemsWithProductSKU(ctx context.Context, bearerToken string, items []dto.OrderItemInput) {
	if s.product == nil || len(items) == 0 {
		return
	}
	cache := map[string]uint64{}
	for i := range items {
		if items[i].SkuID > 0 {
			continue
		}
		code := strings.TrimSpace(items[i].SkuCode)
		if code == "" {
			continue
		}
		if id, ok := cache[code]; ok {
			items[i].SkuID = id
			continue
		}
		id, err := s.product.ResolveSkuIDByCode(ctx, bearerToken, code)
		if err != nil || id == 0 {
			cache[code] = 0
			continue
		}
		cache[code] = id
		items[i].SkuID = id
	}
}

func mapItems(tenantID, orderID uint64, items []dto.OrderItemInput) []model.OrderItem {
	out := make([]model.OrderItem, 0, len(items))
	for i, it := range items {
		qty := it.Quantity
		if qty <= 0 {
			qty = 1
		}
		out = append(out, model.OrderItem{
			TenantID:       tenantID,
			OrderID:        orderID,
			LineNo:         i + 1,
			SkuID:          it.SkuID,
			SkuCode:        it.SkuCode,
			PlatformSkuID:  it.PlatformSkuID,
			PlatformItemID: it.PlatformItemID,
			ProductName:    it.ProductName,
			SkuSpecs:       it.SkuSpecs,
			PicURL:         it.PicURL,
			Quantity:       qty,
			Price:          it.Price,
			TotalAmount:    it.Price * float64(qty),
		})
	}
	return out
}

func mapTradeToIngest(t storesync.TradeOrder) dto.IngestOrderRequest {
	platformOrderID := ""
	if len(t.Tids) > 0 {
		platformOrderID = t.Tids[0]
	}
	sysTid := ""
	if len(t.SysTids) > 0 {
		sysTid = t.SysTids[0]
	}
	if platformOrderID == "" {
		platformOrderID = sysTid
	}
	raw, _ := json.Marshal(t)
	items := make([]dto.OrderItemInput, 0, len(t.Goods))
	for _, g := range t.Goods {
		// 商家编码不从快递助手 outerId 同步，由 OSMS 侧自行填写/绑定
		items = append(items, dto.OrderItemInput{
			PlatformSkuID:  g.SkuID,
			PlatformItemID: g.ItemID,
			ProductName:    g.Title,
			SkuSpecs:       g.SkuName,
			PicURL:         g.PicURL,
			Quantity:       g.Num,
			Price:          g.Price,
		})
	}
	addrFull := t.FormattedReceiver
	if addrFull == "" {
		addrFull = t.ReceiverAddress
	}
	kdzsStatus, kdzsText := normalizeKDZSPlatformStatus(t.TradeStatus, t.StatusText)
	ecomStatus := t.PlatformOrderStatus
	ecomText := t.PlatformOrderStatusText
	// 若 tradeStatus 实际是电商状态码，落入电商状态字段
	if ecomStatus == "" && t.TradeStatus != "" && kdzsStatus != strings.ToLower(strings.TrimSpace(t.TradeStatus)) {
		ecomStatus = t.TradeStatus
	}
	if ecomText == "" && ecomStatus != "" {
		ecomText = ecommerceStatusText(ecomStatus)
	}
	if kdzsText == "" {
		kdzsText = kdzsPlatformStatusText(kdzsStatus)
	}
	// 实付/订单总金额=快递助手 payment（商家实收=实付价+平台优惠，含邮费）；邮费单独落 freight 备查
	freight := t.PostFee
	if freight < 0 {
		freight = 0
	}
	orderTotal := roundMoney(t.Payment)
	logistics := make([]dto.LogisticsInput, 0, len(t.Logistics))
	for _, lg := range t.Logistics {
		no := strings.TrimSpace(lg.TrackingNo)
		if no == "" {
			continue
		}
		logistics = append(logistics, dto.LogisticsInput{
			ExpressCompany: firstNonEmpty(lg.CompanyName, lg.Company),
			ExpressCode:    lg.Company,
			ExpressNo:      no,
			ShippedAt:      firstNonEmpty(lg.ShipTime, t.ShippedAt),
		})
	}
	return dto.IngestOrderRequest{
		SourceChannel:       model.SourceKDZS,
		Platform:            t.Platform,
		PlatformOrderID:     platformOrderID,
		PlatformSysTid:      sysTid,
		ShopID:              t.ShopID,
		ShopName:            t.ShopName,
		Status:              model.StatusPendingAlloc,
		PlatformStatus:      kdzsStatus,
		PlatformStatusText:  kdzsText,
		EcommerceStatus:     ecomStatus,
		EcommerceStatusText: ecomText,
		AfterSaleStatus:     t.AfterSaleStatus,
		AfterSaleStatusText: t.AfterSaleStatusText,
		AgentType:           t.AgentType,
		BuyerNick:           t.BuyerNick,
		BuyerName:           t.ReceiverName,
		BuyerPhone:          t.ReceiverMobile,
		TotalAmount:         orderTotal,
		PayAmount:           orderTotal,
		FreightAmount:       freight,
		PayStatus:           "paid",
		PayTime:             t.PayTime,
		OrderTime:           t.CreateTime,
		Remark:              t.BuyerMemo,
		SellerRemark:        t.SellerMemo,
		SellerFlag:          t.SellerFlag,
		FenFaRemark:         t.FenFaMemo,
		PrinterRemark:       t.PrinterMemo,
		FactoryID:           t.FactoryID,
		FactoryName:         t.FactoryName,
		ExpressCompany:      t.ExpressCompany,
		ExpressCode:         t.ExpressCode,
		ExpressNo:           t.ExpressNo,
		ShippedAt:           t.ShippedAt,
		Logistics:           logistics,
		RawPayload:          string(raw),
		Address: &dto.AddressInput{
			Name:     t.ReceiverName,
			Phone:    t.ReceiverMobile,
			Address:  t.ReceiverAddress,
			FullText: addrFull,
		},
		Items: items,
	}
}

type kdzsIngestHint struct {
	Status             string
	ShipStatus         string
	PlatformStatus     string
	PlatformStatusText string
	AllocType          string
	DropshipMode       string
	AgentType          int
	ShipEntryLocked    bool
	ShipLockReason     string
	ApplySyncAlloc     bool // 按快递助手实态自动写入履约分配
	ClearAlloc         bool // 回到待推单等：清空订单中心分配
	LogRemark          string
}

func resolveKDZSAgentType(agentType int, factoryID, factoryName string) int {
	if agentType == model.AgentTypeFactory {
		return model.AgentTypeFactory
	}
	if agentType == model.AgentTypeSelf {
		return model.AgentTypeSelf
	}
	// 仅厂家名称可作兜底；裸 factoryId 可能是商家自身 factoryUserId，不可当作代发
	if strings.TrimSpace(factoryName) != "" {
		return model.AgentTypeFactory
	}
	_ = factoryID
	return model.AgentTypeSelf
}

func normalizeKDZSPlatformStatus(status, statusText string) (string, string) {
	st := strings.ToLower(strings.TrimSpace(status))
	switch st {
	case model.KDZSWaitAudit, model.KDZSWaitSend, "shipped", "completed":
		text := strings.TrimSpace(statusText)
		if text == "" {
			text = kdzsPlatformStatusText(st)
		}
		return st, text
	case "order_cancelled", "cancelled", "trade_closed", "closed", "cancel":
		text := strings.TrimSpace(statusText)
		if text == "" {
			text = "已取消"
		}
		return "order_cancelled", text
	}
	text := strings.TrimSpace(statusText)
	switch text {
	case "待推单":
		return model.KDZSWaitAudit, text
	case "待发货":
		return model.KDZSWaitSend, text
	case "已发货":
		return "shipped", text
	case "交易完成", "已完成":
		return "completed", text
	case "已取消", "交易关闭", "订单取消":
		return "order_cancelled", text
	}
	return st, text
}

func deriveKDZSIngest(channel string, req dto.IngestOrderRequest) kdzsIngestHint {
	h := kdzsIngestHint{
		Status:    req.Status,
		LogRemark: "同步入库: " + channel,
	}
	if h.Status == "" {
		h.Status = model.StatusPendingAlloc
		h.ShipStatus = model.ShipWaitShip
	}
	if channel != model.SourceKDZS {
		h.ShipEntryLocked = false
		normalizeNonKDZSHint(&h)
		return h
	}

	// 归一快递助手列表态（避免电商 ORDER_PAID 等污染 platformStatus）
	platformStatus, platformText := normalizeKDZSPlatformStatus(req.PlatformStatus, req.PlatformStatusText)
	h.PlatformStatus = platformStatus
	h.PlatformStatusText = platformText

	agentType := resolveKDZSAgentType(req.AgentType, req.FactoryID, req.FactoryName)
	h.AgentType = agentType
	isFactory := agentType == model.AgentTypeFactory

	// 取消/关闭优先于列表态（避免 order_cancelled 落入 default→待分配）
	if closed, reason := ecommerceShouldClose(req.EcommerceStatus, req.EcommerceStatusText, req.AfterSaleStatus); closed {
		return applyClosedHint(&h, reason)
	}
	if platformStatus == "order_cancelled" || strings.Contains(platformStatus, "cancel") {
		return applyClosedHint(&h, "快递助手/电商订单已取消")
	}

	switch strings.ToLower(strings.TrimSpace(platformStatus)) {
	case model.KDZSWaitSend:
		h.ShipStatus = model.ShipWaitShip
		if isFactory {
			h.Status = model.StatusAllocated
			h.AllocType = model.AllocDropship
			h.DropshipMode = model.DropshipKDZSFactory
			h.ApplySyncAlloc = true
			h.ShipEntryLocked = true
			h.ShipLockReason = "快递助手已分配厂家代发，由厂家发货，无需干预"
			h.LogRemark = "同步待发货代发单→已分配+待发货并锁定填单号"
		} else {
			// 自营待发货：快递助手已推单/进入待发货，履约=已分配(自营)
			h.Status = model.StatusAllocated
			h.AllocType = model.AllocSelfShip
			h.DropshipMode = ""
			h.ApplySyncAlloc = true
			h.ClearAlloc = false
			h.ShipEntryLocked = false
			h.ShipLockReason = ""
			h.LogRemark = "同步待发货自营单→已分配+待发货"
		}
	case "shipped":
		h.Status = model.StatusAllocated
		h.ShipStatus = model.ShipShipped
		h.ApplySyncAlloc = true
		h.ShipEntryLocked = true
		h.ShipLockReason = "快递助手已发货"
		if isFactory {
			h.AllocType = model.AllocDropship
			h.DropshipMode = model.DropshipKDZSFactory
			h.LogRemark = "同步已发货代发单→已分配+已发货"
		} else {
			h.AllocType = model.AllocSelfShip
			h.DropshipMode = ""
			h.LogRemark = "同步已发货自营单→已分配+已发货"
		}
	case "completed":
		h.Status = model.StatusCompleted
		h.ShipStatus = model.ShipShipped
		h.ApplySyncAlloc = true
		h.ShipEntryLocked = true
		h.ShipLockReason = "快递助手交易完成"
		if isFactory {
			h.AllocType = model.AllocDropship
			h.DropshipMode = model.DropshipKDZSFactory
		} else {
			h.AllocType = model.AllocSelfShip
			h.DropshipMode = ""
		}
		h.LogRemark = "同步交易完成→已完成+已发货"
	case model.KDZSWaitAudit:
		// 待推单（含撤单/退审回退）：履约侧一律恢复待分配。
		// 快递助手撤单后常仍挂厂家(agentType=2/factory*)，不能再视为「已分配」。
		h.ShipStatus = model.ShipWaitShip
		h.Status = model.StatusPendingAlloc
		h.AllocType = ""
		h.DropshipMode = ""
		h.ApplySyncAlloc = false
		h.ClearAlloc = true
		h.AgentType = model.AgentTypeSelf
		h.ShipEntryLocked = true
		h.ShipLockReason = "快递助手待推单，请先分配；仅自营待发货可填单号"
		h.LogRemark = "同步待推单→清空分配，恢复待分配"
		_ = isFactory
	default:
		h.ShipStatus = model.ShipWaitShip
		if isFactory {
			h.Status = model.StatusAllocated
			h.AllocType = model.AllocDropship
			h.DropshipMode = model.DropshipKDZSFactory
			h.ApplySyncAlloc = true
			h.ShipEntryLocked = true
			h.ShipLockReason = "快递助手厂家代发，无需干预"
			h.LogRemark = "同步代发单→已分配"
		} else {
			h.ShipEntryLocked, h.ShipLockReason = computeShipLock(channel, platformStatus, agentType, "")
		}
	}

	// 电商订单/售后状态影响履约：关闭或锁定（再判一次，覆盖列表态）
	if closed, reason := ecommerceShouldClose(req.EcommerceStatus, req.EcommerceStatusText, req.AfterSaleStatus); closed {
		return applyClosedHint(&h, reason)
	}
	if blocked, reason := ecommerceBlocksFulfillment(req.EcommerceStatus, req.EcommerceStatusText, req.AfterSaleStatus, req.AfterSaleStatusText); blocked {
		h.ShipEntryLocked = true
		h.ShipLockReason = reason
		if h.LogRemark == "" || strings.HasPrefix(h.LogRemark, "同步") {
			h.LogRemark = h.LogRemark + "；" + reason
		}
	}
	return h
}

func applyClosedHint(h *kdzsIngestHint, reason string) kdzsIngestHint {
	h.Status = model.StatusClosed
	h.AllocType = ""
	h.DropshipMode = ""
	h.ApplySyncAlloc = false
	h.ClearAlloc = true
	h.ShipStatus = "" // 关闭时不改发货历史
	h.ShipEntryLocked = true
	h.ShipLockReason = reason
	h.LogRemark = reason
	return *h
}

// normalizeNonKDZSHint 将门店/手工等来源的旧单一 status 归一为履约+发货
func normalizeNonKDZSHint(h *kdzsIngestHint) {
	switch h.Status {
	case model.StatusPendingShip, "":
		h.Status = model.StatusPendingAlloc
		if h.ShipStatus == "" {
			h.ShipStatus = model.ShipWaitShip
		}
	case model.StatusShipped, model.StatusPartialShip:
		h.Status = model.StatusAllocated
		h.ShipStatus = model.ShipShipped
	case model.StatusCompleted:
		if h.ShipStatus == "" {
			h.ShipStatus = model.ShipShipped
		}
	case model.StatusClosed:
		// 关闭不强制改发货
	case model.StatusPendingPayment, model.StatusPendingAlloc, model.StatusAllocated, model.StatusPurchasing:
		if h.ShipStatus == "" {
			h.ShipStatus = model.ShipWaitShip
		}
	}
}

// shouldSyncKDZSAgent 订单是否已挂到快递助手、分配时需同步「自己打单/推厂家」。
// 电商单（kdzs）始终同步；手工单等在建单时已落 DFHAND/sysTid 的也同步。
func shouldSyncKDZSAgent(o *model.Order) bool {
	if o == nil {
		return false
	}
	if o.SourceChannel == model.SourceKDZS {
		return true
	}
	if strings.TrimSpace(o.PlatformSysTid) != "" {
		return true
	}
	platform := strings.ToUpper(strings.TrimSpace(o.Platform))
	return platform == "DFHAND" && strings.TrimSpace(o.PlatformOrderID) != ""
}

func computeShipLock(channel, platformStatus string, agentType int, dropshipMode string) (bool, string) {
	if channel != model.SourceKDZS {
		return false, ""
	}
	if agentType == model.AgentTypeFactory || dropshipMode == model.DropshipKDZSFactory {
		return true, "快递助手厂家代发，填单号入口已锁定"
	}
	if strings.ToLower(strings.TrimSpace(platformStatus)) != model.KDZSWaitSend {
		return true, "仅快递助手「待发货」自营单可填单号发货"
	}
	return false, ""
}

func ecommerceShouldClose(ecomStatus, ecomText, afterSale string) (bool, string) {
	code := strings.ToUpper(strings.TrimSpace(ecomStatus))
	as := strings.ToUpper(strings.TrimSpace(afterSale))
	text := ecomText
	switch code {
	case "TRADE_CLOSED", "ORDER_CANCEL", "ORDER_CANCELLED", "CANCEL", "CANCELLED", "CLOSED",
		"REFUND_SUCCESS", "REFUNDED", "SUCCESS_REFUND", "REFUND_MONEY_FINISH", "REFUND_MONEY_SUCCESS",
		"TRADE_CLOSED_BY_TAOBAO", "TRADE_CLOSED_BY_USER":
		return true, "电商订单已关闭/取消/退款完成"
	}
	if strings.Contains(code, "CANCEL") || strings.HasSuffix(code, "_CLOSED") {
		return true, "电商订单已关闭/取消"
	}
	if strings.Contains(code, "REFUND") && (strings.Contains(code, "SUCCESS") || strings.Contains(code, "FINISH") || strings.Contains(code, "DONE")) {
		return true, "电商订单退款完成"
	}
	switch as {
	case "REFUND_SUCCESS", "REFUND_MONEY_FINISH", "REFUND_MONEY_SUCCESS", "REFUNDED", "SUCCESS_REFUND":
		return true, "售后退款完成，订单关闭"
	}
	if strings.Contains(text, "退款成功") || strings.Contains(text, "退款完成") ||
		strings.Contains(text, "交易关闭") || strings.Contains(text, "订单取消") ||
		strings.Contains(text, "已取消") || strings.EqualFold(text, "ORDER_CANCELLED") {
		return true, "电商订单状态：" + text
	}
	return false, ""
}

func ecommerceBlocksFulfillment(ecomStatus, ecomText, afterSale, afterSaleText string) (bool, string) {
	if closed, reason := ecommerceShouldClose(ecomStatus, ecomText, afterSale); closed {
		return true, reason
	}
	as := strings.ToUpper(strings.TrimSpace(afterSale))
	switch as {
	case "WAIT_SELLER_AGREE", "WAIT_BUYER_RETURN_ITEM", "WAIT_SELLER_CONFIRM_RECEIVE",
		"WAIT_BUYER_MODIFY", "WAIT_SEND_EXCHANGE_ITEM", "WAIT_RECEIVE_EXCHANGE_ITEM":
		label := afterSaleText
		if label == "" {
			label = as
		}
		return true, "存在进行中售后（" + label + "），暂停分配/发货"
	}
	code := strings.ToUpper(strings.TrimSpace(ecomStatus))
	switch code {
	case "REFUNDING", "REFUND", "IN_REFUND", "PARTIAL_REFUNDING", "WAIT_BUYER_PAY", "UNPAID":
		label := ecomText
		if label == "" {
			label = ecommerceStatusText(code)
		}
		return true, "电商订单状态不允许履约（" + label + "）"
	}
	if strings.Contains(ecomText, "退款中") || strings.Contains(ecomText, "申请退款") {
		return true, "电商订单状态不允许履约（" + ecomText + "）"
	}
	if strings.Contains(afterSaleText, "等待卖家同意") || strings.Contains(afterSaleText, "申请退款") {
		return true, "存在进行中售后（" + afterSaleText + "），暂停分配/发货"
	}
	return false, ""
}

func ecommerceStatusText(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "WAIT_BUYER_PAY", "UNPAID":
		return "待付款"
	case "ORDER_PAID", "WAIT_SELLER_SEND_GOODS", "WAIT_SELLER_STOCK_OUT", "PAID":
		return "待发货"
	case "SELLER_CONSIGNED", "ORDER_SHIPPED", "WAIT_BUYER_CONFIRM_GOODS", "SHIPPED":
		return "已发货"
	case "ORDER_COMPLETED", "TRADE_FINISHED", "COMPLETED", "FINISHED", "SUCCESS":
		return "交易完成"
	case "TRADE_CLOSED", "ORDER_CANCEL", "ORDER_CANCELLED", "CANCEL", "CANCELLED", "CLOSED":
		return "交易关闭"
	case "REFUNDING", "REFUND", "IN_REFUND":
		return "退款中"
	case "REFUND_SUCCESS", "REFUNDED", "SUCCESS_REFUND", "REFUND_MONEY_FINISH":
		return "退款成功"
	case "WAIT_SELLER_AGREE":
		return "申请退款"
	case "PARTIAL_REFUNDING":
		return "部分退款中"
	default:
		return code
	}
}

func kdzsPlatformStatusText(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "wait_audit":
		return "待推单"
	case "wait_send":
		return "待发货"
	case "shipped", "seller_consigned", "order_shipped":
		return "已发货"
	case "completed", "trade_finished":
		return "交易完成"
	case "order_cancelled", "cancelled", "trade_closed", "closed":
		return "已取消"
	default:
		return status
	}
}

func coalesceStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// autoSyncDropshipLogistics 订单同步写入物流后，自动推到关联代发采购单（失败只记日志，不阻断同步）。
func (s *OrderService) autoSyncDropshipLogistics(ctx context.Context, o *model.Order, req dto.IngestOrderRequest, bearerToken string) {
	if o == nil || s.supply == nil || strings.TrimSpace(bearerToken) == "" {
		return
	}
	if o.AllocType != model.AllocDropship {
		return
	}
	poNo := strings.TrimSpace(o.PurchaseOrderID)
	if poNo == "" {
		return
	}
	if !ingestHasLogistics(req) && !orderHasExpressShipments(o) && o.ShipStatus != model.ShipShipped {
		return
	}
	list, _, err := s.supply.ListPurchaseOrdersEx(ctx, bearerToken, 0, "dropship", poNo, 1, 20)
	if err != nil {
		log.Printf("[ordercore] auto sync logistics list PO=%s order=%s: %v", poNo, o.OrderNo, err)
		return
	}
	var poID uint64
	for _, it := range list {
		if strings.TrimSpace(it.PoNo) == poNo {
			poID = it.ID
			break
		}
	}
	if poID == 0 {
		return
	}
	if err := s.supply.SyncShipmentsFromOrders(ctx, bearerToken, poID, o.ID); err != nil {
		log.Printf("[ordercore] auto sync logistics PO=%s order=%s: %v", poNo, o.OrderNo, err)
		return
	}
	log.Printf("[ordercore] auto synced logistics PO=%s order=%s", poNo, o.OrderNo)
}

// dedupeMergeShipFenFa 合单发货（同运单号）时，分发备注只保留在销售单 ID 最小的一单，其余清空。
// 快递助手合单常把同一备注复制到每单；同步后需去重，避免采购价翻倍、界面重复显示。
func (s *OrderService) dedupeMergeShipFenFa(ctx context.Context, tenantID uint64, o *model.Order) *model.Order {
	if o == nil {
		return o
	}
	fen := strings.TrimSpace(o.FenFaRemark)
	if fen == "" {
		return o
	}
	expressNos := map[string]struct{}{}
	for _, sh := range o.Shipments {
		no := strings.TrimSpace(sh.ExpressNo)
		if no != "" {
			expressNos[no] = struct{}{}
		}
	}
	if len(expressNos) == 0 {
		return o
	}
	siblingIDs := map[uint64]struct{}{}
	for no := range expressNos {
		ids, err := s.repos.ListOrderIDsByExpressNo(tenantID, no)
		if err != nil {
			log.Printf("[ordercore] list merge-ship siblings express=%s: %v", no, err)
			continue
		}
		for _, id := range ids {
			siblingIDs[id] = struct{}{}
		}
	}
	if len(siblingIDs) < 2 {
		return o
	}
	ids := make([]uint64, 0, len(siblingIDs))
	for id := range siblingIDs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	// 仅处理分发备注与本单相同（或非空同文）的合单成员
	type pair struct {
		id  uint64
		fen string
	}
	var same []pair
	for _, id := range ids {
		cur, err := s.repos.GetOrder(tenantID, id)
		if err != nil || cur == nil {
			continue
		}
		cf := strings.TrimSpace(cur.FenFaRemark)
		if cf == "" {
			continue
		}
		if cf == fen {
			same = append(same, pair{id: id, fen: cf})
		}
	}
	if len(same) < 2 {
		return o
	}
	primaryID := same[0].id
	for _, p := range same {
		if p.id < primaryID {
			primaryID = p.id
		}
	}
	cleared := 0
	for _, p := range same {
		if p.id == primaryID {
			continue
		}
		if err := s.repos.UpdateOrderFields(tenantID, p.id, map[string]any{"fen_fa_remark": ""}); err != nil {
			log.Printf("[ordercore] clear merge-ship fenfa order=%d: %v", p.id, err)
			continue
		}
		st := ""
		if cur, gerr := s.repos.GetOrder(tenantID, p.id); gerr == nil && cur != nil {
			st = cur.Status
		}
		_ = s.repos.AddStatusLog(&model.OrderStatusLog{
			TenantID:   tenantID,
			OrderID:    p.id,
			FromStatus: st,
			ToStatus:   st,
			Action:     "dedupe_fenfa",
			Remark:     fmt.Sprintf("合单发货分发备注已归并至订单#%d，本单清空", primaryID),
		})
		cleared++
	}
	if cleared > 0 {
		log.Printf("[ordercore] dedupe merge-ship fenfa primary=%d cleared=%d text=%q", primaryID, cleared, fen)
	}
	if o.ID == primaryID {
		return o
	}
	// 本单被清空时返回最新状态
	if fresh, err := s.repos.GetOrder(tenantID, o.ID); err == nil && fresh != nil {
		return fresh
	}
	o.FenFaRemark = ""
	return o
}

func ingestHasLogistics(req dto.IngestOrderRequest) bool {
	if strings.TrimSpace(req.ExpressNo) != "" {
		return true
	}
	for _, p := range req.Logistics {
		if strings.TrimSpace(p.ExpressNo) != "" {
			return true
		}
	}
	return false
}

func orderHasExpressShipments(o *model.Order) bool {
	if o == nil {
		return false
	}
	for _, sh := range o.Shipments {
		if strings.TrimSpace(sh.ExpressNo) != "" {
			return true
		}
	}
	return false
}

// syncIngestLogistics 把快递助手物流写入 order_shipments（按单号幂等）。
func syncIngestLogistics(tx *repo.Repos, tenantID, orderID uint64, req dto.IngestOrderRequest) error {
	pkgs := req.Logistics
	if len(pkgs) == 0 {
		no := strings.TrimSpace(req.ExpressNo)
		if no == "" {
			return nil
		}
		pkgs = []dto.LogisticsInput{{
			ExpressCompany: req.ExpressCompany,
			ExpressCode:    req.ExpressCode,
			ExpressNo:      no,
			ShippedAt:      req.ShippedAt,
		}}
	}
	for _, p := range pkgs {
		no := strings.TrimSpace(p.ExpressNo)
		if no == "" {
			continue
		}
		company := firstNonEmpty(p.ExpressCompany, p.ExpressCode)
		shipAt := parseTime(p.ShippedAt)
		if shipAt == nil {
			shipAt = parseTime(req.ShippedAt)
		}
		existing, err := tx.FindShipmentByExpressNo(tenantID, orderID, no)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if existing != nil {
			changed := false
			if company != "" && existing.ExpressCompany != company {
				existing.ExpressCompany = company
				changed = true
			}
			if shipAt != nil && (existing.ShippedAt == nil || !existing.ShippedAt.Equal(*shipAt)) {
				existing.ShippedAt = shipAt
				changed = true
			}
			if changed {
				if err := tx.UpdateShipment(existing); err != nil {
					return err
				}
			}
			continue
		}
		shipNo, err := tx.NextShipmentNo(tenantID)
		if err != nil {
			return err
		}
		if shipAt == nil {
			now := time.Now()
			shipAt = &now
		}
		sh := &model.OrderShipment{
			TenantID:        tenantID,
			OrderID:         orderID,
			ShipmentNo:      shipNo,
			ExpressCompany:  company,
			ExpressNo:       no,
			NeedTracking:    true,
			CallbackStatus:  model.CallbackSkipped,
			CallbackMessage: "快递助手同步",
			ShippedAt:       shipAt,
		}
		if err := tx.CreateShipment(sh); err != nil {
			return err
		}
	}
	return nil
}

func roundMoney(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "idx_orders_tenant_no")
}

func mapStoreSalesToIngest(so storecore.SalesOrder) dto.IngestOrderRequest {
	status := model.StatusPendingAlloc
	switch so.Status {
	case "completed":
		status = model.StatusCompleted
	case "cancelled":
		status = model.StatusClosed
	case "shipping":
		// 历史 shipped 值经 normalizeNonKDZSHint → allocated + shipped
		status = model.StatusShipped
	case "draft", "preview":
		status = model.StatusPendingPayment
	}
	if so.NeedProcurement && (status == model.StatusPendingAlloc || status == model.StatusPendingShip) {
		// 门店侧已标记需采购，同步后便于分配采购发货
	}
	items := make([]dto.OrderItemInput, 0, len(so.Items))
	for _, it := range so.Items {
		items = append(items, dto.OrderItemInput{
			SkuID:       it.SkuID,
			SkuCode:     it.SkuCode,
			ProductName: it.ProductName,
			SkuSpecs:    it.SkuSpecs,
			Quantity:    it.Quantity,
			Price:       it.Price,
		})
	}
	ref := fmt.Sprintf("%d", so.ID)
	return dto.IngestOrderRequest{
		SourceChannel:   model.SourceStore,
		PlatformOrderID: so.OrderNo,
		ExternalRefID:   ref,
		Status:          status,
		BuyerName:       so.CustomerName,
		BuyerPhone:      so.CustomerPhone,
		TotalAmount:     so.TotalAmount,
		PayAmount:       so.PayAmount,
		PayStatus:       so.PayStatus,
		Remark:          so.Remark,
		SellerRemark:    so.SellerRemark,
		Address: &dto.AddressInput{
			Name:     so.CustomerName,
			Phone:    so.CustomerPhone,
			FullText: so.Address,
			Address:  so.Address,
		},
		Items: items,
	}
}

func parseTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return &t
		}
	}
	return nil
}
