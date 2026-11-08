package service

import (
	"context"
	"testing"

	"ordercore/internal/model"
)

func TestMapOrderToPOLinesPartialUsesItemAmount(t *testing.T) {
	s := &OrderService{}
	o := &model.Order{
		PayAmount:   668,
		TotalAmount: 668,
		Items: []model.OrderItem{
			{ID: 2, ProductName: "轴承", Quantity: 1, Price: 398, TotalAmount: 398},
		},
	}
	lines := s.mapOrderToPOLines(context.Background(), "", o, "")
	if len(lines) != 1 {
		t.Fatalf("lines=%d", len(lines))
	}
	if lines[0].SaleAmount != 398 {
		t.Fatalf("saleAmount=%v want 398", lines[0].SaleAmount)
	}
}

func TestMapOrderToPOLinesSkipsShippedDropship(t *testing.T) {
	s := &OrderService{}
	o := &model.Order{
		PayAmount: 668,
		Items: []model.OrderItem{
			{ID: 1, ProductName: "碟片", Quantity: 1, Price: 270, TotalAmount: 270, AllocType: model.AllocDropship, ShipStatus: model.ShipShipped},
			{ID: 2, ProductName: "轴承", Quantity: 1, Price: 398, TotalAmount: 398, AllocType: model.AllocDropship, ShipStatus: model.ShipWaitShip},
		},
	}
	lines := s.mapOrderToPOLines(context.Background(), "", o, "")
	if len(lines) != 1 || lines[0].SaleAmount != 398 {
		t.Fatalf("lines=%+v", lines)
	}
}
