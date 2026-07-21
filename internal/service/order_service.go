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
	"ordercore/internal/integration/supplycore"
	"ordercore/internal/model"
	"ordercore/internal/repo"

	"gorm.io/gorm"
)

type OrderService struct {
	repos       *repo.Repos
	storeSync   *storesync.Client
	storeCore   *storecore.Client
	supply      *supplycore.Client
	onAllocated func(tenantID, orderID uint64)
}

func NewOrderService(repos *repo.Repos, storeSync *storesync.Client, storeCore *storecore.Client, supply *supplycore.Client) *OrderService {
	return &OrderService{repos: repos, storeSync: storeSync, storeCore: storeCore, supply: supply}
}

func (s *OrderService) SetOnAllocated(fn func(tenantID, orderID uint64)) {
	s.onAllocated = fn
}

func (s *OrderService) Dashboard(tenantID uint64, start, end time.Time) (map[string]any, error) {
	start, end, err := repo.NormalizeDashboardRange(start, end)
	if err != nil {
		return nil, err
	}
	cards, err := s.repos.DashboardCards(tenantID, start, end)
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
	trend, err := s.repos.DailyOrderTrend(tenantID, start, end)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"cards":    cards,
		"byStatus": byStatus,
		"bySource": bySource,
		"trend":    trend,
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
		Status:        model.StatusPendingAlloc,
		ShipStatus:    model.ShipWaitShip,
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

func (s *OrderService) Ingest(ctx context.Context, tenantID, operatorID uint64, req dto.IngestOrderRequest, bearerToken string) (*model.Order, bool, error) {
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
	shipStatus := hint.ShipStatus
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
				fields["alloc_remark"] = ""
				fields["allocated_at"] = nil
				// 关闭且未真实发货：不要继续占「待发货」队列
				if existing.ShipStatus != model.ShipShipped {
					fields["ship_status"] = ""
				}
				statusChanged = fromStatus != status
			} else if !terminal && hint.ApplySyncAlloc {
				// 自营自动分配：尊重「撤回分配」标记；厂家代发/已发货以快递助手为准强制同步
				skipSelfAuto := hint.AgentType == model.AgentTypeSelf && existing.SkipAutoAlloc &&
					hint.ShipStatus != model.ShipShipped && hint.Status != model.StatusCompleted
				if !skipSelfAuto {
					fields["alloc_type"] = hint.AllocType
					fields["dropship_mode"] = hint.DropshipMode
					fields["factory_id"] = req.FactoryID
					fields["factory_name"] = req.FactoryName
					if hint.AllocType == model.AllocSelfShip {
						fields["supplier_id"] = 0
						fields["supplier_name"] = ""
					}
					fields["status"] = status
					fields["skip_auto_alloc"] = false
					statusChanged = fromStatus != status
					if existing.AllocatedAt == nil {
						now := time.Now()
						fields["allocated_at"] = &now
					}
				} else {
					fields["factory_id"] = req.FactoryID
					fields["factory_name"] = req.FactoryName
				}
			} else if !terminal && hint.ClearAlloc {
				// 快递助手回到待推单（撤单等）：清空订单中心分配，恢复待分配
				fields["alloc_type"] = ""
				fields["dropship_mode"] = ""
				fields["factory_id"] = req.FactoryID
				fields["factory_name"] = req.FactoryName
				fields["supplier_id"] = 0
				fields["supplier_name"] = ""
				fields["purchase_order_id"] = ""
				fields["alloc_remark"] = ""
				fields["allocated_at"] = nil
				fields["skip_auto_alloc"] = false
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
			}

			// 发货状态独立更新（关闭单不写入待发货）
			closingNow := status == model.StatusClosed
			if shipStatus != "" && existing.Status != model.StatusClosed && !closingNow {
				fields["ship_status"] = shipStatus
				if shipStatus == model.ShipShipped && existing.ShippedAt == nil {
					now := time.Now()
					fields["shipped_at"] = &now
				}
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
		if err != nil {
			return nil, false, err
		}
		s.TryAutoAllocateBySKU(ctx, tenantID, operatorID, o, bearerToken)
		o, _ = s.repos.GetOrder(tenantID, existing.ID)
		return o, false, nil
	}

	orderNo, err := s.repos.NextOrderNo(tenantID)
	if err != nil {
		return nil, false, err
	}
	if shipStatus == "" {
		shipStatus = model.ShipWaitShip
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
		ShipStatus:         shipStatus,
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
	if hint.ApplySyncAlloc {
		now := time.Now()
		o.AllocatedAt = &now
		if shipStatus == model.ShipShipped || status == model.StatusCompleted {
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
	if err != nil {
		return nil, false, err
	}
	s.TryAutoAllocateBySKU(ctx, tenantID, operatorID, out, bearerToken)
	out, _ = s.repos.GetOrder(tenantID, o.ID)
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

	// 代发：先建 SupplyCore 草稿代发单，再推 KDZS；失败则回滚草稿
	var createdPOID uint64
	if allocType == model.AllocDropship {
		poNo, poID, created, err := s.ensureDropshipPurchaseOrder(ctx, o, supplierID, supplierName, bearerToken)
		if err != nil {
			return nil, err
		}
		purchaseOrderID = poNo
		if created {
			createdPOID = poID
		}
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

	items := make([]supplycore.PurchaseOrderItemInput, 0, len(o.Items))
	for _, it := range o.Items {
		qty := it.Quantity
		if qty <= 0 {
			qty = 1
		}
		code := strings.TrimSpace(it.SkuCode)
		if code == "" {
			code = strings.TrimSpace(it.PlatformSkuID)
		}
		items = append(items, supplycore.PurchaseOrderItemInput{
			SkuID:           it.SkuID,
			ProductName:     it.ProductName,
			SupplierSkuCode: code,
			Qty:             qty,
			UnitPrice:       0,
			Remark:          it.SkuSpecs,
		})
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
		Remark:          remark,
		Items:           items,
	})
	if err != nil {
		return "", 0, false, fmt.Errorf("创建 SupplyCore 代发单失败: %w", err)
	}
	return po.PoNo, po.ID, true, nil
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

// cancelLinkedDropshipPOs 撤回分配时取消关联草稿/已下单代发单；已付款则拒绝。
func (s *OrderService) cancelLinkedDropshipPOs(ctx context.Context, orderID uint64, purchaseOrderID, bearerToken string) error {
	if s.supply == nil {
		return nil
	}
	list, _, err := s.supply.ListPurchaseOrders(ctx, bearerToken, orderID, "dropship", 1, 50)
	if err != nil {
		// 有本地采购单号时仍尝试按号提示；列表失败不阻断若无采购单
		if strings.TrimSpace(purchaseOrderID) == "" {
			return nil
		}
		return fmt.Errorf("查询关联代发单失败: %w", err)
	}
	for _, it := range list {
		if it.Status == "cancelled" {
			continue
		}
		if it.PayStatus == "paid" || it.Status == "paid" || it.Status == "partial_shipped" ||
			it.Status == "in_transit" || it.Status == "partial_received" || it.Status == "completed" {
			return fmt.Errorf("关联代发单 %s 已进入付款/履约，不可撤回分配", it.PoNo)
		}
		if it.Status == "draft" || it.Status == "ordered" {
			if _, err := s.supply.CancelPurchaseOrder(ctx, bearerToken, it.ID); err != nil {
				// 草稿优先删除
				if it.Status == "draft" {
					if delErr := s.supply.DeletePurchaseOrder(ctx, bearerToken, it.ID); delErr != nil {
						return fmt.Errorf("取消代发单 %s 失败: %w", it.PoNo, err)
					}
					continue
				}
				return fmt.Errorf("取消代发单 %s 失败: %w", it.PoNo, err)
			}
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
		if err := s.cancelLinkedDropshipPOs(ctx, o.ID, o.PurchaseOrderID, bearerToken); err != nil {
			return nil, err
		}
	}

	kdzsRemark := ""
	if o.SourceChannel == model.SourceKDZS {
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
			"alloc_remark":      "",
			"allocated_at":      nil,
			"status":            model.StatusPendingAlloc,
			"ship_status":       model.ShipWaitShip,
			"agent_type":        model.AgentTypeSelf,
			"ship_entry_locked": locked,
			"ship_lock_reason":  lockReason,
			"skip_auto_alloc":   true,
		}
		if o.SourceChannel == model.SourceKDZS {
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

func (s *OrderService) cancelKDZSPush(ctx context.Context, o *model.Order, token string) error {
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
	if o.ShipStatus == model.ShipShipped {
		return nil, fmt.Errorf("订单已发货")
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
			"ship_status": model.ShipShipped,
			"shipped_at":  now,
		}, &model.OrderStatusLog{
			FromStatus: from,
			ToStatus:   from, // 履约状态不变
			Action:     "ship",
			Remark:     fmt.Sprintf("发货状态→已发货 %s %s", req.ExpressCompany, req.ExpressNo),
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
	if tid := strings.TrimSpace(req.Tid); tid != "" {
		return s.syncKDZSByTid(ctx, tenantID, operatorID, tid, req.Platform, token)
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
						return nil, err
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
		result *storesync.OrderListResult
		err    error
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
	_, err := s.syncKDZSByTid(ctx, tenantID, operatorID, platformOrderID, "", token)
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
			Platform:  platform,
			Tid:       tid,
			PageNo:    1,
			PageSize:  5,
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
		h.ShipStatus = model.ShipWaitShip
		if isFactory {
			// 待推单但快递助手侧已标厂家代发：视为已分配，无需再干预
			h.Status = model.StatusAllocated
			h.AllocType = model.AllocDropship
			h.DropshipMode = model.DropshipKDZSFactory
			h.ApplySyncAlloc = true
			h.ShipEntryLocked = true
			h.ShipLockReason = "快递助手已推厂家代发，无需干预"
			h.LogRemark = "同步待推单代发单→已分配"
		} else {
			h.Status = model.StatusPendingAlloc
			h.ClearAlloc = true
			h.ShipEntryLocked = true
			h.ShipLockReason = "快递助手待推单，请先分配；仅自营待发货可填单号"
			h.LogRemark = "同步待推单→清空分配，恢复待分配"
		}
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
