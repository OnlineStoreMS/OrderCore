package service

import (
	"strings"
	"testing"

	"ordercore/internal/model"
)

func TestExpandOrdersByPackages(t *testing.T) {
	list := []model.Order{{
		ID:              4396,
		OrderNo:         "OC202609190036",
		PlatformOrderID: "6917865244162993928",
		PlatformSysTid:  "29353909801438706257",
		AllocType:       model.AllocMixed,
		Packages: []model.OrderPackage{
			{ID: 803, PlatformSysTid: "29353909801438706257", IsPrimary: true},
			{ID: 1257, PlatformSysTid: "26878583463337148860", FenFaRemark: "0", IsPrimary: false},
		},
		Items: []model.OrderItem{
			{ID: 1, PackageID: 803, AllocType: model.AllocSelfShip, ProductName: "A", Quantity: 1},
			{ID: 2, PackageID: 1257, AllocType: model.AllocDropship, ProductName: "B249", Quantity: 1},
		},
	}}
	out := ExpandOrdersByPackages(list)
	if len(out) != 2 {
		t.Fatalf("len=%d want 2", len(out))
	}
	if out[0].ID != 4396 || out[1].ID != 4396 {
		t.Fatalf("same OC id expected")
	}
	if out[0].PlatformSysTid != "29353909801438706257" || out[1].PlatformSysTid != "26878583463337148860" {
		t.Fatalf("sysTid=%q / %q", out[0].PlatformSysTid, out[1].PlatformSysTid)
	}
	if out[0].AllocType != model.AllocSelfShip || out[1].AllocType != model.AllocDropship {
		t.Fatalf("alloc=%q / %q", out[0].AllocType, out[1].AllocType)
	}
	if len(out[0].Items) != 1 || len(out[1].Items) != 1 {
		t.Fatalf("items per row=%d / %d", len(out[0].Items), len(out[1].Items))
	}
	if out[0].ListRowKey == out[1].ListRowKey {
		t.Fatalf("listRowKey should differ")
	}
}

func TestScopeOrderBySysTid(t *testing.T) {
	o := &model.Order{
		ID: 6965, OrderNo: "OC202611100038", PlatformOrderID: "6931341818656489193",
		PayAmount: 309, AllocType: model.AllocMixed,
		Packages: []model.OrderPackage{
			{ID: 3073, PlatformSysTid: "333", IsPrimary: true, MailNo: "YT1", FenFaRemark: "30"},
			{ID: 3083, PlatformSysTid: "775", MailNo: "YD1"},
		},
		Items: []model.OrderItem{
			{ID: 1, PackageID: 3073, ProductName: "插销", AllocType: model.AllocDropship, DropshipMode: model.DropshipKDZSFactory, FactoryID: "1", FactoryName: "追风", PurchaseOrderID: "PO202611110002", Price: 22, Quantity: 2, TotalAmount: 44, SkuSpecs: "插销+卡簧*2副"},
			{ID: 2, PackageID: 3083, ProductName: "MT900", AllocType: model.AllocDropship, DropshipMode: model.DropshipOSMSSupplier, SupplierName: "凌峰单车", PurchaseOrderID: "PO202611110001", Price: 265, Quantity: 1, TotalAmount: 265, SkuSpecs: "MT900-160mm"},
		},
		Shipments: []model.OrderShipment{
			{ID: 1, ExpressNo: "YT1", Items: []model.OrderShipmentItem{{OrderItemID: 1}}},
			{ID: 2, ExpressNo: "YD1", Items: []model.OrderShipmentItem{{OrderItemID: 2}}},
		},
		StatusLogs: []model.OrderStatusLog{
			{ID: 1, Action: "ingest", Remark: "同步待推单→清空分配，恢复待分配"},
			{ID: 2, Action: "allocate", Remark: "dropship/osms_supplier kdzs=self_print po=PO202611110001 items=1"},
			{ID: 3, Action: "ingest_update", Remark: "同步保留OSMS代发→凌峰单车"},
			{ID: 4, Action: "allocate", Remark: "dropship/kdzs_factory kdzs=push_factory po=PO202611110002 items=1"},
			{ID: 5, Action: "unship", Remark: "供应链删除发货批次，回退运单 YT1"},
			{ID: 6, Action: "ingest_update", Remark: "同步交易完成→已完成+已发货"},
			{ID: 7, Action: "ship", Remark: "发货 YD1"},
			{ID: 8, Action: "repair", Remark: "撤销误同步自营：插销+卡簧未勾选分配，仅保留碟片自营"},
		},
	}
	scoped := ScopeOrderBySysTid(o, "775")
	if scoped == nil || len(scoped.Items) != 1 || scoped.Items[0].ID != 2 {
		t.Fatalf("items=%+v", scoped.Items)
	}
	if scoped.PlatformSysTid != "775" || len(scoped.Packages) != 1 {
		t.Fatalf("pkg=%+v sys=%s", scoped.Packages, scoped.PlatformSysTid)
	}
	if scoped.SupplierName != "凌峰单车" || scoped.PayAmount != 265 {
		t.Fatalf("header supplier=%s pay=%v", scoped.SupplierName, scoped.PayAmount)
	}
	if len(scoped.Shipments) != 1 || scoped.Shipments[0].ExpressNo != "YD1" {
		t.Fatalf("shipments=%+v", scoped.Shipments)
	}
	actions := statusLogRemarks(scoped.StatusLogs)
	if !containsAll(actions, []string{"同步待推单", "PO202611110001", "凌峰单车", "同步交易完成", "YD1"}) {
		t.Fatalf("mt900 logs missing expected: %v", actions)
	}
	if containsAny(actions, []string{"PO202611110002", "YT1", "插销+卡簧"}) {
		t.Fatalf("mt900 logs leaked other package: %v", actions)
	}

	pin := ScopeOrderBySysTid(o, "333")
	pinActions := statusLogRemarks(pin.StatusLogs)
	if !containsAll(pinActions, []string{"同步待推单", "PO202611110002", "YT1", "同步交易完成", "插销+卡簧"}) {
		t.Fatalf("pin logs missing expected: %v", pinActions)
	}
	if containsAny(pinActions, []string{"PO202611110001", "凌峰单车", "YD1"}) {
		t.Fatalf("pin logs leaked other package: %v", pinActions)
	}
}

func statusLogRemarks(logs []model.OrderStatusLog) []string {
	out := make([]string, 0, len(logs))
	for _, lg := range logs {
		out = append(out, lg.Remark)
	}
	return out
}

func containsAll(haystack []string, needles []string) bool {
	for _, n := range needles {
		ok := false
		for _, h := range haystack {
			if strings.Contains(h, n) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func containsAny(haystack []string, needles []string) bool {
	for _, n := range needles {
		for _, h := range haystack {
			if strings.Contains(h, n) {
				return true
			}
		}
	}
	return false
}

func TestExpandOrdersByPackagesSingle(t *testing.T) {
	list := []model.Order{{
		ID: 1, OrderNo: "OC1",
		Packages: []model.OrderPackage{{ID: 1, PlatformSysTid: "s1", IsPrimary: true}},
	}}
	out := ExpandOrdersByPackages(list)
	if len(out) != 1 {
		t.Fatalf("len=%d", len(out))
	}
}

func TestExpandOrdersByPackagesSkipsEmptyPkg(t *testing.T) {
	list := []model.Order{{
		ID: 7021, OrderNo: "OC202611110046",
		Packages: []model.OrderPackage{
			{ID: 3130, PlatformSysTid: "empty-sys", IsPrimary: true},
			{ID: 3131, PlatformSysTid: "goods-sys", IsPrimary: false},
		},
		Items: []model.OrderItem{
			{ID: 1, PackageID: 3131, ProductName: "A", Quantity: 1},
			{ID: 2, PackageID: 3131, ProductName: "B", Quantity: 1},
		},
	}}
	out := ExpandOrdersByPackages(list)
	if len(out) != 1 {
		t.Fatalf("len=%d want 1 (skip empty pkg)", len(out))
	}
	if out[0].PlatformSysTid != "goods-sys" || len(out[0].Items) != 2 {
		t.Fatalf("row sys=%s items=%d", out[0].PlatformSysTid, len(out[0].Items))
	}
}

func TestExpandOrdersByAllocGroupsPartial(t *testing.T) {
	list := []model.Order{{
		ID:      100,
		OrderNo: "OC202611100038",
		Status:  model.StatusPendingAlloc,
		Items: []model.OrderItem{
			{ID: 1, ProductName: "插销+卡簧", Quantity: 2},
			{ID: 2, ProductName: "MT900", Quantity: 1, AllocType: model.AllocSelfShip, SelfOrderNo: "SO1"},
		},
	}}
	out := ExpandOrdersByAllocGroups(list)
	if len(out) != 2 {
		t.Fatalf("len=%d want 2", len(out))
	}
	if out[0].ListRowKey == out[1].ListRowKey {
		t.Fatalf("row keys should differ: %q", out[0].ListRowKey)
	}
	pending := FilterAllocGroupRowsForTab(out, model.StatusPendingAlloc)
	allocated := FilterAllocGroupRowsForTab(out, model.StatusAllocated)
	if len(pending) != 1 || len(pending[0].Items) != 1 || pending[0].Items[0].ID != 1 {
		t.Fatalf("pending=%+v", pending)
	}
	if len(allocated) != 1 || len(allocated[0].Items) != 1 || allocated[0].Items[0].ID != 2 {
		t.Fatalf("allocated=%+v", allocated)
	}
	if allocated[0].Status != model.StatusAllocated || allocated[0].AllocType != model.AllocSelfShip {
		t.Fatalf("allocated header status/alloc=%q/%q", allocated[0].Status, allocated[0].AllocType)
	}
	if pending[0].AllocType != "" {
		t.Fatalf("pending row should clear header alloc, got %q", pending[0].AllocType)
	}
}
