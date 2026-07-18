package storecore

import (
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
		baseURL: trimSlash(baseURL),
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

type SalesOrderItem struct {
	SkuID       uint64  `json:"skuId"`
	SkuCode     string  `json:"skuCode"`
	ProductName string  `json:"productName"`
	SkuSpecs    string  `json:"skuSpecs"`
	Quantity    int     `json:"quantity"`
	Price       float64 `json:"price"`
	Amount      float64 `json:"amount"`
}

type SalesOrder struct {
	ID              uint64           `json:"id"`
	OrderNo         string           `json:"orderNo"`
	Status          string           `json:"status"`
	CustomerName    string           `json:"customerName"`
	CustomerPhone   string           `json:"customerPhone"`
	Address         string           `json:"address"`
	ExpressCompany  string           `json:"expressCompany"`
	ExpressNo       string           `json:"expressNo"`
	TotalAmount     float64          `json:"totalAmount"`
	PayAmount       float64          `json:"payAmount"`
	PayStatus       string           `json:"payStatus"`
	Remark          string           `json:"remark"`
	SellerRemark    string           `json:"sellerRemark"`
	NeedProcurement bool             `json:"needProcurement"`
	Items           []SalesOrderItem `json:"items"`
	CreatedAt       string           `json:"createdAt"`
}

type pageBody struct {
	List     []SalesOrder `json:"list"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
}

type apiBody struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) ListSalesOrders(ctx context.Context, token string, page, pageSize int, status string) (*pageBody, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("storecore url not configured")
	}
	u, _ := url.Parse(c.baseURL + "/api/v1/admin/sales-orders")
	qs := u.Query()
	if page > 0 {
		qs.Set("page", strconv.Itoa(page))
	}
	if pageSize > 0 {
		qs.Set("pageSize", strconv.Itoa(pageSize))
	}
	if status != "" {
		qs.Set("status", status)
	}
	u.RawQuery = qs.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("storecore http %d: %s", resp.StatusCode, string(raw))
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	if body.Code != 200 {
		return nil, fmt.Errorf("storecore: %s", body.Message)
	}
	var pageData pageBody
	if len(body.Data) > 0 {
		if err := json.Unmarshal(body.Data, &pageData); err != nil {
			return nil, err
		}
	}
	return &pageData, nil
}
