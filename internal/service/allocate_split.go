package service

import (
	"context"
	"fmt"
	"log"
	"strings"

	"ordercore/internal/model"
	"ordercore/internal/repo"
)

// resolveAllocateTarget 商品级分配：校验勾选行后始终返回原单（不再拆 #split 子单）。
// 部分勾选时由调用方对选中行写行级履约字段。
func (s *OrderService) resolveAllocateTarget(ctx context.Context, tenantID, operatorID uint64, o *model.Order, itemIDs []uint64) (*model.Order, []uint64, error) {
	_ = ctx
	_ = tenantID
	_ = operatorID
	if o == nil {
		return nil, nil, fmt.Errorf("订单不存在")
	}
	selected, err := selectAllocateItemIDs(o, itemIDs)
	if err != nil {
		return nil, nil, err
	}
	if len(selected) == 0 {
		return o, nil, nil
	}
	// 已分配行不可重复分配（允许整单再分配时覆盖？——禁止已分配行）
	want := normalizeItemIDSet(selected)
	for _, it := range o.Items {
		if _, ok := want[it.ID]; !ok {
			continue
		}
		if strings.TrimSpace(it.AllocType) != "" {
			return nil, nil, fmt.Errorf("商品「%s」已分配，请先撤回后再分配", strings.TrimSpace(it.ProductName))
		}
		if it.ShipStatus == model.ShipShipped || it.ShipStatus == model.ShipPartialShipped {
			return nil, nil, fmt.Errorf("商品「%s」已发货，不可再分配", strings.TrimSpace(it.ProductName))
		}
	}
	return o, selected, nil
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

// orderEligibleForAllocSplitMerge 撤回分配后可合回原单：未发货、无履约占用。
func orderEligibleForAllocSplitMerge(o *model.Order) bool {
	if o == nil {
		return false
	}
	if o.Status == model.StatusCompleted || o.Status == model.StatusClosed {
		return false
	}
	if o.ShipStatus == model.ShipShipped || o.ShipStatus == model.ShipPartialShipped {
		return false
	}
	if strings.TrimSpace(o.AllocType) != "" {
		return false
	}
	if strings.TrimSpace(o.PurchaseOrderID) != "" || strings.TrimSpace(o.SelfOrderNo) != "" {
		return false
	}
	return true
}

// mergeAllocSplitAfterRevoke 商品级拆分子单在撤回分配后，若双方都空闲则合回原销售单。
// 返回合单后的原单；无需合并时返回 (nil, nil)。
func (s *OrderService) mergeAllocSplitAfterRevoke(ctx context.Context, tenantID, operatorID, revokedOrderID uint64) (*model.Order, error) {
	_ = ctx
	o, err := s.repos.GetOrder(tenantID, revokedOrderID)
	if err != nil {
		// 子单可能已被合回删除
		return nil, nil
	}
	if o.SplitFromOrderID > 0 {
		return s.tryMergeSplitChildIntoParent(tenantID, operatorID, o)
	}
	children, err := s.repos.ListBySplitFromOrderID(tenantID, o.ID)
	if err != nil {
		return nil, err
	}
	mergedAny := false
	for i := range children {
		ch := &children[i]
		if !orderEligibleForAllocSplitMerge(ch) {
			continue
		}
		if _, err := s.tryMergeSplitChildIntoParent(tenantID, operatorID, ch); err != nil {
			return nil, err
		}
		mergedAny = true
	}
	if !mergedAny {
		return nil, nil
	}
	return s.repos.GetOrder(tenantID, o.ID)
}

func (s *OrderService) tryMergeSplitChildIntoParent(tenantID, operatorID uint64, child *model.Order) (*model.Order, error) {
	if child == nil || child.SplitFromOrderID == 0 {
		return nil, nil
	}
	if !orderEligibleForAllocSplitMerge(child) {
		return nil, nil
	}
	parent, err := s.repos.GetOrder(tenantID, child.SplitFromOrderID)
	if err != nil {
		return nil, fmt.Errorf("合回原单失败：原销售单不存在")
	}
	if !orderEligibleForAllocSplitMerge(parent) {
		// 原单仍占用中：仅保持子单待分配，下次原单也撤回后再合
		return nil, nil
	}

	moveIDs := make([]uint64, 0, len(child.Items))
	for _, it := range child.Items {
		moveIDs = append(moveIDs, it.ID)
	}
	if len(moveIDs) == 0 {
		// 空壳子单直接删
		if err := s.repos.DeleteOrderCascade(tenantID, child.ID); err != nil {
			return nil, err
		}
		return s.repos.GetOrder(tenantID, parent.ID)
	}

	childPay := child.PayAmount
	if childPay <= 0 {
		childPay = sumItemAmounts(child.Items)
	}
	parentPay := roundMoney(parent.PayAmount + childPay)

	err = s.repos.Transaction(func(tx *repo.Repos) error {
		if err := tx.MoveOrderItems(tenantID, child.ID, parent.ID, moveIDs); err != nil {
			return err
		}
		if err := tx.UpdateOrderFields(tenantID, parent.ID, map[string]any{
			"pay_amount":     parentPay,
			"total_amount":   parentPay,
			"freight_amount": 0,
		}); err != nil {
			return err
		}
		return tx.AddStatusLog(&model.OrderStatusLog{
			TenantID:   tenantID,
			OrderID:    parent.ID,
			FromStatus: parent.Status,
			ToStatus:   parent.Status,
			Action:     "alloc_split_merge",
			Remark:     fmt.Sprintf("撤回分配后合回拆分子单 %s（%d 行）", child.OrderNo, len(moveIDs)),
			OperatorID: operatorID,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("合回原销售单失败: %w", err)
	}
	if err := s.repos.DeleteOrderCascade(tenantID, child.ID); err != nil {
		return nil, fmt.Errorf("合回后清理拆分子单失败: %w", err)
	}
	out, err := s.repos.GetOrder(tenantID, parent.ID)
	if err != nil {
		return nil, err
	}
	log.Printf("[ordercore] alloc-split-merge child=%s -> parent=%s items=%d", child.OrderNo, out.OrderNo, len(moveIDs))
	return out, nil
}
