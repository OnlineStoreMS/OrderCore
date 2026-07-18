package storesync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: stringsTrimRightSlash(baseURL),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func stringsTrimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

type OrderQuery struct {
	Platform      string
	ShopID        string
	TradeStatus   string
	PageNo        int
	PageSize      int
	StartDateTime string
	EndDateTime   string
}

type TradeGoods struct {
	Title   string  `json:"title"`
	SkuName string  `json:"skuName"`
	PicURL  string  `json:"picUrl"`
	Num     int     `json:"num"`
	OuterID string  `json:"outerId"`
	Price   float64 `json:"price"`
}

type TradeOrder struct {
	Platform          string       `json:"platform"`
	PlatformName      string       `json:"platformName"`
	SysTids           []string     `json:"sysTids"`
	Tids              []string     `json:"tids"`
	BuyerNick         string       `json:"buyerNick"`
	ReceiverName      string       `json:"receiverName"`
	ReceiverMobile    string       `json:"receiverMobile"`
	ReceiverAddress   string       `json:"receiverAddress"`
	Payment           float64      `json:"payment"`
	TradeStatus       string       `json:"tradeStatus"`
	StatusText        string       `json:"statusText"`
	CreateTime        string       `json:"createTime"`
	PayTime           string       `json:"payTime"`
	ShopName          string       `json:"shopName"`
	ShopID            string       `json:"shopId"`
	Goods             []TradeGoods `json:"goods"`
	BuyerMemo         string       `json:"buyerMemo"`
	SellerMemo        string       `json:"sellerMemo"`
	AgentType         int          `json:"agentType"`
	FactoryID         string       `json:"factoryId"`
	FactoryName       string       `json:"factoryName"`
	FormattedReceiver string       `json:"formattedReceiver"`
}

type OrderListResult struct {
	Total    int          `json:"total"`
	PageNo   int          `json:"pageNo"`
	PageSize int          `json:"pageSize"`
	Items    []TradeOrder `json:"items"`
}

type apiBody struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) ListOrders(ctx context.Context, token string, q OrderQuery) (*OrderListResult, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("storesyncagent url not configured")
	}
	u, _ := url.Parse(c.baseURL + "/api/v1/admin/orders")
	qs := u.Query()
	if q.Platform != "" {
		qs.Set("platform", q.Platform)
	}
	if q.ShopID != "" {
		qs.Set("shopId", q.ShopID)
	}
	if q.TradeStatus != "" {
		qs.Set("tradeStatus", q.TradeStatus)
	}
	if q.PageNo > 0 {
		qs.Set("pageNo", strconv.Itoa(q.PageNo))
	}
	if q.PageSize > 0 {
		qs.Set("pageSize", strconv.Itoa(q.PageSize))
	}
	if q.StartDateTime != "" {
		qs.Set("startDateTime", q.StartDateTime)
	}
	if q.EndDateTime != "" {
		qs.Set("endDateTime", q.EndDateTime)
	}
	u.RawQuery = qs.Encode()

	var result OrderListResult
	if err := c.get(ctx, token, u.String(), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

type FactoryItem struct {
	ID          string `json:"id"`
	FactoryID   string `json:"factoryId"`
	FactoryName string `json:"factoryName"`
	FactoryNick string `json:"factoryNick"`
	Remark      string `json:"remark"`
}

type FactoryListResult struct {
	Total    int           `json:"total"`
	PageNo   int           `json:"pageNo"`
	PageSize int           `json:"pageSize"`
	Items    []FactoryItem `json:"items"`
}

func (c *Client) ListFactories(ctx context.Context, token, platform string, pageNo, pageSize int) (*FactoryListResult, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("storesyncagent url not configured")
	}
	u, _ := url.Parse(c.baseURL + "/api/v1/admin/factories")
	qs := u.Query()
	if platform != "" {
		qs.Set("platform", platform)
	}
	if pageNo > 0 {
		qs.Set("pageNo", strconv.Itoa(pageNo))
	}
	if pageSize > 0 {
		qs.Set("pageSize", strconv.Itoa(pageSize))
	}
	u.RawQuery = qs.Encode()
	var result FactoryListResult
	if err := c.get(ctx, token, u.String(), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

type SetAgentTypeRequest struct {
	Platform    string   `json:"platform"`
	TradeStatus string   `json:"tradeStatus"`
	Action      string   `json:"action"`
	FactoryID   string   `json:"factoryId"`
	SysTids     []string `json:"sysTids"`
}

func (c *Client) SetOrderAgentType(ctx context.Context, token string, req SetAgentTypeRequest) error {
	return c.post(ctx, token, c.baseURL+"/api/v1/admin/orders/agent-type", req, nil)
}

type ShipCallbackRequest struct {
	Platform       string `json:"platform"`
	ShopID         string `json:"shopId"`
	PlatformTid    string `json:"platformTid"`
	PlatformSysTid string `json:"platformSysTid"`
	ExpressCompany string `json:"expressCompany"`
	ExpressNo      string `json:"expressNo"`
	OrderNo        string `json:"orderNo"`
	Remark         string `json:"remark"`
}

type ShipCallbackResult struct {
	Accepted bool   `json:"accepted"`
	Message  string `json:"message"`
}

func (c *Client) ShipCallback(ctx context.Context, token string, req ShipCallbackRequest) (*ShipCallbackResult, error) {
	var result ShipCallbackResult
	if err := c.post(ctx, token, c.baseURL+"/api/v1/admin/orders/ship-callback", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) get(ctx context.Context, token, fullURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeAPI(resp, out)
}

func (c *Client) post(ctx context.Context, token, fullURL string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeAPI(resp, out)
}

func decodeAPI(resp *http.Response, out any) error {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("storesyncagent http %d: %s", resp.StatusCode, string(raw))
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if body.Code != 200 {
		return fmt.Errorf("storesyncagent: %s", body.Message)
	}
	if out == nil || len(body.Data) == 0 || string(body.Data) == "null" {
		return nil
	}
	return json.Unmarshal(body.Data, out)
}
