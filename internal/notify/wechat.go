package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const defaultWeChatBaseURL = "https://qyapi.weixin.qq.com"

type WeChatAppNotifier struct {
	CorpID     string
	CorpSecret string
	AgentID    int
	ToUser     []string
	BaseURL    string
	Client     *http.Client
}

type wechatResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

type wechatTokenResponse struct {
	wechatResponse
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (n WeChatAppNotifier) SendText(ctx context.Context, title string, fields map[string]string) error {
	if err := n.validate(); err != nil {
		return err
	}
	client := n.client()
	token, err := n.fetchAccessToken(ctx, client)
	if err != nil {
		return err
	}
	content := textContent(title, fields)
	body, err := json.Marshal(map[string]any{
		"touser":  strings.Join(normalizeReceivers(n.ToUser), "|"),
		"msgtype": "text",
		"agentid": n.AgentID,
		"text": map[string]string{
			"content": content,
		},
		"safe": 0,
	})
	if err != nil {
		return err
	}
	endpoint, err := url.Parse(n.baseURL() + "/cgi-bin/message/send")
	if err != nil {
		return err
	}
	query := endpoint.Query()
	query.Set("access_token", token)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("wechat message/send HTTP %d", resp.StatusCode)
	}
	var result wechatResponse
	if err := json.Unmarshal(data, &result); err == nil && result.ErrCode != 0 {
		return fmt.Errorf("wechat message/send errcode %d: %s", result.ErrCode, result.ErrMsg)
	}
	return nil
}

func (n WeChatAppNotifier) SendMarkdown(ctx context.Context, title string, fields map[string]string) error {
	return n.SendText(ctx, title, fields)
}

func (n WeChatAppNotifier) fetchAccessToken(ctx context.Context, client *http.Client) (string, error) {
	endpoint, err := url.Parse(n.baseURL() + "/cgi-bin/gettoken")
	if err != nil {
		return "", err
	}
	query := endpoint.Query()
	query.Set("corpid", n.CorpID)
	query.Set("corpsecret", n.CorpSecret)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("wechat gettoken HTTP %d", resp.StatusCode)
	}
	var result wechatTokenResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("wechat gettoken errcode %d: %s", result.ErrCode, result.ErrMsg)
	}
	if strings.TrimSpace(result.AccessToken) == "" {
		return "", fmt.Errorf("wechat gettoken returned empty access_token")
	}
	return result.AccessToken, nil
}

func (n WeChatAppNotifier) validate() error {
	if strings.TrimSpace(n.CorpID) == "" {
		return fmt.Errorf("wechat corpid is empty")
	}
	if strings.TrimSpace(n.CorpSecret) == "" {
		return fmt.Errorf("wechat corpsecret is empty")
	}
	if n.AgentID <= 0 {
		return fmt.Errorf("wechat agentid is empty")
	}
	if len(normalizeReceivers(n.ToUser)) == 0 {
		return fmt.Errorf("wechat touser is empty")
	}
	return nil
}

func (n WeChatAppNotifier) client() *http.Client {
	if n.Client != nil {
		return n.Client
	}
	return http.DefaultClient
}

func (n WeChatAppNotifier) baseURL() string {
	base := strings.TrimRight(strings.TrimSpace(n.BaseURL), "/")
	if base == "" {
		return defaultWeChatBaseURL
	}
	return base
}

func normalizeReceivers(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == '|' || r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			result = append(result, part)
		}
	}
	return result
}

func textContent(title string, fields map[string]string) string {
	var builder strings.Builder
	normalized := make(map[string]string, len(fields)+1)
	for key, value := range fields {
		normalized[key] = normalizeTextFieldValue(key, value)
	}
	if value := strings.TrimSpace(normalized["操作事件"]); value != "" && strings.TrimSpace(normalized["事件"]) == "" {
		normalized["事件"] = normalizeEventText(value)
		delete(normalized, "操作事件")
	}
	if value := strings.TrimSpace(normalized["时间"]); value != "" && strings.TrimSpace(normalized["发送时间"]) == "" {
		normalized["发送时间"] = value
		delete(normalized, "时间")
	}
	if strings.TrimSpace(normalized["事件"]) == "" {
		normalized["事件"] = normalizeEventText(title)
	}
	preferredKeys := []string{"账号", "事件", "停机模式", "地域", "实例名称", "实例 ID", "发送时间"}
	written := map[string]bool{}
	for _, key := range preferredKeys {
		writeTextField(&builder, key, normalized[key])
		written[key] = true
	}
	keys := make([]string, 0, len(normalized))
	for key := range normalized {
		if !written[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		writeTextField(&builder, key, normalized[key])
	}
	return builder.String()
}

func normalizeTextFieldValue(key, value string) string {
	value = strings.TrimSpace(value)
	switch key {
	case "事件", "操作事件":
		return normalizeEventText(value)
	case "停机模式", "配置停机模式":
		return normalizeStopModeText(value)
	default:
		return value
	}
}

func normalizeEventText(value string) string {
	switch strings.TrimSpace(value) {
	case "手工关机已提交", "手工关机失败":
		return "手工关机"
	case "手工启动已提交", "手工启动失败":
		return "手工启动"
	case "后台自动启动已提交", "后台自动启动失败":
		return "后台自动启动"
	default:
		return value
	}
}

func normalizeStopModeText(value string) string {
	switch strings.TrimSpace(value) {
	case "StopCharging":
		return "节约关机"
	case "KeepCharging":
		return "普通关机"
	default:
		return value
	}
}

func markdownContent(title string, fields map[string]string) string {
	return textContent(title, fields)
}

func writeTextField(builder *strings.Builder, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if builder.Len() > 0 {
		builder.WriteByte('\n')
	}
	builder.WriteString(key)
	builder.WriteString("：")
	builder.WriteString(value)
}
