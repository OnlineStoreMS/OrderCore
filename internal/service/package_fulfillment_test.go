package service

import (
	"testing"

	"ordercore/internal/dto"
	"ordercore/internal/model"
)

func TestMatchIngestItemsToOrder(t *testing.T) {
	roots := []model.OrderItem{
		{ID: 1, PlatformOid: "oid-a", ProductName: "A", SkuSpecs: "sa"},
		{ID: 2, PlatformOid: "oid-b", ProductName: "B", SkuSpecs: "sb"},
		{ID: 3, PlatformOid: "parent-tid", ProductName: "B249", SkuSpecs: "黑色"}, // 代发包 oid=主 tid
		{ID: 99, SplitKind: "partial", ParentOrderItemID: 1, PlatformOid: "skip"},
	}
	ids := matchIngestItemsToOrder(roots, []dto.OrderItemInput{
		{PlatformOid: "oid-a", ProductName: "A", SkuSpecs: "sa"},
		{PlatformOid: "parent-tid", ProductName: "B249", SkuSpecs: "黑色"},
	})
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 3 {
		t.Fatalf("ids=%v", ids)
	}
}

func TestPreserveOSMSItemDropship(t *testing.T) {
	if !preserveOSMSItemDropship(&model.OrderItem{
		AllocType: model.AllocDropship, DropshipMode: model.DropshipOSMSSupplier, SupplierID: 9,
	}) {
		t.Fatal("expected preserve")
	}
	if preserveOSMSItemDropship(&model.OrderItem{
		AllocType: model.AllocDropship, DropshipMode: model.DropshipKDZSFactory, SupplierID: 9,
	}) {
		t.Fatal("kdzs factory should not preserve as OSMS")
	}
}

func TestIngestItemOIDsCoverage(t *testing.T) {
	req := dto.IngestOrderRequest{Items: []dto.OrderItemInput{
		{PlatformOid: "a"}, {PlatformOid: "b"},
	}}
	other := &model.Order{Items: []model.OrderItem{
		{PlatformOid: "c"},
	}}
	if tidSetCovers(ingestItemOIDs(req), orderItemOIDs(other)) {
		t.Fatal("distinct package goods must not be covered")
	}
	other2 := &model.Order{Items: []model.OrderItem{{PlatformOid: "a"}}}
	if !tidSetCovers(ingestItemOIDs(req), orderItemOIDs(other2)) {
		t.Fatal("subset should be covered")
	}
}
