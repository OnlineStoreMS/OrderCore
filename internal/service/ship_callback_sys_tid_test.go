package service

import (
	"testing"

	"ordercore/internal/model"
)

func TestResolveShipCallbackSysTid(t *testing.T) {
	o := &model.Order{
		PlatformSysTid: "333-header",
		Packages: []model.OrderPackage{
			{ID: 1, PlatformSysTid: "333-factory"},
			{ID: 2, PlatformSysTid: "775-supplier"},
		},
		Items: []model.OrderItem{
			{ID: 10, PackageID: 1, AllocType: model.AllocDropship, DropshipMode: model.DropshipKDZSFactory},
			{ID: 20, PackageID: 2, AllocType: model.AllocDropship, DropshipMode: model.DropshipOSMSSupplier},
		},
	}
	sys, err := resolveShipCallbackSysTid(o, []uint64{20})
	if err != nil || sys != "775-supplier" {
		t.Fatalf("supplier sys=%q err=%v", sys, err)
	}
	sys, err = resolveShipCallbackSysTid(o, []uint64{10})
	if err != nil || sys != "333-factory" {
		t.Fatalf("factory sys=%q err=%v", sys, err)
	}
	if _, err := resolveShipCallbackSysTid(o, []uint64{10, 20}); err == nil {
		t.Fatal("expected cross-package error")
	}
}

func TestShipLinesAllKDZSFactory(t *testing.T) {
	o := &model.Order{
		AllocType: model.AllocDropship, DropshipMode: model.DropshipKDZSFactory,
		Items: []model.OrderItem{
			{ID: 10, AllocType: model.AllocDropship, DropshipMode: model.DropshipKDZSFactory},
			{ID: 20, AllocType: model.AllocDropship, DropshipMode: model.DropshipOSMSSupplier},
		},
	}
	if !shipLinesAllKDZSFactory(o, []resolvedShipLine{{OrderItemID: 10}}) {
		t.Fatal("factory-only should block")
	}
	if shipLinesAllKDZSFactory(o, []resolvedShipLine{{OrderItemID: 20}}) {
		t.Fatal("supplier line should not block")
	}
	if shipLinesAllKDZSFactory(o, []resolvedShipLine{{OrderItemID: 10}, {OrderItemID: 20}}) {
		t.Fatal("mixed should not block")
	}
}
