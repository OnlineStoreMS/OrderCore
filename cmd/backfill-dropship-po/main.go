// One-off: 为指定租户「待发货已分配且缺代发单」的订单补建代发采购单。
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"ordercore/internal/config"
	"ordercore/internal/integration/productcore"
	"ordercore/internal/integration/storecore"
	"ordercore/internal/integration/storesync"
	"ordercore/internal/integration/supplycore"
	"ordercore/internal/model"
	jwtmgr "ordercore/internal/pkg/jwt"
	"ordercore/internal/repo"
	"ordercore/internal/service"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	cfgPath := "/config/config.yaml"
	if v := os.Getenv("CONFIG_PATH"); v != "" {
		cfgPath = v
	}
	tenantID := uint64(2)
	if v := os.Getenv("TENANT_ID"); v != "" {
		n, _ := strconv.ParseUint(v, 10, 64)
		if n > 0 {
			tenantID = n
		}
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(cfg.Database.PostgresDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatal(err)
	}
	repos := repo.New(db)
	supply := supplycore.NewClient(cfg.Integrations.SupplyCoreAPIURL)
	product := productcore.NewClient(cfg.Integrations.ProductCoreAPIURL)
	ss := storesync.NewClient(cfg.Integrations.StoreSyncAgentAPIURL)
	sc := storecore.NewClient(cfg.Integrations.StoreCoreAPIURL)
	svc := service.NewOrderService(repos, ss, sc, supply, nil, product)

	jwt := jwtmgr.NewManager(cfg.Auth.JWTSecret)
	token, err := jwt.IssueServiceToken(tenantID, 30*time.Minute)
	if err != nil {
		log.Fatal(err)
	}
	bearer := "Bearer " + token

	var ids []uint64
	err = db.Model(&model.Order{}).
		Where("tenant_id = ? AND alloc_type = ? AND supplier_id > 0 AND ship_status = ? AND status IN ?",
			tenantID, model.AllocDropship, model.ShipWaitShip,
			[]string{model.StatusAllocated, model.StatusPendingShip}).
		Where("purchase_order_id IS NULL OR btrim(purchase_order_id) = ''").
		Pluck("id", &ids).Error
	if err != nil {
		log.Fatal(err)
	}
	if len(ids) == 0 {
		fmt.Println("nothing to backfill")
		return
	}
	ptrs := make([]*model.Order, 0, len(ids))
	for _, id := range ids {
		o, err := repos.GetOrder(tenantID, id)
		if err != nil {
			log.Printf("get order %d: %v", id, err)
			continue
		}
		fmt.Printf("backfill %s supplier=%d %s\n", o.OrderNo, o.SupplierID, o.SupplierName)
		ptrs = append(ptrs, o)
	}
	if err := svc.BackfillDropshipPOs(context.Background(), tenantID, ptrs, bearer); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("done, %d orders\n", len(ptrs))
}
