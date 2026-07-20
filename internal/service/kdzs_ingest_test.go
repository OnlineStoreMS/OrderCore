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
	if h.ApplySyncAlloc || h.Status != model.StatusPendingShip {
		t.Fatalf("self wait_send should stay pending, hint=%+v", h)
	}
}

func TestDeriveKDZSWaitAuditSelfPending(t *testing.T) {
	h := deriveKDZSIngest(model.SourceKDZS, dto.IngestOrderRequest{
		PlatformStatus:     "wait_audit",
		PlatformStatusText: "待推单",
		AgentType:          1,
	})
	if h.ApplySyncAlloc || !h.ClearAlloc || h.Status != model.StatusPendingShip {
		t.Fatalf("hint=%+v", h)
	}
}
