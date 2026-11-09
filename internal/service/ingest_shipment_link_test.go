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
