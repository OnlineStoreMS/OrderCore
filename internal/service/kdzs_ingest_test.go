package service

import (
	"testing"

	"ordercore/internal/dto"
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
	if h.PlatformStatus != model.KDZSWaitSend {
		t.Fatalf("platformStatus=%s", h.PlatformStatus)
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
	if h.Status != model.StatusClosed || h.ApplySyncAlloc || !h.ClearAlloc {
		t.Fatalf("cancelled should close, hint=%+v", h)
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
