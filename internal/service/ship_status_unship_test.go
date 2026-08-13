package service

import (
	"testing"

	"ordercore/internal/model"
)

func TestComputeShipStatus_MultiItemUnship(t *testing.T) {
	itemA := model.OrderItem{ID: 1, Quantity: 1}
	itemB := model.OrderItem{ID: 2, Quantity: 1}

	t.Run("cancel one of two waybills with item rows -> partial", func(t *testing.T) {
		o := &model.Order{
			ShipStatus: model.ShipShipped,
			Items:      []model.OrderItem{itemA, itemB},
			Shipments: []model.OrderShipment{
				{ID: 10, ExpressNo: "SF-A", Items: []model.OrderShipmentItem{{OrderItemID: 1, Qty: 1}}},
				// B already removed (simulating after unship of SF-B)
			},
		}
		got := computeShipStatusAfter(o, nil)
		if got != model.ShipPartialShipped {
			t.Fatalf("got %s want %s", got, model.ShipPartialShipped)
		}
	})

	t.Run("cancel all waybills -> wait_ship", func(t *testing.T) {
		o := &model.Order{
			ShipStatus: model.ShipShipped,
			Items:      []model.OrderItem{itemA, itemB},
			Shipments:  nil,
		}
		got := computeShipStatusAfter(o, nil)
		if got != model.ShipWaitShip {
			t.Fatalf("got %s want %s", got, model.ShipWaitShip)
		}
	})

	t.Run("cancel last of partial -> wait_ship", func(t *testing.T) {
		o := &model.Order{
			ShipStatus: model.ShipPartialShipped,
			Items:      []model.OrderItem{itemA, itemB},
			Shipments:  nil,
		}
		got := computeShipStatusAfter(o, nil)
		if got != model.ShipWaitShip {
			t.Fatalf("got %s want %s", got, model.ShipWaitShip)
		}
	})

	t.Run("both waybills remain with all items -> shipped", func(t *testing.T) {
		o := &model.Order{
			ShipStatus: model.ShipPartialShipped,
			Items:      []model.OrderItem{itemA, itemB},
			Shipments: []model.OrderShipment{
				{ID: 10, Items: []model.OrderShipmentItem{{OrderItemID: 1, Qty: 1}}},
				{ID: 11, Items: []model.OrderShipmentItem{{OrderItemID: 2, Qty: 1}}},
			},
		}
		got := computeShipStatusAfter(o, nil)
		if got != model.ShipShipped {
			t.Fatalf("got %s want %s", got, model.ShipShipped)
		}
	})

	t.Run("legacy no item rows but shipments remain + shipped -> stay shipped", func(t *testing.T) {
		o := &model.Order{
			ShipStatus: model.ShipShipped,
			Items:      []model.OrderItem{itemA, itemB},
			Shipments:  []model.OrderShipment{{ID: 10, ExpressNo: "SF-KEEP"}},
		}
		got := computeShipStatusAfter(o, nil)
		if got != model.ShipShipped {
			t.Fatalf("got %s want %s (一分多包裹无明细)", got, model.ShipShipped)
		}
	})

	t.Run("legacy no item rows and no shipments + shipped -> wait_ship (bugfix)", func(t *testing.T) {
		o := &model.Order{
			ShipStatus: model.ShipShipped,
			Items:      []model.OrderItem{itemA, itemB},
			Shipments:  nil,
		}
		got := computeShipStatusAfter(o, nil)
		if got != model.ShipWaitShip {
			t.Fatalf("got %s want %s", got, model.ShipWaitShip)
		}
	})
}
