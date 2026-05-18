package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs-controller/internal/config"
)

func TestWriteGlobalSettingsPreservesSecretPlaceholdersAndAccounts(t *testing.T) {
	t.Setenv("EC_PASSWORD", "expanded-password")
	t.Setenv("EC_ACCOUNT_CN1_ACCESS_KEY_ID", "expanded-ak")
	t.Setenv("EC_ACCOUNT_CN1_ACCESS_KEY_SECRET", "expanded-sk")
	t.Setenv("EC_WECHAT_CORPSECRET", "expanded-corp-secret")
	path := filepath.Join(t.TempDir(), "settings.yaml")
	original := `
server:
  listen: ":8080"
  refresh_interval: "5m"
  request_timeout: "20s"
  password: "${EC_PASSWORD}"
  state_path: "/data/state.json"

discovery:
  region_refresh_interval: "24h"
  max_concurrency: 4

traffic:
  warning_percent: 95

logging:
  level: "info"

notification:
  enabled: false
  corpid: "ww123"
  corpsecret: "${EC_WECHAT_CORPSECRET}"
  agentid: 1000002
  touser: ["user-a", "user-b"]
  notify_events: ["auto_start", "error"]

keep_alive:
  enabled: true
  target: "spot_only"
  traffic_policy: "manual_only_when_exceeded"
  start_cooldown: "10m"
  stop_mode: "StopCharging"
  include_instance_ids: []

accounts:
  - name: "aliyun-cn"
    site: "china"
    access_key_id: "${EC_ACCOUNT_CN1_ACCESS_KEY_ID}"
    access_key_secret: "${EC_ACCOUNT_CN1_ACCESS_KEY_SECRET}"
    regions: ["auto"]
    mainland_traffic_limit: 20
    overseas_traffic_limit: 200
`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	cfg.Server.RefreshInterval = 2 * time.Minute
	cfg.Traffic.WarningPercent = 88
	cfg.Logging.Level = "debug"
	cfg.Notification.Enabled = true
	cfg.Notification.NotifyEvents = []string{"traffic_exceeded", "error"}
	cfg.KeepAlive.StopMode = "KeepCharging"
	cfg.KeepAlive.IncludeInstanceIDs = []string{"i-1", "i-2"}

	if err := config.WriteGlobalSettings(path, cfg); err != nil {
		t.Fatalf("WriteGlobalSettings() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		`password: "${EC_PASSWORD}"`,
		`corpsecret: "${EC_WECHAT_CORPSECRET}"`,
		`agentid: 1000002`,
		`touser: ["user-a", "user-b"]`,
		`access_key_secret: "${EC_ACCOUNT_CN1_ACCESS_KEY_SECRET}"`,
		`refresh_interval: "2m"`,
		`warning_percent: 88`,
		`level: "debug"`,
		`stop_mode: "KeepCharging"`,
		`include_instance_ids: ["i-1", "i-2"]`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("written config missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "expanded-password") || strings.Contains(text, "expanded-sk") || strings.Contains(text, "expanded-corp-secret") {
		t.Fatalf("written config leaked expanded secret:\n%s", text)
	}
	if strings.Contains(text, "instance_refresh_interval") {
		t.Fatalf("written config kept legacy instance_refresh_interval:\n%s", text)
	}

	reloaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile(rewritten) error = %v", err)
	}
	if reloaded.KeepAlive.StopMode != "KeepCharging" {
		t.Fatalf("reloaded stop mode = %q", reloaded.KeepAlive.StopMode)
	}
	if reloaded.Server.RefreshInterval != 2*time.Minute {
		t.Fatalf("reloaded refresh interval = %s", reloaded.Server.RefreshInterval)
	}
}
