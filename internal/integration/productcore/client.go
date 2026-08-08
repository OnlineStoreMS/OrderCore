package productcore

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
			Timeout: 10 * time.Second,
		},
	}
}

type SkuSearchItem struct {
	SkuID   uint64 `json:"skuId"`
	SkuCode string `json:"skuCode"`
}

type pagePayload struct {
	List []SkuSearchItem `json:"list"`
}

type apiBody struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// ResolveSkuIDByCode 按商家编码精确匹配 ProductCore SKU；找不到返回 0。
func (c *Client) ResolveSkuIDByCode(ctx context.Context, bearerToken, skuCode string) (uint64, error) {
	if c == nil || c.baseURL == "" {
		return 0, nil
	}
	code := strings.TrimSpace(skuCode)
	if code == "" {
		return 0, nil
	}
	q := url.Values{}
	q.Set("keyword", code)
	q.Set("page", "1")
	q.Set("pageSize", "20")
	reqURL := c.baseURL + "/api/v1/admin/super-search?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, err
	}
	token := strings.TrimSpace(bearerToken)
	if token != "" {
		if !strings.HasPrefix(strings.ToLower(token), "bearer ") {
			token = "Bearer " + token
		}
		req.Header.Set("Authorization", token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("productcore: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("productcore http %d", resp.StatusCode)
	}
	var wrapped apiBody
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return 0, err
	}
	if wrapped.Code != 200 {
		return 0, fmt.Errorf("%s", wrapped.Message)
	}
	var page pagePayload
	if err := json.Unmarshal(wrapped.Data, &page); err != nil {
		return 0, err
	}
	codeLower := strings.ToLower(code)
	for _, it := range page.List {
		if strings.EqualFold(strings.TrimSpace(it.SkuCode), code) {
			return it.SkuID, nil
		}
		if strings.ToLower(strings.TrimSpace(it.SkuCode)) == codeLower {
			return it.SkuID, nil
		}
	}
	// 兜底：仅一条且 keyword 为纯数字时可能搜到 id
	if len(page.List) == 1 && page.List[0].SkuID > 0 {
		if _, err := strconv.ParseUint(code, 10, 64); err == nil {
			return page.List[0].SkuID, nil
		}
	}
	return 0, nil
}

// SuperSearchRaw 透传 ProductCore super-search 原始 data，供手工建单商品选择。
func (c *Client) SuperSearchRaw(ctx context.Context, bearerToken, keyword string, page, pageSize int) (json.RawMessage, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("productcore url not configured")
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("keyword is required")
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	q := url.Values{}
	q.Set("keyword", keyword)
	q.Set("page", strconv.Itoa(page))
	q.Set("pageSize", strconv.Itoa(pageSize))
	reqURL := c.baseURL + "/api/v1/admin/super-search?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(bearerToken)
	if token != "" {
		if !strings.HasPrefix(strings.ToLower(token), "bearer ") {
			token = "Bearer " + token
		}
		req.Header.Set("Authorization", token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("productcore: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("productcore http %d", resp.StatusCode)
	}
	var wrapped apiBody
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	if wrapped.Code != 200 {
		return nil, fmt.Errorf("%s", wrapped.Message)
	}
	if len(wrapped.Data) == 0 {
		return json.RawMessage(`{"list":[],"total":0}`), nil
	}
	return wrapped.Data, nil
}
