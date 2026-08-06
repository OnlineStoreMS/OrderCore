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
	SourceChannel     string
	Status            string
	ShipStatus        string
	AllocType         string
	Keyword           string
	Platform          string
	EcommerceWaitShip bool // 兼容：按电商订单「待发货」筛选
	SalesChannel      string // self | dropship，与工作台自营/代发口径一致
	OrderedAtStart    *time.Time
	OrderedAtEnd      *time.Time
	ShippedAtStart    *time.Time
	ShippedAtEnd      *time.Time
	PayTimeStart      *time.Time
	PayTimeEnd        *time.Time
	Page              int
	PageSize          int
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
	if q.ShipStatus != "" {
		tx = tx.Where("ship_status = ?", q.ShipStatus)
		// 待发货工作队列：排除已关闭/已完成（关闭单常残留 wait_ship）
		if q.ShipStatus == model.ShipWaitShip {
			tx = tx.Where("status NOT IN ?", []string{model.StatusClosed, model.StatusCompleted})
		}
	}
	if q.AllocType != "" {
		tx = tx.Where("alloc_type = ?", q.AllocType)
	}
	if q.Platform != "" {
		tx = tx.Where("platform = ?", q.Platform)
	}
	switch strings.ToLower(strings.TrimSpace(q.SalesChannel)) {
	case "self":
		tx = tx.Where("NOT " + sqlIsDropship)
		tx = scopeValidSales(tx)
	case "dropship":
		tx = tx.Where(sqlIsDropship)
		tx = scopeValidSales(tx)
	}
	if q.EcommerceWaitShip {
		tx = tx.Where("ship_status = ?", model.ShipWaitShip).
			Where("status NOT IN ?", []string{model.StatusClosed, model.StatusCompleted}).
			Where(`(
				UPPER(COALESCE(ecommerce_status,'')) IN ('ORDER_PAID','WAIT_SELLER_SEND_GOODS','WAIT_SELLER_STOCK_OUT','PAID')
				OR COALESCE(ecommerce_status_text,'') LIKE '%待发货%'
				OR (COALESCE(ecommerce_status,'') = '' AND platform_status = ?)
			)`, model.KDZSWaitSend)
	}
	if q.OrderedAtStart != nil {
		tx = tx.Where("COALESCE(ordered_at, created_at) >= ?", q.OrderedAtStart)
	}
	if q.OrderedAtEnd != nil {
		tx = tx.Where("COALESCE(ordered_at, created_at) <= ?", q.OrderedAtEnd)
	}
	if q.ShippedAtStart != nil {
		tx = tx.Where("COALESCE(shipped_at, updated_at) >= ?", q.ShippedAtStart)
	}
	if q.ShippedAtEnd != nil {
		tx = tx.Where("COALESCE(shipped_at, updated_at) <= ?", q.ShippedAtEnd)
	}
	if q.PayTimeStart != nil {
		tx = tx.Where("pay_time >= ?", q.PayTimeStart)
	}
	if q.PayTimeEnd != nil {
		tx = tx.Where("pay_time <= ?", q.PayTimeEnd)
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
		Order("COALESCE(ordered_at, created_at) DESC, id DESC").
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
		Preload("StatusLogs", func(db *gorm.DB) *gorm.DB { return db.Order("id DESC") }).
		First(&o).Error
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *Repos) FindByOrderNo(tenantID uint64, orderNo string) (*model.Order, error) {
	var o model.Order
	err := r.db.Where("tenant_id = ? AND order_no = ?", tenantID, strings.TrimSpace(orderNo)).
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

func (r *Repos) FindShipmentByExpressNo(tenantID, orderID uint64, expressNo string) (*model.OrderShipment, error) {
	var s model.OrderShipment
	err := r.db.Where("tenant_id = ? AND order_id = ? AND express_no = ?", tenantID, orderID, strings.TrimSpace(expressNo)).
		First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListOrderIDsByExpressNo 同租户下共用同一运单号的销售单 ID（合单发货）。
func (r *Repos) ListOrderIDsByExpressNo(tenantID uint64, expressNo string) ([]uint64, error) {
	expressNo = strings.TrimSpace(expressNo)
	if expressNo == "" {
		return nil, nil
	}
	var ids []uint64
	err := r.db.Model(&model.OrderShipment{}).
		Where("tenant_id = ? AND express_no = ?", tenantID, expressNo).
		Distinct("order_id").
		Order("order_id ASC").
		Pluck("order_id", &ids).Error
	return ids, err
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

func (r *Repos) FindBindingByFactoryName(tenantID uint64, channel, factoryName string) (*model.SupplierSourceBinding, error) {
	var b model.SupplierSourceBinding
	err := r.db.Where("tenant_id = ? AND source_channel = ? AND external_factory_name = ? AND status = 1", tenantID, channel, factoryName).
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
	prefix := "OC" + time.Now().Format("20060102")
	seq, err := nextSeqFromLast(r.db.Model(&model.Order{}).
		Where("tenant_id = ? AND order_no LIKE ?", tenantID, prefix+"%"),
		"order_no", prefix)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%04d", prefix, seq), nil
}

func (r *Repos) NextShipmentNo(tenantID uint64) (string, error) {
	prefix := "SH" + time.Now().Format("20060102")
	seq, err := nextSeqFromLast(r.db.Model(&model.OrderShipment{}).
		Where("tenant_id = ? AND shipment_no LIKE ?", tenantID, prefix+"%"),
		"shipment_no", prefix)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%04d", prefix, seq), nil
}

// nextSeqFromLast 取当日最大单号序号 +1，避免 COUNT+1 在删单留洞时撞唯一索引。
func nextSeqFromLast(q *gorm.DB, col, prefix string) (int, error) {
	var last string
	if err := q.Order(col + " DESC").Limit(1).Pluck(col, &last).Error; err != nil {
		return 0, err
	}
	seq := 1
	if last != "" && len(last) > len(prefix) {
		var n int
		if _, scanErr := fmt.Sscanf(last[len(prefix):], "%d", &n); scanErr == nil && n >= 0 {
			seq = n + 1
		}
	}
	return seq, nil
}

func (r *Repos) CountByPurchaseOrderID(tenantID uint64, poNo string, excludeOrderID uint64) (int64, error) {
	poNo = strings.TrimSpace(poNo)
	if poNo == "" {
		return 0, nil
	}
	q := r.db.Model(&model.Order{}).Where("tenant_id = ? AND purchase_order_id = ?", tenantID, poNo)
	if excludeOrderID > 0 {
		q = q.Where("id <> ?", excludeOrderID)
	}
	var n int64
	err := q.Count(&n).Error
	return n, err
}

// ListDropshipOrdersMissingPO 已代发分配且绑定供应商、尚未关联 SupplyCore 代发单的销售单。
func (r *Repos) ListDropshipOrdersMissingPO(tenantID uint64, limit int) ([]model.Order, error) {
	if limit <= 0 {
		limit = 500
	}
	var list []model.Order
	err := r.db.Preload("Items").
		Where(
			"tenant_id = ? AND alloc_type = ? AND supplier_id > 0 AND status <> ? AND (purchase_order_id IS NULL OR purchase_order_id = '')",
			tenantID, model.AllocDropship, model.StatusClosed,
		).
		Order("supplier_id ASC, id ASC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

// RelinkPurchaseOrderIDs 将销售单上的代发采购单号从 fromPoNos 批量改为 toPoNo。
// toPoNo 为空时表示清空关联（删除代发单后解绑）。
func (r *Repos) RelinkPurchaseOrderIDs(tenantID uint64, fromPoNos []string, toPoNo string) (int64, error) {
	toPoNo = strings.TrimSpace(toPoNo)
	if len(fromPoNos) == 0 {
		return 0, nil
	}
	cleaned := make([]string, 0, len(fromPoNos))
	seen := map[string]struct{}{}
	for _, n := range fromPoNos {
		n = strings.TrimSpace(n)
		if n == "" || n == toPoNo {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		cleaned = append(cleaned, n)
	}
	if len(cleaned) == 0 {
		return 0, nil
	}
	res := r.db.Model(&model.Order{}).
		Where("tenant_id = ? AND purchase_order_id IN ?", tenantID, cleaned).
		Update("purchase_order_id", toPoNo)
	return res.RowsAffected, res.Error
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
	for _, ch := range model.KnownSourceChannels {
		out[ch] = 0
	}
	for _, r0 := range rows {
		if r0.SourceChannel == "" {
			continue
		}
		out[r0.SourceChannel] = r0.Cnt
	}
	return out, nil
}
