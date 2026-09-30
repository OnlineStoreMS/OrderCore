package service

import (
	"testing"

	"ordercore/internal/model"
)

func TestBasePlatformSysTid(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"abc", "abc"},
		{"abc#split12", "abc"},
		{"abc#dup99", "abc"},
		{"abc#alloc1", "abc"},
	}
	for _, c := range cases {
		if got := basePlatformSysTid(c.in); got != c.want {
			t.Fatalf("basePlatformSysTid(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestOrderEligibleForAllocSplitMerge(t *testing.T) {
	ok := &model.Order{Status: model.StatusPendingAlloc, ShipStatus: model.ShipWaitShip}
	if !orderEligibleForAllocSplitMerge(ok) {
		t.Fatal("pending_alloc should merge")
	}
	allocated := &model.Order{Status: model.StatusAllocated, AllocType: model.AllocSelfShip, ShipStatus: model.ShipWaitShip}
	if orderEligibleForAllocSplitMerge(allocated) {
		t.Fatal("allocated should not merge")
	}
	shipped := &model.Order{Status: model.StatusPendingAlloc, ShipStatus: model.ShipShipped}
	if orderEligibleForAllocSplitMerge(shipped) {
		t.Fatal("shipped should not merge")
	}
	withPO := &model.Order{Status: model.StatusPendingAlloc, PurchaseOrderID: "PO1"}
	if orderEligibleForAllocSplitMerge(withPO) {
		t.Fatal("linked PO should not merge")
	}
}
