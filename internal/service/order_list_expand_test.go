package service

import (
	"testing"

	"ordercore/internal/model"
)

func TestExpandOrdersByPackages(t *testing.T) {
	list := []model.Order{{
		ID:              4396,
		OrderNo:         "OC202609190036",
		PlatformOrderID: "6917865244162993928",
		PlatformSysTid:  "29353909801438706257",
		AllocType:       model.AllocMixed,
		Packages: []model.OrderPackage{
			{ID: 803, PlatformSysTid: "29353909801438706257", IsPrimary: true},
			{ID: 1257, PlatformSysTid: "26878583463337148860", FenFaRemark: "0", IsPrimary: false},
		},
		Items: []model.OrderItem{
			{ID: 1, PackageID: 803, AllocType: model.AllocSelfShip, ProductName: "A", Quantity: 1},
			{ID: 2, PackageID: 1257, AllocType: model.AllocDropship, ProductName: "B249", Quantity: 1},
		},
	}}
	out := ExpandOrdersByPackages(list)
	if len(out) != 2 {
		t.Fatalf("len=%d want 2", len(out))
	}
	if out[0].ID != 4396 || out[1].ID != 4396 {
		t.Fatalf("same OC id expected")
	}
	if out[0].PlatformSysTid != "29353909801438706257" || out[1].PlatformSysTid != "26878583463337148860" {
		t.Fatalf("sysTid=%q / %q", out[0].PlatformSysTid, out[1].PlatformSysTid)
	}
	if out[0].AllocType != model.AllocSelfShip || out[1].AllocType != model.AllocDropship {
		t.Fatalf("alloc=%q / %q", out[0].AllocType, out[1].AllocType)
	}
	if len(out[0].Items) != 1 || len(out[1].Items) != 1 {
		t.Fatalf("items per row=%d / %d", len(out[0].Items), len(out[1].Items))
	}
}

func TestExpandOrdersByPackagesSingle(t *testing.T) {
	list := []model.Order{{
		ID: 1, OrderNo: "OC1",
		Packages: []model.OrderPackage{{ID: 1, PlatformSysTid: "s1", IsPrimary: true}},
	}}
	out := ExpandOrdersByPackages(list)
	if len(out) != 1 {
		t.Fatalf("len=%d", len(out))
	}
}

func TestExpandOrdersByAllocGroupsPartial(t *testing.T) {
	list := []model.Order{{
		ID:      100,
		OrderNo: "OC202611100038",
		Status:  model.StatusPendingAlloc,
		Items: []model.OrderItem{
			{ID: 1, ProductName: "插销+卡簧", Quantity: 2},
			{ID: 2, ProductName: "MT900", Quantity: 1, AllocType: model.AllocSelfShip, SelfOrderNo: "SO1"},
		},
	}}
	out := ExpandOrdersByAllocGroups(list)
	if len(out) != 2 {
		t.Fatalf("len=%d want 2", len(out))
	}
	if out[0].ListRowKey == out[1].ListRowKey {
		t.Fatalf("row keys should differ: %q", out[0].ListRowKey)
	}
	pending := FilterAllocGroupRowsForTab(out, model.StatusPendingAlloc)
	allocated := FilterAllocGroupRowsForTab(out, model.StatusAllocated)
	if len(pending) != 1 || len(pending[0].Items) != 1 || pending[0].Items[0].ID != 1 {
		t.Fatalf("pending=%+v", pending)
	}
	if len(allocated) != 1 || len(allocated[0].Items) != 1 || allocated[0].Items[0].ID != 2 {
		t.Fatalf("allocated=%+v", allocated)
	}
	if allocated[0].Status != model.StatusAllocated || allocated[0].AllocType != model.AllocSelfShip {
		t.Fatalf("allocated header status/alloc=%q/%q", allocated[0].Status, allocated[0].AllocType)
	}
	if pending[0].AllocType != "" {
		t.Fatalf("pending row should clear header alloc, got %q", pending[0].AllocType)
	}
}
