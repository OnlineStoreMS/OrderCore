package service

import (
	"testing"

	"ordercore/internal/model"
)

func TestSelectAllocateItemIDs(t *testing.T) {
	o := &model.Order{
		Items: []model.OrderItem{
			{ID: 1, ProductName: "A", Quantity: 1},
			{ID: 2, ProductName: "B", Quantity: 1},
			{ID: 3, ProductName: "split", Quantity: 1, SplitKind: model.SplitKindPartial, ParentOrderItemID: 1},
		},
	}
	all, err := selectAllocateItemIDs(o, nil)
	if err != nil || len(all) != 2 || all[0] != 1 || all[1] != 2 {
		t.Fatalf("all=%v err=%v", all, err)
	}
	part, err := selectAllocateItemIDs(o, []uint64{2})
	if err != nil || len(part) != 1 || part[0] != 2 {
		t.Fatalf("part=%v err=%v", part, err)
	}
	if _, err := selectAllocateItemIDs(o, []uint64{99}); err == nil {
		t.Fatal("expected error for missing ids")
	}
}

func TestResolveAllocateTargetNoSplit(t *testing.T) {
	s := &OrderService{}
	o := &model.Order{
		ID:     10,
		Status: model.StatusPendingAlloc,
		Items: []model.OrderItem{
			{ID: 1, ProductName: "A", Quantity: 1},
			{ID: 2, ProductName: "B", Quantity: 1},
		},
	}
	got, selected, err := s.resolveAllocateTarget(nil, 1, 1, o, []uint64{1})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 10 {
		t.Fatalf("should return same order, got %d", got.ID)
	}
	if len(selected) != 1 || selected[0] != 1 {
		t.Fatalf("selected=%v", selected)
	}
}

func TestRollupOrderFulfillmentFieldsMixed(t *testing.T) {
	o := &model.Order{
		Items: []model.OrderItem{
			{ID: 1, Quantity: 1, AllocType: model.AllocDropship, ShipStatus: model.ShipWaitShip, PurchaseOrderID: "PO1", SupplierID: 9, SupplierName: "S"},
			{ID: 2, Quantity: 1, AllocType: model.AllocSelfShip, ShipStatus: model.ShipWaitShip, SelfOrderNo: "SO1"},
			{ID: 3, Quantity: 1}, // pending
		},
	}
	fields := rollupOrderFulfillmentFields(o)
	if fields["alloc_type"] != model.AllocMixed {
		t.Fatalf("alloc_type=%v", fields["alloc_type"])
	}
	if fields["status"] != model.StatusAllocated {
		t.Fatalf("status=%v", fields["status"])
	}
}

func TestRollupOrderFulfillmentFieldsMultiPO(t *testing.T) {
	o := &model.Order{
		Items: []model.OrderItem{
			{ID: 1, Quantity: 1, AllocType: model.AllocDropship, ShipStatus: model.ShipWaitShip, PurchaseOrderID: "PO1"},
			{ID: 2, Quantity: 1, AllocType: model.AllocDropship, ShipStatus: model.ShipWaitShip, PurchaseOrderID: "PO2"},
		},
	}
	fields := rollupOrderFulfillmentFields(o)
	if fields["purchase_order_id"] != "" {
		t.Fatalf("multi PO should clear header po, got %v", fields["purchase_order_id"])
	}
	if fields["alloc_type"] != model.AllocDropship {
		t.Fatalf("alloc_type=%v", fields["alloc_type"])
	}
}

func TestRollupOrderFulfillmentFieldsUniform(t *testing.T) {
	o := &model.Order{
		Items: []model.OrderItem{
			{ID: 1, Quantity: 1, AllocType: model.AllocDropship, ShipStatus: model.ShipWaitShip, PurchaseOrderID: "PO1", DropshipMode: model.DropshipOSMSSupplier},
			{ID: 2, Quantity: 1, AllocType: model.AllocDropship, ShipStatus: model.ShipShipped, PurchaseOrderID: "PO1"},
		},
	}
	fields := rollupOrderFulfillmentFields(o)
	if fields["alloc_type"] != model.AllocDropship {
		t.Fatalf("alloc_type=%v", fields["alloc_type"])
	}
	if fields["ship_status"] != model.ShipPartialShipped {
		t.Fatalf("ship_status=%v", fields["ship_status"])
	}
	if fields["purchase_order_id"] != "PO1" {
		t.Fatalf("po=%v", fields["purchase_order_id"])
	}
}

func TestOrderDropshipPONos(t *testing.T) {
	o := &model.Order{
		PurchaseOrderID: "PO-H",
		Items: []model.OrderItem{
			{AllocType: model.AllocDropship, PurchaseOrderID: "PO-A"},
			{AllocType: model.AllocDropship, PurchaseOrderID: "PO-B"},
			{AllocType: model.AllocSelfShip, PurchaseOrderID: "ignore"},
		},
	}
	got := orderDropshipPONos(o)
	if len(got) != 3 {
		t.Fatalf("got=%v", got)
	}
}
