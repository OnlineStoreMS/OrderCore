package repo

import (
	"errors"
	"strings"

	"ordercore/internal/model"

	"gorm.io/gorm"
)

func (r *Repos) GetAllocSettings(tenantID uint64) (*model.AllocSettings, error) {
	var s model.AllocSettings
	err := r.db.Where("tenant_id = ?", tenantID).First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repos) UpsertAllocSettings(s *model.AllocSettings) error {
	var existing model.AllocSettings
	err := r.db.Where("tenant_id = ?", s.TenantID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.Create(s).Error
	}
	if err != nil {
		return err
	}
	existing.Enabled = s.Enabled
	if s.Strategy != "" {
		existing.Strategy = s.Strategy
	}
	s.ID = existing.ID
	return r.db.Save(&existing).Error
}

func (r *Repos) ListSkuSupplierRules(tenantID uint64, keyword string) ([]model.SkuSupplierRule, error) {
	var list []model.SkuSupplierRule
	q := r.db.Where("tenant_id = ?", tenantID)
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("sku_code LIKE ? OR supplier_name LIKE ? OR supplier_code LIKE ? OR remark LIKE ?", like, like, like, like)
	}
	err := q.Order("priority ASC, id DESC").Find(&list).Error
	return list, err
}

func (r *Repos) GetSkuSupplierRule(tenantID, id uint64) (*model.SkuSupplierRule, error) {
	var rule model.SkuSupplierRule
	err := r.db.Where("tenant_id = ? AND id = ?", tenantID, id).First(&rule).Error
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *Repos) FindActiveSkuSupplierRule(tenantID uint64, skuCode string) (*model.SkuSupplierRule, error) {
	var rule model.SkuSupplierRule
	err := r.db.Where("tenant_id = ? AND sku_code = ? AND status = 1", tenantID, skuCode).
		Order("priority ASC, id ASC").First(&rule).Error
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// FindSkuSupplierRuleByCode 按 SKU 查任意状态绑定（优先启用），用于记忆模式 upsert。
func (r *Repos) FindSkuSupplierRuleByCode(tenantID uint64, skuCode string) (*model.SkuSupplierRule, error) {
	var rule model.SkuSupplierRule
	err := r.db.Where("tenant_id = ? AND sku_code = ?", tenantID, skuCode).
		Order("status DESC, priority ASC, id ASC").First(&rule).Error
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *Repos) CreateSkuSupplierRule(rule *model.SkuSupplierRule) error {
	return r.db.Create(rule).Error
}

func (r *Repos) UpdateSkuSupplierRule(rule *model.SkuSupplierRule) error {
	return r.db.Save(rule).Error
}

func (r *Repos) DeleteSkuSupplierRule(tenantID, id uint64) error {
	return r.db.Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&model.SkuSupplierRule{}).Error
}
