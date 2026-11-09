package service

import (
	"testing"

	"ordercore/internal/dto"
)

func TestLogisticsForThisPackageOnlyPrimary(t *testing.T) {
	req := dto.IngestOrderRequest{
		ExpressNo:      "SF1551169181248",
		ExpressCompany: "顺丰速运",
		ShippedAt:      "2026-11-09 00:45:07",
		Logistics: []dto.LogisticsInput{
			{ExpressNo: "YT0090343524511", ExpressCompany: "圆通快递", ShippedAt: "2026-11-08 15:29:03"},
			{ExpressNo: "SF1551169181248", ExpressCompany: "顺丰速运", ShippedAt: "2026-11-09 00:45:07"},
		},
	}
	got := logisticsForThisPackage(req)
	if len(got) != 1 || got[0].ExpressNo != "SF1551169181248" {
		t.Fatalf("got=%+v", got)
	}
}
