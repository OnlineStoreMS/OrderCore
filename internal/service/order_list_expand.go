package service

import (
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
