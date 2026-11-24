package service

import (
	"testing"
	"time"

	"ordercore/internal/dto"
	"ordercore/internal/integration/storesync"
	"ordercore/internal/model"
)

func TestResolvePackageLogisticsRemainderFactoryGetsYT(t *testing.T) {
	shipAt := time.Date(2026, 11, 12, 12, 23, 16, 0, time.Local)
	order := &model.Order{
		ID: 6965,
		Items: []model.OrderItem{
			{ID: 1, PackageID: 3073, PlatformOid: "oid-pin", ProductName: "插销",
				AllocType: model.AllocDropship, DropshipMode: model.DropshipKDZSFactory, FactoryID: "800931", Quantity: 2},
			{ID: 2, PackageID: 3083, PlatformOid: "oid-mt900", ProductName: "MT900",
				AllocType: model.AllocDropship, DropshipMode: model.DropshipOSMSSupplier, SupplierID: 28, Quantity: 1},
		},
		Packages: []model.OrderPackage{
			{ID: 3073, PlatformSysTid: "333220732227939006", IsPrimary: true},
			{ID: 3083, PlatformSysTid: "7755415123517426991"},
		},
		Shipments: []model.OrderShipment{{
			ID: 10, ExpressNo: "435440439478338", ExpressCompany: "韵达快递",
			CallbackStatus: model.CallbackSucceeded, CallbackMessage: "发货回传成功",
			ShippedAt: &shipAt,
			Items:     []model.OrderShipmentItem{{OrderItemID: 2, Qty: 1}},
		}},
	}
	bothLogistics := []dto.LogisticsInput{
		{ExpressNo: "YT0091490844484", ExpressCompany: "圆通快递", ShippedAt: "2026-11-11 16:08:56"},
		{ExpressNo: "435440439478338", ExpressCompany: "韵达快递", ShippedAt: "2026-11-12 12:23:16"},
	}

	// 厂家包：两包头表都是 YT，排除已占用韵达后剩余 YT
	factoryReq := dto.IngestOrderRequest{
		PlatformSysTid: "333220732227939006",
		AgentType:      model.AgentTypeFactory,
		ExpressNo:      "YT0091490844484",
		ExpressCompany: "圆通快递",
		ShippedAt:      "2026-11-11 16:08:57",
		Logistics:      bothLogistics,
		Items:          []dto.OrderItemInput{{PlatformOid: "oid-pin", Quantity: 2}},
	}
	got := resolvePackageLogistics(order, factoryReq)
	if len(got) != 1 || got[0].ExpressNo != "YT0091490844484" {
		t.Fatalf("factory got=%+v want YT", got)
	}

	// 供应商包：已有手动韵达 → 保持韵达，不被头表 YT 冲掉
	supplierReq := dto.IngestOrderRequest{
		PlatformSysTid: "7755415123517426991",
		AgentType:      model.AgentTypeSelf,
		ExpressNo:      "YT0091490844484",
		ExpressCompany: "圆通快递",
		ShippedAt:      "2026-11-12 12:23:16",
		Logistics:      bothLogistics,
		Items:          []dto.OrderItemInput{{PlatformOid: "oid-mt900", Quantity: 1}},
	}
	got2 := resolvePackageLogistics(order, supplierReq)
	if len(got2) != 1 || got2[0].ExpressNo != "435440439478338" {
		t.Fatalf("supplier got=%+v want yunda", got2)
	}
}

func TestResolvePackageLogisticsShippedAtWhenNoClaim(t *testing.T) {
	order := &model.Order{
		ID: 1,
		Items: []model.OrderItem{
			{ID: 1, PackageID: 10, PlatformOid: "a", AllocType: model.AllocDropship,
				DropshipMode: model.DropshipKDZSFactory, FactoryID: "1", Quantity: 1},
		},
		Packages: []model.OrderPackage{{ID: 10, PlatformSysTid: "sys1", IsPrimary: true}},
	}
	req := dto.IngestOrderRequest{
		PlatformSysTid: "sys1",
		AgentType:      model.AgentTypeFactory,
		ExpressNo:      "YT0091490844484", // 头表错号
		ShippedAt:      "2026-11-12 12:23:16",
		Logistics: []dto.LogisticsInput{
			{ExpressNo: "YT0091490844484", ExpressCompany: "圆通", ShippedAt: "2026-11-11 16:08:56"},
			{ExpressNo: "435440439478338", ExpressCompany: "韵达", ShippedAt: "2026-11-12 12:23:16"},
		},
		Items: []dto.OrderItemInput{{PlatformOid: "a", Quantity: 1}},
	}
	got := resolvePackageLogistics(order, req)
	if len(got) != 1 || got[0].ExpressNo != "435440439478338" {
		t.Fatalf("got=%+v want yunda by shippedAt", got)
	}
}

func TestPickIngestPrimaryUsesPerParcelShipTime(t *testing.T) {
	// 模拟修复后：每条物流保留自己的 shipTime，再用包级 consignTime 挑选
	list := []dto.LogisticsInput{
		{ExpressNo: "YT0091490844484", ExpressCompany: "圆通", ShippedAt: "2026-11-11 16:08:56"},
		{ExpressNo: "435440439478338", ExpressCompany: "韵达", ShippedAt: "2026-11-12 12:23:16"},
	}
	got := pickIngestPrimaryLogistics(list, "2026-11-12 12:23:16")
	if got.ExpressNo != "435440439478338" {
		t.Fatalf("got=%+v", got)
	}
	got2 := pickIngestPrimaryLogistics(list, "2026-11-11 16:08:57")
	if got2.ExpressNo != "YT0091490844484" {
		t.Fatalf("got2=%+v", got2)
	}
}

func TestResolvePackageLogisticsLegacyNoOrder(t *testing.T) {
	req := dto.IngestOrderRequest{
		ExpressNo: "SF1",
		Logistics: []dto.LogisticsInput{
			{ExpressNo: "YT1"},
			{ExpressNo: "SF1"},
		},
	}
	got := logisticsForThisPackage(req)
	if len(got) != 1 || got[0].ExpressNo != "SF1" {
		t.Fatalf("got=%+v", got)
	}
}

func TestExpressOwnedByOtherAuthoritative(t *testing.T) {
	order := &model.Order{
		Items: []model.OrderItem{
			{ID: 1, AllocType: model.AllocDropship, DropshipMode: model.DropshipKDZSFactory, FactoryID: "1"},
			{ID: 2, AllocType: model.AllocDropship, DropshipMode: model.DropshipOSMSSupplier},
		},
		Shipments: []model.OrderShipment{{
			ExpressNo: "435", CallbackStatus: model.CallbackSucceeded,
			Items: []model.OrderShipmentItem{{OrderItemID: 2}},
		}},
	}
	if !expressOwnedByOtherAuthoritative(order, "435", map[uint64]struct{}{1: {}}) {
		t.Fatal("expected owned by item 2")
	}
	if expressOwnedByOtherAuthoritative(order, "435", map[uint64]struct{}{2: {}}) {
		t.Fatal("candidate includes owner, should not block")
	}
}

func TestResolvePackageLogisticsRepairsWrongKDZSLink(t *testing.T) {
	// 助手曾把 YT 错挂到 MT900：不能因此把 YT 当权威占坑
	order := &model.Order{
		ID: 6965,
		Items: []model.OrderItem{
			{ID: 1, PackageID: 3073, PlatformOid: "oid-pin", AllocType: model.AllocDropship,
				DropshipMode: model.DropshipKDZSFactory, FactoryID: "800931", Quantity: 2},
			{ID: 2, PackageID: 3083, PlatformOid: "oid-mt900", AllocType: model.AllocDropship,
				DropshipMode: model.DropshipOSMSSupplier, Quantity: 1},
		},
		Packages: []model.OrderPackage{
			{ID: 3073, PlatformSysTid: "333220732227939006", IsPrimary: true},
			{ID: 3083, PlatformSysTid: "7755415123517426991"},
		},
		Shipments: []model.OrderShipment{{
			ID: 5940, ExpressNo: "YT0091490844484", ExpressCompany: "圆通快递",
			CallbackStatus: model.CallbackSkipped, CallbackMessage: "快递助手同步",
			Items: []model.OrderShipmentItem{
				{OrderItemID: 1, Qty: 2},
				{OrderItemID: 2, Qty: 1},
			},
		}},
	}
	both := []dto.LogisticsInput{
		{ExpressNo: "YT0091490844484", ExpressCompany: "圆通", ShippedAt: "2026-11-11 16:08:56"},
		{ExpressNo: "435440439478338", ExpressCompany: "韵达", ShippedAt: "2026-11-12 12:23:16"},
	}
	mtReq := dto.IngestOrderRequest{
		PlatformSysTid: "7755415123517426991", AgentType: model.AgentTypeSelf,
		ExpressNo: "435440439478338", ShippedAt: "2026-11-12 12:23:16", Logistics: both,
		Items: []dto.OrderItemInput{{PlatformOid: "oid-mt900", Quantity: 1}},
	}
	got := resolvePackageLogistics(order, mtReq)
	if len(got) != 1 || got[0].ExpressNo != "435440439478338" {
		t.Fatalf("MT900 should take yunda despite wrong YT link, got=%+v", got)
	}
	pinReq := dto.IngestOrderRequest{
		PlatformSysTid: "333220732227939006", AgentType: model.AgentTypeFactory,
		ExpressNo: "YT0091490844484", ShippedAt: "2026-11-11 16:08:57", Logistics: both,
		Items: []dto.OrderItemInput{{PlatformOid: "oid-pin", Quantity: 2}},
	}
	got2 := resolvePackageLogistics(order, pinReq)
	if len(got2) != 1 || got2[0].ExpressNo != "YT0091490844484" {
		t.Fatalf("pin should keep YT, got=%+v", got2)
	}
}

func TestMapTradeToIngestPicksByShippedAt(t *testing.T) {
	tMT := storesync.TradeOrder{
		Platform: "FXG",
		SysTids:  []string{"7755415123517426991"},
		Tids:     []string{"6931341818656489193", "6931341818656554729"},
		AgentType: 1,
		ExpressNo: "YT0091490844484",
		ExpressCompany: "圆通快递",
		ShippedAt: "2026-11-12 12:23:16",
		Logistics: []storesync.TradeLogistics{
			{TrackingNo: "YT0091490844484", CompanyName: "圆通快递", ShipTime: "2026-11-11 16:08:56"},
			{TrackingNo: "435440439478338", CompanyName: "韵达快递", ShipTime: "2026-11-12 12:23:16"},
		},
		Goods: []storesync.TradeGoods{{Oid: "6931341818656554729", SkuName: "MT900", Num: 1}},
	}
	ing := mapTradeToIngest(tMT)
	if ing.ExpressNo != "435440439478338" {
		t.Fatalf("map ExpressNo=%s want yunda; logistics=%+v shippedAt=%s", ing.ExpressNo, ing.Logistics, ing.ShippedAt)
	}
	for _, lg := range ing.Logistics {
		if lg.ExpressNo == "YT0091490844484" && lg.ShippedAt != "2026-11-11 16:08:56" {
			t.Fatalf("YT shipTime overwritten: %+v", lg)
		}
		if lg.ExpressNo == "435440439478338" && lg.ShippedAt != "2026-11-12 12:23:16" {
			t.Fatalf("yunda shipTime overwritten: %+v", lg)
		}
	}
	order := &model.Order{
		ID: 6965,
		Items: []model.OrderItem{
			{ID: 1, PackageID: 3073, PlatformOid: "6931341818656489193", AllocType: model.AllocDropship, DropshipMode: model.DropshipKDZSFactory, FactoryID: "800931", Quantity: 2},
			{ID: 2, PackageID: 3083, PlatformOid: "6931341818656554729", AllocType: model.AllocDropship, DropshipMode: model.DropshipOSMSSupplier, Quantity: 1},
		},
		Packages: []model.OrderPackage{
			{ID: 3073, PlatformSysTid: "333220732227939006", IsPrimary: true},
			{ID: 3083, PlatformSysTid: "7755415123517426991"},
		},
	}
	ing.PlatformSysTid = "7755415123517426991"
	ing.Items = []dto.OrderItemInput{{PlatformOid: "6931341818656554729", Quantity: 1}}
	got := resolvePackageLogistics(order, ing)
	if len(got) != 1 || got[0].ExpressNo != "435440439478338" {
		t.Fatalf("resolve got=%+v", got)
	}
}
