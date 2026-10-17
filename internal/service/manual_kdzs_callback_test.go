package service

import (
	"testing"

	"ordercore/internal/model"
)

func TestManualNeedsKDZSShipCallback(t *testing.T) {
	if manualNeedsKDZSShipCallback(nil) {
		t.Fatal("nil")
	}
	plain := &model.Order{SourceChannel: model.SourceManual}
	if manualNeedsKDZSShipCallback(plain) {
		t.Fatal("manual without tid should skip")
	}
	withSys := &model.Order{SourceChannel: model.SourceManual, PlatformSysTid: "1559974545083219968"}
	if !manualNeedsKDZSShipCallback(withSys) {
		t.Fatal("manual with sysTid should callback")
	}
	kdzs := &model.Order{SourceChannel: model.SourceKDZS, PlatformSysTid: "x"}
	if manualNeedsKDZSShipCallback(kdzs) {
		t.Fatal("kdzs channel not covered by manual helper")
	}
}
