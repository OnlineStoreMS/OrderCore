package service

import (
	"testing"

	"ordercore/internal/model"
)

func TestApplyFulfillmentStatusForShip(t *testing.T) {
	cases := []struct {
		name     string
		status   string
		alloc    string
		ship     string
		wantStat string
		wantSet  bool
	}{
		{name: "alloc full ship", status: model.StatusAllocated, ship: model.ShipShipped, wantStat: model.StatusShipped, wantSet: true},
		{name: "purchasing partial", status: model.StatusPurchasing, ship: model.ShipPartialShipped, wantStat: model.StatusPartialShip, wantSet: true},
		{name: "shipped unship wait purchase", status: model.StatusShipped, alloc: model.AllocPurchaseThenShip, ship: model.ShipWaitShip, wantStat: model.StatusPurchasing, wantSet: true},
		{name: "partial unship wait", status: model.StatusPartialShip, ship: model.ShipWaitShip, wantStat: model.StatusAllocated, wantSet: true},
		{name: "closed ignored", status: model.StatusClosed, ship: model.ShipShipped, wantSet: false},
		{name: "pending ignored", status: model.StatusPendingAlloc, ship: model.ShipShipped, wantSet: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := &model.Order{Status: tc.status, AllocType: tc.alloc}
			fields := map[string]any{}
			applyFulfillmentStatusForShip(o, tc.ship, fields)
			got, ok := fields["status"].(string)
			if tc.wantSet != ok {
				t.Fatalf("set=%v want %v fields=%v", ok, tc.wantSet, fields)
			}
			if tc.wantSet && got != tc.wantStat {
				t.Fatalf("status=%q want %q", got, tc.wantStat)
			}
		})
	}
}
