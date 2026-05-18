package config_test

import (
	"strings"
	"testing"
	"time"

	"ecs-controller/internal/config"
)

func TestLoadBytesExpandsEnvironmentVariablesAndDefaults(t *testing.T) {
	t.Setenv("EC_PASSWORD", "secret-password")

	cfg, err := config.LoadBytes([]byte(`
server:
  password: "${EC_PASSWORD}"
logging:
  level: "debug"
notification:
  enabled: true
  corpid: "corp-id"
  corpsecret: "corp-secret"
  agentid: 1000002
  touser: ["user-a", "user-b"]
  notify_events: ["auto_start", "manual_required"]
accounts:
  - name: "cn"
    site: "china"
    access_key_id: "ak"
    access_key_secret: "sk"
    regions: ["auto"]
    mainland_traffic_limit: 20
    overseas_traffic_limit: 200
`))
	if err != nil {
		t.Fatalf("LoadBytes() error = %v", err)
	}

	if cfg.Server.Password != "secret-password" {
		t.Fatalf("password = %q, want secret-password", cfg.Server.Password)
	}
	if cfg.Server.Listen != ":8080" {
		t.Fatalf("listen = %q, want :8080", cfg.Server.Listen)
	}
	if cfg.Server.RefreshInterval != 5*time.Minute {
		t.Fatalf("refresh interval = %s, want 5m", cfg.Server.RefreshInterval)
	}
	if cfg.KeepAlive.TrafficPolicy != "manual_only_when_exceeded" {
		t.Fatalf("traffic policy = %q", cfg.KeepAlive.TrafficPolicy)
	}
	if cfg.KeepAlive.StopMode != "StopCharging" {
		t.Fatalf("stop mode = %q, want StopCharging", cfg.KeepAlive.StopMode)
	}
	if cfg.Logging.Level != "debug" {
		t.Fatalf("logging level = %q, want debug", cfg.Logging.Level)
	}
	if !cfg.Notification.Enabled {
		t.Fatal("notification enabled = false, want true")
	}
	if cfg.Notification.WeChatCorpID != "corp-id" {
		t.Fatalf("corpid = %q, want corp-id", cfg.Notification.WeChatCorpID)
	}
	if cfg.Notification.WeChatCorpSecret != "corp-secret" {
		t.Fatalf("corpsecret = %q, want corp-secret", cfg.Notification.WeChatCorpSecret)
	}
	if cfg.Notification.WeChatAgentID != 1000002 {
		t.Fatalf("agentid = %d, want 1000002", cfg.Notification.WeChatAgentID)
	}
	if len(cfg.Notification.WeChatToUser) != 2 || cfg.Notification.WeChatToUser[1] != "user-b" {
		t.Fatalf("touser = %#v, want [user-a user-b]", cfg.Notification.WeChatToUser)
	}
	if len(cfg.Notification.NotifyEvents) != 2 || cfg.Notification.NotifyEvents[0] != "auto_start" {
		t.Fatalf("notify events = %#v", cfg.Notification.NotifyEvents)
	}
	if len(cfg.Accounts) != 1 {
		t.Fatalf("accounts len = %d, want 1", len(cfg.Accounts))
	}
	if cfg.Accounts[0].MainlandTrafficLimit != 20 {
		t.Fatalf("mainland traffic limit = %v, want 20", cfg.Accounts[0].MainlandTrafficLimit)
	}
	if cfg.Accounts[0].OverseasTrafficLimit != 200 {
		t.Fatalf("overseas traffic limit = %v, want 200", cfg.Accounts[0].OverseasTrafficLimit)
	}
	if len(cfg.Accounts[0].Regions) != 1 || cfg.Accounts[0].Regions[0] != "auto" {
		t.Fatalf("regions = %#v, want [auto]", cfg.Accounts[0].Regions)
	}
}

func TestLoadBytesRejectsLegacyStatePath(t *testing.T) {
	_, err := config.LoadBytes([]byte(`
server:
  password: "secret"
  state_path: "/data/state.json"
accounts:
  - name: "cn"
    site: "china"
    access_key_id: "ak"
    access_key_secret: "sk"
`))
	if err == nil {
		t.Fatal("LoadBytes() error = nil, want unknown state_path error")
	}
	if !strings.Contains(err.Error(), "state_path") {
		t.Fatalf("LoadBytes() error = %v, want state_path", err)
	}
}

func TestLoadBytesRejectsMissingAccounts(t *testing.T) {
	_, err := config.LoadBytes([]byte(`
server:
  listen: ":8080"
`))
	if err == nil {
		t.Fatal("LoadBytes() error = nil, want validation error")
	}
}

func TestLoadBytesParsesSingleWeChatReceiver(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(`
server:
  password: "secret"
notification:
  touser: "single-user"
accounts:
  - name: "cn"
    site: "china"
    access_key_id: "ak"
    access_key_secret: "sk"
`))
	if err != nil {
		t.Fatalf("LoadBytes() error = %v", err)
	}
	if len(cfg.Notification.WeChatToUser) != 1 || cfg.Notification.WeChatToUser[0] != "single-user" {
		t.Fatalf("touser = %#v, want [single-user]", cfg.Notification.WeChatToUser)
	}
}

func TestLoadBytesUsesEnvironmentAccountAliases(t *testing.T) {
	t.Setenv("EC_ACCOUNTS", "CN1,INTL_PROD")
	t.Setenv("EC_ACCOUNT_CN1_NAME", "cn-main")
	t.Setenv("EC_ACCOUNT_CN1_SITE", "china")
	t.Setenv("EC_ACCOUNT_CN1_ACCESS_KEY_ID", "cn-ak")
	t.Setenv("EC_ACCOUNT_CN1_ACCESS_KEY_SECRET", "cn-sk")
	t.Setenv("EC_ACCOUNT_CN1_REGIONS", "cn-hangzhou,cn-shanghai")
	t.Setenv("EC_ACCOUNT_CN1_MAINLAND_TRAFFIC_LIMIT", "30")
	t.Setenv("EC_ACCOUNT_CN1_OVERSEAS_TRAFFIC_LIMIT", "220")
	t.Setenv("EC_ACCOUNT_INTL_PROD_NAME", "intl-main")
	t.Setenv("EC_ACCOUNT_INTL_PROD_SITE", "international")
	t.Setenv("EC_ACCOUNT_INTL_PROD_ACCESS_KEY_ID", "intl-ak")
	t.Setenv("EC_ACCOUNT_INTL_PROD_ACCESS_KEY_SECRET", "intl-sk")
	t.Setenv("EC_ACCOUNT_INTL_PROD_REGIONS", "ap-southeast-1")
	t.Setenv("EC_REFRESH_INTERVAL", "10m")
	t.Setenv("EC_TRAFFIC_WARNING_PERCENT", "90")
	t.Setenv("EC_NOTIFY_ENABLED", "true")
	t.Setenv("EC_WECHAT_CORPID", "corp-env")
	t.Setenv("EC_WECHAT_CORPSECRET", "secret-env")
	t.Setenv("EC_WECHAT_AGENTID", "1000003")
	t.Setenv("EC_WECHAT_TOUSER", "user-a,user-b")

	cfg, err := config.LoadBytes([]byte(`
server:
  password: "secret"
`))
	if err != nil {
		t.Fatalf("LoadBytes() error = %v", err)
	}
	if cfg.Server.RefreshInterval != 10*time.Minute {
		t.Fatalf("refresh interval = %s, want 10m", cfg.Server.RefreshInterval)
	}
	if cfg.Traffic.WarningPercent != 90 {
		t.Fatalf("warning percent = %v, want 90", cfg.Traffic.WarningPercent)
	}
	if !cfg.Notification.Enabled || cfg.Notification.WeChatCorpID != "corp-env" || cfg.Notification.WeChatAgentID != 1000003 {
		t.Fatalf("wechat notification config = %#v", cfg.Notification)
	}
	if len(cfg.Notification.WeChatToUser) != 2 || cfg.Notification.WeChatToUser[1] != "user-b" {
		t.Fatalf("wechat receivers = %#v", cfg.Notification.WeChatToUser)
	}
	if len(cfg.Accounts) != 2 {
		t.Fatalf("accounts len = %d, want 2", len(cfg.Accounts))
	}
	if cfg.Accounts[0].Name != "cn-main" || cfg.Accounts[0].AccessKeyID != "cn-ak" || cfg.Accounts[0].Regions[1] != "cn-shanghai" {
		t.Fatalf("first account = %#v", cfg.Accounts[0])
	}
	if cfg.Accounts[0].MainlandTrafficLimit != 30 || cfg.Accounts[0].OverseasTrafficLimit != 220 {
		t.Fatalf("first account traffic limits = %.0f/%.0f", cfg.Accounts[0].MainlandTrafficLimit, cfg.Accounts[0].OverseasTrafficLimit)
	}
	if cfg.Accounts[1].Name != "intl-main" || cfg.Accounts[1].Site != "international" || cfg.Accounts[1].Regions[0] != "ap-southeast-1" {
		t.Fatalf("second account = %#v", cfg.Accounts[1])
	}
}

func TestLoadBytesRejectsInvalidEnvironmentDuration(t *testing.T) {
	t.Setenv("EC_ACCOUNTS", "CN1")
	t.Setenv("EC_ACCOUNT_CN1_ACCESS_KEY_ID", "ak")
	t.Setenv("EC_ACCOUNT_CN1_ACCESS_KEY_SECRET", "sk")
	t.Setenv("EC_REFRESH_INTERVAL", "ten-minutes")

	_, err := config.LoadBytes([]byte(`
server:
  password: "secret"
`))
	if err == nil {
		t.Fatal("LoadBytes() error = nil, want invalid duration error")
	}
	if !strings.Contains(err.Error(), "EC_REFRESH_INTERVAL") {
		t.Fatalf("LoadBytes() error = %v, want env var name", err)
	}
}

func TestLoadBytesDefaultsNotificationEvents(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(`
server:
  password: "secret"
accounts:
  - name: "cn"
    site: "china"
    access_key_id: "ak"
    access_key_secret: "sk"
`))
	if err != nil {
		t.Fatalf("LoadBytes() error = %v", err)
	}

	want := []string{"auto_start", "manual_start", "manual_stop", "manual_required", "traffic_exceeded", "error"}
	if len(cfg.Notification.NotifyEvents) != len(want) {
		t.Fatalf("notify events = %#v, want %#v", cfg.Notification.NotifyEvents, want)
	}
	for index, event := range want {
		if cfg.Notification.NotifyEvents[index] != event {
			t.Fatalf("notify events = %#v, want %#v", cfg.Notification.NotifyEvents, want)
		}
	}
}

func TestLoadBytesRejectsUnknownNotificationEvent(t *testing.T) {
	_, err := config.LoadBytes([]byte(`
server:
  password: "secret"
notification:
  notify_events: ["auto_start", "unknown_event"]
accounts:
  - name: "cn"
    site: "china"
    access_key_id: "ak"
    access_key_secret: "sk"
`))
	if err == nil {
		t.Fatal("LoadBytes() error = nil, want validation error")
	}
}

func TestLoadBytesParsesStopMode(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(`
server:
  password: "secret"
keep_alive:
  stop_mode: "KeepCharging"
accounts:
  - name: "cn"
    site: "china"
    access_key_id: "ak"
    access_key_secret: "sk"
`))
	if err != nil {
		t.Fatalf("LoadBytes() error = %v", err)
	}
	if cfg.KeepAlive.StopMode != "KeepCharging" {
		t.Fatalf("stop mode = %q, want KeepCharging", cfg.KeepAlive.StopMode)
	}
}

func TestLoadBytesRejectsUnsupportedStopMode(t *testing.T) {
	_, err := config.LoadBytes([]byte(`
server:
  password: "secret"
keep_alive:
  stop_mode: "release"
accounts:
  - name: "cn"
    site: "china"
    access_key_id: "ak"
    access_key_secret: "sk"
`))
	if err == nil {
		t.Fatal("LoadBytes() error = nil, want validation error")
	}
}
