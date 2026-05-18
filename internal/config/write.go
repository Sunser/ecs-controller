package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func WriteGlobalSettings(path string, cfg Config) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("config path is empty")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data = nil
	} else if err != nil {
		return err
	}
	mode := os.FileMode(0600)
	if stat, err := os.Stat(path); err == nil {
		mode = stat.Mode()
	}
	original := strings.ReplaceAll(string(data), "\r\n", "\n")
	accounts := sectionBlock(original, "accounts")
	content := renderGlobalSettings(original, cfg)
	if strings.TrimSpace(accounts) != "" {
		content = strings.TrimRight(content, "\n") + "\n\n" + strings.TrimLeft(accounts, "\n")
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

func renderGlobalSettings(original string, cfg Config) string {
	var builder strings.Builder
	builder.WriteString("server:\n")
	writeKeyValue(&builder, "listen", rawOrQuoted(original, "server", "listen", cfg.Server.Listen))
	writeKeyValue(&builder, "refresh_interval", quote(formatDuration(cfg.Server.RefreshInterval.String())))
	writeKeyValue(&builder, "request_timeout", quote(formatDuration(cfg.Server.RequestTimeout.String())))
	writeKeyValue(&builder, "password", rawOrEnvPlaceholder(original, "server", "password", "EC_PASSWORD", cfg.Server.Password))
	writeKeyValue(&builder, "state_path", rawOrQuoted(original, "server", "state_path", cfg.Server.StatePath))

	builder.WriteString("\ndiscovery:\n")
	writeKeyValue(&builder, "region_refresh_interval", quote(formatDuration(cfg.Discovery.RegionRefreshInterval.String())))
	writeKeyValue(&builder, "max_concurrency", rawOrString(original, "discovery", "max_concurrency", strconv.Itoa(cfg.Discovery.MaxConcurrency)))

	builder.WriteString("\ntraffic:\n")
	writeKeyValue(&builder, "warning_percent", formatFloat(cfg.Traffic.WarningPercent))

	builder.WriteString("\nlogging:\n")
	writeKeyValue(&builder, "level", quote(cfg.Logging.Level))

	builder.WriteString("\nnotification:\n")
	writeKeyValue(&builder, "enabled", strconv.FormatBool(cfg.Notification.Enabled))
	writeKeyValue(&builder, "corpid", rawOrEnvPlaceholder(original, "notification", "corpid", "EC_WECHAT_CORPID", cfg.Notification.WeChatCorpID))
	writeKeyValue(&builder, "corpsecret", rawOrEnvPlaceholder(original, "notification", "corpsecret", "EC_WECHAT_CORPSECRET", cfg.Notification.WeChatCorpSecret))
	writeKeyValue(&builder, "agentid", rawOrString(original, "notification", "agentid", "${EC_WECHAT_AGENTID}"))
	writeKeyValue(&builder, "touser", rawOrString(original, "notification", "touser", quote("${EC_WECHAT_TOUSER}")))
	writeKeyValue(&builder, "notify_events", renderList(cfg.Notification.NotifyEvents))

	builder.WriteString("\nkeep_alive:\n")
	writeKeyValue(&builder, "enabled", strconv.FormatBool(cfg.KeepAlive.Enabled))
	writeKeyValue(&builder, "target", quote(cfg.KeepAlive.Target))
	writeKeyValue(&builder, "traffic_policy", quote(cfg.KeepAlive.TrafficPolicy))
	writeKeyValue(&builder, "start_cooldown", quote(formatDuration(cfg.KeepAlive.StartCooldown.String())))
	writeKeyValue(&builder, "stop_mode", quote(cfg.KeepAlive.StopMode))
	writeKeyValue(&builder, "include_instance_ids", renderList(cfg.KeepAlive.IncludeInstanceIDs))
	return builder.String()
}

func writeKeyValue(builder *strings.Builder, key, value string) {
	builder.WriteString("  ")
	builder.WriteString(key)
	builder.WriteString(": ")
	builder.WriteString(value)
	builder.WriteByte('\n')
}

func rawOrQuoted(original, section, key, fallback string) string {
	if raw := rawSectionValue(original, section, key); raw != "" {
		return raw
	}
	return quote(fallback)
}

func rawOrEnvPlaceholder(original, section, key, envName, fallback string) string {
	if raw := rawSectionValue(original, section, key); raw != "" {
		return raw
	}
	if envName != "" && fallback != "" {
		return quote("${" + envName + "}")
	}
	return quote(fallback)
}

func rawOrString(original, section, key, fallback string) string {
	if raw := rawSectionValue(original, section, key); raw != "" {
		return raw
	}
	return fallback
}

func rawOrList(original, section, key string, fallback []string) string {
	if raw := rawSectionValue(original, section, key); raw != "" {
		return raw
	}
	return renderList(fallback)
}

func rawSectionValue(text, section, key string) string {
	current := ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && strings.HasSuffix(trimmed, ":") {
			current = strings.TrimSuffix(trimmed, ":")
			continue
		}
		if current != section {
			continue
		}
		parsedKey, value, ok := parseKeyValue(trimmed)
		if ok && parsedKey == key {
			return value
		}
	}
	return ""
}

func sectionBlock(text, section string) string {
	lines := strings.Split(text, "\n")
	position := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && trimmed == section+":" {
			return text[position:]
		}
		position += len(line) + 1
	}
	return ""
}

func quote(value string) string {
	return strconv.Quote(value)
}

func renderList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parts = append(parts, quote(value))
	}
	if len(parts) == 0 {
		return "[]"
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func formatDuration(value string) string {
	if strings.HasSuffix(value, "h0m0s") {
		return strings.TrimSuffix(value, "0m0s")
	}
	if strings.HasSuffix(value, "m0s") {
		return strings.TrimSuffix(value, "0s")
	}
	return value
}
