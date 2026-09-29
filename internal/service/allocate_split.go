package service

import (
	"context"
	"fmt"
	"log"
	"strings"

	"ordercore/internal/model"
	"ordercore/internal/repo"
)

// resolveAllocateTarget 商品级分配：勾选部分根行时拆出履约子单，再对子单做整单分配。
// itemIDs 空或覆盖全部可履约根行 → 原单；否则新建 SplitFrom 子单并迁走勾选行。
func (s *OrderService) resolveAllocateTarget(ctx context.Context, tenantID, operatorID uint64, o *model.Order, itemIDs []uint64) (*model.Order, error) {
	if o == nil {
		return nil, fmt.Errorf("订单不存在")
	}
	roots := fulfillableRootItems(o)
	if len(roots) == 0 {
		return o, nil
	}
	want := normalizeItemIDSet(itemIDs)
	if len(want) == 0 {
		return o, nil
	}
	selected := make([]model.OrderItem, 0, len(want))
	for _, it := range roots {
		if _, ok := want[it.ID]; ok {
			selected = append(selected, it)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("请勾选本单待分配的商品")
	}
	if len(selected) == len(roots) {
		return o, nil
	}
	if o.ShipStatus == model.ShipPartialShipped || o.ShipStatus == model.ShipShipped {
		return nil, fmt.Errorf("已部分发货的订单请整单分配或先处理发货，暂不支持再拆商品分配")
	}
	if strings.TrimSpace(o.PurchaseOrderID) != "" || strings.TrimSpace(o.SelfOrderNo) != "" {
		return nil, fmt.Errorf("订单已关联代发/自营单，请先撤回分配后再按商品拆分")
	}
	child, err := s.splitOrderForAllocate(ctx, tenantID, operatorID, o, selected)
	if err != nil {
		return nil, err
	}
	return child, nil
}

func normalizeItemIDSet(ids []uint64) map[uint64]struct{} {
	out := map[uint64]struct{}{}
	for _, id := range ids {
		if id > 0 {
			out[id] = struct{}{}
		}
	}
	return out
}

func fulfillableRootItems(o *model.Order) []model.OrderItem {
	if o == nil {
		return nil
	}
	out := make([]model.OrderItem, 0, len(o.Items))
	for _, it := range o.Items {
		if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
			continue
		}
		if orderItemExcludedFromFulfillment(it) {
			continue
		}
		out = append(out, it)
	}
	return out
}

func collectMoveItemIDs(o *model.Order, selectedRoots []model.OrderItem) []uint64 {
	rootSet := map[uint64]struct{}{}
	ids := make([]uint64, 0, len(selectedRoots)*2)
	for _, it := range selectedRoots {
		rootSet[it.ID] = struct{}{}
		ids = append(ids, it.ID)
	}
	for _, it := range o.Items {
		if it.ParentOrderItemID == 0 {
			continue
		}
		if _, ok := rootSet[it.ParentOrderItemID]; ok {
			ids = append(ids, it.ID)
		}
	}
	return ids
}

func sumItemAmounts(items []model.OrderItem) float64 {
	var sum float64
	for _, it := range items {
		if strings.TrimSpace(it.SplitKind) != "" {
			continue
		}
		if it.TotalAmount > 0 {
			sum += it.TotalAmount
		} else {
			sum += it.Price * float64(it.Quantity)
		}
	}
	return roundMoney(sum)
}

// splitOrderForAllocate 把勾选商品迁到新履约单（同主单 tid，sysTid 加 #split 后缀保证唯一）。
func (s *OrderService) splitOrderForAllocate(ctx context.Context, tenantID, operatorID uint64, parent *model.Order, selectedRoots []model.OrderItem) (*model.Order, error) {
	moveIDs := collectMoveItemIDs(parent, selectedRoots)
	if len(moveIDs) == 0 {
		return nil, fmt.Errorf("没有可拆分的商品行")
	}
	childPay := sumItemAmounts(selectedRoots)
	parentRemainPay := roundMoney(parent.PayAmount - childPay)
	if parentRemainPay < 0 {
		parentRemainPay = sumItemAmounts(fulfillableRootItems(parent)) - childPay
		if parentRemainPay < 0 {
			parentRemainPay = 0
		}
	}

	var child *model.Order
	err := s.repos.Transaction(func(tx *repo.Repos) error {
		orderNo, nerr := tx.NextOrderNo(tenantID)
		if nerr != nil {
			return nerr
		}
		baseSys := basePlatformSysTid(parent.PlatformSysTid)
		child = &model.Order{
			TenantID:            tenantID,
			OrderNo:             orderNo,
			SourceChannel:       parent.SourceChannel,
			Platform:            parent.Platform,
			PlatformOrderID:     parent.PlatformOrderID,
			PlatformSysTid:      "", // 创建后再写 #split{id}
			ShopID:              parent.ShopID,
			ShopName:            parent.ShopName,
			ManualSourceID:      parent.ManualSourceID,
			ManualSourceName:    parent.ManualSourceName,
			ExternalRefID:       "",
			Status:              model.StatusPendingAlloc,
			ShipStatus:          model.ShipWaitShip,
			BuyerNick:           parent.BuyerNick,
			BuyerName:           parent.BuyerName,
			BuyerPhone:          parent.BuyerPhone,
			TotalAmount:         childPay,
			PayAmount:           childPay,
			FreightAmount:       0,
			PayStatus:           parent.PayStatus,
			PayTime:             parent.PayTime,
			OrderedAt:           parent.OrderedAt,
			PlatformStatus:      parent.PlatformStatus,
			PlatformStatusText:  parent.PlatformStatusText,
			EcommerceStatus:     parent.EcommerceStatus,
			EcommerceStatusText: parent.EcommerceStatusText,
			AfterSaleStatus:     parent.AfterSaleStatus,
			AfterSaleStatusText: parent.AfterSaleStatusText,
			AgentType:           0,
			Remark:              parent.Remark,
			SellerRemark:        parent.SellerRemark,
			SellerFlag:          parent.SellerFlag,
			FenFaRemark:         parent.FenFaRemark,
			PrinterRemark:       parent.PrinterRemark,
			RawPayload:          parent.RawPayload,
			SplitFromOrderID:    parent.ID,
		}
		if parent.Address != nil {
			addr := *parent.Address
			addr.ID = 0
			addr.OrderID = 0
			child.Address = &addr
		}
		if err := tx.CreateOrder(child); err != nil {
			return err
		}
		sysTid := baseSys
		if sysTid == "" {
			sysTid = strings.TrimSpace(parent.PlatformOrderID)
		}
		if sysTid != "" {
			child.PlatformSysTid = fmt.Sprintf("%s#split%d", sysTid, child.ID)
			if err := tx.UpdateOrderFields(tenantID, child.ID, map[string]any{
				"platform_sys_tid": child.PlatformSysTid,
			}); err != nil {
				return err
			}
		}
		if err := tx.MoveOrderItems(tenantID, parent.ID, child.ID, moveIDs); err != nil {
			return err
		}
		parentFields := map[string]any{
			"pay_amount":    parentRemainPay,
			"total_amount":  parentRemainPay,
			"freight_amount": 0,
		}
		if err := tx.UpdateOrderFields(tenantID, parent.ID, parentFields); err != nil {
			return err
		}
		if err := tx.AddStatusLog(&model.OrderStatusLog{
			TenantID:   tenantID,
			OrderID:    parent.ID,
			FromStatus: parent.Status,
			ToStatus:   parent.Status,
			Action:     "alloc_split_out",
			Remark:     fmt.Sprintf("商品级分配拆出 %s（%d 行）", child.OrderNo, len(selectedRoots)),
			OperatorID: operatorID,
		}); err != nil {
			return err
		}
		return tx.AddStatusLog(&model.OrderStatusLog{
			TenantID:   tenantID,
			OrderID:    child.ID,
			FromStatus: "",
			ToStatus:   model.StatusPendingAlloc,
			Action:     "alloc_split_in",
			Remark:     fmt.Sprintf("由 %s 商品级拆分产生", parent.OrderNo),
			OperatorID: operatorID,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("商品拆分失败: %w", err)
	}
	_ = ctx
	out, err := s.repos.GetOrder(tenantID, child.ID)
	if err != nil {
		return nil, err
	}
	log.Printf("[ordercore] alloc-split parent=%s -> child=%s items=%d", parent.OrderNo, out.OrderNo, len(selectedRoots))
	return out, nil
}
