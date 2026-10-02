package service

import (
	"fmt"
	"strings"
	"time"

	"ordercore/internal/model"
)

// selectAllocateItemIDs 解析勾选行：空=全部可履约根行；否则仅勾选根行。
func selectAllocateItemIDs(o *model.Order, itemIDs []uint64) ([]uint64, error) {
	roots := fulfillableRootItems(o)
	if len(roots) == 0 {
		return nil, nil
	}
	want := normalizeItemIDSet(itemIDs)
	if len(want) == 0 {
		out := make([]uint64, 0, len(roots))
		for _, it := range roots {
			out = append(out, it.ID)
		}
		return out, nil
	}
	out := make([]uint64, 0, len(want))
	for _, it := range roots {
		if _, ok := want[it.ID]; ok {
			out = append(out, it.ID)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("请勾选本单待分配的商品")
	}
	return out, nil
}

func filterItemsByIDs(items []model.OrderItem, ids []uint64) []model.OrderItem {
	want := normalizeItemIDSet(ids)
	if len(want) == 0 {
		return items
	}
	out := make([]model.OrderItem, 0, len(want))
	for _, it := range items {
		if _, ok := want[it.ID]; ok {
			out = append(out, it)
		}
	}
	return out
}

func itemAllocFields(allocType, dropshipMode, supplierName, factoryID, factoryName, purchaseOrderID, selfOrderNo string, supplierID uint64, now time.Time) map[string]any {
	return map[string]any{
		"alloc_type":        allocType,
		"dropship_mode":     dropshipMode,
		"supplier_id":       supplierID,
		"supplier_name":     supplierName,
		"factory_id":        factoryID,
		"factory_name":      factoryName,
		"purchase_order_id": purchaseOrderID,
		"self_order_no":     selfOrderNo,
		"ship_status":       model.ShipWaitShip,
		"allocated_at":      now,
		"updated_at":        now,
	}
}

// rollupOrderFulfillmentFields 根据行级履约汇总头表字段。
func rollupOrderFulfillmentFields(o *model.Order) map[string]any {
	roots := fulfillableRootItems(o)
	if len(roots) == 0 {
		return nil
	}

	var (
		allocTypes   = map[string]struct{}{}
		shipStatuses = map[string]struct{}{}
		poNos        = map[string]struct{}{}
		selfNos      = map[string]struct{}{}
		supplierID   uint64
		supplierName string
		factoryID    string
		factoryName  string
		dropshipMode string
		agentType    int
		anyAlloc     bool
		allAlloc     = true
		latestAlloc  *time.Time
	)

	for _, it := range roots {
		at := strings.TrimSpace(it.AllocType)
		if at == "" {
			allAlloc = false
			continue
		}
		anyAlloc = true
		allocTypes[at] = struct{}{}
		ss := strings.TrimSpace(it.ShipStatus)
		if ss == "" {
			ss = model.ShipWaitShip
		}
		shipStatuses[ss] = struct{}{}
		if po := strings.TrimSpace(it.PurchaseOrderID); po != "" {
			poNos[po] = struct{}{}
		}
		if so := strings.TrimSpace(it.SelfOrderNo); so != "" {
			selfNos[so] = struct{}{}
		}
		if supplierID == 0 && it.SupplierID > 0 {
			supplierID = it.SupplierID
			supplierName = it.SupplierName
		}
		if factoryID == "" && strings.TrimSpace(it.FactoryID) != "" {
			factoryID = it.FactoryID
			factoryName = it.FactoryName
		}
		if dropshipMode == "" && strings.TrimSpace(it.DropshipMode) != "" {
			dropshipMode = it.DropshipMode
		}
		if it.AllocatedAt != nil && (latestAlloc == nil || it.AllocatedAt.After(*latestAlloc)) {
			t := *it.AllocatedAt
			latestAlloc = &t
		}
		if at == model.AllocDropship && it.DropshipMode == model.DropshipKDZSFactory {
			agentType = model.AgentTypeFactory
		} else if agentType == 0 && (at == model.AllocSelfShip || at == model.AllocDropship) {
			agentType = model.AgentTypeSelf
		}
	}

	fields := map[string]any{}
	if !anyAlloc {
		fields["alloc_type"] = ""
		fields["dropship_mode"] = ""
		fields["supplier_id"] = 0
		fields["supplier_name"] = ""
		fields["factory_id"] = ""
		fields["factory_name"] = ""
		fields["purchase_order_id"] = ""
		fields["self_order_no"] = ""
		fields["allocated_at"] = nil
		fields["status"] = model.StatusPendingAlloc
		fields["ship_status"] = model.ShipWaitShip
		fields["agent_type"] = model.AgentTypeSelf
		return fields
	}

	allocType := ""
	if len(allocTypes) == 1 {
		for k := range allocTypes {
			allocType = k
		}
	} else {
		allocType = model.AllocMixed
	}

	shipStatus := model.ShipWaitShip
	_, hasShipped := shipStatuses[model.ShipShipped]
	_, hasPartial := shipStatuses[model.ShipPartialShipped]
	_, hasWait := shipStatuses[model.ShipWaitShip]
	if hasShipped && !hasWait && !hasPartial && allAlloc {
		shipStatus = model.ShipShipped
	} else if hasShipped || hasPartial {
		shipStatus = model.ShipPartialShipped
	}

	status := model.StatusAllocated
	if allocType == model.AllocPurchaseThenShip && len(allocTypes) == 1 {
		status = model.StatusPurchasing
	}

	poNo := ""
	if len(poNos) == 1 {
		for k := range poNos {
			poNo = k
		}
	}
	selfNo := ""
	if len(selfNos) == 1 {
		for k := range selfNos {
			selfNo = k
		}
	}

	fields["alloc_type"] = allocType
	fields["dropship_mode"] = dropshipMode
	fields["supplier_id"] = supplierID
	fields["supplier_name"] = supplierName
	fields["factory_id"] = factoryID
	fields["factory_name"] = factoryName
	fields["purchase_order_id"] = poNo
	fields["self_order_no"] = selfNo
	fields["allocated_at"] = latestAlloc
	fields["status"] = status
	fields["ship_status"] = shipStatus
	if agentType == 0 {
		agentType = model.AgentTypeSelf
	}
	fields["agent_type"] = agentType
	return fields
}

func orderHasItemLevelAlloc(o *model.Order) bool {
	if o == nil {
		return false
	}
	for _, it := range o.Items {
		if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
			continue
		}
		if strings.TrimSpace(it.AllocType) != "" {
			return true
		}
	}
	return false
}

func orderHasUnallocatedRoots(o *model.Order) bool {
	for _, it := range fulfillableRootItems(o) {
		if strings.TrimSpace(it.AllocType) == "" {
			return true
		}
	}
	return false
}

func orderHasAllocType(o *model.Order, allocType string) bool {
	if o == nil || allocType == "" {
		return false
	}
	if strings.TrimSpace(o.AllocType) == allocType {
		return true
	}
	for _, it := range o.Items {
		if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
			continue
		}
		if strings.TrimSpace(it.AllocType) == allocType {
			return true
		}
	}
	return false
}

func orderDropshipPONos(o *model.Order) []string {
	if o == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	add := func(po string) {
		po = strings.TrimSpace(po)
		if po == "" {
			return
		}
		if _, ok := seen[po]; ok {
			return
		}
		seen[po] = struct{}{}
		out = append(out, po)
	}
	add(o.PurchaseOrderID)
	for _, it := range o.Items {
		if strings.TrimSpace(it.AllocType) == model.AllocDropship {
			add(it.PurchaseOrderID)
		}
	}
	return out
}

func platformOidsForItemIDs(o *model.Order, itemIDs []uint64) []string {
	want := normalizeItemIDSet(itemIDs)
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, it := range o.Items {
		if len(want) > 0 {
			if _, ok := want[it.ID]; !ok {
				continue
			}
		}
		oid := strings.TrimSpace(it.PlatformOid)
		if oid == "" {
			continue
		}
		if _, ok := seen[oid]; ok {
			continue
		}
		seen[oid] = struct{}{}
		out = append(out, oid)
	}
	return out
}
