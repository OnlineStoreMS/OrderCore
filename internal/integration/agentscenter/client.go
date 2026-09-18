package agentscenter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func NewClient(baseURL, token string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(token) == "" {
		return nil
	}
	return &Client{
		BaseURL: baseURL,
		Token:   strings.TrimSpace(token),
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
}

type CreatedJob struct {
	ID uint64 `json:"id"`
}

type JobStatus struct {
	ID           uint64  `json:"id"`
	Status       string  `json:"status"`
	ErrorMessage string  `json:"errorMessage"`
	ResultJSON   string  `json:"resultJson"`
	StartedAt    *string `json:"startedAt,omitempty"`
	FinishedAt   *string `json:"finishedAt,omitempty"`
}

type createJobBody struct {
	TenantID         uint64 `json:"tenantId"`
	JobType          string `json:"jobType"`
	Platform         string `json:"platform"`
	PlatformShopID   string `json:"platformShopId"`
	PlatformShopName string `json:"platformShopName"`
	ParamsJSON       string `json:"paramsJson"`
	Source           string `json:"source"`
	Priority         int    `json:"priority"`
}

func (c *Client) CreateJob(tenantID uint64, jobType, platform, platformShopID, platformShopName, paramsJSON, source string) (*CreatedJob, error) {
	if c == nil {
		return nil, fmt.Errorf("AgentsCenter 未配置")
	}
	if source == "" {
		source = "ordercore"
	}
	body := createJobBody{
		TenantID:         tenantID,
		JobType:          jobType,
		Platform:         platform,
		PlatformShopID:   platformShopID,
		PlatformShopName: platformShopName,
		ParamsJSON:       paramsJSON,
		Source:           source,
		Priority:         80,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/api/v1/internal/jobs", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", c.Token)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("AgentsCenter HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var envelope struct {
		Data CreatedJob `json:"data"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return nil, err
	}
	if envelope.Data.ID == 0 {
		return nil, fmt.Errorf("AgentsCenter 未返回 jobId")
	}
	return &envelope.Data, nil
}

func (c *Client) GetJobs(tenantID uint64, ids []uint64) ([]JobStatus, error) {
	if c == nil {
		return nil, fmt.Errorf("AgentsCenter 未配置")
	}
	if len(ids) == 0 {
		return nil, nil
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			parts = append(parts, fmt.Sprintf("%d", id))
		}
	}
	if len(parts) == 0 {
		return nil, nil
	}
	q := url.Values{}
	q.Set("tenantId", fmt.Sprintf("%d", tenantID))
	q.Set("ids", strings.Join(parts, ","))
	u := fmt.Sprintf("%s/api/v1/internal/jobs?%s", c.BaseURL, q.Encode())
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Internal-Token", c.Token)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("AgentsCenter HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var envelope struct {
		Data struct {
			List []JobStatus `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return nil, err
	}
	return envelope.Data.List, nil
}
