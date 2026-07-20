package supplycore

import (
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

type pagePayload struct {
	List     []SupplierItem `json:"list"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

type apiBody struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) ListSuppliers(ctx context.Context, bearerToken, keyword string, page, pageSize int) ([]SupplierItem, int64, error) {
	if c == nil || c.baseURL == "" {
		return nil, 0, fmt.Errorf("supplycore 未配置")
	}
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, 0, err
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", bearerToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("supplycore request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode >= 400 {
		return nil, 0, fmt.Errorf("supplycore http %d", resp.StatusCode)
	}

	var wrapped apiBody
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, 0, err
	}
	if wrapped.Code != 200 {
		msg := wrapped.Message
		if msg == "" {
			msg = "supplycore error"
		}
		return nil, 0, fmt.Errorf("%s", msg)
	}

	var pageData pagePayload
	if err := json.Unmarshal(wrapped.Data, &pageData); err != nil {
		return nil, 0, err
	}
	if pageData.List == nil {
		pageData.List = []SupplierItem{}
	}
	return pageData.List, pageData.Total, nil
}
