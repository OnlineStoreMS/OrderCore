package service

import (
	"log"
	"strings"
	"time"

	"ordercore/internal/dto"
	"ordercore/internal/model"
)

// applyPackageItemFulfillmentFromIngest 按本轮快递助手系统编号（包裹）写行级履约：
// 商品挂 package_id，alloc_type 跟该包 agentType/状态；再 rollup 头表。
// OSMS 线下代发行不覆盖。
func (s *OrderService) applyPackageItemFulfillmentFromIngest(tenantID uint64, o *model.Order, req dto.IngestOrderRequest, hint kdzsIngestHint) *model.Order {
	if o == nil || strings.TrimSpace(req.SourceChannel) != model.SourceKDZS {
		return o
	}
	sysTid := strings.TrimSpace(req.PlatformSysTid)
	if sysTid == "" {
		return o
	}
	pkg, err := s.repos.FindPackageBySysTid(tenantID, sysTid)
	if err != nil || pkg == nil || pkg.ID == 0 || pkg.OrderID != o.ID {
		return o
	}

	cur, err := s.repos.GetOrder(tenantID, o.ID)
	if err != nil || cur == nil {
		return o
	}

	matchedIDs := matchIngestItemsToOrder(cur.Items, req.Items)
	if len(matchedIDs) == 0 {
		return cur
	}

	now := time.Now()
	for _, id := range matchedIDs {
		var it *model.OrderItem
		for i := range cur.Items {
			if cur.Items[i].ID == id {
				it = &cur.Items[i]
				break
			}
		}
		if it == nil {
			continue
		}
		fields := map[string]any{
			"package_id": pkg.ID,
			"updated_at": now,
		}
		if preserveOSMSItemDropship(it) {
			_ = s.repos.UpdateOrderItemFields(tenantID, id, fields)
			continue
		}
		if hint.ClearAlloc {
			fields["alloc_type"] = ""
			fields["dropship_mode"] = ""
			fields["supplier_id"] = 0
			fields["supplier_name"] = ""
			fields["factory_id"] = ""
			fields["factory_name"] = ""
			fields["purchase_order_id"] = ""
			fields["self_order_no"] = ""
			fields["ship_status"] = model.ShipWaitShip
			fields["allocated_at"] = nil
		} else if hint.ApplySyncAlloc && strings.TrimSpace(hint.AllocType) != "" {
			// 已自营分配的行，同步到已发货时不要改成渠道已发
			alloc := hint.AllocType
			mode := hint.DropshipMode
			if alloc == model.AllocChannelShip &&
				(it.AllocType == model.AllocSelfShip || it.AllocType == model.AllocPurchaseThenShip) {
				alloc = it.AllocType
				mode = it.DropshipMode
			}
			fields["alloc_type"] = alloc
			fields["dropship_mode"] = mode
			fields["ship_status"] = coalesceShip(hint.ShipStatus)
			if it.AllocatedAt == nil {
				fields["allocated_at"] = now
			}
			if alloc == model.AllocDropship && mode == model.DropshipKDZSFactory {
				fields["factory_id"] = strings.TrimSpace(req.FactoryID)
				fields["factory_name"] = strings.TrimSpace(req.FactoryName)
				if sid, sname := s.resolveBoundSupplier(tenantID, req.FactoryID, req.FactoryName); sid > 0 {
					fields["supplier_id"] = sid
					fields["supplier_name"] = sname
				}
			} else if alloc == model.AllocSelfShip || alloc == model.AllocChannelShip {
				fields["supplier_id"] = 0
				fields["supplier_name"] = ""
				fields["factory_id"] = ""
				fields["factory_name"] = ""
				fields["purchase_order_id"] = ""
			}
		}
		if err := s.repos.UpdateOrderItemFields(tenantID, id, fields); err != nil {
			log.Printf("[ordercore] package item fulfill order=%s item=%d: %v", cur.OrderNo, id, err)
		}
	}

	cur, _ = s.repos.GetOrder(tenantID, o.ID)
	if cur == nil {
		return o
	}
	s.rollupPackageFulfillmentHeader(tenantID, cur)
	cur, _ = s.repos.GetOrder(tenantID, o.ID)
	if cur != nil {
		return cur
	}
	return o
}

func preserveOSMSItemDropship(it *model.OrderItem) bool {
	if it == nil {
		return false
	}
	return strings.TrimSpace(it.AllocType) == model.AllocDropship &&
		strings.TrimSpace(it.DropshipMode) == model.DropshipOSMSSupplier &&
		it.SupplierID > 0
}

// matchIngestItemsToOrder 把本轮包裹商品匹配到 OC 行（优先 oid，再 item+sku，再品名规格）。
func matchIngestItemsToOrder(items []model.OrderItem, inputs []dto.OrderItemInput) []uint64 {
	roots := make([]model.OrderItem, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
			continue
		}
		roots = append(roots, it)
	}
	used := map[uint64]struct{}{}
	out := make([]uint64, 0, len(inputs))
	for _, in := range inputs {
		id := matchRootItemID(roots, used,
			strings.TrimSpace(in.PlatformOid),
			strings.TrimSpace(in.PlatformItemID),
			strings.TrimSpace(in.PlatformSkuID),
			strings.TrimSpace(in.ProductName),
			strings.TrimSpace(in.SkuSpecs),
		)
		if id == 0 {
			continue
		}
		used[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func matchRootItemID(roots []model.OrderItem, used map[uint64]struct{}, oid, itemID, skuID, name, specs string) uint64 {
	if oid != "" {
		for i := range roots {
			if _, ok := used[roots[i].ID]; ok {
				continue
			}
			if strings.TrimSpace(roots[i].PlatformOid) == oid {
				return roots[i].ID
			}
		}
	}
	if itemID != "" {
		for i := range roots {
			if _, ok := used[roots[i].ID]; ok {
				continue
			}
			if strings.TrimSpace(roots[i].PlatformItemID) == itemID &&
				(skuID == "" || strings.TrimSpace(roots[i].PlatformSkuID) == skuID) {
				return roots[i].ID
			}
		}
	}
	if name != "" {
		for i := range roots {
			if _, ok := used[roots[i].ID]; ok {
				continue
			}
			if strings.TrimSpace(roots[i].ProductName) == name &&
				strings.TrimSpace(roots[i].SkuSpecs) == specs {
				return roots[i].ID
			}
		}
	}
	return 0
}

func (s *OrderService) rollupPackageFulfillmentHeader(tenantID uint64, o *model.Order) {
	if o == nil {
		return
	}
	fields := rollupOrderFulfillmentFields(o)
	if len(fields) == 0 {
		return
	}
	// 已完成/关闭保留头表状态；已发货保留 ship_status
	if o.Status == model.StatusCompleted || o.Status == model.StatusClosed {
		delete(fields, "status")
	}
	if o.ShipStatus == model.ShipShipped {
		fields["ship_status"] = model.ShipShipped
	}
	if err := s.repos.UpdateOrderFields(tenantID, o.ID, fields); err != nil {
		log.Printf("[ordercore] rollup package fulfill order=%s: %v", o.OrderNo, err)
	}
}

// ingestItemOIDs 本轮包裹商品 oid 集合。
func ingestItemOIDs(req dto.IngestOrderRequest) map[string]struct{} {
	out := map[string]struct{}{}
	for _, it := range req.Items {
		if oid := strings.TrimSpace(it.PlatformOid); oid != "" {
			out[oid] = struct{}{}
		}
	}
	return out
}

func orderItemOIDs(o *model.Order) map[string]struct{} {
	out := map[string]struct{}{}
	if o == nil {
		return out
	}
	for _, it := range o.Items {
		if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
			continue
		}
		if oid := strings.TrimSpace(it.PlatformOid); oid != "" {
			out[oid] = struct{}{}
		}
	}
	return out
}
