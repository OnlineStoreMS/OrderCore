package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"ordercore/internal/dto"
	"ordercore/internal/model"
	jwtmgr "ordercore/internal/pkg/jwt"
	"ordercore/internal/pkg/notify"
	"ordercore/internal/repo"
)

type SettingsService struct {
	repos  *repo.Repos
	orders *OrderService
	jwt    *jwtmgr.Manager
	notify *notify.Client
}

func NewSettingsService(repos *repo.Repos, orders *OrderService, jwt *jwtmgr.Manager) *SettingsService {
	return &SettingsService{
		repos:  repos,
		orders: orders,
		jwt:    jwt,
		notify: notify.NewClient(),
	}
}

type SyncJobParams struct {
	PageSize          int      `json:"pageSize"`
	TradeStatuses     []string `json:"tradeStatuses"`
	DateRangeDays     int      `json:"dateRangeDays"`
	RefreshOpenOrders bool     `json:"refreshOpenOrders"`
	Platform          string   `json:"platform"`
}

func (s *SettingsService) EnsureDefaultSyncJobs(tenantID uint64) error {
	defaults := []struct {
		jobType string
		name    string
		params  SyncJobParams
	}{
		{
			jobType: model.SyncJobKDZS,
			name:    "定时同步电商订单",
			params: SyncJobParams{
				PageSize:          50,
				TradeStatuses:     []string{"wait_audit", "wait_send"},
				DateRangeDays:     30,
				RefreshOpenOrders: true,
			},
		},
		{
			jobType: model.SyncJobStore,
			name:    "定时同步门店订单",
			params: SyncJobParams{
				PageSize: 50,
			},
		},
	}
	for _, d := range defaults {
		if _, err := s.repos.GetSyncJobByType(tenantID, d.jobType); err == nil {
			continue
		}
		raw, _ := json.Marshal(d.params)
		job := &model.SyncJob{
			TenantID:        tenantID,
			JobType:         d.jobType,
			Name:            d.name,
			Enabled:         false,
			IntervalMinutes: 15,
			ParamsJSON:      string(raw),
		}
		if err := s.repos.CreateSyncJob(job); err != nil {
			return err
		}
	}
	return nil
}

func (s *SettingsService) ListSyncJobs(tenantID uint64) ([]model.SyncJob, error) {
	if err := s.EnsureDefaultSyncJobs(tenantID); err != nil {
		return nil, err
	}
	return s.repos.ListSyncJobs(tenantID)
}

func (s *SettingsService) UpdateSyncJob(tenantID, id uint64, enabled *bool, intervalMinutes *int, paramsJSON *string, name *string) (*model.SyncJob, error) {
	job, err := s.repos.GetSyncJob(tenantID, id)
	if err != nil {
		return nil, err
	}
	if enabled != nil {
		job.Enabled = *enabled
	}
	if intervalMinutes != nil {
		iv := *intervalMinutes
		if iv < 5 {
			iv = 5
		}
		if iv > 1440 {
			iv = 1440
		}
		job.IntervalMinutes = iv
	}
	if paramsJSON != nil {
		job.ParamsJSON = *paramsJSON
	}
	if name != nil && strings.TrimSpace(*name) != "" {
		job.Name = strings.TrimSpace(*name)
	}
	if err := s.repos.SaveSyncJob(job); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *SettingsService) RunSyncJob(ctx context.Context, tenantID, id uint64, bearerToken string) (map[string]int, error) {
	job, err := s.repos.GetSyncJob(tenantID, id)
	if err != nil {
		return nil, err
	}
	token := strings.TrimPrefix(strings.TrimSpace(bearerToken), "Bearer ")
	if token == "" && s.jwt != nil {
		token, err = s.jwt.IssueServiceToken(tenantID, 30*time.Minute)
		if err != nil {
			return nil, fmt.Errorf("签发同步令牌失败: %w", err)
		}
	}

	stats, runErr := s.executeSyncJob(ctx, job, token)
	now := time.Now()
	ok := runErr == nil
	errMsg := ""
	if runErr != nil {
		errMsg = runErr.Error()
	}
	raw, _ := json.Marshal(stats)
	_ = s.repos.UpdateSyncJobRun(job.ID, ok, errMsg, string(raw), now)
	if runErr != nil {
		return stats, runErr
	}
	return stats, nil
}

func (s *SettingsService) executeSyncJob(ctx context.Context, job *model.SyncJob, token string) (map[string]int, error) {
	var params SyncJobParams
	_ = json.Unmarshal([]byte(job.ParamsJSON), &params)
	if params.PageSize <= 0 {
		params.PageSize = 50
	}
	if len(params.TradeStatuses) == 0 {
		params.TradeStatuses = []string{"wait_audit", "wait_send"}
	}
	if params.DateRangeDays <= 0 {
		params.DateRangeDays = 30
	}

	end := time.Now()
	start := end.AddDate(0, 0, -params.DateRangeDays+1)
	startStr := start.Format("2006-01-02") + " 00:00:00"
	endStr := end.Format("2006-01-02") + " 23:59:59"

	switch job.JobType {
	case model.SyncJobKDZS:
		stats, err := s.orders.SyncFromKDZS(ctx, job.TenantID, 0, dto.SyncKDZSRequest{
			Platform:      params.Platform,
			TradeStatuses: params.TradeStatuses,
			PageSize:      params.PageSize,
			StartTime:     startStr,
			EndTime:       endStr,
		}, token)
		if err != nil {
			return stats, err
		}
		if params.RefreshOpenOrders {
			refreshed, rerr := s.orders.RefreshOpenKDZSOrders(ctx, job.TenantID, 0, token, 80)
			if stats == nil {
				stats = map[string]int{}
			}
			stats["refreshed"] = refreshed
			// 列表同步已成功时，刷新限流/部分失败不整单失败（避免误报且状态已写入）
			if rerr != nil {
				log.Printf("[sync-job] refresh open orders partial error (list sync ok): %v", rerr)
				stats["refreshErrors"] = 1
			}
		}
		return stats, nil
	case model.SyncJobStore:
		return s.orders.SyncFromStore(ctx, job.TenantID, 0, dto.SyncStoreRequest{
			Page: 1,
			Size: params.PageSize,
		}, token)
	default:
		return nil, fmt.Errorf("未知任务类型: %s", job.JobType)
	}
}

func (s *SettingsService) RunDueSyncJobs(ctx context.Context) {
	jobs, err := s.repos.ListEnabledSyncJobs()
	if err != nil {
		return
	}
	now := time.Now()
	for i := range jobs {
		job := &jobs[i]
		iv := time.Duration(job.IntervalMinutes) * time.Minute
		if iv < 5*time.Minute {
			iv = 5 * time.Minute
		}
		if job.LastRunAt != nil && now.Sub(*job.LastRunAt) < iv {
			continue
		}
		_, _ = s.RunSyncJob(ctx, job.TenantID, job.ID, "")
	}
}

// ---- channels / push rules ----

func (s *SettingsService) ListChannels(tenantID uint64) ([]model.NotificationChannel, error) {
	return s.repos.ListChannels(tenantID)
}

func (s *SettingsService) CreateChannel(tenantID uint64, ch *model.NotificationChannel) (*model.NotificationChannel, error) {
	ch.TenantID = tenantID
	ch.ChannelType = strings.TrimSpace(ch.ChannelType)
	if ch.ChannelType != model.ChannelFeishuWebhook && ch.ChannelType != model.ChannelWecomWebhook {
		return nil, fmt.Errorf("不支持的推送方式，目前支持飞书机器人/企微机器人")
	}
	if strings.TrimSpace(ch.Name) == "" || strings.TrimSpace(ch.WebhookURL) == "" {
		return nil, fmt.Errorf("名称与 Webhook 地址必填")
	}
	if err := s.repos.CreateChannel(ch); err != nil {
		return nil, err
	}
	return ch, nil
}

func (s *SettingsService) UpdateChannel(tenantID, id uint64, patch *model.NotificationChannel) (*model.NotificationChannel, error) {
	ch, err := s.repos.GetChannel(tenantID, id)
	if err != nil {
		return nil, err
	}
	if patch.Name != "" {
		ch.Name = patch.Name
	}
	if patch.WebhookURL != "" {
		ch.WebhookURL = patch.WebhookURL
	}
	if patch.Secret != "" {
		ch.Secret = patch.Secret
	}
	if patch.ChannelType != "" {
		ch.ChannelType = patch.ChannelType
	}
	ch.Enabled = patch.Enabled
	ch.Remark = patch.Remark
	if err := s.repos.SaveChannel(ch); err != nil {
		return nil, err
	}
	return ch, nil
}

func (s *SettingsService) DeleteChannel(tenantID, id uint64) error {
	return s.repos.DeleteChannel(tenantID, id)
}

func (s *SettingsService) TestChannel(ctx context.Context, tenantID, id uint64) error {
	ch, err := s.repos.GetChannel(tenantID, id)
	if err != nil {
		return err
	}
	text := "【订单中心】推送渠道测试消息 " + time.Now().Format("2006-01-02 15:04:05")
	return s.sendChannel(ctx, ch, text)
}

func (s *SettingsService) ListPushRules(tenantID uint64) ([]model.PushRule, error) {
	return s.repos.ListPushRules(tenantID)
}

func (s *SettingsService) CreatePushRule(tenantID uint64, rule *model.PushRule) (*model.PushRule, error) {
	rule.TenantID = tenantID
	if rule.Event == "" {
		rule.Event = model.PushEventAllocated
	}
	if rule.ChannelID == 0 {
		return nil, fmt.Errorf("请选择推送渠道")
	}
	if _, err := s.repos.GetChannel(tenantID, rule.ChannelID); err != nil {
		return nil, fmt.Errorf("推送渠道不存在")
	}
	if err := s.repos.CreatePushRule(rule); err != nil {
		return nil, err
	}
	return rule, nil
}

func (s *SettingsService) UpdatePushRule(tenantID, id uint64, patch *model.PushRule) (*model.PushRule, error) {
	rule, err := s.repos.GetPushRule(tenantID, id)
	if err != nil {
		return nil, err
	}
	if patch.ChannelID > 0 {
		rule.ChannelID = patch.ChannelID
	}
	if patch.Event != "" {
		rule.Event = patch.Event
	}
	rule.SupplierID = patch.SupplierID
	rule.Enabled = patch.Enabled
	rule.Remark = patch.Remark
	if err := s.repos.SavePushRule(rule); err != nil {
		return nil, err
	}
	return rule, nil
}

func (s *SettingsService) DeletePushRule(tenantID, id uint64) error {
	return s.repos.DeletePushRule(tenantID, id)
}

func (s *SettingsService) PushOrder(ctx context.Context, tenantID, orderID uint64, event string) error {
	o, err := s.repos.GetOrder(tenantID, orderID)
	if err != nil {
		return err
	}
	if event == "" {
		event = model.PushEventManual
	}
	return s.pushOrderEvent(ctx, o, event)
}

func (s *SettingsService) PushAllocatedAsync(tenantID, orderID uint64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		o, err := s.repos.GetOrder(tenantID, orderID)
		if err != nil {
			return
		}
		_ = s.pushOrderEvent(ctx, o, model.PushEventAllocated)
	}()
}

func (s *SettingsService) pushOrderEvent(ctx context.Context, o *model.Order, event string) error {
	if o.SupplierID == 0 && event == model.PushEventAllocated {
		// 自营/无供应商可不推；仍允许手动推时选默认规则 supplier_id=0
	}
	rules, err := s.repos.FindPushRules(o.TenantID, o.SupplierID, event)
	if err != nil {
		return err
	}
	if len(rules) == 0 && event == model.PushEventManual {
		rules, err = s.repos.FindPushRules(o.TenantID, o.SupplierID, model.PushEventAllocated)
		if err != nil {
			return err
		}
	}
	if len(rules) == 0 {
		return fmt.Errorf("未配置推送规则（供应商或默认规则）")
	}

	text := buildOrderPushText(o, event)
	var lastErr error
	sent := 0
	for _, rule := range rules {
		ch, err := s.repos.GetChannel(o.TenantID, rule.ChannelID)
		if err != nil || !ch.Enabled {
			continue
		}
		now := time.Now()
		log := &model.PushLog{
			TenantID:     o.TenantID,
			OrderID:      o.ID,
			SupplierID:   o.SupplierID,
			ChannelID:    ch.ID,
			Event:        event,
			ChannelType:  ch.ChannelType,
			PayloadBrief: truncate(text, 200),
			SentAt:       &now,
		}
		if err := s.sendChannel(ctx, ch, text); err != nil {
			log.Status = "failed"
			log.ErrorMessage = err.Error()
			lastErr = err
		} else {
			log.Status = "succeeded"
			sent++
		}
		_ = s.repos.CreatePushLog(log)
	}
	if sent == 0 {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("没有可用的推送渠道")
	}
	return nil
}

func (s *SettingsService) sendChannel(ctx context.Context, ch *model.NotificationChannel, text string) error {
	switch ch.ChannelType {
	case model.ChannelFeishuWebhook:
		return s.notify.SendFeishuText(ctx, ch.WebhookURL, ch.Secret, text)
	case model.ChannelWecomWebhook:
		return s.notify.SendWecomText(ctx, ch.WebhookURL, text)
	default:
		return fmt.Errorf("不支持的渠道类型: %s", ch.ChannelType)
	}
}

func (s *SettingsService) ListPushLogs(tenantID, orderID uint64) ([]model.PushLog, error) {
	return s.repos.ListPushLogs(tenantID, orderID, 50)
}

func buildOrderPushText(o *model.Order, event string) string {
	title := "订单推送"
	if event == model.PushEventAllocated {
		title = "订单已分配"
	}
	var b strings.Builder
	b.WriteString("【订单中心】" + title + "\n")
	b.WriteString("内部单号：" + o.OrderNo + "\n")
	if o.PlatformOrderID != "" {
		b.WriteString("平台单号：" + o.PlatformOrderID + "\n")
	}
	if o.Platform != "" {
		b.WriteString("平台：" + o.Platform + "\n")
	}
	b.WriteString(fmt.Sprintf("买家：%s %s\n", coalesce(o.BuyerNick, o.BuyerName), o.BuyerPhone))
	if o.Address != nil {
		addr := o.Address.FullText
		if addr == "" {
			addr = o.Address.Address
		}
		b.WriteString("收件：" + addr + "\n")
	}
	if len(o.Items) > 0 {
		b.WriteString("商品：")
		parts := make([]string, 0, len(o.Items))
		for _, it := range o.Items {
			parts = append(parts, fmt.Sprintf("%s×%d", coalesce(it.ProductName, it.SkuCode), it.Quantity))
		}
		b.WriteString(strings.Join(parts, "；") + "\n")
	}
	b.WriteString(fmt.Sprintf("金额：%.2f\n", o.PayAmount))
	if o.AllocType != "" {
		b.WriteString("分配：" + o.AllocType)
		if o.DropshipMode != "" {
			b.WriteString("/" + o.DropshipMode)
		}
		b.WriteString("\n")
	}
	if o.SupplierName != "" {
		b.WriteString("供应商：" + o.SupplierName + "\n")
	}
	if o.Remark != "" {
		b.WriteString("留言：" + o.Remark + "\n")
	}
	if o.SellerRemark != "" {
		b.WriteString("卖家备注：" + o.SellerRemark + "\n")
	}
	return b.String()
}

func coalesce(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
