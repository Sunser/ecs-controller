package monitor

import (
	"fmt"
	"strings"
	"time"

	"ecs-controller/internal/applog"
	"ecs-controller/internal/config"
)

type SettingsView struct {
	Server       ServerSettingsView       `json:"server"`
	Discovery    DiscoverySettingsView    `json:"discovery"`
	Traffic      TrafficSettingsView      `json:"traffic"`
	KeepAlive    KeepAliveSettingsView    `json:"keep_alive"`
	Logging      LoggingSettingsView      `json:"logging"`
	Notification NotificationSettingsView `json:"notification"`
}

type ServerSettingsView struct {
	RefreshInterval string `json:"refresh_interval"`
	RequestTimeout  string `json:"request_timeout"`
}

type DiscoverySettingsView struct {
	RegionRefreshInterval string `json:"region_refresh_interval"`
}

type TrafficSettingsView struct {
	WarningPercent float64 `json:"warning_percent"`
	ExceededAction string  `json:"exceeded_action"`
}

type KeepAliveSettingsView struct {
	Enabled            bool     `json:"enabled"`
	Target             string   `json:"target"`
	TrafficPolicy      string   `json:"traffic_policy"`
	StartCooldown      string   `json:"start_cooldown"`
	StopMode           string   `json:"stop_mode"`
	IncludeInstanceIDs []string `json:"include_instance_ids"`
}

type LoggingSettingsView struct {
	Level string `json:"level"`
}

type NotificationSettingsView struct {
	Enabled                      bool     `json:"enabled"`
	NotifyEvents                 []string `json:"notify_events"`
	ManualRequiredNotifyInterval string   `json:"manual_required_notify_interval"`
}

func (s *Service) Settings() SettingsView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settingsLocked()
}

func (s *Service) UpdateSettings(update SettingsView) error {
	if err := validateSettings(update); err != nil {
		return err
	}

	cooldown, err := time.ParseDuration(update.KeepAlive.StartCooldown)
	if err != nil {
		return fmt.Errorf("start_cooldown format is invalid")
	}
	refreshInterval, err := time.ParseDuration(update.Server.RefreshInterval)
	if err != nil {
		return fmt.Errorf("refresh_interval format is invalid")
	}
	requestTimeout, err := time.ParseDuration(update.Server.RequestTimeout)
	if err != nil {
		return fmt.Errorf("request_timeout format is invalid")
	}
	regionRefreshInterval, err := time.ParseDuration(update.Discovery.RegionRefreshInterval)
	if err != nil {
		return fmt.Errorf("region_refresh_interval format is invalid")
	}
	manualRequiredNotifyInterval, err := time.ParseDuration(update.Notification.ManualRequiredNotifyInterval)
	if err != nil {
		return fmt.Errorf("manual_required_notify_interval format is invalid")
	}

	s.mu.Lock()
	s.cfg.Server.RefreshInterval = refreshInterval
	s.cfg.Server.RequestTimeout = requestTimeout
	s.cfg.Discovery.RegionRefreshInterval = regionRefreshInterval
	s.cfg.Traffic.WarningPercent = update.Traffic.WarningPercent
	s.cfg.Traffic.ExceededAction = update.Traffic.ExceededAction
	s.cfg.KeepAlive.Enabled = update.KeepAlive.Enabled
	s.cfg.KeepAlive.Target = update.KeepAlive.Target
	s.cfg.KeepAlive.TrafficPolicy = update.KeepAlive.TrafficPolicy
	s.cfg.KeepAlive.StartCooldown = cooldown
	s.cfg.KeepAlive.StopMode = update.KeepAlive.StopMode
	s.cfg.KeepAlive.IncludeInstanceIDs = cleanList(update.KeepAlive.IncludeInstanceIDs)
	s.cfg.Logging.Level = update.Logging.Level
	s.cfg.Notification.Enabled = update.Notification.Enabled
	s.cfg.Notification.NotifyEvents = cleanList(update.Notification.NotifyEvents)
	s.cfg.Notification.ManualRequiredNotifyInterval = manualRequiredNotifyInterval
	view := s.settingsLocked()
	updatedConfig := copyConfig(s.cfg)
	s.mu.Unlock()
	applog.SetLevel(view.Logging.Level)

	if s.configPath == "" {
		return nil
	}
	return config.WriteGlobalSettings(s.configPath, updatedConfig)
}

func (s *Service) settingsLocked() SettingsView {
	return SettingsView{
		Server: ServerSettingsView{
			RefreshInterval: formatDuration(s.cfg.Server.RefreshInterval),
			RequestTimeout:  formatDuration(s.cfg.Server.RequestTimeout),
		},
		Discovery: DiscoverySettingsView{
			RegionRefreshInterval: formatDuration(s.cfg.Discovery.RegionRefreshInterval),
		},
		Traffic: TrafficSettingsView{
			WarningPercent: s.cfg.Traffic.WarningPercent,
			ExceededAction: s.cfg.Traffic.ExceededAction,
		},
		KeepAlive: KeepAliveSettingsView{
			Enabled:            s.cfg.KeepAlive.Enabled,
			Target:             s.cfg.KeepAlive.Target,
			TrafficPolicy:      s.cfg.KeepAlive.TrafficPolicy,
			StartCooldown:      formatDuration(s.cfg.KeepAlive.StartCooldown),
			StopMode:           s.cfg.KeepAlive.StopMode,
			IncludeInstanceIDs: append([]string(nil), s.cfg.KeepAlive.IncludeInstanceIDs...),
		},
		Logging: LoggingSettingsView{
			Level: s.cfg.Logging.Level,
		},
		Notification: NotificationSettingsView{
			Enabled:                      s.cfg.Notification.Enabled,
			NotifyEvents:                 append([]string(nil), s.cfg.Notification.NotifyEvents...),
			ManualRequiredNotifyInterval: formatDuration(s.cfg.Notification.ManualRequiredNotifyInterval),
		},
	}
}

func formatDuration(duration time.Duration) string {
	text := duration.String()
	if strings.HasSuffix(text, "h0m0s") {
		return strings.TrimSuffix(text, "0m0s")
	}
	if strings.HasSuffix(text, "m0s") {
		return strings.TrimSuffix(text, "0s")
	}
	return text
}

func validateSettings(settings SettingsView) error {
	if settings.Traffic.WarningPercent <= 0 {
		return fmt.Errorf("warning_percent must be greater than 0")
	}
	switch settings.Traffic.ExceededAction {
	case "notify_only", "notify_and_stop":
	default:
		return fmt.Errorf("unsupported traffic.exceeded_action: %s", settings.Traffic.ExceededAction)
	}
	if _, err := time.ParseDuration(settings.Server.RefreshInterval); err != nil {
		return fmt.Errorf("refresh_interval format is invalid")
	}
	refreshInterval, _ := time.ParseDuration(settings.Server.RefreshInterval)
	if refreshInterval <= 0 {
		return fmt.Errorf("refresh_interval must be greater than 0")
	}
	if _, err := time.ParseDuration(settings.Server.RequestTimeout); err != nil {
		return fmt.Errorf("request_timeout format is invalid")
	}
	requestTimeout, _ := time.ParseDuration(settings.Server.RequestTimeout)
	if requestTimeout <= 0 {
		return fmt.Errorf("request_timeout must be greater than 0")
	}
	if _, err := time.ParseDuration(settings.Discovery.RegionRefreshInterval); err != nil {
		return fmt.Errorf("region_refresh_interval format is invalid")
	}
	regionRefreshInterval, _ := time.ParseDuration(settings.Discovery.RegionRefreshInterval)
	if regionRefreshInterval <= 0 {
		return fmt.Errorf("region_refresh_interval must be greater than 0")
	}
	switch settings.KeepAlive.Target {
	case "spot_only", "all", "include_list", "disabled":
	default:
		return fmt.Errorf("unsupported keep_alive target: %s", settings.KeepAlive.Target)
	}
	switch settings.KeepAlive.TrafficPolicy {
	case "manual_only_when_exceeded", "ignore_limit", "pause_when_exceeded":
	default:
		return fmt.Errorf("unsupported traffic_policy: %s", settings.KeepAlive.TrafficPolicy)
	}
	if settings.Traffic.ExceededAction == "notify_and_stop" && settings.KeepAlive.TrafficPolicy == "ignore_limit" {
		return fmt.Errorf("traffic.exceeded_action=notify_and_stop cannot be used with traffic_policy=ignore_limit")
	}
	if _, err := time.ParseDuration(settings.KeepAlive.StartCooldown); err != nil {
		return fmt.Errorf("start_cooldown format is invalid")
	}
	switch settings.KeepAlive.StopMode {
	case "StopCharging", "KeepCharging":
	default:
		return fmt.Errorf("unsupported stop_mode: %s", settings.KeepAlive.StopMode)
	}
	switch settings.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("unsupported logging level: %s", settings.Logging.Level)
	}
	if err := validateNotifyEvents(settings.Notification.NotifyEvents); err != nil {
		return err
	}
	if _, err := time.ParseDuration(settings.Notification.ManualRequiredNotifyInterval); err != nil {
		return fmt.Errorf("manual_required_notify_interval format is invalid")
	}
	manualRequiredNotifyInterval, _ := time.ParseDuration(settings.Notification.ManualRequiredNotifyInterval)
	if manualRequiredNotifyInterval <= 0 {
		return fmt.Errorf("manual_required_notify_interval must be greater than 0")
	}
	return nil
}

func cleanList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func validateNotifyEvents(events []string) error {
	for _, event := range events {
		switch event {
		case "all", "auto_start", "manual_start", "manual_stop", "manual_required", "traffic_exceeded", "traffic_stop", "error":
		default:
			return fmt.Errorf("unsupported notification event: %s", event)
		}
	}
	return nil
}
