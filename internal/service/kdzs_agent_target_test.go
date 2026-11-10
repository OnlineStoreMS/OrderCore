package service

import (
	"testing"

	"ordercore/internal/model"
)

func TestCollectKDZSAgentPackageIDsSkipsRefunded(t *testing.T) {
	o := &model.Order{
		Items: []model.OrderItem{
			{ID: 1, PackageID: 10, PlatformOid: "a", AfterSaleStatus: "REFUND_MONEY_FINISH", LineOrderStatus: "ORDER_CANCELLED", Quantity: 2},
			{ID: 2, PackageID: 11, PlatformOid: "b", LineOrderStatus: "ORDER_PAID", Quantity: 2, ShipStatus: model.ShipWaitShip},
		},
	}
	got := collectKDZSAgentPackageIDs(o, nil, nil)
	if len(got) != 1 || got[0] != 11 {
		t.Fatalf("got=%v want [11]", got)
	}
	got = collectKDZSAgentPackageIDs(o, []uint64{2}, nil)
	if len(got) != 1 || got[0] != 11 {
		t.Fatalf("selected got=%v", got)
	}
}
