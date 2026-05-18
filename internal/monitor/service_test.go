package monitor

import (
	"os"
	"strings"
	"testing"
	"time"

	"ecs-controller/internal/aliyun"
	"ecs-controller/internal/config"
)

func TestTrafficScopesUseSeparateCDTQuotaPools(t *testing.T) {
	scopes := trafficScopes(aliyun.TrafficResult{
		Source:     "cdt",
		MainlandGB: 10,
		OverseasGB: 50,
	}, 20, 200)

	if len(scopes) != 2 {
		t.Fatalf("len(scopes) = %d, want 2", len(scopes))
	}
	if scopes[0].Key != aliyun.CDTScopeMainland || scopes[0].LimitGB != 20 || scopes[0].UsagePercent != 50 {
		t.Fatalf("mainland scope = %#v, want 10/20/50%%", scopes[0])
	}
	if scopes[1].Key != aliyun.CDTScopeOverseas || scopes[1].LimitGB != 200 || scopes[1].UsagePercent != 25 {
		t.Fatalf("overseas scope = %#v, want 50/200/25%%", scopes[1])
	}
}

func TestTrafficRegionsPreserveCDTRegionUsage(t *testing.T) {
	regions := trafficRegions(aliyun.TrafficResult{
		Source: "cdt",
		RegionUsages: []aliyun.CDTRegionUsage{
			{RegionID: "ap-northeast-1", Scope: aliyun.CDTScopeOverseas, GB: 20},
			{RegionID: "ap-southeast-1", Scope: aliyun.CDTScopeOverseas, GB: 50},
		},
	})

	if len(regions) != 2 {
		t.Fatalf("len(regions) = %d, want 2", len(regions))
	}
	if regions[0].RegionID != "ap-southeast-1" || regions[0].Name != "新加坡" || regions[0].TrafficGB != 50 {
		t.Fatalf("first region = %#v, want Singapore 50GB", regions[0])
	}
	if regions[1].RegionID != "ap-northeast-1" || regions[1].Name != "日本" || regions[1].TrafficGB != 20 {
		t.Fatalf("second region = %#v, want Japan 20GB", regions[1])
	}
}

func TestRegionDisplayNameIncludesMainlandCity(t *testing.T) {
	if got := regionDisplayName("cn-hangzhou"); got != "华东1（杭州）" {
		t.Fatalf("regionDisplayName(cn-hangzhou) = %q, want 华东1（杭州）", got)
	}
}

func TestRegionTrafficSelectsInstanceRegionQuotaPool(t *testing.T) {
	traffic := aliyun.TrafficResult{Source: "cdt", MainlandGB: 18, OverseasGB: 40}

	scope, used, limit, percent := regionTraffic(traffic, "cn-hangzhou", 20, 200)
	if scope != aliyun.CDTScopeMainland || used != 18 || limit != 20 || percent != 90 {
		t.Fatalf("mainland region traffic = %s %.0f %.0f %.0f, want mainland 18 20 90", scope, used, limit, percent)
	}

	scope, used, limit, percent = regionTraffic(traffic, "ap-southeast-1", 20, 200)
	if scope != aliyun.CDTScopeOverseas || used != 40 || limit != 200 || percent != 20 {
		t.Fatalf("overseas region traffic = %s %.0f %.0f %.0f, want overseas 40 200 20", scope, used, limit, percent)
	}
}

func TestEffectiveStopModeUsesStopChargingForNonPrepaidInstances(t *testing.T) {
	if got := effectiveStopMode("StopCharging", InstanceSnapshot{InstanceChargeType: "PostPaid"}); got != "StopCharging" {
		t.Fatalf("postpaid StopCharging effective mode = %q, want StopCharging", got)
	}
	if got := effectiveStopMode("StopCharging", InstanceSnapshot{Spot: true, InstanceChargeType: "PostPaid"}); got != "StopCharging" {
		t.Fatalf("spot StopCharging effective mode = %q, want StopCharging", got)
	}
	if got := effectiveStopMode("StopCharging", InstanceSnapshot{InstanceChargeType: "PrePaid"}); got != "KeepCharging" {
		t.Fatalf("prepaid StopCharging effective mode = %q, want KeepCharging", got)
	}
	if got := effectiveStopMode("KeepCharging", InstanceSnapshot{InstanceChargeType: "PostPaid"}); got != "KeepCharging" {
		t.Fatalf("configured KeepCharging effective mode = %q, want KeepCharging", got)
	}
}

func TestNotificationFieldsWithTimeAddsLocalTime(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	originalLocal := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = originalLocal })
	now := time.Date(2026, 5, 19, 21, 8, 9, 0, location)

	fields := notificationFieldsWithTime(map[string]string{"账号": "Huhu"}, now)

	if fields["发送时间"] != "2026-05-19 21:08:09" {
		t.Fatalf("send time field = %q, want 2026-05-19 21:08:09", fields["发送时间"])
	}
	if _, ok := fields["时间"]; ok {
		t.Fatalf("legacy time field should not be present: %#v", fields)
	}
	if fields["账号"] != "Huhu" {
		t.Fatalf("account field = %q, want Huhu", fields["账号"])
	}
}

func TestUpdateSettingsPersistsDiscoveryAndNotificationSettings(t *testing.T) {
	state, err := OpenStateStore(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("OpenStateStore() error = %v", err)
	}
	service := NewService(testConfigWithTrafficPolicy("manual_only_when_exceeded"), state)
	update := service.Settings()
	update.Discovery.RegionRefreshInterval = "12h"
	update.Notification.Enabled = true
	update.Notification.NotifyEvents = []string{"traffic_exceeded", "error"}

	if err := service.UpdateSettings(update); err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}

	got := service.Settings()
	if got.Discovery.RegionRefreshInterval != "12h" {
		t.Fatalf("region refresh interval = %q, want 12h", got.Discovery.RegionRefreshInterval)
	}
	if !got.Notification.Enabled {
		t.Fatal("notification enabled = false, want true")
	}
	if len(got.Notification.NotifyEvents) != 2 || got.Notification.NotifyEvents[0] != "traffic_exceeded" {
		t.Fatalf("notify events = %#v", got.Notification.NotifyEvents)
	}
}

func TestUpdateSettingsWritesGlobalConfigFile(t *testing.T) {
	t.Setenv("EC_PASSWORD", "expanded-password")
	t.Setenv("EC_ACCOUNT_CN1_ACCESS_KEY_ID", "expanded-ak")
	t.Setenv("EC_ACCOUNT_CN1_ACCESS_KEY_SECRET", "expanded-sk")
	configPath := t.TempDir() + "/settings.yaml"
	configText := `
server:
  listen: ":8080"
  refresh_interval: "5m"
  request_timeout: "20s"
  password: "${EC_PASSWORD}"

discovery:
  region_refresh_interval: "24h"
  max_concurrency: 4

traffic:
  warning_percent: 95

logging:
  level: "info"

notification:
  enabled: false
  corpid: ""
  corpsecret: ""
  agentid: 0
  touser: []
  notify_events: ["auto_start", "error"]

keep_alive:
  enabled: true
  target: "spot_only"
  traffic_policy: "manual_only_when_exceeded"
  start_cooldown: "10m"
  stop_mode: "StopCharging"
  include_instance_ids: []

accounts:
  - name: "cn"
    site: "china"
    access_key_id: "${EC_ACCOUNT_CN1_ACCESS_KEY_ID}"
    access_key_secret: "${EC_ACCOUNT_CN1_ACCESS_KEY_SECRET}"
    regions: ["auto"]
    mainland_traffic_limit: 20
    overseas_traffic_limit: 200
`
	if err := os.WriteFile(configPath, []byte(configText), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	state, err := OpenStateStore(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("OpenStateStore() error = %v", err)
	}
	service := NewService(cfg, state, configPath)
	update := service.Settings()
	update.KeepAlive.StopMode = "KeepCharging"
	update.Server.RefreshInterval = "2m"
	update.Traffic.WarningPercent = 90

	if err := service.UpdateSettings(update); err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `stop_mode: "KeepCharging"`) {
		t.Fatalf("config was not updated:\n%s", text)
	}
	if !strings.Contains(text, `password: "${EC_PASSWORD}"`) || strings.Contains(text, "expanded-sk") {
		t.Fatalf("config did not preserve secret placeholders:\n%s", text)
	}
}

func testConfigWithTrafficPolicy(policy string) config.Config {
	return config.Config{
		Server: config.ServerConfig{
			Listen:          ":8080",
			RefreshInterval: 5 * time.Minute,
			RequestTimeout:  20 * time.Second,
			Password:        "secret",
		},
		Discovery: config.DiscoveryConfig{
			RegionRefreshInterval: 24 * time.Hour,
			MaxConcurrency:        4,
		},
		Traffic: config.TrafficConfig{WarningPercent: 95},
		Logging: config.LoggingConfig{Level: "info"},
		Notification: config.NotificationConfig{
			NotifyEvents: []string{"auto_start", "manual_start", "manual_stop", "manual_required", "traffic_exceeded", "error"},
		},
		KeepAlive: config.KeepAliveConfig{
			Enabled:       true,
			Target:        "spot_only",
			TrafficPolicy: policy,
			StartCooldown: 10 * time.Minute,
			StopMode:      "StopCharging",
		},
		Accounts: []config.AccountConfig{
			{
				Name:                 "test",
				Site:                 "china",
				AccessKeyID:          "ak",
				AccessKeySecret:      "sk",
				Regions:              []string{"auto"},
				MainlandTrafficLimit: 20,
				OverseasTrafficLimit: 200,
			},
		},
	}
}
