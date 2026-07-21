package supplycore

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
			Timeout: 15 * time.Second,
		},
	}
}

type SupplierItem struct {
	ID          uint64 `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	ShortName   string `json:"shortName"`
	Status      int8   `json:"status"`
	ContactName string `json:"contactName"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Remark      string `json:"remark"`
}

type PurchaseOrderItemInput struct {
	SkuID           uint64  `json:"skuId,omitempty"`
	OfferID         uint64  `json:"offerId,omitempty"`
	ProductName     string  `json:"productName,omitempty"`
	SupplierSkuCode string  `json:"supplierSkuCode,omitempty"`
	Qty             int     `json:"qty"`
	UnitPrice       float64 `json:"unitPrice"`
	Remark          string  `json:"remark,omitempty"`
}

type PurchaseOrderInput struct {
	SupplierID      uint64                    `json:"supplierId"`
	FulfillmentType string                    `json:"fulfillmentType"`
	RefSoID         uint64                    `json:"refSoId,omitempty"`
	RefTraceID      string                    `json:"refTraceId,omitempty"`
	Remark          string                    `json:"remark,omitempty"`
	Items           []PurchaseOrderItemInput  `json:"items"`
}

type PurchaseOrderDetail struct {
	ID              uint64 `json:"id"`
	PoNo            string `json:"poNo"`
	SupplierID      uint64 `json:"supplierId"`
	Status          string `json:"status"`
	PayStatus       string `json:"payStatus"`
	FulfillmentType string `json:"fulfillmentType"`
	RefSoID         uint64 `json:"refSoId"`
	RefTraceID      string `json:"refTraceId"`
	TotalAmount     float64 `json:"totalAmount"`
}

type PurchaseOrderListItem struct {
	ID              uint64 `json:"id"`
	PoNo            string `json:"poNo"`
	SupplierID      uint64 `json:"supplierId"`
	Status          string `json:"status"`
	PayStatus       string `json:"payStatus"`
	FulfillmentType string `json:"fulfillmentType"`
	RefSoID         uint64 `json:"refSoId"`
	RefTraceID      string `json:"refTraceId"`
}

type pagePayload struct {
	List     []SupplierItem `json:"list"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

type poPagePayload struct {
	List     []PurchaseOrderListItem `json:"list"`
	Total    int64                   `json:"total"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"pageSize"`
}

type apiBody struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) authHeader(bearerToken string) string {
	token := strings.TrimSpace(bearerToken)
	if token == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = "Bearer " + token
	}
	return token
}

func (c *Client) doJSON(ctx context.Context, method, reqURL, bearerToken string, body any, out any) error {
	if c == nil || c.baseURL == "" {
		return fmt.Errorf("supplycore 未配置")
	}
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
	if auth := c.authHeader(bearerToken); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("supplycore request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = fmt.Sprintf("supplycore http %d", resp.StatusCode)
		}
		return fmt.Errorf("%s", msg)
	}
	var wrapped apiBody
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return err
	}
	if wrapped.Code != 200 && wrapped.Code != 201 {
		msg := wrapped.Message
		if msg == "" {
			msg = "supplycore error"
		}
		return fmt.Errorf("%s", msg)
	}
	if out == nil || len(wrapped.Data) == 0 || string(wrapped.Data) == "null" {
		return nil
	}
	return json.Unmarshal(wrapped.Data, out)
}

func (c *Client) ListSuppliers(ctx context.Context, bearerToken, keyword string, page, pageSize int) ([]SupplierItem, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	q := url.Values{}
	if keyword != "" {
		q.Set("keyword", keyword)
	}
	q.Set("page", strconv.Itoa(page))
	q.Set("pageSize", strconv.Itoa(pageSize))
	reqURL := c.baseURL + "/api/v1/admin/suppliers?" + q.Encode()

	var pageData pagePayload
	if err := c.doJSON(ctx, http.MethodGet, reqURL, bearerToken, nil, &pageData); err != nil {
		return nil, 0, err
	}
	if pageData.List == nil {
		pageData.List = []SupplierItem{}
	}
	return pageData.List, pageData.Total, nil
}

func (c *Client) CreatePurchaseOrder(ctx context.Context, bearerToken string, in PurchaseOrderInput) (*PurchaseOrderDetail, error) {
	reqURL := c.baseURL + "/api/v1/admin/purchase-orders"
	var out PurchaseOrderDetail
	if err := c.doJSON(ctx, http.MethodPost, reqURL, bearerToken, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListPurchaseOrders(ctx context.Context, bearerToken string, refSoID uint64, fulfillmentType string, page, pageSize int) ([]PurchaseOrderListItem, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	q := url.Values{}
	if refSoID > 0 {
		q.Set("refSoId", strconv.FormatUint(refSoID, 10))
	}
	if fulfillmentType != "" {
		q.Set("fulfillmentType", fulfillmentType)
	}
	q.Set("page", strconv.Itoa(page))
	q.Set("pageSize", strconv.Itoa(pageSize))
	reqURL := c.baseURL + "/api/v1/admin/purchase-orders?" + q.Encode()
	var pageData poPagePayload
	if err := c.doJSON(ctx, http.MethodGet, reqURL, bearerToken, nil, &pageData); err != nil {
		return nil, 0, err
	}
	if pageData.List == nil {
		pageData.List = []PurchaseOrderListItem{}
	}
	return pageData.List, pageData.Total, nil
}

func (c *Client) GetPurchaseOrder(ctx context.Context, bearerToken string, id uint64) (*PurchaseOrderDetail, error) {
	reqURL := c.baseURL + "/api/v1/admin/purchase-orders/" + strconv.FormatUint(id, 10)
	var out PurchaseOrderDetail
	if err := c.doJSON(ctx, http.MethodGet, reqURL, bearerToken, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CancelPurchaseOrder(ctx context.Context, bearerToken string, id uint64) (*PurchaseOrderDetail, error) {
	reqURL := c.baseURL + "/api/v1/admin/purchase-orders/" + strconv.FormatUint(id, 10) + "/cancel"
	var out PurchaseOrderDetail
	if err := c.doJSON(ctx, http.MethodPost, reqURL, bearerToken, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeletePurchaseOrder(ctx context.Context, bearerToken string, id uint64) error {
	reqURL := c.baseURL + "/api/v1/admin/purchase-orders/" + strconv.FormatUint(id, 10)
	return c.doJSON(ctx, http.MethodDelete, reqURL, bearerToken, nil, nil)
}
