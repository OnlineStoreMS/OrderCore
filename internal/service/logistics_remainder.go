package service

import (
	"strings"

	"ordercore/internal/dto"
	"ordercore/internal/model"
)

const kdzsSyncCallbackMsg = "快递助手同步"

// isFactoryDropshipItem 厂家代发行（助手推厂家发货，运单通常由助手同步）。
func isFactoryDropshipItem(it model.OrderItem) bool {
	if strings.TrimSpace(it.DropshipMode) == model.DropshipKDZSFactory {
		return true
	}
	// 历史：仅头表/行 alloc=dropship + 有 factory，无 mode 时也当厂家
	if strings.TrimSpace(it.AllocType) == model.AllocDropship &&
		strings.TrimSpace(it.FactoryID) != "" &&
		strings.TrimSpace(it.DropshipMode) != model.DropshipOSMSSupplier {
		return true
	}
	return false
}

// isAuthoritativeLocalShipment 本地权威运单：仅手动回传/非助手同步写入。
// 注意：不能把「助手同步但已错挂到供应商行」当成权威，否则错挂会自我巩固、剩下法失效。
func isAuthoritativeLocalShipment(sh model.OrderShipment, order *model.Order) bool {
	if strings.TrimSpace(sh.ExpressNo) == "" {
		return false
	}
	msg := strings.TrimSpace(sh.CallbackMessage)
	if sh.CallbackStatus == model.CallbackSucceeded {
		return true
	}
	if msg != "" && msg != kdzsSyncCallbackMsg {
		return true
	}
	return false
}

// claimedExpressNosByLocal 已被供应商/自营/手动占坑的运单号。
func claimedExpressNosByLocal(order *model.Order) map[string]struct{} {
	out := map[string]struct{}{}
	if order == nil {
		return out
	}
	for _, sh := range order.Shipments {
		no := strings.TrimSpace(sh.ExpressNo)
		if no == "" || !isAuthoritativeLocalShipment(sh, order) {
			continue
		}
		out[no] = struct{}{}
	}
	return out
}

// localAuthoritativeExpressForItems 本包商品已挂的权威运单号（若有多个取先出现的）。
func localAuthoritativeExpressForItems(order *model.Order, itemIDs map[uint64]struct{}) string {
	if order == nil || len(itemIDs) == 0 {
		return ""
	}
	for _, sh := range order.Shipments {
		no := strings.TrimSpace(sh.ExpressNo)
		if no == "" || !isAuthoritativeLocalShipment(sh, order) {
			continue
		}
		for _, sit := range sh.Items {
			if _, ok := itemIDs[sit.OrderItemID]; ok {
				return no
			}
		}
	}
	return ""
}

// ingestPackageItemIDs 本轮 ingest 对应的销售根行（按 sysTid 包裹 / 请求 oid）。
func ingestPackageItemIDs(order *model.Order, req dto.IngestOrderRequest) map[uint64]struct{} {
	out := map[uint64]struct{}{}
	if order == nil {
		return out
	}
	sysTid := strings.TrimSpace(req.PlatformSysTid)
	wantOids := map[string]struct{}{}
	for _, it := range req.Items {
		if oid := strings.TrimSpace(it.PlatformOid); oid != "" {
			wantOids[oid] = struct{}{}
		}
	}
	var pkgID uint64
	if sysTid != "" {
		for _, p := range order.Packages {
			if strings.TrimSpace(p.PlatformSysTid) == sysTid {
				pkgID = p.ID
				break
			}
		}
	}
	for _, it := range order.Items {
		if it.ParentOrderItemID > 0 || strings.TrimSpace(it.SplitKind) != "" {
			continue
		}
		if orderItemExcludedFromFulfillment(it) {
			continue
		}
		matched := false
		if pkgID > 0 && it.PackageID == pkgID {
			matched = true
		}
		if !matched && len(wantOids) > 0 {
			if _, ok := wantOids[strings.TrimSpace(it.PlatformOid)]; ok {
				matched = true
			}
		}
		if matched {
			out[it.ID] = struct{}{}
		}
	}
	// 尚无包裹绑定时：用请求商品反查本地行
	if len(out) == 0 {
		ids := filterFulfillableItemIDs(order, matchIngestItemsToOrder(order.Items, req.Items))
		for _, id := range ids {
			out[id] = struct{}{}
		}
	}
	return out
}

func packageItemsAreFactoryDropship(order *model.Order, itemIDs map[uint64]struct{}, reqAgentType int) bool {
	if reqAgentType == model.AgentTypeFactory {
		return true
	}
	if order == nil || len(itemIDs) == 0 {
		return false
	}
	factory, nonFactory := 0, 0
	for _, it := range order.Items {
		if _, ok := itemIDs[it.ID]; !ok {
			continue
		}
		if isFactoryDropshipItem(it) {
			factory++
		} else if strings.TrimSpace(it.AllocType) != "" {
			nonFactory++
		}
	}
	if factory > 0 && nonFactory == 0 {
		return true
	}
	return false
}

func collectIngestLogisticsCandidates(req dto.IngestOrderRequest) []dto.LogisticsInput {
	seen := map[string]struct{}{}
	out := make([]dto.LogisticsInput, 0, len(req.Logistics)+1)
	add := func(lg dto.LogisticsInput) {
		no := strings.TrimSpace(lg.ExpressNo)
		if no == "" {
			return
		}
		if _, ok := seen[no]; ok {
			return
		}
		seen[no] = struct{}{}
		if strings.TrimSpace(lg.ShippedAt) == "" {
			lg.ShippedAt = req.ShippedAt
		}
		if strings.TrimSpace(lg.ExpressCompany) == "" {
			lg.ExpressCompany = firstNonEmpty(req.ExpressCompany, req.ExpressCode)
		}
		if strings.TrimSpace(lg.ExpressCode) == "" {
			lg.ExpressCode = req.ExpressCode
		}
		lg.ExpressNo = no
		out = append(out, lg)
	}
	for _, lg := range req.Logistics {
		add(lg)
	}
	if primary := strings.TrimSpace(req.ExpressNo); primary != "" {
		add(dto.LogisticsInput{
			ExpressCompany: firstNonEmpty(req.ExpressCompany, req.ExpressCode),
			ExpressCode:    req.ExpressCode,
			ExpressNo:      primary,
			ShippedAt:      req.ShippedAt,
		})
	}
	return out
}

func filterLogisticsExcluding(list []dto.LogisticsInput, exclude map[string]struct{}) []dto.LogisticsInput {
	if len(list) == 0 {
		return nil
	}
	if len(exclude) == 0 {
		return list
	}
	out := make([]dto.LogisticsInput, 0, len(list))
	for _, lg := range list {
		no := strings.TrimSpace(lg.ExpressNo)
		if _, ok := exclude[no]; ok {
			continue
		}
		out = append(out, lg)
	}
	return out
}

func pickLogisticsFromRemainder(remainder []dto.LogisticsInput, req dto.IngestOrderRequest) dto.LogisticsInput {
	if len(remainder) == 0 {
		return dto.LogisticsInput{}
	}
	if primary := pickIngestPrimaryLogistics(remainder, req.ShippedAt); strings.TrimSpace(primary.ExpressNo) != "" {
		// shippedAt 命中或最新；若头表单号仍在剩余中且 shippedAt 未精确命中，优先头表
		head := strings.TrimSpace(req.ExpressNo)
		if head != "" {
			shippedAt := strings.TrimSpace(req.ShippedAt)
			exactHit := false
			if shippedAt != "" {
				for _, lg := range remainder {
					st := strings.TrimSpace(lg.ShippedAt)
					if st == "" {
						continue
					}
					if st == shippedAt || strings.HasPrefix(shippedAt, st) || strings.HasPrefix(st, shippedAt) ||
						(len(st) >= 16 && len(shippedAt) >= 16 && st[:16] == shippedAt[:16]) {
						exactHit = true
						break
					}
				}
			}
			if !exactHit {
				for _, lg := range remainder {
					if strings.TrimSpace(lg.ExpressNo) == head {
						return lg
					}
				}
			}
		}
		return primary
	}
	if len(remainder) == 1 {
		return remainder[0]
	}
	return dto.LogisticsInput{}
}

// resolvePackageLogistics 剩下法：本地权威单号占坑后，本包只落剩余运单；已有本地权威则保留不覆盖。
func resolvePackageLogistics(order *model.Order, req dto.IngestOrderRequest) []dto.LogisticsInput {
	itemIDs := ingestPackageItemIDs(order, req)
	if local := localAuthoritativeExpressForItems(order, itemIDs); local != "" {
		// 本包商品已有手动/供应商运单：保持，不让助手头表 YT 冲掉
		for _, sh := range order.Shipments {
			if strings.TrimSpace(sh.ExpressNo) != local {
				continue
			}
			return []dto.LogisticsInput{{
				ExpressCompany: sh.ExpressCompany,
				ExpressNo:      local,
				ShippedAt:      formatShipmentTime(sh),
			}}
		}
		return []dto.LogisticsInput{{ExpressNo: local, ShippedAt: req.ShippedAt}}
	}

	candidates := collectIngestLogisticsCandidates(req)
	if len(candidates) == 0 {
		return nil
	}

	claimed := claimedExpressNosByLocal(order)
	// 本包自己的权威号不应再当「别人占用」；上面已 return。此处 claimed 全是其它行占用。
	remainder := filterLogisticsExcluding(candidates, claimed)
	factoryPkg := packageItemsAreFactoryDropship(order, itemIDs, req.AgentType)

	if !factoryPkg && req.AgentType != model.AgentTypeFactory {
		// 供应商/自营包：无本地权威时，不要抢已被占用后只剩的厂家单号去错挂
		if len(claimed) > 0 && len(remainder) == 0 {
			return nil
		}
		// 剩余仅 1 个且头表却是另一个已被占用的号 → 不写（典型：头表 YT 已属厂家，本包应是韵达但韵达已本地占用）
		head := strings.TrimSpace(req.ExpressNo)
		if head != "" {
			if _, taken := claimed[head]; taken {
				return nil
			}
		}
		if len(remainder) == 0 {
			// 无占用时退回旧逻辑：只信头表/唯一物流
			return logisticsForThisPackageLegacy(req)
		}
		picked := pickLogisticsFromRemainder(remainder, req)
		if strings.TrimSpace(picked.ExpressNo) == "" {
			return nil
		}
		return []dto.LogisticsInput{picked}
	}

	// 厂家代发包：走剩余
	if len(remainder) == 0 {
		// 无本地占用时允许旧逻辑
		if len(claimed) == 0 {
			return logisticsForThisPackageLegacy(req)
		}
		return nil
	}
	picked := pickLogisticsFromRemainder(remainder, req)
	if strings.TrimSpace(picked.ExpressNo) == "" {
		return nil
	}
	return []dto.LogisticsInput{picked}
}

func formatShipmentTime(sh model.OrderShipment) string {
	if sh.ShippedAt == nil {
		return ""
	}
	return sh.ShippedAt.Format("2006-01-02 15:04:05")
}

// logisticsForThisPackageLegacy 旧行为：只落头表 ExpressNo（或唯一物流）。
func logisticsForThisPackageLegacy(req dto.IngestOrderRequest) []dto.LogisticsInput {
	primary := strings.TrimSpace(req.ExpressNo)
	if primary != "" {
		for _, p := range req.Logistics {
			if strings.TrimSpace(p.ExpressNo) == primary {
				out := p
				if strings.TrimSpace(out.ShippedAt) == "" {
					out.ShippedAt = req.ShippedAt
				}
				if strings.TrimSpace(out.ExpressCompany) == "" {
					out.ExpressCompany = firstNonEmpty(req.ExpressCompany, req.ExpressCode)
				}
				return []dto.LogisticsInput{out}
			}
		}
		return []dto.LogisticsInput{{
			ExpressCompany: firstNonEmpty(req.ExpressCompany, req.ExpressCode),
			ExpressCode:    req.ExpressCode,
			ExpressNo:      primary,
			ShippedAt:      req.ShippedAt,
		}}
	}
	if len(req.Logistics) == 1 {
		return req.Logistics
	}
	if len(req.Logistics) > 1 {
		if primary := pickIngestPrimaryLogistics(req.Logistics, req.ShippedAt); strings.TrimSpace(primary.ExpressNo) != "" {
			return []dto.LogisticsInput{primary}
		}
	}
	return nil
}

// logisticsForThisPackage 兼容旧调用：无订单上下文时走 legacy。
func logisticsForThisPackage(req dto.IngestOrderRequest) []dto.LogisticsInput {
	return logisticsForThisPackageLegacy(req)
}

// expressOwnedByOtherAuthoritative 该运单已被其它（非 candidate）权威商品占用。
func expressOwnedByOtherAuthoritative(order *model.Order, expressNo string, candidateIDs map[uint64]struct{}) bool {
	expressNo = strings.TrimSpace(expressNo)
	if order == nil || expressNo == "" {
		return false
	}
	for _, sh := range order.Shipments {
		if strings.TrimSpace(sh.ExpressNo) != expressNo {
			continue
		}
		if !isAuthoritativeLocalShipment(sh, order) {
			continue
		}
		for _, sit := range sh.Items {
			if sit.OrderItemID == 0 {
				continue
			}
			if _, ok := candidateIDs[sit.OrderItemID]; ok {
				continue
			}
			return true
		}
	}
	return false
}

// filterItemsCompatibleWithExpress 厂家运单只链厂家行；权威供应商运单只链非厂家行。
func filterItemsCompatibleWithExpress(order *model.Order, expressNo string, ids []uint64) []uint64 {
	if order == nil || len(ids) == 0 {
		return ids
	}
	expressNo = strings.TrimSpace(expressNo)
	byID := map[uint64]model.OrderItem{}
	for _, it := range order.Items {
		byID[it.ID] = it
	}
	// 若运单权威挂在非厂家行上，只允许非厂家
	authoritativeNonFactory := false
	authoritativeFactory := false
	for _, sh := range order.Shipments {
		if strings.TrimSpace(sh.ExpressNo) != expressNo {
			continue
		}
		if !isAuthoritativeLocalShipment(sh, order) {
			continue
		}
		for _, sit := range sh.Items {
			it, ok := byID[sit.OrderItemID]
			if !ok {
				continue
			}
			if isFactoryDropshipItem(it) {
				authoritativeFactory = true
			} else {
				authoritativeNonFactory = true
			}
		}
	}
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		it, ok := byID[id]
		if !ok {
			continue
		}
		factory := isFactoryDropshipItem(it)
		if authoritativeNonFactory && !authoritativeFactory && factory {
			continue
		}
		if authoritativeFactory && !authoritativeNonFactory && !factory && strings.TrimSpace(it.AllocType) != "" {
			continue
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		return ids
	}
	return out
}
