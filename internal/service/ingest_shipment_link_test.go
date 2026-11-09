package service

import (
	"testing"

	"ordercore/internal/dto"
	"ordercore/internal/model"
)

func TestMatchIngestItemsToOrderForShipment(t *testing.T) {
	order := &model.Order{
		Items: []model.OrderItem{
			{ID: 1, ProductName: "碟片", SkuSpecs: "CL700", PlatformOid: "oid-a", Quantity: 1},
			{ID: 2, ProductName: "轴承", SkuSpecs: "T47", PlatformOid: "oid-b", Quantity: 1},
		},
	}
	ids := matchIngestItemsToOrder(order.Items, []dto.OrderItemInput{
		{PlatformOid: "oid-a", ProductName: "碟片", Quantity: 1},
	})
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("ids=%v", ids)
	}
}

func TestFilterFulfillableSkipsRefunded(t *testing.T) {
	order := &model.Order{
		Items: []model.OrderItem{
			{ID: 1, Quantity: 1, ProductName: "飞轮", SkuSpecs: "R8101", ShipStatus: model.ShipShipped, AllocType: model.AllocSelfShip},
			{ID: 2, Quantity: 1, ProductName: "链条", AfterSaleStatus: "REFUND_MONEY_FINISH", LineOrderStatus: "ORDER_CANCELLED"},
		},
	}
	got := filterFulfillableItemIDs(order, []uint64{2, 1})
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("got=%v", got)
	}
}

func TestSoleUncoveredShippedPrefersFlywheel(t *testing.T) {
	order := &model.Order{
		Items: []model.OrderItem{
			{ID: 10, Quantity: 1, ProductName: "飞轮", SkuSpecs: "R8101-12速飞轮 11-34T", AllocType: model.AllocSelfShip, ShipStatus: model.ShipShipped, PackageID: 1},
			{ID: 11, Quantity: 1, ProductName: "链条", AfterSaleStatus: "REFUND_MONEY_FINISH", LineOrderStatus: "ORDER_CANCELLED", PackageID: 2},
		},
	}
	ids := soleUncoveredShippedItemIDs(order, nil)
	if len(ids) != 1 || ids[0] != 10 {
		t.Fatalf("ids=%v want [10]", ids)
	}
	lines := resolveShipmentLinesByExpress(order, "79203276978006", nil, func(string) *model.OrderPackage {
		return &model.OrderPackage{ID: 2} // mail 错挂退款包裹
	})
	if len(lines) != 1 || lines[0].OrderItemID != 10 || lines[0].SkuSpecs != "R8101-12速飞轮 11-34T" {
		t.Fatalf("lines=%+v", lines)
	}
}
