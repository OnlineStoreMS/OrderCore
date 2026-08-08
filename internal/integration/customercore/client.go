package customercore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    trimSlash(baseURL),
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

type UpsertByPhoneInput struct {
	TenantID      uint64        `json:"tenantId"`
	Phone         string        `json:"phone"`
	DisplayName   string        `json:"displayName"`
	Source        string        `json:"source"`
	Remark        string        `json:"remark"`
	ChannelType   string        `json:"channelType"`
	ChannelUserID string        `json:"channelUserId"`
	Address       *AddressInput `json:"address,omitempty"`
}

type UpsertResult struct {
	CustomerID uint64 `json:"customerId"`
	Created    bool   `json:"created"`
	Customer   struct {
		ID           uint64 `json:"id"`
		DisplayName  string `json:"displayName"`
		PrimaryPhone string `json:"primaryPhone"`
	} `json:"customer"`
}

type AddressInput struct {
	ContactName string `json:"contactName"`
	Phone       string `json:"phone"`
	Province    string `json:"province"`
	City        string `json:"city"`
	District    string `json:"district"`
	Detail      string `json:"detail"`
	Label       string `json:"label"`
	IsDefault   *int8  `json:"isDefault,omitempty"`
}

type AddressItem struct {
	ID          uint64 `json:"id"`
	CustomerID  uint64 `json:"customerId"`
	ContactName string `json:"contactName"`
	Phone       string `json:"phone"`
	Province    string `json:"province"`
	City        string `json:"city"`
	District    string `json:"district"`
	Detail      string `json:"detail"`
	Label       string `json:"label"`
	IsDefault   int8   `json:"isDefault"`
}

type apiBody struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) UpsertByPhone(in UpsertByPhoneInput) (*UpsertResult, error) {
	var out UpsertResult
	if err := c.postJSON("/api/v1/internal/customers/upsert-by-phone", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetByPhone(tenantID uint64, phone string) (map[string]interface{}, error) {
	q := url.Values{}
	q.Set("phone", phone)
	if tenantID > 0 {
		q.Set("tenantId", fmt.Sprintf("%d", tenantID))
	}
	var out map[string]interface{}
	if err := c.getJSON("/api/v1/internal/customers/by-phone?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

type RecipientSearchItem struct {
	CustomerID   uint64 `json:"customerId"`
	AddressID    uint64 `json:"addressId"`
	DisplayName  string `json:"displayName"`
	PrimaryPhone string `json:"primaryPhone"`
	ContactName  string `json:"contactName"`
	Phone        string `json:"phone"`
	Province     string `json:"province"`
	City         string `json:"city"`
	District     string `json:"district"`
	Detail       string `json:"detail"`
	Label        string `json:"label"`
	IsDefault    int8   `json:"isDefault"`
}

type RecipientSearchResult struct {
	List     []RecipientSearchItem `json:"list"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"pageSize"`
}

func (c *Client) SearchRecipients(tenantID uint64, keyword string, page, pageSize int) (*RecipientSearchResult, error) {
	q := url.Values{}
	if tenantID > 0 {
		q.Set("tenantId", fmt.Sprintf("%d", tenantID))
	}
	if keyword != "" {
		q.Set("keyword", keyword)
	}
	if page > 0 {
		q.Set("page", fmt.Sprintf("%d", page))
	}
	if pageSize > 0 {
		q.Set("pageSize", fmt.Sprintf("%d", pageSize))
	}
	var out RecipientSearchResult
	if err := c.getJSON("/api/v1/internal/recipients/search?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	if out.List == nil {
		out.List = []RecipientSearchItem{}
	}
	return &out, nil
}

func (c *Client) ListAddresses(tenantID, customerID uint64) ([]AddressItem, error) {
	q := ""
	if tenantID > 0 {
		q = "?tenantId=" + fmt.Sprintf("%d", tenantID)
	}
	var out []AddressItem
	if err := c.getJSON(fmt.Sprintf("/api/v1/internal/customers/%d/addresses%s", customerID, q), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []AddressItem{}
	}
	return out, nil
}

func (c *Client) CreateAddress(tenantID, customerID uint64, in AddressInput) (*AddressItem, error) {
	q := ""
	if tenantID > 0 {
		q = "?tenantId=" + fmt.Sprintf("%d", tenantID)
	}
	var out AddressItem
	if err := c.postJSON(fmt.Sprintf("/api/v1/internal/customers/%d/addresses%s", customerID, q), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateAddress(tenantID, customerID, addrID uint64, in AddressInput) (*AddressItem, error) {
	q := ""
	if tenantID > 0 {
		q = "?tenantId=" + fmt.Sprintf("%d", tenantID)
	}
	var out AddressItem
	if err := c.putJSON(fmt.Sprintf("/api/v1/internal/customers/%d/addresses/%d%s", customerID, addrID, q), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteAddress(tenantID, customerID, addrID uint64) error {
	q := ""
	if tenantID > 0 {
		q = "?tenantId=" + fmt.Sprintf("%d", tenantID)
	}
	return c.deleteJSON(fmt.Sprintf("/api/v1/internal/customers/%d/addresses/%d%s", customerID, addrID, q))
}

func (c *Client) postJSON(path string, in, out interface{}) error {
	b, _ := json.Marshal(in)
	resp, err := c.httpClient.Post(c.baseURL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decode(resp, out)
}

func (c *Client) putJSON(path string, in, out interface{}) error {
	b, _ := json.Marshal(in)
	req, err := http.NewRequest(http.MethodPut, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decode(resp, out)
}

func (c *Client) deleteJSON(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decode(resp, nil)
}

func (c *Client) getJSON(path string, out interface{}) error {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decode(resp, out)
}

func decode(resp *http.Response, out interface{}) error {
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var body apiBody
	if err := json.Unmarshal(b, &body); err != nil {
		return err
	}
	if resp.StatusCode >= 400 || body.Code != 200 {
		msg := body.Message
		if msg == "" {
			msg = string(b)
		}
		return fmt.Errorf("customercore: %s", msg)
	}
	if out == nil || len(body.Data) == 0 || string(body.Data) == "null" {
		return nil
	}
	return json.Unmarshal(body.Data, out)
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
