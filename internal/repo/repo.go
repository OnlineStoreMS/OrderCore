package repo

import (
	"fmt"
	"strings"
	"time"

	"ordercore/internal/model"

	"gorm.io/gorm"
)

type Repos struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repos {
	return &Repos{db: db}
}

func (r *Repos) DB() *gorm.DB { return r.db }

// WithTx returns a repo bound to the given transaction.
func (r *Repos) WithTx(tx *gorm.DB) *Repos {
	return &Repos{db: tx}
}

// Transaction runs fn inside a DB transaction.
func (r *Repos) Transaction(fn func(txRepos *Repos) error) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return fn(r.WithTx(tx))
	})
}

type OrderListQuery struct {
	SourceChannel string
	Status        string
	AllocType     string
	Keyword       string
	Page          int
	PageSize      int
}

func (r *Repos) ListOrders(tenantID uint64, q OrderListQuery) ([]model.Order, int64, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	tx := r.db.Model(&model.Order{}).Where("tenant_id = ?", tenantID)
	if q.SourceChannel != "" {
		tx = tx.Where("source_channel = ?", q.SourceChannel)
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if q.AllocType != "" {
		tx = tx.Where("alloc_type = ?", q.AllocType)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + strings.ToLower(kw) + "%"
		tx = tx.Where(
			"LOWER(order_no) LIKE ? OR LOWER(platform_order_id) LIKE ? OR LOWER(buyer_name) LIKE ? OR LOWER(buyer_phone) LIKE ? OR LOWER(buyer_nick) LIKE ?",
			like, like, like, like, like,
		)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.Order
	err := tx.Preload("Items").Preload("Address").Preload("Shipments").
		Order("id DESC").
		Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).
		Find(&list).Error
	return list, total, err
}

func (r *Repos) GetOrder(tenantID, id uint64) (*model.Order, error) {
	var o model.Order
	err := r.db.Where("tenant_id = ? AND id = ?", tenantID, id).
		Preload("Items").
		Preload("Address").
		Preload("Shipments", func(db *gorm.DB) *gorm.DB { return db.Order("id ASC") }).
		Preload("StatusLogs", func(db *gorm.DB) *gorm.DB { return db.Order("id ASC") }).
		First(&o).Error
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *Repos) FindBySourcePlatform(tenantID uint64, channel, platformOrderID string) (*model.Order, error) {
	var o model.Order
	err := r.db.Where("tenant_id = ? AND source_channel = ? AND platform_order_id = ?", tenantID, channel, platformOrderID).
		Preload("Items").Preload("Address").
		First(&o).Error
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *Repos) FindByExternalRef(tenantID uint64, channel, externalRefID string) (*model.Order, error) {
	var o model.Order
	err := r.db.Where("tenant_id = ? AND source_channel = ? AND external_ref_id = ?", tenantID, channel, externalRefID).
		Preload("Items").Preload("Address").
		First(&o).Error
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *Repos) CreateOrder(o *model.Order) error {
	return r.db.Create(o).Error
}

func (r *Repos) SaveOrder(o *model.Order) error {
	return r.db.Session(&gorm.Session{FullSaveAssociations: true}).Save(o).Error
}

func (r *Repos) UpdateOrderFields(tenantID, id uint64, fields map[string]interface{}) error {
	res := r.db.Model(&model.Order{}).Where("tenant_id = ? AND id = ?", tenantID, id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repos) ReplaceItems(tenantID, orderID uint64, items []model.OrderItem) error {
	if err := r.db.Where("tenant_id = ? AND order_id = ?", tenantID, orderID).Delete(&model.OrderItem{}).Error; err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	for i := range items {
		items[i].TenantID = tenantID
		items[i].OrderID = orderID
		items[i].ID = 0
	}
	return r.db.Create(&items).Error
}

func (r *Repos) UpsertAddress(addr *model.OrderAddress) error {
	var existing model.OrderAddress
	err := r.db.Where("tenant_id = ? AND order_id = ?", addr.TenantID, addr.OrderID).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return r.db.Create(addr).Error
	}
	if err != nil {
		return err
	}
	addr.ID = existing.ID
	return r.db.Save(addr).Error
}

func (r *Repos) AddStatusLog(log *model.OrderStatusLog) error {
	return r.db.Create(log).Error
}

// TransitionOrder atomically updates order fields and appends a status log.
func (r *Repos) TransitionOrder(tenantID, orderID uint64, fields map[string]interface{}, log *model.OrderStatusLog) error {
	if err := r.UpdateOrderFields(tenantID, orderID, fields); err != nil {
		return err
	}
	if log != nil {
		log.TenantID = tenantID
		log.OrderID = orderID
		if err := r.AddStatusLog(log); err != nil {
			return fmt.Errorf("persist status log: %w", err)
		}
	}
	return nil
}

func (r *Repos) CreateShipment(s *model.OrderShipment) error {
	return r.db.Create(s).Error
}

func (r *Repos) UpdateShipment(s *model.OrderShipment) error {
	return r.db.Save(s).Error
}

func (r *Repos) ListBindings(tenantID uint64) ([]model.SupplierSourceBinding, error) {
	var list []model.SupplierSourceBinding
	err := r.db.Where("tenant_id = ?", tenantID).Order("id DESC").Find(&list).Error
	return list, err
}

func (r *Repos) GetBinding(tenantID, id uint64) (*model.SupplierSourceBinding, error) {
	var b model.SupplierSourceBinding
	err := r.db.Where("tenant_id = ? AND id = ?", tenantID, id).First(&b).Error
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *Repos) FindBindingByFactory(tenantID uint64, channel, factoryID string) (*model.SupplierSourceBinding, error) {
	var b model.SupplierSourceBinding
	err := r.db.Where("tenant_id = ? AND source_channel = ? AND external_factory_id = ? AND status = 1", tenantID, channel, factoryID).
		First(&b).Error
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *Repos) FindBindingBySupplier(tenantID, supplierID uint64, channel string) (*model.SupplierSourceBinding, error) {
	var b model.SupplierSourceBinding
	err := r.db.Where("tenant_id = ? AND supplier_id = ? AND source_channel = ? AND status = 1", tenantID, supplierID, channel).
		First(&b).Error
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *Repos) CreateBinding(b *model.SupplierSourceBinding) error {
	return r.db.Create(b).Error
}

func (r *Repos) UpdateBinding(b *model.SupplierSourceBinding) error {
	return r.db.Save(b).Error
}

func (r *Repos) DeleteBinding(tenantID, id uint64) error {
	return r.db.Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&model.SupplierSourceBinding{}).Error
}

func (r *Repos) NextOrderNo(tenantID uint64) (string, error) {
	prefix := time.Now().Format("20060102")
	var count int64
	if err := r.db.Model(&model.Order{}).Where("tenant_id = ? AND order_no LIKE ?", tenantID, "OC"+prefix+"%").Count(&count).Error; err != nil {
		return "", err
	}
	return fmt.Sprintf("OC%s%04d", prefix, count+1), nil
}

func (r *Repos) NextShipmentNo(tenantID uint64) (string, error) {
	prefix := time.Now().Format("20060102")
	var count int64
	if err := r.db.Model(&model.OrderShipment{}).Where("tenant_id = ? AND shipment_no LIKE ?", tenantID, "SH"+prefix+"%").Count(&count).Error; err != nil {
		return "", err
	}
	return fmt.Sprintf("SH%s%04d", prefix, count+1), nil
}

func (r *Repos) CountByStatus(tenantID uint64) (map[string]int64, error) {
	type row struct {
		Status string
		Cnt    int64
	}
	var rows []row
	err := r.db.Model(&model.Order{}).Select("status, count(*) as cnt").
		Where("tenant_id = ?", tenantID).Group("status").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, r0 := range rows {
		out[r0.Status] = r0.Cnt
	}
	return out, nil
}

func (r *Repos) CountBySource(tenantID uint64) (map[string]int64, error) {
	type row struct {
		SourceChannel string
		Cnt           int64
	}
	var rows []row
	err := r.db.Model(&model.Order{}).Select("source_channel, count(*) as cnt").
		Where("tenant_id = ?", tenantID).Group("source_channel").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, r0 := range rows {
		out[r0.SourceChannel] = r0.Cnt
	}
	return out, nil
}
