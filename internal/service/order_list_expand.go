package service

import (
	"fmt"
	"strings"

	"ordercore/internal/model"
)

// ExpandOrdersByPackages 按快递助手系统编号展开列表行：一 tid 多包时拆成多行。
// 同一 OC 仍共享 id；详情用 ?sysTid= 进入单包视图。
func ExpandOrdersByPackages(list []model.Order) []model.Order {
	if len(list) == 0 {
		return list
	}
	out := make([]model.Order, 0, len(list))
	for i := range list {
		o := list[i]
		pkgs := o.Packages
		if len(pkgs) <= 1 {
			row := o
			if len(pkgs) == 1 {
				sys := strings.TrimSpace(pkgs[0].PlatformSysTid)
				row.ListRowKey = fmt.Sprintf("%d:pkg:%s", o.ID, sys)
				if sys != "" {
					row.PlatformSysTid = sys
				}
				applyPackageRemarks(&row, pkgs[0])
				applyPackageRowHeader(&row, pkgs[0], row.Items)
			} else if row.ListRowKey == "" {
				row.ListRowKey = fmt.Sprintf("%d", o.ID)
			}
			out = append(out, row)
			continue
		}
		// 先按包裹筛商品；有商品的包才拆行。全空时退回整单一行，避免「无效空包」占行。
		type pkgRow struct {
			pkg   model.OrderPackage
			items []model.OrderItem
		}
		rows := make([]pkgRow, 0, len(pkgs))
		for _, p := range pkgs {
			items := filterItemsForPackage(o.Items, p)
			if len(items) == 0 {
				continue
			}
			rows = append(rows, pkgRow{pkg: p, items: items})
		}
		if len(rows) == 0 {
			row := o
			if row.ListRowKey == "" {
				row.ListRowKey = fmt.Sprintf("%d", o.ID)
			}
			out = append(out, row)
			continue
		}
		for _, pr := range rows {
			p := pr.pkg
			row := o
			sys := strings.TrimSpace(p.PlatformSysTid)
			row.PlatformSysTid = sys
			row.ListRowKey = fmt.Sprintf("%d:pkg:%s", o.ID, sys)
			applyPackageRemarks(&row, p)
			row.Packages = []model.OrderPackage{p}
			row.Items = pr.items
			applyPackageRowHeader(&row, p, row.Items)
			out = append(out, row)
		}
	}
	return out
}

func filterItemsForPackage(all []model.OrderItem, p model.OrderPackage) []model.OrderItem {
	rootIDs := map[uint64]struct{}{}
	out := make([]model.OrderItem, 0, len(all))
	for _, it := range all {
		if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
			continue
		}
		if it.PackageID == p.ID || (it.PackageID == 0 && p.IsPrimary) {
			out = append(out, it)
			rootIDs[it.ID] = struct{}{}
		}
	}
	for _, it := range all {
		if it.ParentOrderItemID == 0 {
			continue
		}
		if _, ok := rootIDs[it.ParentOrderItemID]; ok {
			out = append(out, it)
		}
	}
	return out
}

// applyPackageRemarks 按包裹覆盖分发/打单备注；空字符串表示本包无备注，不得继承头表。
func applyPackageRemarks(row *model.Order, p model.OrderPackage) {
	if row == nil {
		return
	}
	row.FenFaRemark = strings.TrimSpace(p.FenFaRemark)
	row.PrinterRemark = strings.TrimSpace(p.PrinterRemark)
}

func applyPackageRowHeader(row *model.Order, p model.OrderPackage, items []model.OrderItem) {
	if row == nil {
		return
	}
	roots := make([]model.OrderItem, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
			continue
		}
		roots = append(roots, it)
	}
	if len(roots) == 0 {
		return
	}
	// 未分配 / 已分配混在同一包时按分配组头逻辑取统一值；否则用第一行
	key := allocGroupKey(roots[0])
	uniform := true
	for _, it := range roots[1:] {
		if allocGroupKey(it) != key {
			uniform = false
			break
		}
	}
	if uniform {
		applyAllocGroupHeader(row, key, roots)
	} else if at := uniformItemAlloc(roots); at != "" {
		row.AllocType = at
	}
	_ = p
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
				if row.ListRowKey == "" {
					row.ListRowKey = fmt.Sprintf("%d:%s", o.ID, groups[0].key)
				}
				applyAllocGroupHeader(&row, groups[0].key, groups[0].roots)
			} else if row.ListRowKey == "" {
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
			// 若组内商品同属一包，带上该包 sysTid 便于详情深链
			if pkgID := uniformPackageID(g.roots); pkgID > 0 {
				for _, p := range o.Packages {
					if p.ID == pkgID {
						row.Packages = []model.OrderPackage{p}
						row.PlatformSysTid = p.PlatformSysTid
						row.ListRowKey = fmt.Sprintf("%d:pkg:%s", o.ID, strings.TrimSpace(p.PlatformSysTid))
						applyPackageRemarks(&row, p)
						break
					}
				}
			}
			out = append(out, row)
		}
	}
	return out
}

func uniformPackageID(roots []model.OrderItem) uint64 {
	if len(roots) == 0 {
		return 0
	}
	id := roots[0].PackageID
	if id == 0 {
		return 0
	}
	for _, it := range roots[1:] {
		if it.PackageID != id {
			return 0
		}
	}
	return id
}

// ExpandOrdersForList 列表拆行：多包裹优先按系统编号；否则按分配组。
func ExpandOrdersForList(list []model.Order) []model.Order {
	if len(list) == 0 {
		return list
	}
	out := make([]model.Order, 0, len(list)*2)
	for i := range list {
		o := list[i]
		if len(o.Packages) > 1 {
			out = append(out, ExpandOrdersByPackages([]model.Order{o})...)
			continue
		}
		out = append(out, ExpandOrdersByAllocGroups([]model.Order{o})...)
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

// ScopeOrderBySysTid 详情裁剪：只保留指定系统编号包裹及其商品/运单（不写库）。
func ScopeOrderBySysTid(o *model.Order, sysTid string) *model.Order {
	if o == nil {
		return nil
	}
	sysTid = strings.TrimSpace(sysTid)
	if sysTid == "" {
		return o
	}
	var pkg *model.OrderPackage
	for i := range o.Packages {
		if strings.TrimSpace(o.Packages[i].PlatformSysTid) == sysTid {
			p := o.Packages[i]
			pkg = &p
			break
		}
	}
	if pkg == nil {
		return o
	}
	siblings := make([]model.OrderPackage, 0, len(o.Packages))
	for _, p := range o.Packages {
		if strings.TrimSpace(p.PlatformSysTid) == sysTid {
			continue
		}
		siblings = append(siblings, p)
	}
	scoped := *o
	scoped.PlatformSysTid = pkg.PlatformSysTid
	scoped.Packages = []model.OrderPackage{*pkg}
	scoped.SiblingPackages = siblings
	// 必须用本包备注覆盖头表（空也要清空），否则渠道包会误显示厂家包的 151
	applyPackageRemarks(&scoped, *pkg)
	scoped.Items = filterItemsForPackage(o.Items, *pkg)
	rootIDs := map[uint64]struct{}{}
	for _, it := range scoped.Items {
		if it.ParentOrderItemID == 0 && strings.TrimSpace(it.SplitKind) == "" {
			rootIDs[it.ID] = struct{}{}
		}
	}
	applyPackageRowHeader(&scoped, *pkg, scoped.Items)
	// 本包商品金额（展示用）
	var pay float64
	for _, it := range scoped.Items {
		if _, ok := rootIDs[it.ID]; !ok {
			continue
		}
		if it.TotalAmount > 0 {
			pay += it.TotalAmount
		} else {
			pay += it.Price * float64(maxInt(it.Quantity, 1))
		}
	}
	if pay > 0 {
		scoped.PayAmount = roundMoney(pay)
		scoped.TotalAmount = scoped.PayAmount
	}
	mail := strings.TrimSpace(pkg.MailNo)
	shipments := make([]model.OrderShipment, 0, len(o.Shipments))
	for _, sh := range o.Shipments {
		keep := false
		if mail != "" && strings.TrimSpace(sh.ExpressNo) == mail {
			keep = true
		}
		if !keep {
			for _, sit := range sh.Items {
				if _, ok := rootIDs[sit.OrderItemID]; ok {
					keep = true
					break
				}
			}
		}
		if keep {
			shipments = append(shipments, sh)
		}
	}
	scoped.Shipments = shipments
	scoped.StatusLogs = filterStatusLogsForPackage(o, *pkg, scoped.Items)
	scoped.ListRowKey = fmt.Sprintf("%d:pkg:%s", o.ID, sysTid)
	return &scoped
}

// ScopeOrderByPackageID 按包裹 id 裁剪详情。
func ScopeOrderByPackageID(o *model.Order, packageID uint64) *model.Order {
	if o == nil || packageID == 0 {
		return o
	}
	for _, p := range o.Packages {
		if p.ID == packageID {
			return ScopeOrderBySysTid(o, p.PlatformSysTid)
		}
	}
	return o
}

// filterStatusLogsForPackage 按本包运单/采购单/商品线索裁剪状态流水。
// 明确指向其他包裹的日志剔除；无包裹线索的整单生命周期日志两端都保留。
func filterStatusLogsForPackage(o *model.Order, pkg model.OrderPackage, items []model.OrderItem) []model.OrderStatusLog {
	if o == nil || len(o.StatusLogs) == 0 {
		if o == nil {
			return nil
		}
		return o.StatusLogs
	}
	own := collectPackageLogTokens(pkg, items)
	other := map[string]struct{}{}
	sys := strings.TrimSpace(pkg.PlatformSysTid)
	for _, p := range o.Packages {
		if strings.TrimSpace(p.PlatformSysTid) == sys {
			continue
		}
		for t := range collectPackageLogTokens(p, filterItemsForPackage(o.Items, p)) {
			if _, mine := own[t]; mine {
				continue
			}
			other[t] = struct{}{}
		}
	}
	if len(own) == 0 && len(other) == 0 {
		return o.StatusLogs
	}
	out := make([]model.OrderStatusLog, 0, len(o.StatusLogs))
	for _, lg := range o.StatusLogs {
		text := strings.ToLower(strings.TrimSpace(lg.Remark + " " + lg.Action))
		hitsOwn := tokenHits(text, own)
		hitsOther := tokenHits(text, other)
		if hitsOther && !hitsOwn {
			continue
		}
		out = append(out, lg)
	}
	return out
}

func collectPackageLogTokens(pkg model.OrderPackage, items []model.OrderItem) map[string]struct{} {
	out := map[string]struct{}{}
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || len([]rune(s)) < 2 {
			return
		}
		out[s] = struct{}{}
	}
	add(pkg.PlatformSysTid)
	add(pkg.MailNo)
	add("systid=" + strings.TrimSpace(pkg.PlatformSysTid))
	for _, it := range items {
		add(it.PurchaseOrderID)
		add(it.SelfOrderNo)
		add(it.SupplierName)
		add(it.FactoryName)
		add(it.FactoryID)
		add(it.PlatformOid)
		addSkuSpecTokens(add, it.SkuSpecs)
		// 商品名过长时取 sku 规格已够；短名仍收录便于人工备注命中
		name := strings.TrimSpace(it.ProductName)
		if name != "" {
			runes := []rune(name)
			if len(runes) <= 24 {
				add(name)
			}
		}
	}
	return out
}

// addSkuSpecTokens 收录完整规格及常见前缀（插销+卡簧*2副 → 插销+卡簧；MT900-160mm → MT900）。
func addSkuSpecTokens(add func(string), sku string) {
	sku = strings.TrimSpace(sku)
	if sku == "" {
		return
	}
	add(sku)
	cut := len(sku)
	for _, sep := range []string{"*", "（", "(", " "} {
		if i := strings.Index(sku, sep); i > 0 && i < cut {
			cut = i
		}
	}
	head := strings.TrimSpace(sku[:cut])
	if head != "" && head != sku {
		add(head)
	}
	if i := strings.Index(head, "-"); i >= 2 {
		add(strings.TrimSpace(head[:i]))
	}
}

func tokenHits(text string, tokens map[string]struct{}) bool {
	if text == "" || len(tokens) == 0 {
		return false
	}
	for t := range tokens {
		if strings.Contains(text, t) {
			return true
		}
	}
	return false
}

// sysTidsForItemIDs 从内存包裹解析商品所属系统编号（写流水备注用）。
func sysTidsForItemIDs(o *model.Order, itemIDs []uint64) []string {
	if o == nil || len(itemIDs) == 0 || len(o.Packages) == 0 {
		return nil
	}
	want := map[uint64]struct{}{}
	for _, id := range itemIDs {
		if id > 0 {
			want[id] = struct{}{}
		}
	}
	pkgWant := map[uint64]struct{}{}
	for _, it := range o.Items {
		if _, ok := want[it.ID]; !ok {
			continue
		}
		if it.PackageID > 0 {
			pkgWant[it.PackageID] = struct{}{}
		}
	}
	if len(pkgWant) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(pkgWant))
	for _, p := range o.Packages {
		if _, ok := pkgWant[p.ID]; !ok {
			continue
		}
		sys := strings.TrimSpace(p.PlatformSysTid)
		if sys == "" {
			continue
		}
		if _, ok := seen[sys]; ok {
			continue
		}
		seen[sys] = struct{}{}
		out = append(out, sys)
	}
	return out
}

func formatSysTidRemarkHint(sysTids []string) string {
	if len(sysTids) == 0 {
		return ""
	}
	return " sysTid=" + strings.Join(sysTids, ",")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
