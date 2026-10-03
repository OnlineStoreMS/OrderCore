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
