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
	); err != nil {
		return err
	}
	return ensureIndexes(db)
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
		`).Error
	default:
		return nil
	}
}
