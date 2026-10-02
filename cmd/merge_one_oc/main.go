// 一次性工具：AutoMigrate 后把同 platform_order_id 的兄弟 OC 并入 keeper。
// 用法: go run ./cmd/merge_one_oc -tid=6918151835580247816
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"ordercore/internal/config"
	"ordercore/internal/database"
	"ordercore/internal/model"
	"ordercore/internal/repo"
)

func main() {
	cfgPath := flag.String("config", "configs/config.yaml", "config path")
	tid := flag.String("tid", "", "platform_order_id to merge")
	tenantID := flag.Uint64("tenant", 1, "tenant id")
	flag.Parse()
	if strings.TrimSpace(*tid) == "" {
		fmt.Fprintln(os.Stderr, "usage: merge_one_oc -tid=PLATFORM_ORDER_ID")
		os.Exit(2)
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	db, err := database.Connect(&cfg.Database)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	if err := database.AutoMigrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	r := repo.New(db)

	list, err := r.ListBySourcePlatform(*tenantID, model.SourceKDZS, *tid)
	if err != nil {
		log.Fatalf("list: %v", err)
	}
	if len(list) == 0 {
		log.Printf("no orders for tid=%s", *tid)
		return
	}

	var keeper *model.Order
	score := func(o *model.Order) int {
		if o.SplitFromOrderID > 0 || strings.Contains(o.PlatformSysTid, "#split") {
			return -50
		}
		sc := 10
		if o.Status == model.StatusClosed {
			sc -= 5
		}
		return sc
	}
	for i := range list {
		o := &list[i]
		if keeper == nil || score(o) > score(keeper) || (score(o) == score(keeper) && o.ID < keeper.ID) {
			keeper = o
		}
	}
	if keeper == nil {
		log.Fatal("no keeper")
	}
	keeper, err = r.GetOrder(*tenantID, keeper.ID)
	if err != nil {
		log.Fatalf("get keeper: %v", err)
	}
	log.Printf("keeper=%s id=%d sysTid=%s", keeper.OrderNo, keeper.ID, keeper.PlatformSysTid)

	for i := range list {
		sib := &list[i]
		if sib.ID == keeper.ID {
			continue
		}
		full, gerr := r.GetOrder(*tenantID, sib.ID)
		if gerr != nil {
			log.Printf("skip %s: %v", sib.OrderNo, gerr)
			continue
		}
		log.Printf("merge sibling=%s id=%d sysTid=%s alloc=%s into keeper", full.OrderNo, full.ID, full.PlatformSysTid, full.AllocType)

		// 把头表履约落到行上（迁移前）
		now := time.Now()
		for _, it := range full.Items {
			if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
				continue
			}
			if strings.TrimSpace(it.AllocType) != "" {
				continue
			}
			if strings.TrimSpace(full.AllocType) == "" {
				continue
			}
			_ = r.UpdateOrderItemFields(*tenantID, it.ID, map[string]any{
				"alloc_type":        full.AllocType,
				"dropship_mode":     full.DropshipMode,
				"supplier_id":       full.SupplierID,
				"supplier_name":     full.SupplierName,
				"factory_id":        full.FactoryID,
				"factory_name":      full.FactoryName,
				"purchase_order_id": full.PurchaseOrderID,
				"self_order_no":     full.SelfOrderNo,
				"ship_status":       coalesce(full.ShipStatus, model.ShipWaitShip),
				"allocated_at":      full.AllocatedAt,
				"updated_at":        now,
			})
		}

		sysTid := strings.TrimSpace(baseSys(full.PlatformSysTid))
		if sysTid != "" && !strings.Contains(sysTid, "#split") {
			_ = r.UpsertOrderPackage(&model.OrderPackage{
				TenantID:           *tenantID,
				OrderID:            keeper.ID,
				PlatformSysTid:     sysTid,
				FenFaRemark:        full.FenFaRemark,
				PrinterRemark:      full.PrinterRemark,
				PlatformStatus:     full.PlatformStatus,
				PlatformStatusText: full.PlatformStatusText,
				IsPrimary:          false,
			})
		}

		itemIDs := make([]uint64, 0, len(full.Items))
		for _, it := range full.Items {
			itemIDs = append(itemIDs, it.ID)
		}
		if err := r.MoveOrderItems(*tenantID, full.ID, keeper.ID, itemIDs); err != nil {
			log.Printf("move items %s: %v", full.OrderNo, err)
			continue
		}
		// 迁运单
		_ = db.Exec(`UPDATE order_shipments SET order_id = ? WHERE tenant_id = ? AND order_id = ?`, keeper.ID, *tenantID, full.ID).Error
		_ = db.Exec(`UPDATE order_shipment_items SET order_id = ? WHERE tenant_id = ? AND order_id = ?`, keeper.ID, *tenantID, full.ID).Error

		if err := r.DeleteOrderCascade(*tenantID, full.ID); err != nil {
			log.Printf("delete sibling %s: %v", full.OrderNo, err)
			continue
		}
		log.Printf("merged+deleted %s", full.OrderNo)
	}

	// 给 keeper 现有行补齐头表履约（若行空）
	keeper, _ = r.GetOrder(*tenantID, keeper.ID)
	now := time.Now()
	for _, it := range keeper.Items {
		if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
			continue
		}
		if strings.TrimSpace(it.AllocType) != "" {
			continue
		}
		if strings.TrimSpace(keeper.AllocType) == "" {
			continue
		}
		_ = r.UpdateOrderItemFields(*tenantID, it.ID, map[string]any{
			"alloc_type":        keeper.AllocType,
			"dropship_mode":     keeper.DropshipMode,
			"supplier_id":       keeper.SupplierID,
			"supplier_name":     keeper.SupplierName,
			"factory_id":        keeper.FactoryID,
			"factory_name":      keeper.FactoryName,
			"purchase_order_id": keeper.PurchaseOrderID,
			"self_order_no":     keeper.SelfOrderNo,
			"ship_status":       coalesce(keeper.ShipStatus, model.ShipWaitShip),
			"allocated_at":      keeper.AllocatedAt,
			"updated_at":        now,
		})
	}
	keeperSys := baseSys(keeper.PlatformSysTid)
	if keeperSys != "" {
		_ = r.UpsertOrderPackage(&model.OrderPackage{
			TenantID:           *tenantID,
			OrderID:            keeper.ID,
			PlatformSysTid:     keeperSys,
			FenFaRemark:        keeper.FenFaRemark,
			PrinterRemark:      keeper.PrinterRemark,
			PlatformStatus:     keeper.PlatformStatus,
			PlatformStatusText: keeper.PlatformStatusText,
			IsPrimary:          true,
		})
	}

	// 简单 rollup：有多种 alloc_type 则 mixed
	keeper, _ = r.GetOrder(*tenantID, keeper.ID)
	types := map[string]struct{}{}
	for _, it := range keeper.Items {
		if strings.TrimSpace(it.SplitKind) != "" || it.ParentOrderItemID > 0 {
			continue
		}
		if at := strings.TrimSpace(it.AllocType); at != "" {
			types[at] = struct{}{}
		}
	}
	fields := map[string]any{}
	if len(types) > 1 {
		fields["alloc_type"] = model.AllocMixed
	} else if len(types) == 1 {
		for k := range types {
			fields["alloc_type"] = k
		}
	}
	if len(fields) > 0 {
		_ = r.UpdateOrderFields(*tenantID, keeper.ID, fields)
	}
	log.Printf("done keeper=%s items=%d packages will show after reload", keeper.OrderNo, len(keeper.Items))
}

func baseSys(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{"#split", "#dup", "#alloc"} {
		if i := strings.Index(s, sep); i >= 0 {
			return s[:i]
		}
	}
	return s
}

func coalesce(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
