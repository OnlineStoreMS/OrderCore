package selfcore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != ""
}

type SelfOrderItemInput struct {
	PimSkuID      uint64  `json:"pimSkuId,omitempty"`
	SkuCode       string  `json:"skuCode,omitempty"`
	ProductName   string  `json:"productName,omitempty"`
	SkuSpecs      string  `json:"skuSpecs,omitempty"`
	PicURL        string  `json:"picUrl,omitempty"`
	Qty           int     `json:"qty"`
	SaleUnitPrice float64 `json:"saleUnitPrice,omitempty"`
	SaleAmount    float64 `json:"saleAmount,omitempty"`
	RefSoID       uint64  `json:"refSoId,omitempty"`
	RefOrderItemID uint64 `json:"refOrderItemId,omitempty"`
	RefOrderNo    string  `json:"refOrderNo,omitempty"`
	Remark        string  `json:"remark,omitempty"`
}

type SelfOrderInput struct {
	WarehouseID   uint64               `json:"warehouseId,omitempty"`
	RefSoID       uint64               `json:"refSoId,omitempty"`
	RefTraceID    string               `json:"refTraceId,omitempty"`
	SaleAmount    float64              `json:"saleAmount,omitempty"`
	BuyerName     string               `json:"buyerName,omitempty"`
	BuyerPhone    string               `json:"buyerPhone,omitempty"`
	Address       string               `json:"address,omitempty"`
	Remark        string               `json:"remark,omitempty"`
	SourceChannel string               `json:"sourceChannel,omitempty"`
	Platform      string               `json:"platform,omitempty"`
	ShopName      string               `json:"shopName,omitempty"`
	ManualSourceName string            `json:"manualSourceName,omitempty"`
	BuyerRemark   string               `json:"buyerRemark,omitempty"`
	SellerRemark  string               `json:"sellerRemark,omitempty"`
	FenFaRemark   string               `json:"fenFaRemark,omitempty"`
	PrinterRemark string               `json:"printerRemark,omitempty"`
	OrderedAt     string               `json:"orderedAt,omitempty"`
	// CreatedAt 创建自营单时间；分配建单时传 allocatedAt
	CreatedAt string `json:"createdAt,omitempty"`
	PayStatus     string               `json:"payStatus,omitempty"`
	PaidAt        string               `json:"paidAt,omitempty"`
	Items         []SelfOrderItemInput `json:"items"`
}

type SelfOrderDetail struct {
	ID     uint64 `json:"id"`
	SoNo   string `json:"soNo"`
	Status string `json:"status"`
	RefSoID uint64 `json:"refSoId"`
}

type listPayload struct {
	List []SelfOrderDetail `json:"list"`
}

type apiBody struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) ListByRefSoID(ctx context.Context, bearerToken string, refSoID uint64) ([]SelfOrderDetail, error) {
	if !c.Enabled() || refSoID == 0 {
		return nil, nil
	}
	q := url.Values{}
	q.Set("refSoId", strconv.FormatUint(refSoID, 10))
	q.Set("page", "1")
	q.Set("pageSize", "20")
	var page listPayload
	if err := c.doJSON(ctx, http.MethodGet, bearerToken, "/api/v1/admin/self-orders?"+q.Encode(), nil, &page); err != nil {
		return nil, err
	}
	if page.List == nil {
		return []SelfOrderDetail{}, nil
	}
	return page.List, nil
}

func (c *Client) CreateSelfOrder(ctx context.Context, bearerToken string, in SelfOrderInput) (*SelfOrderDetail, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("SelfCore 未配置")
	}
	var out SelfOrderDetail
	if err := c.doJSON(ctx, http.MethodPost, bearerToken, "/api/v1/admin/self-orders", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelByRefSoID 按销售单取消关联自营单（撤回分配）。
func (c *Client) CancelByRefSoID(ctx context.Context, bearerToken string, refSoID uint64, reason string) ([]SelfOrderDetail, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("SelfCore 未配置")
	}
	if refSoID == 0 {
		return nil, nil
	}
	body := map[string]any{
		"refSoId": refSoID,
		"reason":  reason,
	}
	var out []SelfOrderDetail
	if err := c.doJSON(ctx, http.MethodPost, bearerToken, "/api/v1/admin/self-orders/cancel-by-ref-so", body, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return []SelfOrderDetail{}, nil
	}
	return out, nil
}

// DeleteByRefSoID 按销售单硬删除关联自营单（手工单删除级联）。
func (c *Client) DeleteByRefSoID(ctx context.Context, bearerToken string, refSoID uint64) (int, error) {
	if !c.Enabled() {
		return 0, fmt.Errorf("SelfCore 未配置")
	}
	if refSoID == 0 {
		return 0, nil
	}
	body := map[string]any{"refSoId": refSoID}
	var out struct {
		Deleted int `json:"deleted"`
	}
	if err := c.doJSON(ctx, http.MethodPost, bearerToken, "/api/v1/admin/self-orders/delete-by-ref-so", body, &out); err != nil {
		return 0, err
	}
	return out.Deleted, nil
}

// SyncShipmentsByRefSoID 订单发货后自动把物流同步到自营单（best-effort）。
func (c *Client) SyncShipmentsByRefSoID(ctx context.Context, bearerToken string, refSoID uint64) error {
	if !c.Enabled() || refSoID == 0 {
		return nil
	}
	body := map[string]any{"refSoId": refSoID}
	return c.doJSON(ctx, http.MethodPost, bearerToken, "/api/v1/admin/self-orders/sync-shipments-by-ref-so", body, nil)
}

// SyncSplitItemsByRefSo 拆分计划保存后同步到关联自营单（best-effort）。
func (c *Client) SyncSplitItemsByRefSo(ctx context.Context, bearerToken string, refSoID uint64, mode string, lines []map[string]any) error {
	if !c.Enabled() || refSoID == 0 {
		return nil
	}
	body := map[string]any{
		"refSoId": refSoID,
		"mode":    mode,
		"lines":   lines,
	}
	return c.doJSON(ctx, http.MethodPost, bearerToken, "/api/v1/admin/self-orders/sync-split-items-by-ref-so", body, nil)
}

// RemoveShipmentsByTracking 取消快递单后按运单号清除自营物流（best-effort）。
func (c *Client) RemoveShipmentsByTracking(ctx context.Context, bearerToken string, refSoID uint64, trackingNo string) error {
	if !c.Enabled() || refSoID == 0 || strings.TrimSpace(trackingNo) == "" {
		return nil
	}
	body := map[string]any{
		"refSoId":    refSoID,
		"trackingNo": strings.TrimSpace(trackingNo),
	}
	return c.doJSON(ctx, http.MethodPost, bearerToken, "/api/v1/admin/self-orders/remove-shipments-by-tracking", body, nil)
}

func (c *Client) doJSON(ctx context.Context, method, bearerToken, path string, body any, out any) error {
	reqURL := c.baseURL + path
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return err
	}
	if bearerToken != "" {
		if !strings.HasPrefix(bearerToken, "Bearer ") {
			bearerToken = "Bearer " + bearerToken
		}
		req.Header.Set("Authorization", bearerToken)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("selfcore request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("selfcore http %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var wrapped apiBody
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return fmt.Errorf("selfcore decode: %w", err)
	}
	if wrapped.Code != 200 && wrapped.Code != 201 {
		msg := wrapped.Message
		if msg == "" {
			msg = "selfcore error"
		}
		return fmt.Errorf("%s", msg)
	}
	if out == nil || len(wrapped.Data) == 0 || string(wrapped.Data) == "null" {
		return nil
	}
	return json.Unmarshal(wrapped.Data, out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
