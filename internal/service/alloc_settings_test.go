package service

import (
	"testing"

	"ordercore/internal/model"
)

func TestResolveUniformSupplierFromRules(t *testing.T) {
	rules := map[string]*model.SkuSupplierRule{
		"SKU-A": {SupplierID: 10, SupplierName: "S1"},
		"SKU-B": {SupplierID: 10, SupplierName: "S1"},
		"SKU-C": {SupplierID: 20, SupplierName: "S2"},
	}

	same, ok := resolveUniformSupplier([]string{"SKU-A", "SKU-B"}, rules)
	if !ok || same == nil || same.SupplierID != 10 {
		t.Fatalf("expected same supplier 10, got %+v ok=%v", same, ok)
	}

	if _, ok := resolveUniformSupplier([]string{"SKU-A", "SKU-C"}, rules); ok {
		t.Fatal("different suppliers must not resolve")
	}
	if _, ok := resolveUniformSupplier([]string{"SKU-A", "MISSING"}, rules); ok {
		t.Fatal("missing binding must not resolve")
	}
	if _, ok := resolveUniformSupplier([]string{""}, rules); ok {
		t.Fatal("empty sku must not resolve")
	}
}

func resolveUniformSupplier(skuCodes []string, rules map[string]*model.SkuSupplierRule) (*model.SkuSupplierRule, bool) {
	var picked *model.SkuSupplierRule
	for _, code := range skuCodes {
		if code == "" {
			return nil, false
		}
		rule, ok := rules[code]
		if !ok || rule == nil {
			return nil, false
		}
		if picked == nil {
			picked = rule
			continue
		}
		if rule.SupplierID != picked.SupplierID {
			return nil, false
		}
	}
	if picked == nil {
		return nil, false
	}
	return picked, true
}
