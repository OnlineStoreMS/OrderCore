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
		&model.OrderShipmentItem{},
		&model.SupplierSourceBinding{},
		&model.AllocSettings{},
		&model.SkuSupplierRule{},
		&model.SyncJob{},
		&model.NotificationChannel{},
		&model.PushRule{},
		&model.PushLog{},
		&model.ManualOrderSource{},
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
		// closed 未发货：清空发货态，避免混入待发货队列
		`UPDATE orders SET ship_status = ''
		 WHERE status = 'closed' AND shipped_at IS NULL
		   AND (ship_status IS NULL OR ship_status = '' OR ship_status = 'wait_ship')`,
		// closed 已发货
		`UPDATE orders SET ship_status = 'shipped'
		 WHERE status = 'closed' AND shipped_at IS NOT NULL
		   AND (ship_status IS NULL OR ship_status = '' OR ship_status = 'wait_ship')`,
		// safety net：仅开放履约单补 wait_ship
		`UPDATE orders SET ship_status = 'wait_ship'
		 WHERE (ship_status IS NULL OR ship_status = '')
		   AND status IN ('pending_alloc','allocated','purchasing','pending_payment')`,
		// 取消/退款完成单不应落在待分配/已分配
		`UPDATE orders SET
			status = 'closed',
			alloc_type = '',
			dropship_mode = '',
			supplier_id = 0,
			supplier_name = '',
			factory_id = '',
			factory_name = '',
			purchase_order_id = '',
			alloc_remark = '',
			allocated_at = NULL,
			ship_entry_locked = true,
			ship_lock_reason = '电商订单已关闭/取消/退款完成'
		 WHERE status NOT IN ('closed','completed')
		   AND (
			 UPPER(COALESCE(ecommerce_status,'')) IN (
			   'ORDER_CANCELLED','ORDER_CANCEL','TRADE_CLOSED','CANCEL','CANCELLED','CLOSED',
			   'REFUND_SUCCESS','REFUNDED','SUCCESS_REFUND','REFUND_MONEY_FINISH','REFUND_MONEY_SUCCESS'
			 )
			 OR LOWER(COALESCE(platform_status,'')) IN ('order_cancelled','cancelled','trade_closed','closed','cancel')
			 OR COALESCE(ecommerce_status_text,'') LIKE '%订单取消%'
			 OR COALESCE(ecommerce_status_text,'') LIKE '%已取消%'
			 OR COALESCE(ecommerce_status_text,'') LIKE '%交易关闭%'
			 OR COALESCE(ecommerce_status_text,'') LIKE '%退款成功%'
			 OR COALESCE(ecommerce_status_text,'') LIKE '%退款完成%'
			 OR UPPER(COALESCE(ecommerce_status,'')) LIKE '%CANCEL%'
		   )`,
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
		// 抖店同一主单 tid 可对应多个快递助手包裹（不同 sysTid），platform_order_id 不能再唯一。
		// 幂等键改为 platform_sys_tid；platform_order_id 仅保留普通检索索引。
		steps := []string{
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_tenant_no ON orders (tenant_id, order_no)`,
			`DROP INDEX IF EXISTS idx_orders_source_platform`,
			`CREATE INDEX IF NOT EXISTS idx_orders_platform_order_id ON orders (platform_order_id)`,
			// 历史脏数据：同一 sysTid 多条时保留「非 closed 优先、id 最小」一条，其余打 #dup 后缀腾出唯一键
			`WITH d AS (
				SELECT id,
					ROW_NUMBER() OVER (
						PARTITION BY tenant_id, source_channel, platform_sys_tid
						ORDER BY CASE WHEN status = 'closed' THEN 1 ELSE 0 END, id ASC
					) AS rn
				FROM orders
				WHERE COALESCE(platform_sys_tid, '') <> ''
			)
			UPDATE orders o
			SET platform_sys_tid = o.platform_sys_tid || '#dup' || o.id::text,
				status = CASE WHEN o.status = 'closed' THEN o.status ELSE 'closed' END,
				updated_at = NOW()
			FROM d
			WHERE o.id = d.id AND d.rn > 1`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_source_systid ON orders (tenant_id, source_channel, platform_sys_tid) WHERE platform_sys_tid <> ''`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_source_ext ON orders (tenant_id, source_channel, external_ref_id) WHERE external_ref_id <> ''`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_ship_tenant_no ON order_shipments (tenant_id, shipment_no)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_bind_supplier_factory ON supplier_source_bindings (tenant_id, source_channel, external_factory_id) WHERE status = 1`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_sku_supplier_rule_active ON sku_supplier_rules (tenant_id, sku_code) WHERE status = 1`,
		}
		for _, sql := range steps {
			if err := db.Exec(sql).Error; err != nil {
				return fmt.Errorf("ensureIndexes: %w\nsql=%s", err, sql)
			}
		}
		return nil
	default:
		return nil
	}
}
