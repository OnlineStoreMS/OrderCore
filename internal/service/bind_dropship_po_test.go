package service

import (
	"testing"

	"ordercore/internal/model"
)

func TestBindDropshipPODoesNotInfectUnallocatedItems(t *testing.T) {
	// 纯逻辑：legacyWholeOrder 判定 + 行过滤条件
	o := &model.Order{
		AllocType:  model.AllocDropship,
		SupplierID: 1,
		Items: []model.OrderItem{
			{ID: 1, Quantity: 1, AllocType: model.AllocDropship, PurchaseOrderID: ""},
			{ID: 2, Quantity: 1, AllocType: "", PurchaseOrderID: ""},
		},
	}
	if !orderHasItemLevelAlloc(o) {
		t.Fatal("expected item-level alloc")
	}
	legacy := o.AllocType == model.AllocDropship && !orderHasItemLevelAlloc(o)
	if legacy {
		t.Fatal("partial item alloc must not use legacy whole-order stamp")
	}
	bindable := 0
	for _, it := range o.Items {
		at := it.AllocType
		if at == model.AllocDropship {
			bindable++
			continue
		}
		if at != "" || !legacy {
			continue
		}
		bindable++
	}
	if bindable != 1 {
		t.Fatalf("bindable=%d want 1", bindable)
	}
}

func TestBindDropshipPOLegacyWholeOrder(t *testing.T) {
	o := &model.Order{
		AllocType:  model.AllocDropship,
		SupplierID: 1,
		Items: []model.OrderItem{
			{ID: 1, Quantity: 1},
			{ID: 2, Quantity: 1},
		},
	}
	legacy := o.AllocType == model.AllocDropship && !orderHasItemLevelAlloc(o)
	if !legacy {
		t.Fatal("expected legacy whole-order")
	}
}
