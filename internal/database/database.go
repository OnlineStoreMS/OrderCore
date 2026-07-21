package database

import (
	"fmt"
	"os"
	"path/filepath"

	"ordercore/internal/config"
	"ordercore/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Connect(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch cfg.Driver {
	case "postgres":
		dialector = postgres.Open(cfg.PostgresDSN)
	case "sqlite":
		if err := os.MkdirAll(filepath.Dir(cfg.SQLitePath), 0o755); err != nil {
			return nil, err
		}
		dialector = sqlite.Open(cfg.SQLitePath)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", cfg.Driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	return db, nil
}

func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&model.Order{},
		&model.OrderItem{},
		&model.OrderAddress{},
		&model.OrderStatusLog{},
		&model.OrderShipment{},
		&model.SupplierSourceBinding{},
		&model.AllocSettings{},
		&model.SkuSupplierRule{},
		&model.SyncJob{},
		&model.NotificationChannel{},
		&model.PushRule{},
		&model.PushLog{},
	); err != nil {
		return err
	}
	if err := backfillFulfillmentShipStatus(db); err != nil {
		return err
	}
	return ensureIndexes(db)
}

// backfillFulfillmentShipStatus 将历史单一 status 拆成履约 + 发货（幂等）
func backfillFulfillmentShipStatus(db *gorm.DB) error {
	steps := []string{
		// pending_ship → pending_alloc + wait_ship
		`UPDATE orders SET status = 'pending_alloc', ship_status = 'wait_ship'
		 WHERE status = 'pending_ship'`,
		// open fulfillment without ship_status
		`UPDATE orders SET ship_status = 'wait_ship'
		 WHERE (ship_status IS NULL OR ship_status = '')
		   AND status IN ('pending_alloc','allocated','purchasing','pending_payment')`,
		// legacy shipped / partial_ship → allocated|purchasing + shipped
		`UPDATE orders SET
			status = CASE WHEN alloc_type = 'purchase_then_ship' THEN 'purchasing' ELSE 'allocated' END,
			ship_status = 'shipped'
		 WHERE status IN ('shipped','partial_ship')`,
		// completed → ship shipped
		`UPDATE orders SET ship_status = 'shipped'
		 WHERE status = 'completed' AND (ship_status IS NULL OR ship_status = '' OR ship_status = 'wait_ship')`,
		// closed: infer ship from shipped_at
		`UPDATE orders SET ship_status = CASE WHEN shipped_at IS NOT NULL THEN 'shipped' ELSE 'wait_ship' END
		 WHERE status = 'closed' AND (ship_status IS NULL OR ship_status = '')`,
		// safety net
		`UPDATE orders SET ship_status = 'wait_ship'
		 WHERE ship_status IS NULL OR ship_status = ''`,
	}
	for _, sql := range steps {
		if err := db.Exec(sql).Error; err != nil {
			return fmt.Errorf("backfill fulfillment/ship status: %w", err)
		}
	}
	return nil
}

func ensureIndexes(db *gorm.DB) error {
	switch db.Dialector.Name() {
	case "postgres":
		return db.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_tenant_no ON orders (tenant_id, order_no);
			CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_source_platform ON orders (tenant_id, source_channel, platform_order_id) WHERE platform_order_id <> '';
			CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_source_ext ON orders (tenant_id, source_channel, external_ref_id) WHERE external_ref_id <> '';
			CREATE UNIQUE INDEX IF NOT EXISTS idx_ship_tenant_no ON order_shipments (tenant_id, shipment_no);
			CREATE UNIQUE INDEX IF NOT EXISTS idx_bind_supplier_factory ON supplier_source_bindings (tenant_id, source_channel, external_factory_id) WHERE status = 1;
			CREATE UNIQUE INDEX IF NOT EXISTS idx_sku_supplier_rule_active ON sku_supplier_rules (tenant_id, sku_code) WHERE status = 1;
		`).Error
	default:
		return nil
	}
}
