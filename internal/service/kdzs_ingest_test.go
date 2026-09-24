package service

import (
	"testing"

	"ordercore/internal/dto"
	"ordercore/internal/integration/storesync"
	"ordercore/internal/model"
)

func TestDeriveKDZSFactoryFromFactoryName(t *testing.T) {
	h := deriveKDZSIngest(model.SourceKDZS, dto.IngestOrderRequest{
		PlatformStatus:     "ORDER_PAID",
		PlatformStatusText: "待发货",
		FactoryName:        "13817054118",
		AgentType:          0,
	})
	if h.AgentType != model.AgentTypeFactory {
		t.Fatalf("agentType=%d", h.AgentType)
	}
	if !h.ApplySyncAlloc || h.Status != model.StatusAllocated || h.AllocType != model.AllocDropship {
		t.Fatalf("hint=%+v", h)
	}
	if h.ShipStatus != model.ShipWaitShip {
		t.Fatalf("shipStatus=%s", h.ShipStatus)
	}
	// 电商「待发货」不能推断为 wait_send；厂家代发走 default 分支仍会已分配
	if h.PlatformStatus == model.KDZSWaitSend {
		t.Fatalf("ORDER_PAID+待发货 must not become wait_send, platformStatus=%s", h.PlatformStatus)
	}
}

func TestDeriveKDZSOrderPaidSelfStaysPending(t *testing.T) {
	// 待推单在助手侧，但详情常回 ORDER_PAID + 文案「待发货」——不可误成自营已分配
	h := deriveKDZSIngest(model.SourceKDZS, dto.IngestOrderRequest{
		PlatformStatus:     "ORDER_PAID",
		PlatformStatusText: "待发货",
		AgentType:          1,
	})
	if h.Status != model.StatusPendingAlloc || h.ApplySyncAlloc || h.AllocType == model.AllocSelfShip {
		t.Fatalf("ORDER_PAID self should stay pending_alloc, hint=%+v", h)
	}
	if h.PlatformStatus == model.KDZSWaitSend {
		t.Fatalf("must not normalize ecommerce 待发货 to wait_send, got %s", h.PlatformStatus)
	}
}

func TestDeriveKDZSSelfIgnoresBareFactoryID(t *testing.T) {
	h := deriveKDZSIngest(model.SourceKDZS, dto.IngestOrderRequest{
		PlatformStatus:     "wait_send",
		PlatformStatusText: "待发货",
		FactoryID:          "800888",
		AgentType:          1,
	})
	if h.AgentType != model.AgentTypeSelf {
		t.Fatalf("agentType=%d", h.AgentType)
	}
	if !h.ApplySyncAlloc || h.Status != model.StatusAllocated || h.AllocType != model.AllocSelfShip || h.ShipStatus != model.ShipWaitShip {
		t.Fatalf("self wait_send should be allocated+self_ship+wait_ship, hint=%+v", h)
	}
}

func TestDeriveKDZSWaitAuditSelf(t *testing.T) {
	h := deriveKDZSIngest(model.SourceKDZS, dto.IngestOrderRequest{
		PlatformStatus:     "wait_audit",
		PlatformStatusText: "待推单",
		AgentType:          1,
	})
	if h.Status != model.StatusPendingAlloc || !h.ClearAlloc || h.ShipStatus != model.ShipWaitShip {
		t.Fatalf("wait_audit self should be pending_alloc, hint=%+v", h)
	}
}

func TestDeriveKDZSWaitAuditAfterFactoryRevoke(t *testing.T) {
	// 撤单回待推单后，快递助手仍可能挂厂家；履约侧必须清分配
	h := deriveKDZSIngest(model.SourceKDZS, dto.IngestOrderRequest{
		PlatformStatus:     "wait_audit",
		PlatformStatusText: "待推单",
		AgentType:          2,
		FactoryID:          "903134",
		FactoryName:        "13817054118",
	})
	if h.Status != model.StatusPendingAlloc || !h.ClearAlloc || h.ApplySyncAlloc {
		t.Fatalf("wait_audit after revoke should clear alloc, hint=%+v", h)
	}
	if h.AgentType != model.AgentTypeSelf {
		t.Fatalf("wait_audit should reset agentType to self for re-alloc, got %d", h.AgentType)
	}
	if h.AllocType != "" || h.DropshipMode != "" {
		t.Fatalf("alloc should be empty, hint=%+v", h)
	}
}

func TestDeriveKDZSShippedSelf(t *testing.T) {
	h := deriveKDZSIngest(model.SourceKDZS, dto.IngestOrderRequest{
		PlatformStatus:     "shipped",
		PlatformStatusText: "已发货",
		AgentType:          1,
	})
	if !h.ApplySyncAlloc || h.Status != model.StatusAllocated || h.AllocType != model.AllocSelfShip || h.ShipStatus != model.ShipShipped {
		t.Fatalf("shipped self hint=%+v", h)
	}
}

func TestDeriveKDZSOrderCancelled(t *testing.T) {
	h := deriveKDZSIngest(model.SourceKDZS, dto.IngestOrderRequest{
		PlatformStatus:      "order_cancelled",
		PlatformStatusText:  "已取消",
		EcommerceStatus:     "ORDER_CANCELLED",
		EcommerceStatusText: "ORDER_CANCELLED",
		AgentType:           1,
	})
	if h.Status != model.StatusClosed || h.ApplySyncAlloc || h.ClearAlloc {
		t.Fatalf("cancelled should close but keep alloc (ClearAlloc=false), hint=%+v", h)
	}
}

func TestDeriveKDZSRefundFinishKeepsAlloc(t *testing.T) {
	h := deriveKDZSIngest(model.SourceKDZS, dto.IngestOrderRequest{
		PlatformStatus:      "wait_send",
		EcommerceStatus:     "ORDER_CANCELLED",
		EcommerceStatusText: "交易关闭",
		AfterSaleStatus:     "REFUND_MONEY_FINISH",
		AgentType:           2,
		FactoryID:           "800931",
		FactoryName:         "18956949877",
	})
	if h.Status != model.StatusClosed || h.ClearAlloc {
		t.Fatalf("refund/cancel close must not auto clear alloc, hint=%+v", h)
	}
}

func TestDeriveKDZSRefundMoneyFinish(t *testing.T) {
	h := deriveKDZSIngest(model.SourceKDZS, dto.IngestOrderRequest{
		PlatformStatus:  "wait_send",
		EcommerceStatus: "REFUND_MONEY_FINISH",
		AgentType:       1,
	})
	if h.Status != model.StatusClosed {
		t.Fatalf("refund finish should close, hint=%+v", h)
	}
}

func TestCloseDetachReason(t *testing.T) {
	if got := closeDetachReason(dto.IngestOrderRequest{
		EcommerceStatus: "REFUND_MONEY_FINISH",
		AfterSaleStatus: "REFUND_MONEY_FINISH",
	}); got != "退款完成" {
		t.Fatalf("want 退款完成 got %q", got)
	}
	if got := closeDetachReason(dto.IngestOrderRequest{
		EcommerceStatus:     "ORDER_CANCELLED",
		EcommerceStatusText: "交易关闭",
	}); got != "交易关闭" {
		t.Fatalf("want 交易关闭 got %q", got)
	}
}

func TestTradeGoodsExcludedFromFulfillment(t *testing.T) {
	cases := []struct {
		g    storesync.TradeGoods
		want bool
	}{
		{storesync.TradeGoods{Num: 1, AfterSaleStatus: "REFUND_SUCCESS"}, true},
		{storesync.TradeGoods{Num: 1, OrderStatus: "TRADE_CLOSED"}, true},
		{storesync.TradeGoods{Num: 1, AfterSaleStatus: "WAIT_SELLER_AGREE"}, false},
		{storesync.TradeGoods{Num: 1, AfterSaleStatus: "REFUND_MONEY_NONE", OrderStatus: "ORDER_PAID"}, false},
		{storesync.TradeGoods{Num: 0}, true},
	}
	for i, c := range cases {
		if got := tradeGoodsExcludedFromFulfillment(c.g); got != c.want {
			t.Fatalf("case %d got %v want %v", i, got, c.want)
		}
	}
}

func TestMapTradeToIngestKeepsRefundedGoods(t *testing.T) {
	req := mapTradeToIngest(storesync.TradeOrder{
		Platform:            "FXG",
		Tids:                []string{"tid1"},
		SysTids:             []string{"sys1"},
		TradeStatus:         "wait_send",
		PlatformOrderStatus: "ORDER_PAID",
		AfterSaleStatus:     "REFUND_MONEY_NONE",
		Payment:             135,
		Goods: []storesync.TradeGoods{
			{Title: "盘片", SkuName: "50-34T", Num: 1, Price: 158, AfterSaleStatus: "REFUND_SUCCESS", AfterSaleStatusText: "退款成功", OrderStatus: "TRADE_CLOSED"},
			{Title: "链条", SkuName: "HG95", Num: 1, Price: 135, AfterSaleStatus: "REFUND_MONEY_NONE", OrderStatus: "ORDER_PAID", SkuID: "sku-hg95"},
		},
	})
	if len(req.Items) != 2 {
		t.Fatalf("items=%+v", req.Items)
	}
	if req.Items[0].SkuSpecs != "50-34T" || req.Items[0].AfterSaleStatus != "REFUND_SUCCESS" {
		t.Fatalf("refunded item=%+v", req.Items[0])
	}
	if req.Items[1].SkuSpecs != "HG95" || req.Items[1].PlatformSkuID != "sku-hg95" {
		t.Fatalf("item=%+v", req.Items[1])
	}
	if !tradeGoodsExcludedFromFulfillment(storesync.TradeGoods{Num: 1, AfterSaleStatus: req.Items[0].AfterSaleStatus, OrderStatus: req.Items[0].LineOrderStatus}) {
		t.Fatalf("refunded line should still be excluded from fulfillment")
	}
}

func TestIngestChildPlatformIDs(t *testing.T) {
	req := dto.IngestOrderRequest{
		PlatformOrderID: "parent-tid",
		RawPayload:      `{"tids":["parent-tid","child-oid-1","child-oid-2","parent-tid"]}`,
	}
	got := ingestChildPlatformIDs(req)
	if len(got) != 2 || got[0] != "child-oid-1" || got[1] != "child-oid-2" {
		t.Fatalf("got=%v", got)
	}
}

func TestOrderSafeToSupersedeAsChildDup(t *testing.T) {
	if !orderSafeToSupersedeAsChildDup(&model.Order{Status: model.StatusClosed}) {
		t.Fatal("closed should be safe")
	}
	if orderSafeToSupersedeAsChildDup(&model.Order{Status: model.StatusClosed, PurchaseOrderID: "PO1"}) {
		t.Fatal("with PO should not be safe")
	}
	if orderSafeToSupersedeAsChildDup(&model.Order{Status: model.StatusAllocated}) {
		t.Fatal("allocated should not be safe")
	}
}
