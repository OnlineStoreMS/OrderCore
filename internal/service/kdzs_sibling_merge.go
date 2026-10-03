package service

import (
	"fmt"
	"log"
	"strings"
	"time"

	"ordercore/internal/model"
	"ordercore/internal/repo"
)

// forceMergeKDZSSiblingsIntoKeeper 同平台主单 tid 只保留一张 OC：把其余兄弟（含 #split）迁入 keeper 后删除。
// 已有相同 platform_oid 的商品行不重复迁入；运单按快递单号去重。
func (s *OrderService) forceMergeKDZSSiblingsIntoKeeper(tenantID uint64, seed *model.Order) *model.Order {
	if seed == nil || strings.TrimSpace(seed.SourceChannel) != model.SourceKDZS {
		return seed
	}
	tid := strings.TrimSpace(seed.PlatformOrderID)
	if tid == "" {
		return seed
	}
	keeper := s.findKDZSParentKeeper(tenantID, tid)
	if keeper == nil {
		return seed
	}
	// 若 seed 本身是更好的主单（极少），仍以 findKDZSParentKeeper 为准
	fullKeeper, err := s.repos.GetOrder(tenantID, keeper.ID)
	if err != nil || fullKeeper == nil {
		return seed
	}
	list, err := s.repos.ListBySourcePlatform(tenantID, model.SourceKDZS, tid)
	if err != nil {
		log.Printf("[ordercore] force-merge list tid=%s: %v", tid, err)
		return fullKeeper
	}
	merged := 0
	for i := range list {
		sib := &list[i]
		if sib.ID == fullKeeper.ID {
			continue
		}
		if err := s.mergeOneKDZSSiblingIntoKeeper(tenantID, fullKeeper, sib); err != nil {
			log.Printf("[ordercore] force-merge sibling=%s -> keeper=%s: %v", sib.OrderNo, fullKeeper.OrderNo, err)
			continue
		}
		merged++
		log.Printf("[ordercore] force-merged sibling=%s into keeper=%s tid=%s", sib.OrderNo, fullKeeper.OrderNo, tid)
	}
	out, err := s.repos.GetOrder(tenantID, fullKeeper.ID)
	if err != nil {
		return fullKeeper
	}
	if merged > 0 {
		s.rollupAfterSiblingMerge(tenantID, out, merged)
		out, _ = s.repos.GetOrder(tenantID, fullKeeper.ID)
	}
	if out != nil {
		// 并入后按明细重算实付，避免残留单包裹金额
		sum := sumItemAmounts(out.Items)
		if sum > 0 && (roundMoney(out.PayAmount) != sum || roundMoney(out.TotalAmount) != sum) {
			_ = s.repos.UpdateOrderFields(tenantID, out.ID, map[string]any{
				"pay_amount":   sum,
				"total_amount": sum,
			})
			out.PayAmount = sum
			out.TotalAmount = sum
		}
		hydrateOrderPackageRemarks(out)
		if fen := effectivePackageFenFaRemark(out); fen != "" && strings.TrimSpace(out.FenFaRemark) != fen {
			_ = s.repos.UpdateOrderFields(tenantID, out.ID, map[string]any{"fen_fa_remark": fen})
			out.FenFaRemark = fen
		}
		return out
	}
	return fullKeeper
}

func (s *OrderService) mergeOneKDZSSiblingIntoKeeper(tenantID uint64, keeper, sibling *model.Order) error {
	if keeper == nil || sibling == nil || sibling.ID == keeper.ID {
		return nil
	}
	full, err := s.repos.GetOrder(tenantID, sibling.ID)
	if err != nil {
		return err
	}
	keeperFull, err := s.repos.GetOrder(tenantID, keeper.ID)
	if err != nil {
		return err
	}

	keeperOID := map[string]uint64{}
	for _, it := range keeperFull.Items {
		if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
			continue
		}
		oid := strings.TrimSpace(it.PlatformOid)
		if oid != "" {
			keeperOID[oid] = it.ID
		}
	}
	keeperExpress := map[string]struct{}{}
	for _, sh := range keeperFull.Shipments {
		if no := strings.TrimSpace(sh.ExpressNo); no != "" {
			keeperExpress[no] = struct{}{}
		}
	}

	now := time.Now()
	moveIDs := make([]uint64, 0)
	for _, it := range full.Items {
		oid := strings.TrimSpace(it.PlatformOid)
		if oid != "" {
			if kid, ok := keeperOID[oid]; ok {
				// 同 oid 已在 keeper：把兄弟行履约补到空行上，不迁入
				if strings.TrimSpace(it.AllocType) != "" {
					var keeperItem *model.OrderItem
					for i := range keeperFull.Items {
						if keeperFull.Items[i].ID == kid {
							keeperItem = &keeperFull.Items[i]
							break
						}
					}
					if keeperItem != nil && strings.TrimSpace(keeperItem.AllocType) == "" {
						_ = s.repos.UpdateOrderItemFields(tenantID, kid, map[string]any{
							"alloc_type":        it.AllocType,
							"dropship_mode":     it.DropshipMode,
							"supplier_id":       it.SupplierID,
							"supplier_name":     it.SupplierName,
							"factory_id":        it.FactoryID,
							"factory_name":      it.FactoryName,
							"purchase_order_id": it.PurchaseOrderID,
							"self_order_no":     it.SelfOrderNo,
							"ship_status":       it.ShipStatus,
							"allocated_at":      it.AllocatedAt,
							"updated_at":        now,
						})
					}
				} else if strings.TrimSpace(full.AllocType) != "" {
					var keeperItem *model.OrderItem
					for i := range keeperFull.Items {
						if keeperFull.Items[i].ID == kid {
							keeperItem = &keeperFull.Items[i]
							break
						}
					}
					if keeperItem != nil && strings.TrimSpace(keeperItem.AllocType) == "" {
						_ = s.repos.UpdateOrderItemFields(tenantID, kid, map[string]any{
							"alloc_type":        full.AllocType,
							"dropship_mode":     full.DropshipMode,
							"supplier_id":       full.SupplierID,
							"supplier_name":     full.SupplierName,
							"factory_id":        full.FactoryID,
							"factory_name":      full.FactoryName,
							"purchase_order_id": full.PurchaseOrderID,
							"self_order_no":     full.SelfOrderNo,
							"ship_status":       coalesceShip(full.ShipStatus),
							"allocated_at":      full.AllocatedAt,
							"updated_at":        now,
						})
					}
				}
				continue
			}
		}
		// 兄弟行无 alloc 时把头表履约落到行上再迁
		if strings.TrimSpace(it.AllocType) == "" && strings.TrimSpace(full.AllocType) != "" {
			_ = s.repos.UpdateOrderItemFields(tenantID, it.ID, map[string]any{
				"alloc_type":        full.AllocType,
				"dropship_mode":     full.DropshipMode,
				"supplier_id":       full.SupplierID,
				"supplier_name":     full.SupplierName,
				"factory_id":        full.FactoryID,
				"factory_name":      full.FactoryName,
				"purchase_order_id": full.PurchaseOrderID,
				"self_order_no":     full.SelfOrderNo,
				"ship_status":       coalesceShip(full.ShipStatus),
				"allocated_at":      full.AllocatedAt,
				"updated_at":        now,
			})
		}
		moveIDs = append(moveIDs, it.ID)
	}
	if len(moveIDs) > 0 {
		if err := s.repos.MoveOrderItems(tenantID, full.ID, keeper.ID, moveIDs); err != nil {
			return fmt.Errorf("move items: %w", err)
		}
	}

	sysTid := basePlatformSysTid(full.PlatformSysTid)
	if sysTid != "" {
		_ = s.repos.UpsertOrderPackage(&model.OrderPackage{
			TenantID:           tenantID,
			OrderID:            keeper.ID,
			PlatformSysTid:     sysTid,
			FenFaRemark:        full.FenFaRemark,
			PrinterRemark:      full.PrinterRemark,
			PlatformStatus:     full.PlatformStatus,
			PlatformStatusText: full.PlatformStatusText,
			IsPrimary:          false,
		})
	}

	// 运单：无同快递单号才迁
	for _, sh := range full.Shipments {
		no := strings.TrimSpace(sh.ExpressNo)
		if no != "" {
			if _, ok := keeperExpress[no]; ok {
				continue
			}
		}
		_ = s.repos.DB().Exec(
			`UPDATE order_shipments SET order_id = ? WHERE tenant_id = ? AND id = ?`,
			keeper.ID, tenantID, sh.ID,
		).Error
		_ = s.repos.DB().Exec(
			`UPDATE order_shipment_items SET order_id = ? WHERE tenant_id = ? AND shipment_id = ?`,
			keeper.ID, tenantID, sh.ID,
		).Error
	}

	return s.repos.DeleteOrderCascade(tenantID, full.ID)
}

func coalesceShip(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return model.ShipWaitShip
	}
	return s
}

func (s *OrderService) rollupAfterSiblingMerge(tenantID uint64, o *model.Order, merged int) {
	if o == nil || merged <= 0 {
		return
	}
	fields := rollupOrderFulfillmentFields(o)
	if len(fields) > 0 {
		// 已发货/完成的头表状态不要被 rollup 打回 wait_ship
		if o.ShipStatus == model.ShipShipped || o.Status == model.StatusCompleted || o.Status == model.StatusClosed {
			delete(fields, "status")
			if o.ShipStatus == model.ShipShipped {
				fields["ship_status"] = model.ShipShipped
			}
		}
		_ = s.repos.UpdateOrderFields(tenantID, o.ID, fields)
	}
	_ = s.repos.Transaction(func(tx *repo.Repos) error {
		return tx.AddStatusLog(&model.OrderStatusLog{
			TenantID:   tenantID,
			OrderID:    o.ID,
			FromStatus: o.Status,
			ToStatus:   o.Status,
			Action:     "kdzs_sibling_merge",
			Remark:     fmt.Sprintf("同主单 %d 个包裹并入单一 OC", merged),
		})
	})
}
