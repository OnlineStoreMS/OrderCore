package service

import (
	"testing"

	"ordercore/internal/model"
)

func TestPreserveLocalShipAgainstKDZSWait(t *testing.T) {
	shipped := &model.Order{ShipStatus: model.ShipShipped}
	if !preserveLocalShipAgainstKDZSWait(shipped, model.ShipWaitShip) {
		t.Fatal("shipped + wait_ship should preserve")
	}
	if preserveLocalShipAgainstKDZSWait(shipped, model.ShipShipped) {
		t.Fatal("incoming shipped should not block upgrade path")
	}

	withWaybill := &model.Order{
		ShipStatus: model.ShipWaitShip,
		Shipments:  []model.OrderShipment{{ExpressNo: "SF5148199925811"}},
	}
	if !preserveLocalShipAgainstKDZSWait(withWaybill, model.ShipWaitShip) {
		t.Fatal("local waybill + wait_ship should preserve")
	}

	plain := &model.Order{ShipStatus: model.ShipWaitShip}
	if preserveLocalShipAgainstKDZSWait(plain, model.ShipWaitShip) {
		t.Fatal("no local ship should not preserve")
	}
}

func TestRestoreShipStatusFromLocalShipments(t *testing.T) {
	o := &model.Order{
		ShipStatus: model.ShipWaitShip,
		Items:      []model.OrderItem{{ID: 1, Quantity: 1}},
		Shipments: []model.OrderShipment{{
			ExpressNo: "SF1",
			Items:     []model.OrderShipmentItem{{OrderItemID: 1, Qty: 1}},
		}},
	}
	if got := restoreShipStatusFromLocalShipments(o); got != model.ShipShipped {
		t.Fatalf("got %s want shipped", got)
	}

	partial := &model.Order{
		ShipStatus: model.ShipWaitShip,
		Items:      []model.OrderItem{{ID: 1, Quantity: 2}},
		Shipments: []model.OrderShipment{{
			ExpressNo: "SF1",
			Items:     []model.OrderShipmentItem{{OrderItemID: 1, Qty: 1}},
		}},
	}
	if got := restoreShipStatusFromLocalShipments(partial); got != model.ShipPartialShipped {
		t.Fatalf("got %s want partial_shipped", got)
	}
}

func TestItemCoveredByExpressShipment(t *testing.T) {
	o := &model.Order{
		Items: []model.OrderItem{{ID: 10, Quantity: 1}},
		Shipments: []model.OrderShipment{{
			ExpressNo: "SF1",
			Items:     []model.OrderShipmentItem{{OrderItemID: 10, Qty: 1}},
		}},
	}
	if !itemCoveredByExpressShipment(o, 10) {
		t.Fatal("expected covered")
	}
	if itemCoveredByExpressShipment(o, 99) {
		t.Fatal("unknown item should not be covered")
	}
}
