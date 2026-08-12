package shippingcore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
			Timeout: 20 * time.Second,
		},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != ""
}

type KdzsAccountDetail struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Mobile    string `json:"mobile"`
	Enabled   bool   `json:"enabled"`
	IsDefault bool   `json:"isDefault"`
	Active    bool   `json:"active"`
}

// DefaultKdzsAccount 发货中心「默认」快递助手账号（与 SSA 默认可不一致）。
func (c *Client) DefaultKdzsAccount(ctx context.Context, token string) (*KdzsAccountDetail, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("shippingcore 未配置")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/admin/kdzs/account-details", nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(token, "Bearer "))
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("shippingcore http %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var wrap struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Items []KdzsAccountDetail `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, fmt.Errorf("decode shippingcore: %w", err)
	}
	if wrap.Code != 0 && wrap.Code != 200 {
		return nil, fmt.Errorf("shippingcore: %s", firstNonEmpty(wrap.Message, "请求失败"))
	}
	var firstEnabled *KdzsAccountDetail
	for i := range wrap.Data.Items {
		it := &wrap.Data.Items[i]
		if !it.Enabled {
			continue
		}
		if firstEnabled == nil {
			firstEnabled = it
		}
		if it.IsDefault {
			return it, nil
		}
	}
	if firstEnabled != nil {
		return firstEnabled, nil
	}
	return nil, fmt.Errorf("发货中心未配置可用的默认快递助手账号，请先在发货中心「快递助手账号」中设置默认")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// DeleteShipmentsByOrderCore 按订单中心销售单删除发货运单（手工单删除级联）。
func (c *Client) DeleteShipmentsByOrderCore(ctx context.Context, token string, orderCoreOrderID uint64, sourceRef string) (int, error) {
	if !c.Enabled() {
		return 0, fmt.Errorf("shippingcore 未配置")
	}
	body := map[string]any{
		"orderCoreOrderId": orderCoreOrderID,
		"sourceRef":        strings.TrimSpace(sourceRef),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/admin/shipments/delete-by-ordercore", bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(token, "Bearer "))
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("shippingcore http %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var wrap struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Deleted int `json:"deleted"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return 0, fmt.Errorf("decode shippingcore: %w", err)
	}
	if wrap.Code != 0 && wrap.Code != 200 {
		return 0, fmt.Errorf("shippingcore: %s", firstNonEmpty(wrap.Message, "请求失败"))
	}
	return wrap.Data.Deleted, nil
}
