package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTextContentUsesPreferredFieldOrder(t *testing.T) {
	content := textContent("手工关机已提交", map[string]string{
		"实例 ID": "i-123",
		"账号":    "aliyun-cn",
		"地域":    "cn-hangzhou",
		"时间":    "2026-05-19 21:00:00",
		"停机模式":  "StopCharging",
	})

	if !strings.Contains(content, "事件：手工关机") {
		t.Fatalf("content = %q", content)
	}
	if strings.Index(content, "账号：aliyun-cn") > strings.Index(content, "地域：cn-hangzhou") ||
		strings.Index(content, "账号：aliyun-cn") > strings.Index(content, "事件：手工关机") ||
		strings.Index(content, "事件：手工关机") > strings.Index(content, "停机模式：节约关机") ||
		strings.Index(content, "停机模式：节约关机") > strings.Index(content, "地域：cn-hangzhou") ||
		strings.Index(content, "地域：cn-hangzhou") > strings.Index(content, "实例 ID：i-123") ||
		strings.Index(content, "实例 ID：i-123") > strings.Index(content, "发送时间：2026-05-19 21:00:00") {
		t.Fatalf("fields are not in preferred order: %q", content)
	}
}

func TestWeChatAppNotifierSendsTextMessage(t *testing.T) {
	var tokenRequested bool
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			tokenRequested = true
			if r.URL.Query().Get("corpid") != "corp-id" || r.URL.Query().Get("corpsecret") != "corp-secret" {
				t.Fatalf("gettoken query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"token-1","expires_in":7200}`))
			return
		case "/cgi-bin/message/send":
			if r.URL.Query().Get("access_token") != "token-1" {
				t.Fatalf("access_token = %q, want token-1", r.URL.Query().Get("access_token"))
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	err := WeChatAppNotifier{
		CorpID:     "corp-id",
		CorpSecret: "corp-secret",
		AgentID:    1000002,
		ToUser:     []string{"user-a", "user-b"},
		BaseURL:    server.URL,
		Client:     server.Client(),
	}.SendText(context.Background(), "手工关机已提交", map[string]string{
		"账号":    "aliyun-cn",
		"地域":    "cn-hangzhou",
		"实例名称":  "web-1",
		"实例 ID": "i-123",
		"停机模式":  "StopCharging",
		"发送时间":  "2026-05-19 21:00:00",
	})
	if err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	if !tokenRequested {
		t.Fatal("gettoken was not requested")
	}
	if payload["msgtype"] != "text" {
		t.Fatalf("msgtype = %#v, want text", payload["msgtype"])
	}
	if payload["agentid"].(float64) != 1000002 {
		t.Fatalf("agentid = %#v, want 1000002", payload["agentid"])
	}
	if payload["touser"] != "user-a|user-b" {
		t.Fatalf("touser = %#v, want user-a|user-b", payload["touser"])
	}
	textPayload, ok := payload["text"].(map[string]any)
	if !ok {
		t.Fatalf("text payload = %#v", payload["text"])
	}
	if !strings.Contains(textPayload["content"].(string), "账号：aliyun-cn") ||
		!strings.Contains(textPayload["content"].(string), "事件：手工关机") ||
		!strings.Contains(textPayload["content"].(string), "停机模式：节约关机") ||
		!strings.Contains(textPayload["content"].(string), "发送时间：2026-05-19 21:00:00") {
		t.Fatalf("content = %#v", textPayload["content"])
	}
}

func TestWeChatAppNotifierReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"token-1","expires_in":7200}`))
		case "/cgi-bin/message/send":
			_, _ = w.Write([]byte(`{"errcode":40003,"errmsg":"invalid touser"}`))
		}
	}))
	defer server.Close()

	err := WeChatAppNotifier{
		CorpID:     "corp-id",
		CorpSecret: "corp-secret",
		AgentID:    1000002,
		ToUser:     []string{"bad-user"},
		BaseURL:    server.URL,
		Client:     server.Client(),
	}.SendText(context.Background(), "ECS 通知", nil)
	if err == nil {
		t.Fatal("SendText() error = nil, want notification API error")
	}
	if !strings.Contains(err.Error(), "40003") {
		t.Fatalf("error = %v, want errcode", err)
	}
}
