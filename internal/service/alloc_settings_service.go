package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"ordercore/internal/dto"
	"ordercore/internal/model"

	"gorm.io/gorm"
)

func (s *OrderService) GetAllocSettings(tenantID uint64) (*model.AllocSettings, error) {
	cfg, err := s.repos.GetAllocSettings(tenantID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &model.AllocSettings{
			TenantID: tenantID,
			Enabled:  false,
			Strategy: model.AllocStrategyBindDropshipOnly,
		}, nil
	}
	return cfg, err
}

func (s *OrderService) UpdateAllocSettings(tenantID uint64, req dto.AllocSettingsRequest) (*model.AllocSettings, error) {
	strategy := strings.TrimSpace(req.Strategy)
	if strategy == "" {
		strategy = model.AllocStrategyBindDropshipOnly
	}
	if strategy != model.AllocStrategyBindDropshipOnly {
		return nil, fmt.Errorf("暂不支持的策略: %s", strategy)
	}
	cfg := &model.AllocSettings{
		TenantID: tenantID,
		Enabled:  req.Enabled,
		Strategy: strategy,
	}
	if err := s.repos.UpsertAllocSettings(cfg); err != nil {
		return nil, err
	}
	return s.GetAllocSettings(tenantID)
}

func (s *OrderService) ListSkuSupplierRules(tenantID uint64, keyword string) ([]model.SkuSupplierRule, error) {
	return s.repos.ListSkuSupplierRules(tenantID, keyword)
}

const autoAllocRemark = "SKU绑定自动分配"
const memoryAllocRemark = "记忆分配"

// rememberSkuSupplierBindings 代发分配成功后写入/更新 SKU→供应商（记忆模式）。
func (s *OrderService) rememberSkuSupplierBindings(tenantID uint64, items []model.OrderItem, supplierID uint64, supplierCode, supplierName string) {
	if supplierID == 0 || strings.TrimSpace(supplierName) == "" || len(items) == 0 {
		return
	}
	supplierName = strings.TrimSpace(supplierName)
	seen := map[string]struct{}{}
	for _, it := range items {
		skuCode := strings.TrimSpace(it.SkuCode)
		if skuCode == "" {
			continue
		}
		if _, ok := seen[skuCode]; ok {
			continue
		}
		seen[skuCode] = struct{}{}

		existing, err := s.repos.FindSkuSupplierRuleByCode(tenantID, skuCode)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			rule := &model.SkuSupplierRule{
				TenantID:     tenantID,
				SkuCode:      skuCode,
				SupplierID:   supplierID,
				SupplierCode: supplierCode,
				SupplierName: supplierName,
				Priority:     100,
				Status:       1,
				Remark:       memoryAllocRemark,
			}
			if err := s.repos.CreateSkuSupplierRule(rule); err != nil {
				log.Printf("[alloc-memory] create sku=%s failed: %v", skuCode, err)
			}
			continue
		}
		if err != nil {
			log.Printf("[alloc-memory] lookup sku=%s failed: %v", skuCode, err)
			continue
		}
		existing.SupplierID = supplierID
		existing.SupplierCode = supplierCode
		existing.SupplierName = supplierName
		existing.Status = 1
		if strings.TrimSpace(existing.Remark) == "" {
			existing.Remark = memoryAllocRemark
		}
		if err := s.repos.UpdateSkuSupplierRule(existing); err != nil {
			log.Printf("[alloc-memory] update sku=%s failed: %v", skuCode, err)
		}
	}
}

func (s *OrderService) CreateSkuSupplierRule(tenantID uint64, req dto.SkuSupplierRuleRequest) (*model.SkuSupplierRule, error) {
	skuCode := strings.TrimSpace(req.SkuCode)
	if skuCode == "" {
		return nil, fmt.Errorf("skuCode 必填")
	}
	if req.SupplierID == 0 || strings.TrimSpace(req.SupplierName) == "" {
		return nil, fmt.Errorf("请选择供应商")
	}
	status := int8(1)
	if req.Status != nil {
		status = *req.Status
	}
	priority := req.Priority
	if priority <= 0 {
		priority = 100
	}
	if status == 1 {
		if _, err := s.repos.FindActiveSkuSupplierRule(tenantID, skuCode); err == nil {
			return nil, fmt.Errorf("SKU「%s」已有启用的供应商绑定", skuCode)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	rule := &model.SkuSupplierRule{
		TenantID:     tenantID,
		SkuCode:      skuCode,
		SupplierID:   req.SupplierID,
		SupplierCode: req.SupplierCode,
		SupplierName: strings.TrimSpace(req.SupplierName),
		Priority:     priority,
		Status:       status,
		Remark:       req.Remark,
	}
	if err := s.repos.CreateSkuSupplierRule(rule); err != nil {
		return nil, err
	}
	return rule, nil
}

func (s *OrderService) UpdateSkuSupplierRule(tenantID, id uint64, req dto.SkuSupplierRuleRequest) (*model.SkuSupplierRule, error) {
	rule, err := s.repos.GetSkuSupplierRule(tenantID, id)
	if err != nil {
		return nil, err
	}
	skuCode := strings.TrimSpace(req.SkuCode)
	if skuCode == "" {
		return nil, fmt.Errorf("skuCode 必填")
	}
	if req.SupplierID == 0 || strings.TrimSpace(req.SupplierName) == "" {
		return nil, fmt.Errorf("请选择供应商")
	}
	status := rule.Status
	if req.Status != nil {
		status = *req.Status
	}
	priority := req.Priority
	if priority <= 0 {
		priority = rule.Priority
	}
	if priority <= 0 {
		priority = 100
	}
	if status == 1 {
		if existing, err := s.repos.FindActiveSkuSupplierRule(tenantID, skuCode); err == nil && existing.ID != id {
			return nil, fmt.Errorf("SKU「%s」已有启用的供应商绑定", skuCode)
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	rule.SkuCode = skuCode
	rule.SupplierID = req.SupplierID
	rule.SupplierCode = req.SupplierCode
	rule.SupplierName = strings.TrimSpace(req.SupplierName)
	rule.Priority = priority
	rule.Status = status
	rule.Remark = req.Remark
	if err := s.repos.UpdateSkuSupplierRule(rule); err != nil {
		return nil, err
	}
	return rule, nil
}

func (s *OrderService) DeleteSkuSupplierRule(tenantID, id uint64) error {
	return s.repos.DeleteSkuSupplierRule(tenantID, id)
}

// TryAutoAllocateBySKU 按分配设置与 SKU→供应商绑定尝试自动代发。
// 条件不满足或失败时静默跳过（仅打日志），不阻断同步。
func (s *OrderService) TryAutoAllocateBySKU(ctx context.Context, tenantID, operatorID uint64, order *model.Order, bearerToken string) {
	if order == nil {
		return
	}
	if order.Status != model.StatusPendingAlloc || order.AllocType != "" {
		return
	}
	if order.SkipAutoAlloc {
		return
	}
	cfg, err := s.GetAllocSettings(tenantID)
	if err != nil || cfg == nil || !cfg.Enabled {
		return
	}
	if cfg.Strategy != "" && cfg.Strategy != model.AllocStrategyBindDropshipOnly {
		return
	}

	o, err := s.repos.GetOrder(tenantID, order.ID)
	if err != nil || o == nil {
		return
	}
	if o.Status != model.StatusPendingAlloc || o.AllocType != "" || o.SkipAutoAlloc {
		return
	}
	if len(o.Items) == 0 {
		return
	}

	var supplierID uint64
	var supplierName string
	for _, it := range o.Items {
		skuCode := strings.TrimSpace(it.SkuCode)
		if skuCode == "" {
			return
		}
		rule, err := s.repos.FindActiveSkuSupplierRule(tenantID, skuCode)
		if err != nil {
			return
		}
		if supplierID == 0 {
			supplierID = rule.SupplierID
			supplierName = rule.SupplierName
			continue
		}
		if rule.SupplierID != supplierID {
			// 多供应商不拆单、不自动分
			return
		}
	}
	if supplierID == 0 {
		return
	}

	_, err = s.Allocate(ctx, tenantID, operatorID, o.ID, dto.AllocateRequest{
		AllocType:    model.AllocDropship,
		SupplierID:   supplierID,
		SupplierName: supplierName,
		Remark:       autoAllocRemark,
	}, bearerToken)
	if err != nil {
		log.Printf("[alloc-auto] order=%d tenant=%d sku auto-alloc failed: %v", o.ID, tenantID, err)
		return
	}
	log.Printf("[alloc-auto] order=%d tenant=%d allocated dropship supplier=%d", o.ID, tenantID, supplierID)
}
