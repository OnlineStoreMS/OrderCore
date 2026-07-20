package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ordercore/internal/dto"
	"ordercore/internal/integration/storecore"
	"ordercore/internal/integration/storesync"
	"ordercore/internal/model"
	"ordercore/internal/repo"

	"gorm.io/gorm"
)

type OrderService struct {
	repos      *repo.Repos
	storeSync  *storesync.Client
	storeCore  *storecore.Client
}

func NewOrderService(repos *repo.Repos, storeSync *storesync.Client, storeCore *storecore.Client) *OrderService {
	return &OrderService{repos: repos, storeSync: storeSync, storeCore: storeCore}
}

func (s *OrderService) Dashboard(tenantID uint64) (map[string]any, error) {
	byStatus, err := s.repos.CountByStatus(tenantID)
	if err != nil {
		return nil, err
	}
	bySource, err := s.repos.CountBySource(tenantID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"byStatus": byStatus,
		"bySource": bySource,
	}, nil
}

func (s *OrderService) List(tenantID uint64, q repo.OrderListQuery) ([]model.Order, int64, error) {
	return s.repos.ListOrders(tenantID, q)
}

func (s *OrderService) Get(tenantID, id uint64) (*model.Order, error) {
	return s.repos.GetOrder(tenantID, id)
}

func (s *OrderService) CreateManual(tenantID, operatorID uint64, req dto.ManualCreateOrderRequest) (*model.Order, error) {
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("订单明细不能为空")
	}
	orderNo, err := s.repos.NextOrderNo(tenantID)
	if err != nil {
		return nil, err
	}
	o := &model.Order{
		TenantID:      tenantID,
		OrderNo:       orderNo,
		SourceChannel: model.SourceManual,
		Status:        model.StatusPendingShip,
		BuyerName:     req.BuyerName,
		BuyerPhone:    req.BuyerPhone,
		BuyerNick:     req.BuyerNick,
		TotalAmount:   req.TotalAmount,
		PayAmount:     req.PayAmount,
		FreightAmount: req.FreightAmount,
		PayStatus:     "paid",
		Remark:        req.Remark,
		SellerRemark:  req.SellerRemark,
	}
	now := time.Now()
	o.PayTime = &now
	for i, it := range req.Items {
		qty := it.Quantity
		if qty <= 0 {
			qty = 1
		}
		amt := it.Price * float64(qty)
		o.Items = append(o.Items, model.OrderItem{
			TenantID:    tenantID,
			LineNo:      i + 1,
			SkuID:       it.SkuID,
			SkuCode:     it.SkuCode,
			ProductName: it.ProductName,
			SkuSpecs:    it.SkuSpecs,
			PicURL:      it.PicURL,
			Quantity:    qty,
			Price:       it.Price,
			TotalAmount: amt,
		})
		if o.TotalAmount == 0 {
			o.TotalAmount += amt
		}
	}
	if o.PayAmount == 0 {
		o.PayAmount = o.TotalAmount
	}
	if req.Address != nil {
		o.Address = mapAddress(tenantID, 0, req.Address)
	}
	err = s.repos.Transaction(func(tx *repo.Repos) error {
		if err := tx.CreateOrder(o); err != nil {
			return err
		}
		return tx.AddStatusLog(&model.OrderStatusLog{
			TenantID:   tenantID,
			OrderID:    o.ID,
			ToStatus:   o.Status,
			Action:     "create_manual",
			Remark:     "手工建单",
			OperatorID: operatorID,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repos.GetOrder(tenantID, o.ID)
}

func (s *OrderService) Ingest(tenantID, operatorID uint64, req dto.IngestOrderRequest) (*model.Order, bool, error) {
	channel := strings.TrimSpace(req.SourceChannel)
	if channel == "" {
		return nil, false, fmt.Errorf("sourceChannel 必填")
	}
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

	status := req.Status
	if status == "" {
		status = model.StatusPendingShip
	}
	if existing != nil {
		fromStatus := existing.Status
		err = s.repos.Transaction(func(tx *repo.Repos) error {
			// 已分配/已发货的订单不覆盖分配字段，只刷新基础信息
			fields := map[string]any{
				"platform":              req.Platform,
				"platform_sys_tid":      req.PlatformSysTid,
				"shop_id":               req.ShopID,
				"shop_name":             req.ShopName,
				"buyer_nick":            req.BuyerNick,
				"buyer_name":            req.BuyerName,
				"buyer_phone":           req.BuyerPhone,
				"total_amount":          req.TotalAmount,
				"pay_amount":            req.PayAmount,
				"freight_amount":        req.FreightAmount,
				"pay_status":            req.PayStatus,
				"platform_status":       req.PlatformStatus,
				"platform_status_text":  req.PlatformStatusText,
				"remark":                req.Remark,
				"seller_remark":         req.SellerRemark,
				"raw_payload":           req.RawPayload,
			}
			if t := parseTime(req.PayTime); t != nil {
				fields["pay_time"] = t
			}
			if t := parseTime(req.OrderTime); t != nil {
				fields["ordered_at"] = t
			}
			statusChanged := false
			if existing.AllocType == "" {
				fields["factory_id"] = req.FactoryID
				fields["factory_name"] = req.FactoryName
				fields["status"] = status
				statusChanged = fromStatus != status
			}
			if err := tx.UpdateOrderFields(tenantID, existing.ID, fields); err != nil {
				return err
			}
			items := mapItems(tenantID, existing.ID, req.Items)
			if err := tx.ReplaceItems(tenantID, existing.ID, items); err != nil {
				return err
			}
			if req.Address != nil {
				addr := mapAddress(tenantID, existing.ID, req.Address)
				if err := tx.UpsertAddress(addr); err != nil {
					return err
				}
			}
			if statusChanged {
				return tx.AddStatusLog(&model.OrderStatusLog{
					TenantID:   tenantID,
					OrderID:    existing.ID,
					FromStatus: fromStatus,
					ToStatus:   status,
					Action:     "ingest_update",
					Remark:     "同步更新状态: " + channel,
					OperatorID: operatorID,
				})
			}
			return nil
		})
		if err != nil {
			return nil, false, err
		}
		o, err := s.repos.GetOrder(tenantID, existing.ID)
		return o, false, err
	}

	orderNo, err := s.repos.NextOrderNo(tenantID)
	if err != nil {
		return nil, false, err
	}
	o := &model.Order{
		TenantID:        tenantID,
		OrderNo:         orderNo,
		SourceChannel:   channel,
		Platform:        req.Platform,
		PlatformOrderID: req.PlatformOrderID,
		PlatformSysTid:  req.PlatformSysTid,
		ShopID:          req.ShopID,
		ShopName:        req.ShopName,
		ExternalRefID:   req.ExternalRefID,
		Status:          status,
		BuyerNick:       req.BuyerNick,
		BuyerName:       req.BuyerName,
		BuyerPhone:      req.BuyerPhone,
		TotalAmount:     req.TotalAmount,
		PayAmount:       req.PayAmount,
		FreightAmount:   req.FreightAmount,
		PayStatus:          req.PayStatus,
		PlatformStatus:     req.PlatformStatus,
		PlatformStatusText: req.PlatformStatusText,
		Remark:             req.Remark,
		SellerRemark:       req.SellerRemark,
		FactoryID:          req.FactoryID,
		FactoryName:        req.FactoryName,
		RawPayload:         req.RawPayload,
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
		return tx.AddStatusLog(&model.OrderStatusLog{
			TenantID:   tenantID,
			OrderID:    o.ID,
			ToStatus:   o.Status,
			Action:     "ingest",
			Remark:     "同步入库: " + channel,
			OperatorID: operatorID,
		})
	})
	if err != nil {
		return nil, false, err
	}
	out, err := s.repos.GetOrder(tenantID, o.ID)
	return out, true, err
}

func (s *OrderService) Allocate(ctx context.Context, tenantID, operatorID uint64, orderID uint64, req dto.AllocateRequest, bearerToken string) (*model.Order, error) {
	o, err := s.repos.GetOrder(tenantID, orderID)
	if err != nil {
		return nil, err
	}
	if o.Status == model.StatusShipped || o.Status == model.StatusCompleted || o.Status == model.StatusClosed {
		return nil, fmt.Errorf("当前状态不可分配")
	}

	allocType := strings.TrimSpace(req.AllocType)
	dropshipMode := strings.TrimSpace(req.DropshipMode)
	switch allocType {
	case model.AllocSelfShip:
		dropshipMode = ""
	case model.AllocDropship:
		if dropshipMode == "" {
			return nil, fmt.Errorf("代发发货需指定 dropshipMode")
		}
		if dropshipMode != model.DropshipKDZSFactory && dropshipMode != model.DropshipOSMSSupplier {
			return nil, fmt.Errorf("无效的 dropshipMode")
		}
	case model.AllocPurchaseThenShip:
		dropshipMode = ""
	default:
		return nil, fmt.Errorf("无效的分配类型")
	}

	supplierID := req.SupplierID
	supplierName := req.SupplierName
	factoryID := req.FactoryID
	factoryName := req.FactoryName

	if allocType == model.AllocDropship {
		switch dropshipMode {
		case model.DropshipKDZSFactory:
			if factoryID == "" {
				return nil, fmt.Errorf("快递助手厂家代发需指定 factoryId")
			}
			if supplierID == 0 {
				if b, err := s.repos.FindBindingByFactory(tenantID, model.SourceKDZS, factoryID); err == nil {
					supplierID = b.SupplierID
					supplierName = b.SupplierName
					if factoryName == "" {
						factoryName = b.ExternalFactoryName
					}
				}
			}
		case model.DropshipOSMSSupplier:
			if supplierID == 0 {
				return nil, fmt.Errorf("OSMS 供应商代发需指定 supplierId")
			}
		}
	}

	nextStatus := model.StatusAllocated
	if allocType == model.AllocPurchaseThenShip {
		nextStatus = model.StatusPurchasing
	}
	now := time.Now()
	from := o.Status
	pushKDZS := allocType == model.AllocDropship && dropshipMode == model.DropshipKDZSFactory &&
		o.SourceChannel == model.SourceKDZS

	// 外部推送放在事务外；落库用事务保证状态+流水+发货单一致
	if pushKDZS {
		if err := s.pushKDZSFactory(ctx, o, factoryID, bearerToken); err != nil {
			return nil, fmt.Errorf("推送快递助手失败: %w", err)
		}
	}

	err = s.repos.Transaction(func(tx *repo.Repos) error {
		fields := map[string]any{
			"alloc_type":        allocType,
			"dropship_mode":     dropshipMode,
			"supplier_id":       supplierID,
			"supplier_name":     supplierName,
			"factory_id":        factoryID,
			"factory_name":      factoryName,
			"purchase_order_id": req.PurchaseOrderID,
			"alloc_remark":      req.Remark,
			"status":            nextStatus,
			"allocated_at":      now,
		}
		if err := tx.TransitionOrder(tenantID, orderID, fields, &model.OrderStatusLog{
			FromStatus: from,
			ToStatus:   nextStatus,
			Action:     "allocate",
			Remark:     fmt.Sprintf("%s/%s %s", allocType, dropshipMode, req.Remark),
			OperatorID: operatorID,
		}); err != nil {
			return err
		}

		if !pushKDZS {
			return nil
		}
		shipNo, err := tx.NextShipmentNo(tenantID)
		if err != nil {
			return err
		}
		shippedAt := time.Now()
		sh := &model.OrderShipment{
			TenantID:        tenantID,
			OrderID:         orderID,
			ShipmentNo:      shipNo,
			NeedTracking:    false,
			CallbackStatus:  model.CallbackSucceeded,
			CallbackMessage: "已推送快递助手厂家代发",
			CallbackAt:      &shippedAt,
			ShippedAt:       &shippedAt,
			Remark:          "KDZS 厂家代发推送",
		}
		if err := tx.CreateShipment(sh); err != nil {
			return err
		}
		return tx.TransitionOrder(tenantID, orderID, map[string]any{
			"status":     model.StatusShipped,
			"shipped_at": shippedAt,
		}, &model.OrderStatusLog{
			FromStatus: nextStatus,
			ToStatus:   model.StatusShipped,
			Action:     "kdzs_push_factory",
			Remark:     "推送厂家代发成功",
			OperatorID: operatorID,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repos.GetOrder(tenantID, orderID)
}

func (s *OrderService) pushKDZSFactory(ctx context.Context, o *model.Order, factoryID, token string) error {
	if s.storeSync == nil {
		return fmt.Errorf("StoreSyncAgent 未配置")
	}
	sysTid := o.PlatformSysTid
	if sysTid == "" {
		sysTid = o.PlatformOrderID
	}
	if sysTid == "" {
		return fmt.Errorf("缺少平台系统单号，无法推送")
	}
	return s.storeSync.SetOrderAgentType(ctx, token, storesync.SetAgentTypeRequest{
		Platform:    o.Platform,
		TradeStatus: o.PlatformStatus,
		Action:      "push_factory",
		FactoryID:   factoryID,
		SysTids:     []string{sysTid},
	})
}

func (s *OrderService) Ship(ctx context.Context, tenantID, operatorID, orderID uint64, req dto.ShipRequest, bearerToken string) (*model.Order, error) {
	o, err := s.repos.GetOrder(tenantID, orderID)
	if err != nil {
		return nil, err
	}
	if o.Status == model.StatusClosed {
		return nil, fmt.Errorf("订单已关闭")
	}
	if o.AllocType == model.AllocDropship && o.DropshipMode == model.DropshipKDZSFactory {
		return nil, fmt.Errorf("快递助手厂家代发无需手工填单号")
	}
	if strings.TrimSpace(req.ExpressNo) == "" {
		return nil, fmt.Errorf("物流单号不能为空")
	}

	shipNo, err := s.repos.NextShipmentNo(tenantID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	sh := &model.OrderShipment{
		TenantID:       tenantID,
		OrderID:        orderID,
		ShipmentNo:     shipNo,
		ExpressCompany: req.ExpressCompany,
		ExpressNo:      strings.TrimSpace(req.ExpressNo),
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
			sh.CallbackMessage = cbErr.Error()
		} else {
			sh.CallbackStatus = model.CallbackSucceeded
			sh.CallbackMessage = msg
			sh.CallbackAt = &now
		}
	} else {
		sh.CallbackStatus = model.CallbackSkipped
		sh.CallbackMessage = "未回传来源平台"
	}

	from := o.Status
	err = s.repos.Transaction(func(tx *repo.Repos) error {
		if err := tx.CreateShipment(sh); err != nil {
			return err
		}
		return tx.TransitionOrder(tenantID, orderID, map[string]any{
			"status":     model.StatusShipped,
			"shipped_at": now,
		}, &model.OrderStatusLog{
			FromStatus: from,
			ToStatus:   model.StatusShipped,
			Action:     "ship",
			Remark:     fmt.Sprintf("%s %s", req.ExpressCompany, req.ExpressNo),
			OperatorID: operatorID,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repos.GetOrder(tenantID, orderID)
}

func (s *OrderService) callbackSource(ctx context.Context, o *model.Order, sh *model.OrderShipment, token string) (string, error) {
	switch o.SourceChannel {
	case model.SourceKDZS:
		if s.storeSync == nil {
			return "", fmt.Errorf("StoreSyncAgent 未配置")
		}
		res, err := s.storeSync.ShipCallback(ctx, token, storesync.ShipCallbackRequest{
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

func (s *OrderService) SyncFromKDZS(ctx context.Context, tenantID, operatorID uint64, req dto.SyncKDZSRequest, token string) (map[string]int, error) {
	if s.storeSync == nil {
		return nil, fmt.Errorf("StoreSyncAgent 未配置")
	}
	pageNo := req.PageNo
	if pageNo <= 0 {
		pageNo = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	statuses := make([]string, 0, 2)
	if len(req.TradeStatuses) > 0 {
		statuses = append(statuses, req.TradeStatuses...)
	} else if strings.TrimSpace(req.TradeStatus) != "" {
		statuses = append(statuses, strings.TrimSpace(req.TradeStatus))
	} else {
		// 默认同步待推单 + 待发货
		statuses = []string{"wait_audit", "wait_send"}
	}

	created, updated, fetched, total := 0, 0, 0, 0
	seen := map[string]struct{}{}
	for _, status := range statuses {
		result, err := s.storeSync.ListOrders(ctx, token, storesync.OrderQuery{
			Platform:      req.Platform,
			ShopID:        req.ShopID,
			TradeStatus:   status,
			PageNo:        pageNo,
			PageSize:      pageSize,
			StartDateTime: req.StartTime,
			EndDateTime:   req.EndTime,
		})
		if err != nil {
			return nil, err
		}
		fetched += len(result.Items)
		total += result.Total
		for _, t := range result.Items {
			ingest := mapTradeToIngest(t)
			if ingest.PlatformStatus == "" {
				ingest.PlatformStatus = status
			}
			if ingest.PlatformStatusText == "" {
				ingest.PlatformStatusText = kdzsPlatformStatusText(ingest.PlatformStatus)
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
			_, isNew, err := s.Ingest(tenantID, operatorID, ingest)
			if err != nil {
				return nil, err
			}
			if isNew {
				created++
			} else {
				updated++
			}
		}
	}
	return map[string]int{"created": created, "updated": updated, "fetched": fetched, "total": total}, nil
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
		_, isNew, err := s.Ingest(tenantID, operatorID, ingest)
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

func mapItems(tenantID, orderID uint64, items []dto.OrderItemInput) []model.OrderItem {
	out := make([]model.OrderItem, 0, len(items))
	for i, it := range items {
		qty := it.Quantity
		if qty <= 0 {
			qty = 1
		}
		out = append(out, model.OrderItem{
			TenantID:    tenantID,
			OrderID:     orderID,
			LineNo:      i + 1,
			SkuID:       it.SkuID,
			SkuCode:     it.SkuCode,
			ProductName: it.ProductName,
			SkuSpecs:    it.SkuSpecs,
			PicURL:      it.PicURL,
			Quantity:    qty,
			Price:       it.Price,
			TotalAmount: it.Price * float64(qty),
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
		items = append(items, dto.OrderItemInput{
			SkuCode:     g.OuterID,
			ProductName: g.Title,
			SkuSpecs:    g.SkuName,
			PicURL:      g.PicURL,
			Quantity:    g.Num,
			Price:       g.Price,
		})
	}
	addrFull := t.FormattedReceiver
	if addrFull == "" {
		addrFull = t.ReceiverAddress
	}
	statusText := t.StatusText
	if statusText == "" {
		statusText = kdzsPlatformStatusText(t.TradeStatus)
	}
	return dto.IngestOrderRequest{
		SourceChannel:      model.SourceKDZS,
		Platform:           t.Platform,
		PlatformOrderID:    platformOrderID,
		PlatformSysTid:     sysTid,
		ShopID:             t.ShopID,
		ShopName:           t.ShopName,
		Status:             model.StatusPendingShip,
		PlatformStatus:     t.TradeStatus,
		PlatformStatusText: statusText,
		BuyerNick:          t.BuyerNick,
		BuyerName:          t.ReceiverName,
		BuyerPhone:         t.ReceiverMobile,
		TotalAmount:        t.Payment,
		PayAmount:          t.Payment,
		PayStatus:          "paid",
		PayTime:            t.PayTime,
		OrderTime:          t.CreateTime,
		Remark:             t.BuyerMemo,
		SellerRemark:       t.SellerMemo,
		FactoryID:          t.FactoryID,
		FactoryName:        t.FactoryName,
		RawPayload:         string(raw),
		Address: &dto.AddressInput{
			Name:     t.ReceiverName,
			Phone:    t.ReceiverMobile,
			Address:  t.ReceiverAddress,
			FullText: addrFull,
		},
		Items: items,
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
	default:
		return status
	}
}

func mapStoreSalesToIngest(so storecore.SalesOrder) dto.IngestOrderRequest {
	status := model.StatusPendingShip
	switch so.Status {
	case "completed":
		status = model.StatusCompleted
	case "cancelled":
		status = model.StatusClosed
	case "shipping":
		status = model.StatusShipped
	case "draft", "preview":
		status = model.StatusPendingPayment
	}
	if so.NeedProcurement && status == model.StatusPendingShip {
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
		SourceChannel: model.SourceStore,
		PlatformOrderID: so.OrderNo,
		ExternalRefID: ref,
		Status:        status,
		BuyerName:     so.CustomerName,
		BuyerPhone:    so.CustomerPhone,
		TotalAmount:   so.TotalAmount,
		PayAmount:     so.PayAmount,
		PayStatus:     so.PayStatus,
		Remark:        so.Remark,
		SellerRemark:  so.SellerRemark,
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
