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
	repos     *repo.Repos
	storeSync *storesync.Client
	storeCore *storecore.Client
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

	hint := deriveKDZSIngest(channel, req)
	status := hint.Status
	platformStatus := coalesceStr(hint.PlatformStatus, req.PlatformStatus)
	platformStatusText := coalesceStr(hint.PlatformStatusText, req.PlatformStatusText)
	if existing != nil {
		fromStatus := existing.Status
		err = s.repos.Transaction(func(tx *repo.Repos) error {
			fields := map[string]any{
				"platform":             req.Platform,
				"platform_sys_tid":     req.PlatformSysTid,
				"shop_id":              req.ShopID,
				"shop_name":            req.ShopName,
				"buyer_nick":           req.BuyerNick,
				"buyer_name":           req.BuyerName,
				"buyer_phone":          req.BuyerPhone,
				"total_amount":         req.TotalAmount,
				"pay_amount":           req.PayAmount,
				"freight_amount":       req.FreightAmount,
				"pay_status":           req.PayStatus,
				"platform_status":       platformStatus,
				"platform_status_text":  platformStatusText,
				"ecommerce_status":      req.EcommerceStatus,
				"ecommerce_status_text": req.EcommerceStatusText,
				"after_sale_status":     req.AfterSaleStatus,
				"after_sale_status_text": req.AfterSaleStatusText,
				"agent_type":            hint.AgentType,
				"ship_entry_locked":     hint.ShipEntryLocked,
				"ship_lock_reason":      hint.ShipLockReason,
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
			terminal := existing.Status == model.StatusShipped || existing.Status == model.StatusCompleted || existing.Status == model.StatusClosed
			// 退款成功/交易关闭：同步关闭（不覆盖已发货/已完成）
			if !terminal && status == model.StatusClosed {
				fields["status"] = status
				statusChanged = fromStatus != status
			} else if !terminal && hint.ApplyDropshipAlloc {
				// 快递助手已代发：强制同步为已分配（覆盖此前误标的自营，终端态除外）
				fields["alloc_type"] = hint.AllocType
				fields["dropship_mode"] = hint.DropshipMode
				fields["factory_id"] = coalesceStr(req.FactoryID, existing.FactoryID)
				fields["factory_name"] = coalesceStr(req.FactoryName, existing.FactoryName)
				fields["status"] = status
				statusChanged = fromStatus != status
				if existing.AllocatedAt == nil {
					now := time.Now()
					fields["allocated_at"] = &now
				}
			} else if existing.AllocType == "" && !terminal {
				fields["factory_id"] = req.FactoryID
				fields["factory_name"] = req.FactoryName
				fields["status"] = status
				statusChanged = fromStatus != status
			} else if !terminal {
				fields["factory_id"] = coalesceStr(req.FactoryID, existing.FactoryID)
				fields["factory_name"] = coalesceStr(req.FactoryName, existing.FactoryName)
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
		return o, false, err
	}

	orderNo, err := s.repos.NextOrderNo(tenantID)
	if err != nil {
		return nil, false, err
	}
	o := &model.Order{
		TenantID:           tenantID,
		OrderNo:            orderNo,
		SourceChannel:      channel,
		Platform:           req.Platform,
		PlatformOrderID:    req.PlatformOrderID,
		PlatformSysTid:     req.PlatformSysTid,
		ShopID:             req.ShopID,
		ShopName:           req.ShopName,
		ExternalRefID:      req.ExternalRefID,
		Status:             status,
		AllocType:          hint.AllocType,
		DropshipMode:       hint.DropshipMode,
		BuyerNick:          req.BuyerNick,
		BuyerName:          req.BuyerName,
		BuyerPhone:         req.BuyerPhone,
		TotalAmount:        req.TotalAmount,
		PayAmount:          req.PayAmount,
		FreightAmount:      req.FreightAmount,
		PayStatus:          req.PayStatus,
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
		SellerRemark:       req.SellerRemark,
		FactoryID:          req.FactoryID,
		FactoryName:        req.FactoryName,
		RawPayload:         req.RawPayload,
	}
	if hint.ApplyDropshipAlloc {
		now := time.Now()
		o.AllocatedAt = &now
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
			Remark:     hint.LogRemark,
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

	switch allocType {
	case model.AllocSelfShip, model.AllocPurchaseThenShip:
		dropshipMode = ""
		if o.SourceChannel == model.SourceKDZS {
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
			if o.SourceChannel == model.SourceKDZS {
				kdzsAction = "push_factory"
			}
		} else {
			dropshipMode = model.DropshipOSMSSupplier
			factoryID = ""
			factoryName = ""
			agentType = model.AgentTypeSelf
			if o.SourceChannel == model.SourceKDZS {
				kdzsAction = "self_print"
			}
		}
	default:
		return nil, fmt.Errorf("无效的分配类型")
	}

	nextStatus := model.StatusAllocated
	if allocType == model.AllocPurchaseThenShip {
		nextStatus = model.StatusPurchasing
	}
	locked, lockReason := computeShipLock(o.SourceChannel, o.PlatformStatus, agentType, dropshipMode)

	if kdzsAction != "" {
		if err := s.setKDZSAgentType(ctx, o, kdzsAction, factoryID, bearerToken); err != nil {
			return nil, fmt.Errorf("同步快递助手失败: %w", err)
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
			"purchase_order_id": req.PurchaseOrderID,
			"alloc_remark":      req.Remark,
			"status":            nextStatus,
			"allocated_at":      now,
			"agent_type":        agentType,
			"ship_entry_locked": locked,
			"ship_lock_reason":  lockReason,
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
			Remark:     fmt.Sprintf("%s/%s kdzs=%s %s", allocType, dropshipMode, kdzsAction, req.Remark),
			OperatorID: operatorID,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repos.GetOrder(tenantID, orderID)
}

func (s *OrderService) setKDZSAgentType(ctx context.Context, o *model.Order, action, factoryID, token string) error {
	if s.storeSync == nil {
		return fmt.Errorf("StoreSyncAgent 未配置")
	}
	sysTid := o.PlatformSysTid
	if sysTid == "" {
		sysTid = o.PlatformOrderID
	}
	if sysTid == "" {
		return fmt.Errorf("缺少平台系统单号，无法同步快递助手")
	}
	tradeStatus := o.PlatformStatus
	if tradeStatus == "" {
		tradeStatus = model.KDZSWaitAudit
	}
	return s.storeSync.SetOrderAgentType(ctx, token, storesync.SetAgentTypeRequest{
		Platform:    o.Platform,
		TradeStatus: tradeStatus,
		Action:      action,
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
	if o.ShipEntryLocked {
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
			// 快递助手列表态以本次同步筛选项为准；电商平台状态保留在 ecommerceStatus
			ingest.PlatformStatus = status
			ingest.PlatformStatusText = kdzsPlatformStatusText(status)
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
	for _, o := range orders {
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
			Platform:  platform,
			Tid:       tid,
			PageNo:    1,
			PageSize:  5,
		})
		if err != nil {
			lastErr = err
			continue
		}
		if result == nil || len(result.Items) == 0 {
			continue
		}
		ingest := mapTradeToIngest(result.Items[0])
		// 回查结果若仍是电商状态码，优先保留库内已有快递助手列表态
		normStatus, normText := normalizeKDZSPlatformStatus(ingest.PlatformStatus, ingest.PlatformStatusText)
		if normStatus == "" || (normStatus != model.KDZSWaitAudit && normStatus != model.KDZSWaitSend &&
			normStatus != "shipped" && normStatus != "completed") {
			if o.PlatformStatus == model.KDZSWaitAudit || o.PlatformStatus == model.KDZSWaitSend ||
				o.PlatformStatus == "shipped" || o.PlatformStatus == "completed" {
				ingest.PlatformStatus = o.PlatformStatus
				ingest.PlatformStatusText = o.PlatformStatusText
			} else {
				ingest.PlatformStatus = normStatus
				ingest.PlatformStatusText = normText
			}
		} else {
			ingest.PlatformStatus = normStatus
			ingest.PlatformStatusText = normText
		}
		if _, _, err := s.Ingest(tenantID, operatorID, ingest); err != nil {
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
	return dto.IngestOrderRequest{
		SourceChannel:       model.SourceKDZS,
		Platform:            t.Platform,
		PlatformOrderID:     platformOrderID,
		PlatformSysTid:      sysTid,
		ShopID:              t.ShopID,
		ShopName:            t.ShopName,
		Status:              model.StatusPendingShip,
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
		TotalAmount:         t.Payment,
		PayAmount:           t.Payment,
		PayStatus:           "paid",
		PayTime:             t.PayTime,
		OrderTime:           t.CreateTime,
		Remark:              t.BuyerMemo,
		SellerRemark:        t.SellerMemo,
		FactoryID:           t.FactoryID,
		FactoryName:         t.FactoryName,
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
	PlatformStatus     string
	PlatformStatusText string
	AllocType          string
	DropshipMode       string
	AgentType          int
	ShipEntryLocked    bool
	ShipLockReason     string
	ApplyDropshipAlloc bool
	LogRemark          string
}

func resolveKDZSAgentType(agentType int, factoryID, factoryName string) int {
	if agentType == model.AgentTypeFactory {
		return model.AgentTypeFactory
	}
	if agentType == model.AgentTypeSelf {
		return model.AgentTypeSelf
	}
	// agentType 未返回时：有厂家即视为代发（快递助手真实信息）
	if strings.TrimSpace(factoryID) != "" || strings.TrimSpace(factoryName) != "" {
		return model.AgentTypeFactory
	}
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
	}
	return st, text
}

func deriveKDZSIngest(channel string, req dto.IngestOrderRequest) kdzsIngestHint {
	h := kdzsIngestHint{
		Status:    req.Status,
		LogRemark: "同步入库: " + channel,
	}
	if h.Status == "" {
		h.Status = model.StatusPendingShip
	}
	if channel != model.SourceKDZS {
		h.ShipEntryLocked = false
		return h
	}

	// 归一快递助手列表态（避免电商 ORDER_PAID 等污染 platformStatus）
	platformStatus, platformText := normalizeKDZSPlatformStatus(req.PlatformStatus, req.PlatformStatusText)
	h.PlatformStatus = platformStatus
	h.PlatformStatusText = platformText

	agentType := resolveKDZSAgentType(req.AgentType, req.FactoryID, req.FactoryName)
	h.AgentType = agentType
	isFactory := agentType == model.AgentTypeFactory

	switch strings.ToLower(strings.TrimSpace(platformStatus)) {
	case model.KDZSWaitSend:
		if isFactory {
			h.Status = model.StatusAllocated
			h.AllocType = model.AllocDropship
			h.DropshipMode = model.DropshipKDZSFactory
			h.ApplyDropshipAlloc = true
			h.ShipEntryLocked = true
			h.ShipLockReason = "快递助手已分配厂家代发，由厂家发货，无需干预"
			h.LogRemark = "同步待发货代发单→已分配并锁定填单号"
		} else {
			h.Status = model.StatusPendingShip
			h.ShipEntryLocked = false
			h.ShipLockReason = ""
			h.LogRemark = "同步待发货自营单"
		}
	case model.KDZSWaitAudit:
		if isFactory {
			// 待推单但快递助手侧已标厂家代发：视为已分配，无需再干预
			h.Status = model.StatusAllocated
			h.AllocType = model.AllocDropship
			h.DropshipMode = model.DropshipKDZSFactory
			h.ApplyDropshipAlloc = true
			h.ShipEntryLocked = true
			h.ShipLockReason = "快递助手已推厂家代发，无需干预"
			h.LogRemark = "同步待推单代发单→已分配"
		} else {
			h.Status = model.StatusPendingShip
			h.ShipEntryLocked = true
			h.ShipLockReason = "快递助手待推单，请先分配；仅自营待发货可填单号"
			h.LogRemark = "同步待推单"
		}
	default:
		if isFactory {
			h.Status = model.StatusAllocated
			h.AllocType = model.AllocDropship
			h.DropshipMode = model.DropshipKDZSFactory
			h.ApplyDropshipAlloc = true
			h.ShipEntryLocked = true
			h.ShipLockReason = "快递助手厂家代发，无需干预"
			h.LogRemark = "同步代发单→已分配"
		} else {
			h.ShipEntryLocked, h.ShipLockReason = computeShipLock(channel, platformStatus, agentType, "")
		}
	}

	// 电商订单/售后状态影响履约：关闭或锁定
	if closed, reason := ecommerceShouldClose(req.EcommerceStatus, req.EcommerceStatusText, req.AfterSaleStatus); closed {
		h.Status = model.StatusClosed
		h.AllocType = ""
		h.DropshipMode = ""
		h.ApplyDropshipAlloc = false
		h.ShipEntryLocked = true
		h.ShipLockReason = reason
		h.LogRemark = reason
		return h
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
	case "TRADE_CLOSED", "ORDER_CANCEL", "CANCEL", "CLOSED", "REFUND_SUCCESS", "REFUNDED", "SUCCESS_REFUND":
		return true, "电商订单已关闭/退款成功"
	}
	if as == "REFUND_SUCCESS" {
		return true, "售后退款成功，订单关闭"
	}
	if strings.Contains(text, "退款成功") || strings.Contains(text, "交易关闭") {
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
	case "TRADE_CLOSED", "ORDER_CANCEL", "CANCEL", "CLOSED":
		return "交易关闭"
	case "REFUNDING", "REFUND", "IN_REFUND":
		return "退款中"
	case "REFUND_SUCCESS", "REFUNDED", "SUCCESS_REFUND":
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
