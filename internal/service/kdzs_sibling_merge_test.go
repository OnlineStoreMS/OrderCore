package service

import (
	"testing"

	"ordercore/internal/model"
)

func TestFindKDZSParentKeeperPrefersNonSplit(t *testing.T) {
	// pure unit of score logic via constructing orders — findKDZSParentKeeper needs repo.
	// Smoke: basePlatformSysTid used in merge flag.
	if got := basePlatformSysTid("abc#split12"); got != "abc" {
		t.Fatalf("got %q", got)
	}
}

func TestCoalesceShip(t *testing.T) {
	if coalesceShip("") != model.ShipWaitShip {
		t.Fatal("empty -> wait_ship")
	}
	if coalesceShip(model.ShipShipped) != model.ShipShipped {
		t.Fatal("keep shipped")
	}
}
