package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) SendFeishuText(ctx context.Context, webhookURL, secret, text string) error {
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]any{"text": text},
	}
	return c.postFeishu(ctx, webhookURL, secret, payload)
}

func (c *Client) SendWecomText(ctx context.Context, webhookURL, text string) error {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return fmt.Errorf("webhook url is required")
	}
	body, _ := json.Marshal(map[string]any{
		"msgtype": "text",
		"text":    map[string]any{"content": text},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("wecom webhook http %d: %s", resp.StatusCode, trimBody(raw))
	}
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	_ = json.Unmarshal(raw, &result)
	if result.ErrCode != 0 {
		return fmt.Errorf("wecom webhook: %s", result.ErrMsg)
	}
	return nil
}

func (c *Client) postFeishu(ctx context.Context, webhookURL, secret string, payload map[string]any) error {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return fmt.Errorf("webhook url is required")
	}
	ts := time.Now().Unix()
	payload["timestamp"] = strconv.FormatInt(ts, 10)
	if secret = strings.TrimSpace(secret); secret != "" {
		sign, err := feishuSign(secret, ts)
		if err != nil {
			return err
		}
		payload["sign"] = sign
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("feishu webhook http %d: %s", resp.StatusCode, trimBody(raw))
	}
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(raw, &result)
	if result.Code != 0 && result.Msg != "" {
		return fmt.Errorf("feishu webhook: %s", result.Msg)
	}
	return nil
}

func feishuSign(secret string, timestamp int64) (string, error) {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	mac := hmac.New(sha256.New, []byte(stringToSign))
	if _, err := mac.Write(nil); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func trimBody(raw []byte) string {
	if len(raw) > 200 {
		return string(raw[:200]) + "..."
	}
	return string(raw)
}
