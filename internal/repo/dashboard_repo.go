package repo

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"ordercore/internal/model"
)

// DashboardCards 工作台核心指标
type DashboardCards struct {
	PendingAlloc      int64   `json:"pendingAlloc"`
	WaitShipEcommerce int64   `json:"waitShipEcommerce"`
	Allocated         int64   `json:"allocated"`
	Purchasing        int64   `json:"purchasing"`
	Shipped           int64   `json:"shipped"`
	TodayOrders       int64   `json:"todayOrders"`
	TodayAmount       float64 `json:"todayAmount"`
	WeekOrders        int64   `json:"weekOrders"`
	WeekAmount        float64 `json:"weekAmount"`
	MonthOrders       int64   `json:"monthOrders"`
	MonthAmount       float64 `json:"monthAmount"`
	// 选中时间范围内的汇总（与趋势图同口径）
	RangeOrders    int64   `json:"rangeOrders"`
	RangeAmount    float64 `json:"rangeAmount"`
	RangeSelfAmt   float64 `json:"rangeSelfAmount"`
	RangeDropAmt   float64 `json:"rangeDropshipAmount"`
	RangeStart     string  `json:"rangeStart"`
	RangeEnd       string  `json:"rangeEnd"`
}

// DailyTrendPoint 日趋势
type DailyTrendPoint struct {
	Date           string  `json:"date"`
	OrderCount     int64   `json:"orderCount"`
	Amount         float64 `json:"amount"`
	SelfAmount     float64 `json:"selfAmount"`
	DropshipAmount float64 `json:"dropshipAmount"`
}

const sqlAmt = `CASE WHEN pay_amount > 0 THEN pay_amount ELSE total_amount END`

const sqlIsDropship = `(
	COALESCE(agent_type, 0) = 2
	OR COALESCE(alloc_type, '') = 'dropship'
	OR COALESCE(dropship_mode, '') IN ('kdzs_factory', 'osms_supplier')
)`

func scopeValidSales(tx *gorm.DB) *gorm.DB {
	return tx.
		Where("status <> ?", model.StatusClosed).
		Where(`NOT (
			UPPER(COALESCE(ecommerce_status,'')) IN (
				'ORDER_CANCELLED','ORDER_CANCEL','TRADE_CLOSED','CANCEL','CANCELLED','CLOSED',
				'REFUND_SUCCESS','REFUNDED','SUCCESS_REFUND','REFUND_MONEY_FINISH','REFUND_MONEY_SUCCESS',
				'TRADE_CLOSED_BY_TAOBAO','TRADE_CLOSED_BY_USER'
			)
			OR UPPER(COALESCE(ecommerce_status,'')) LIKE '%CANCEL%'
			OR (
				UPPER(COALESCE(ecommerce_status,'')) LIKE '%REFUND%'
				AND (
					UPPER(COALESCE(ecommerce_status,'')) LIKE '%SUCCESS%'
					OR UPPER(COALESCE(ecommerce_status,'')) LIKE '%FINISH%'
					OR UPPER(COALESCE(ecommerce_status,'')) LIKE '%DONE%'
				)
			)
			OR UPPER(COALESCE(after_sale_status,'')) IN ('REFUND_SUCCESS','REFUND_MONEY_FINISH','REFUND_MONEY_SUCCESS','REFUNDED')
			OR COALESCE(ecommerce_status_text,'') LIKE '%退款成功%'
			OR COALESCE(ecommerce_status_text,'') LIKE '%退款完成%'
			OR COALESCE(ecommerce_status_text,'') LIKE '%交易关闭%'
			OR COALESCE(ecommerce_status_text,'') LIKE '%订单取消%'
			OR COALESCE(ecommerce_status_text,'') LIKE '%已取消%'
			OR LOWER(COALESCE(platform_status,'')) IN ('order_cancelled','cancelled','trade_closed','closed','cancel')
		)`)
}

// NormalizeDashboardRange 规范化趋势时间窗（闭区间按日）；默认近 7 天，最长 90 天。
func NormalizeDashboardRange(start, end time.Time) (time.Time, time.Time, error) {
	now := time.Now()
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if start.IsZero() && end.IsZero() {
		end = today
		start = today.AddDate(0, 0, -6)
	} else {
		if start.IsZero() {
			start = end.AddDate(0, 0, -6)
		}
		if end.IsZero() {
			end = today
		}
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
		end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, loc)
	}
	if end.Before(start) {
		start, end = end, start
	}
	if end.After(today) {
		end = today
	}
	days := int(end.Sub(start).Hours()/24) + 1
	if days > 90 {
		return time.Time{}, time.Time{}, fmt.Errorf("时间范围最长 90 天")
	}
	if days < 1 {
		return time.Time{}, time.Time{}, fmt.Errorf("无效时间范围")
	}
	return start, end, nil
}

func (r *Repos) DashboardCards(tenantID uint64, rangeStart, rangeEnd time.Time) (*DashboardCards, error) {
	out := &DashboardCards{}
	out.RangeStart = rangeStart.Format("2006-01-02")
	out.RangeEnd = rangeEnd.Format("2006-01-02")

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
	rangeEndExclusive := rangeEnd.AddDate(0, 0, 1)

	type sumRow struct {
		Cnt int64
		Amt float64
	}
	sumFrom := func(start time.Time) (int64, float64, error) {
		var row sumRow
		tx := r.db.Model(&model.Order{}).
			Select("count(*) as cnt, COALESCE(SUM("+sqlAmt+"),0) as amt").
			Where("tenant_id = ?", tenantID).
			Where("COALESCE(ordered_at, created_at) >= ?", start)
		tx = scopeValidSales(tx)
		err := tx.Scan(&row).Error
		return row.Cnt, row.Amt, err
	}
	sumBetween := func(start, endEx time.Time) (int64, float64, error) {
		var row sumRow
		tx := r.db.Model(&model.Order{}).
			Select("count(*) as cnt, COALESCE(SUM("+sqlAmt+"),0) as amt").
			Where("tenant_id = ?", tenantID).
			Where("COALESCE(ordered_at, created_at) >= ? AND COALESCE(ordered_at, created_at) < ?", start, endEx)
		tx = scopeValidSales(tx)
		err := tx.Scan(&row).Error
		return row.Cnt, row.Amt, err
	}
	sumChannelBetween := func(start, endEx time.Time, dropship bool) (float64, error) {
		var amt float64
		tx := r.db.Model(&model.Order{}).
			Select("COALESCE(SUM("+sqlAmt+"),0)").
			Where("tenant_id = ?", tenantID).
			Where("COALESCE(ordered_at, created_at) >= ? AND COALESCE(ordered_at, created_at) < ?", start, endEx)
		tx = scopeValidSales(tx)
		if dropship {
			tx = tx.Where(sqlIsDropship)
		} else {
			tx = tx.Where("NOT " + sqlIsDropship)
		}
		return amt, tx.Scan(&amt).Error
	}

	var err error
	if out.TodayOrders, out.TodayAmount, err = sumFrom(dayStart); err != nil {
		return nil, err
	}
	if out.WeekOrders, out.WeekAmount, err = sumFrom(weekStart); err != nil {
		return nil, err
	}
	if out.MonthOrders, out.MonthAmount, err = sumFrom(monthStart); err != nil {
		return nil, err
	}
	if out.RangeOrders, out.RangeAmount, err = sumBetween(rangeStart, rangeEndExclusive); err != nil {
		return nil, err
	}
	if out.RangeSelfAmt, err = sumChannelBetween(rangeStart, rangeEndExclusive, false); err != nil {
		return nil, err
	}
	if out.RangeDropAmt, err = sumChannelBetween(rangeStart, rangeEndExclusive, true); err != nil {
		return nil, err
	}
	return out, nil
}

// DailyOrderTrend 按日趋势（含起止日，闭区间）
func (r *Repos) DailyOrderTrend(tenantID uint64, start, end time.Time) ([]DailyTrendPoint, error) {
	start, end, err := NormalizeDashboardRange(start, end)
	if err != nil {
		return nil, err
	}
	endExclusive := end.AddDate(0, 0, 1)
	days := int(end.Sub(start).Hours()/24) + 1

	type row struct {
		Day            string
		Cnt            int64
		Amt            float64
		SelfAmount     float64
		DropshipAmount float64
	}
	var rows []row
	tx := r.db.Model(&model.Order{}).
		Select(`to_char(date_trunc('day', COALESCE(ordered_at, created_at)), 'YYYY-MM-DD') as day,
			count(*) as cnt,
			COALESCE(SUM(`+sqlAmt+`),0) as amt,
			COALESCE(SUM(CASE WHEN NOT `+sqlIsDropship+` THEN `+sqlAmt+` ELSE 0 END),0) as self_amount,
			COALESCE(SUM(CASE WHEN `+sqlIsDropship+` THEN `+sqlAmt+` ELSE 0 END),0) as dropship_amount`).
		Where("tenant_id = ?", tenantID).
		Where("COALESCE(ordered_at, created_at) >= ? AND COALESCE(ordered_at, created_at) < ?", start, endExclusive)
	tx = scopeValidSales(tx)
	if err := tx.Group("day").Order("day ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	byDay := map[string]DailyTrendPoint{}
	for _, r0 := range rows {
		byDay[r0.Day] = DailyTrendPoint{
			Date:           r0.Day,
			OrderCount:     r0.Cnt,
			Amount:         r0.Amt,
			SelfAmount:     r0.SelfAmount,
			DropshipAmount: r0.DropshipAmount,
		}
	}
	out := make([]DailyTrendPoint, 0, days)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		if p, ok := byDay[d]; ok {
			out = append(out, p)
		} else {
			out = append(out, DailyTrendPoint{Date: d})
		}
	}
	return out, nil
}
