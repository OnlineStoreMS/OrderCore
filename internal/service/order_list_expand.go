package service

import (
	"fmt"
	"strings"

	"ordercore/internal/model"
)

// ExpandOrdersByPackages 按快递助手系统编号展开列表行：一 tid 多包时搜订单号命中多行（与助手列表一致）。
// 同一 OC 仍共享 id，详情仍进同一张单。
func ExpandOrdersByPackages(list []model.Order) []model.Order {
	if len(list) == 0 {
		return list
	}
	out := make([]model.Order, 0, len(list))
	for i := range list {
		o := list[i]
		pkgs := o.Packages
		if len(pkgs) <= 1 {
			out = append(out, o)
			continue
		}
		for _, p := range pkgs {
			row := o
			row.PlatformSysTid = p.PlatformSysTid
			if fen := strings.TrimSpace(p.FenFaRemark); fen != "" {
				row.FenFaRemark = fen
			}
			row.Packages = []model.OrderPackage{p}
			items := make([]model.OrderItem, 0, len(o.Items))
			for _, it := range o.Items {
				if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
					continue
				}
				if it.PackageID == p.ID || (it.PackageID == 0 && p.IsPrimary) {
					items = append(items, it)
				}
			}
			if len(items) > 0 {
				row.Items = items
			} else {
				row.Items = o.Items
			}
			// 行级角标：该包统一履约则盖到头表，便于列表一眼区分自营/代发
			if at := uniformItemAlloc(items); at != "" {
				row.AllocType = at
			}
			out = append(out, row)
		}
	}
	return out
}

// ExpandOrdersByAllocGroups 按商品分配组拆列表行：未分配 / 不同自营代发组合各占一行。
// 部分分配单在「已分配」与「待分配」各自只看到对应商品，互不混排。
func ExpandOrdersByAllocGroups(list []model.Order) []model.Order {
	if len(list) == 0 {
		return list
	}
	out := make([]model.Order, 0, len(list)*2)
	for i := range list {
		o := list[i]
		type allocGroup struct {
			key   string
			roots []model.OrderItem
		}
		groups := make([]allocGroup, 0, 2)
		idx := map[string]int{}
		for _, it := range o.Items {
			if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
				continue
			}
			key := allocGroupKey(it)
			if j, ok := idx[key]; ok {
				groups[j].roots = append(groups[j].roots, it)
				continue
			}
			idx[key] = len(groups)
			groups = append(groups, allocGroup{key: key, roots: []model.OrderItem{it}})
		}
		if len(groups) <= 1 {
			row := o
			if len(groups) == 1 {
				row.ListRowKey = fmt.Sprintf("%d:%s", o.ID, groups[0].key)
				applyAllocGroupHeader(&row, groups[0].key, groups[0].roots)
			} else {
				row.ListRowKey = fmt.Sprintf("%d", o.ID)
			}
			out = append(out, row)
			continue
		}
		for _, g := range groups {
			row := o
			row.ListRowKey = fmt.Sprintf("%d:%s", o.ID, g.key)
			row.Items = filterItemsForAllocRoots(o.Items, g.roots)
			applyAllocGroupHeader(&row, g.key, g.roots)
			out = append(out, row)
		}
	}
	return out
}

// FilterAllocGroupRowsForTab 分配页按 Tab 只保留对应分组行。
func FilterAllocGroupRowsForTab(list []model.Order, status string) []model.Order {
	status = strings.TrimSpace(status)
	if status != model.StatusPendingAlloc && status != model.StatusAllocated {
		return list
	}
	out := make([]model.Order, 0, len(list))
	for i := range list {
		o := list[i]
		hasAlloc, hasUnalloc := false, false
		for _, it := range o.Items {
			if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
				continue
			}
			if orderItemExcludedFromFulfillment(it) {
				continue
			}
			if strings.TrimSpace(it.AllocType) != "" {
				hasAlloc = true
			} else {
				hasUnalloc = true
			}
		}
		if status == model.StatusPendingAlloc && hasUnalloc {
			out = append(out, o)
		}
		if status == model.StatusAllocated && hasAlloc {
			out = append(out, o)
		}
	}
	return out
}

func allocGroupKey(it model.OrderItem) string {
	a := strings.TrimSpace(it.AllocType)
	if a == "" {
		return "unalloc"
	}
	return fmt.Sprintf("%s|%s|%d|%s|%s|%s",
		a,
		strings.TrimSpace(it.DropshipMode),
		it.SupplierID,
		strings.TrimSpace(it.FactoryID),
		strings.TrimSpace(it.PurchaseOrderID),
		strings.TrimSpace(it.SelfOrderNo),
	)
}

func filterItemsForAllocRoots(all []model.OrderItem, roots []model.OrderItem) []model.OrderItem {
	rootIDs := make(map[uint64]struct{}, len(roots))
	for _, it := range roots {
		rootIDs[it.ID] = struct{}{}
	}
	out := make([]model.OrderItem, 0, len(roots)*2)
	for _, it := range all {
		if _, ok := rootIDs[it.ID]; ok {
			out = append(out, it)
			continue
		}
		if it.ParentOrderItemID > 0 {
			if _, ok := rootIDs[it.ParentOrderItemID]; ok {
				out = append(out, it)
			}
		}
	}
	return out
}

func applyAllocGroupHeader(row *model.Order, key string, roots []model.OrderItem) {
	if row == nil {
		return
	}
	if key == "unalloc" {
		row.AllocType = ""
		row.DropshipMode = ""
		row.SupplierID = 0
		row.SupplierName = ""
		row.FactoryID = ""
		row.FactoryName = ""
		row.PurchaseOrderID = ""
		row.SelfOrderNo = ""
		return
	}
	if len(roots) == 0 {
		return
	}
	first := roots[0]
	if at := uniformItemAlloc(roots); at != "" {
		row.AllocType = at
	} else {
		row.AllocType = strings.TrimSpace(first.AllocType)
	}
	row.DropshipMode = first.DropshipMode
	row.SupplierID = first.SupplierID
	row.SupplierName = first.SupplierName
	row.FactoryID = first.FactoryID
	row.FactoryName = first.FactoryName
	row.PurchaseOrderID = first.PurchaseOrderID
	row.SelfOrderNo = first.SelfOrderNo
	// 部分分配头表常为 pending_alloc：已分配组行展示为已分配，便于撤回勾选
	if row.Status == model.StatusPendingAlloc {
		row.Status = model.StatusAllocated
	}
}

func uniformItemAlloc(items []model.OrderItem) string {
	at := ""
	for _, it := range items {
		a := strings.TrimSpace(it.AllocType)
		if a == "" {
			return ""
		}
		if at == "" {
			at = a
			continue
		}
		if at != a {
			return ""
		}
	}
	return at
}
