package repo

import (
	"ordercore/internal/model"
	"time"
)

func (r *Repos) ListSyncJobs(tenantID uint64) ([]model.SyncJob, error) {
	var list []model.SyncJob
	err := r.db.Where("tenant_id = ?", tenantID).Order("id ASC").Find(&list).Error
	return list, err
}

func (r *Repos) GetSyncJob(tenantID, id uint64) (*model.SyncJob, error) {
	var j model.SyncJob
	err := r.db.Where("tenant_id = ? AND id = ?", tenantID, id).First(&j).Error
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (r *Repos) GetSyncJobByType(tenantID uint64, jobType string) (*model.SyncJob, error) {
	var j model.SyncJob
	err := r.db.Where("tenant_id = ? AND job_type = ?", tenantID, jobType).First(&j).Error
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (r *Repos) SaveSyncJob(j *model.SyncJob) error {
	return r.db.Save(j).Error
}

func (r *Repos) CreateSyncJob(j *model.SyncJob) error {
	return r.db.Create(j).Error
}

func (r *Repos) ListEnabledSyncJobs() ([]model.SyncJob, error) {
	var list []model.SyncJob
	err := r.db.Where("enabled = ?", true).Find(&list).Error
	return list, err
}

func (r *Repos) UpdateSyncJobRun(id uint64, ok bool, errMsg, statsJSON string, at time.Time) error {
	return r.db.Model(&model.SyncJob{}).Where("id = ?", id).Updates(map[string]any{
		"last_run_at":     at,
		"last_run_ok":     ok,
		"last_error":      errMsg,
		"last_stats_json": statsJSON,
	}).Error
}

func (r *Repos) ListOpenKDZSOrders(tenantID uint64, limit int) ([]model.Order, error) {
	if limit <= 0 {
		limit = 100
	}
	var list []model.Order
	err := r.db.Where(
		"tenant_id = ? AND source_channel = ? AND status IN ?",
		tenantID, model.SourceKDZS,
		[]string{model.StatusPendingShip, model.StatusAllocated, model.StatusPurchasing},
	).Order("updated_at ASC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *Repos) ListDistinctTenantIDsFromOrders() ([]uint64, error) {
	var ids []uint64
	err := r.db.Model(&model.Order{}).Distinct("tenant_id").Pluck("tenant_id", &ids).Error
	return ids, err
}

func (r *Repos) ListChannels(tenantID uint64) ([]model.NotificationChannel, error) {
	var list []model.NotificationChannel
	err := r.db.Where("tenant_id = ?", tenantID).Order("id DESC").Find(&list).Error
	return list, err
}

func (r *Repos) GetChannel(tenantID, id uint64) (*model.NotificationChannel, error) {
	var ch model.NotificationChannel
	err := r.db.Where("tenant_id = ? AND id = ?", tenantID, id).First(&ch).Error
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (r *Repos) CreateChannel(ch *model.NotificationChannel) error {
	return r.db.Create(ch).Error
}

func (r *Repos) SaveChannel(ch *model.NotificationChannel) error {
	return r.db.Save(ch).Error
}

func (r *Repos) DeleteChannel(tenantID, id uint64) error {
	return r.db.Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&model.NotificationChannel{}).Error
}

func (r *Repos) ListPushRules(tenantID uint64) ([]model.PushRule, error) {
	var list []model.PushRule
	err := r.db.Where("tenant_id = ?", tenantID).Order("id DESC").Find(&list).Error
	return list, err
}

func (r *Repos) FindPushRules(tenantID, supplierID uint64, event string) ([]model.PushRule, error) {
	var list []model.PushRule
	err := r.db.Where(
		"tenant_id = ? AND enabled = ? AND event = ? AND (supplier_id = ? OR supplier_id = 0)",
		tenantID, true, event, supplierID,
	).Order("supplier_id DESC").Find(&list).Error
	return list, err
}

func (r *Repos) CreatePushRule(rule *model.PushRule) error {
	return r.db.Create(rule).Error
}

func (r *Repos) SavePushRule(rule *model.PushRule) error {
	return r.db.Save(rule).Error
}

func (r *Repos) GetPushRule(tenantID, id uint64) (*model.PushRule, error) {
	var rule model.PushRule
	err := r.db.Where("tenant_id = ? AND id = ?", tenantID, id).First(&rule).Error
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *Repos) DeletePushRule(tenantID, id uint64) error {
	return r.db.Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&model.PushRule{}).Error
}

func (r *Repos) CreatePushLog(log *model.PushLog) error {
	return r.db.Create(log).Error
}

func (r *Repos) ListPushLogs(tenantID, orderID uint64, limit int) ([]model.PushLog, error) {
	if limit <= 0 {
		limit = 50
	}
	q := r.db.Where("tenant_id = ?", tenantID)
	if orderID > 0 {
		q = q.Where("order_id = ?", orderID)
	}
	var list []model.PushLog
	err := q.Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}
