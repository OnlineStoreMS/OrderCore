package repo

import (
	"time"

	"ordercore/internal/model"
)

// DashboardCards 工作台核心指标
type DashboardCards struct {
	PendingAlloc      int64   `json:"pendingAlloc"`      // 履约待分配
	WaitShipEcommerce int64   `json:"waitShipEcommerce"` // 发货待发货（含已分配锁发货）
	Allocated         int64   `json:"allocated"`         // 已分配
	Purchasing        int64   `json:"purchasing"`        // 采购中
	Shipped           int64   `json:"shipped"`           // 已发货
	TodayOrders       int64   `json:"todayOrders"`
	TodayAmount       float64 `json:"todayAmount"`
	WeekOrders        int64   `json:"weekOrders"`
	WeekAmount        float64 `json:"weekAmount"`
	MonthOrders       int64   `json:"monthOrders"`
	MonthAmount       float64 `json:"monthAmount"`
}

// DailyTrendPoint 日趋势
type DailyTrendPoint struct {
	Date       string  `json:"date"`
	OrderCount int64   `json:"orderCount"`
	Amount     float64 `json:"amount"`
}

func (r *Repos) DashboardCards(tenantID uint64) (*DashboardCards, error) {
	out := &DashboardCards{}

	if err := r.db.Model(&model.Order{}).Where("tenant_id = ? AND status = ?", tenantID, model.StatusPendingAlloc).
		Count(&out.PendingAlloc).Error; err != nil {
		return nil, err
	}
	if err := r.db.Model(&model.Order{}).Where("tenant_id = ? AND status = ?", tenantID, model.StatusAllocated).
		Count(&out.Allocated).Error; err != nil {
		return nil, err
	}
	if err := r.db.Model(&model.Order{}).Where("tenant_id = ? AND status = ?", tenantID, model.StatusPurchasing).
		Count(&out.Purchasing).Error; err != nil {
		return nil, err
	}
	if err := r.db.Model(&model.Order{}).Where("tenant_id = ? AND ship_status = ?", tenantID, model.ShipShipped).
		Count(&out.Shipped).Error; err != nil {
		return nil, err
	}

	// 待发货：以发货状态为准（含已分配仍待发货）
	if err := r.db.Model(&model.Order{}).
		Where("tenant_id = ?", tenantID).
		Where("ship_status = ?", model.ShipWaitShip).
		Where("status NOT IN ?", []string{model.StatusClosed, model.StatusCompleted}).
		Count(&out.WaitShipEcommerce).Error; err != nil {
		return nil, err
	}

	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weekStart := dayStart.AddDate(0, 0, -6)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	type sumRow struct {
		Cnt int64
		Amt float64
	}
	sumRange := func(start time.Time) (int64, float64, error) {
		var row sumRow
		err := r.db.Model(&model.Order{}).
			Select("count(*) as cnt, COALESCE(SUM(CASE WHEN pay_amount > 0 THEN pay_amount ELSE total_amount END),0) as amt").
			Where("tenant_id = ?", tenantID).
			Where("COALESCE(ordered_at, created_at) >= ?", start).
			Where("status <> ?", model.StatusClosed).
			Scan(&row).Error
		return row.Cnt, row.Amt, err
	}
	var err error
	if out.TodayOrders, out.TodayAmount, err = sumRange(dayStart); err != nil {
		return nil, err
	}
	if out.WeekOrders, out.WeekAmount, err = sumRange(weekStart); err != nil {
		return nil, err
	}
	if out.MonthOrders, out.MonthAmount, err = sumRange(monthStart); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repos) DailyOrderTrend(tenantID uint64, days int) ([]DailyTrendPoint, error) {
	if days <= 0 {
		days = 14
	}
	if days > 90 {
		days = 90
	}
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	from := dayStart.AddDate(0, 0, -(days - 1))

	type row struct {
		Day string
		Cnt int64
		Amt float64
	}
	var rows []row
	err := r.db.Model(&model.Order{}).
		Select("to_char(date_trunc('day', COALESCE(ordered_at, created_at)), 'YYYY-MM-DD') as day, count(*) as cnt, COALESCE(SUM(CASE WHEN pay_amount > 0 THEN pay_amount ELSE total_amount END),0) as amt").
		Where("tenant_id = ?", tenantID).
		Where("COALESCE(ordered_at, created_at) >= ?", from).
		Where("status <> ?", model.StatusClosed).
		Group("day").
		Order("day ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	byDay := map[string]DailyTrendPoint{}
	for _, r0 := range rows {
		byDay[r0.Day] = DailyTrendPoint{Date: r0.Day, OrderCount: r0.Cnt, Amount: r0.Amt}
	}
	out := make([]DailyTrendPoint, 0, days)
	for i := 0; i < days; i++ {
		d := from.AddDate(0, 0, i).Format("2006-01-02")
		if p, ok := byDay[d]; ok {
			out = append(out, p)
		} else {
			out = append(out, DailyTrendPoint{Date: d})
		}
	}
	return out, nil
}
