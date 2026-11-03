package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"ordercore/internal/integration/supplycore"
	"ordercore/internal/pkg/jwt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 运维补跑：代发单采购价（分发备注）+ 物流回写。
// 用法:
//
//	JWT_SECRET=... ORDERCORE_DSN=... SUPPLY_URL=http://127.0.0.1:8092 go run ./cmd/repair-dropship-sync
func main() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("JWT_SECRET required")
	}
	supplyURL := env("SUPPLY_URL", "http://127.0.0.1:8092")
	dsn := os.Getenv("ORDERCORE_DSN")
	if dsn == "" {
		log.Fatal("ORDERCORE_DSN required")
	}
	tenantID := uint64(1)

	j := jwt.NewManager(secret)
	tok, err := j.IssueServiceToken(tenantID, 45*time.Minute)
	if err != nil {
		log.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatal(err)
	}
	type row struct {
		ID              uint64
		OrderNo         string
		PurchaseOrderID string
		FenFaRemark     string
	}
	var orders []row
	err = db.Raw(`
SELECT o.id, o.order_no, o.purchase_order_id, COALESCE(o.fen_fa_remark,'') AS fen_fa_remark
FROM orders o
WHERE o.tenant_id = ? AND TRIM(COALESCE(o.purchase_order_id,'')) <> ''
  AND o.status NOT IN ('closed')
  AND (
    TRIM(COALESCE(o.fen_fa_remark,'')) <> ''
    OR EXISTS (SELECT 1 FROM order_packages p WHERE p.order_id=o.id AND TRIM(COALESCE(p.fen_fa_remark,''))<>'')
    OR o.ship_status IN ('shipped','partial_shipped')
  )
ORDER BY o.id DESC
LIMIT 800`, tenantID).Scan(&orders).Error
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("candidate orders=%d\n", len(orders))

	supply := supplycore.NewClient(supplyURL)
	ctx := context.Background()
	seenPO := map[string]struct{}{}
	priceN, shipN := 0, 0
	for _, o := range orders {
		poNo := o.PurchaseOrderID
		if poNo == "" {
			continue
		}
		list, _, err := supply.ListPurchaseOrdersEx(ctx, tok, 0, "dropship", poNo, 1, 5)
		if err != nil || len(list) == 0 {
			if err != nil {
				log.Printf("list %s: %v", poNo, err)
			}
			continue
		}
		var po *supplycore.PurchaseOrderListItem
		for i := range list {
			if list[i].PoNo == poNo {
				po = &list[i]
				break
			}
		}
		if po == nil || po.Status == "cancelled" {
			continue
		}
		if _, ok := seenPO[poNo]; !ok && po.PayStatus != "paid" && po.PayStatus != "partial" && po.Status != "completed" {
			seenPO[poNo] = struct{}{}
			if err := supply.SyncPurchasePrices(ctx, tok, po.ID); err != nil {
				log.Printf("price %s: %v", poNo, err)
			} else {
				priceN++
				fmt.Printf("price-synced %s (order %s)\n", poNo, o.OrderNo)
			}
		}
		if err := supply.SyncShipmentsFromOrders(ctx, tok, po.ID, o.ID); err != nil {
			log.Printf("ship %s order=%s: %v", poNo, o.OrderNo, err)
		} else {
			shipN++
		}
		time.Sleep(80 * time.Millisecond)
	}
	fmt.Printf("done pricePO=%d logisticsCalls=%d\n", priceN, shipN)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
