package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	SiteChina         = "china"
	SiteInternational = "international"
)

type Config struct {
	Server       ServerConfig
	Discovery    DiscoveryConfig
	Traffic      TrafficConfig
	KeepAlive    KeepAliveConfig
	Logging      LoggingConfig
	Notification NotificationConfig
	Accounts     []AccountConfig
}

type ServerConfig struct {
	Listen          string
	RefreshInterval time.Duration
	RequestTimeout  time.Duration
	Password        string
}

type DiscoveryConfig struct {
	RegionRefreshInterval time.Duration
	MaxConcurrency        int
}

type TrafficConfig struct {
	WarningPercent float64
}

type LoggingConfig struct {
	Level string
}

type NotificationConfig struct {
	Enabled          bool
	WeChatCorpID     string
	WeChatCorpSecret string
	WeChatAgentID    int
	WeChatToUser     []string
	NotifyEvents     []string
}

type KeepAliveConfig struct {
	Enabled            bool
	Target             string
	TrafficPolicy      string
	StartCooldown      time.Duration
	StopMode           string
	IncludeInstanceIDs []string
}

type AccountConfig struct {
	Name                 string
	Site                 string
	AccessKeyID          string
	AccessKeySecret      string
	Regions              []string
	MainlandTrafficLimit float64
	OverseasTrafficLimit float64
}

func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return LoadBytes(nil)
	}
	if err != nil {
		return Config{}, err
	}
	return loadBytes(data, false)
}

func LoadBytes(data []byte) (Config, error) {
	return loadBytes(data, true)
}

func loadBytes(data []byte, applyGlobalEnv bool) (Config, error) {
	cfg := defaultConfig()
	lines := strings.Split(os.ExpandEnv(string(data)), "\n")
	section := ""
	var currentAccount *AccountConfig

	for index, raw := range lines {
		line := stripComment(strings.TrimRight(raw, "\r"))
		if strings.TrimSpace(line) == "" {
			continue
		}

		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(line, " ") && strings.HasSuffix(trimmed, ":") {
			section = strings.TrimSuffix(trimmed, ":")
			currentAccount = nil
			continue
		}

		switch section {
		case "server":
			key, value, ok := parseKeyValue(trimmed)
			if !ok {
				return Config{}, lineError(index, "server 配置格式错误")
			}
			if err := applyServer(&cfg.Server, key, value); err != nil {
				return Config{}, lineError(index, err.Error())
			}
		case "discovery":
			key, value, ok := parseKeyValue(trimmed)
			if !ok {
				return Config{}, lineError(index, "discovery 配置格式错误")
			}
			if err := applyDiscovery(&cfg.Discovery, key, value); err != nil {
				return Config{}, lineError(index, err.Error())
			}
		case "traffic":
			key, value, ok := parseKeyValue(trimmed)
			if !ok {
				return Config{}, lineError(index, "traffic 配置格式错误")
			}
			if err := applyTraffic(&cfg.Traffic, key, value); err != nil {
				return Config{}, lineError(index, err.Error())
			}
		case "logging":
			key, value, ok := parseKeyValue(trimmed)
			if !ok {
				return Config{}, lineError(index, "logging 配置格式错误")
			}
			if err := applyLogging(&cfg.Logging, key, value); err != nil {
				return Config{}, lineError(index, err.Error())
			}
		case "notification":
			key, value, ok := parseKeyValue(trimmed)
			if !ok {
				return Config{}, lineError(index, "notification 配置格式错误")
			}
			if err := applyNotification(&cfg.Notification, key, value); err != nil {
				return Config{}, lineError(index, err.Error())
			}
		case "keep_alive":
			key, value, ok := parseKeyValue(trimmed)
			if !ok {
				return Config{}, lineError(index, "keep_alive 配置格式错误")
			}
			if err := applyKeepAlive(&cfg.KeepAlive, key, value); err != nil {
				return Config{}, lineError(index, err.Error())
			}
		case "accounts":
			if strings.HasPrefix(trimmed, "- ") {
				account := AccountConfig{Site: SiteChina, Regions: []string{"auto"}}
				cfg.Accounts = append(cfg.Accounts, account)
				currentAccount = &cfg.Accounts[len(cfg.Accounts)-1]
				inline := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
				if inline != "" {
					key, value, ok := parseKeyValue(inline)
					if !ok {
						return Config{}, lineError(index, "accounts 列表项格式错误")
					}
					if err := applyAccount(currentAccount, key, value); err != nil {
						return Config{}, lineError(index, err.Error())
					}
				}
				continue
			}
			if currentAccount == nil {
				return Config{}, lineError(index, "账号字段必须写在 '- name:' 后面")
			}
			key, value, ok := parseKeyValue(trimmed)
			if !ok {
				return Config{}, lineError(index, "账号配置格式错误")
			}
			if err := applyAccount(currentAccount, key, value); err != nil {
				return Config{}, lineError(index, err.Error())
			}
		default:
			return Config{}, lineError(index, fmt.Sprintf("未知配置段 %q", section))
		}
	}

	if err := applyEnv(&cfg, applyGlobalEnv); err != nil {
		return Config{}, err
	}
	if err := validate(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Listen:          ":8080",
			RefreshInterval: 5 * time.Minute,
			RequestTimeout:  20 * time.Second,
		},
		Discovery: DiscoveryConfig{
			RegionRefreshInterval: 24 * time.Hour,
			MaxConcurrency:        4,
		},
		Traffic: TrafficConfig{WarningPercent: 95},
		Logging: LoggingConfig{Level: "info"},
		Notification: NotificationConfig{
			NotifyEvents: []string{"auto_start", "manual_start", "manual_stop", "manual_required", "traffic_exceeded", "error"},
		},
		KeepAlive: KeepAliveConfig{
			Enabled:       true,
			Target:        "spot_only",
			TrafficPolicy: "manual_only_when_exceeded",
			StartCooldown: 10 * time.Minute,
			StopMode:      "StopCharging",
		},
	}
}

func applyServer(cfg *ServerConfig, key, value string) error {
	switch key {
	case "listen":
		cfg.Listen = scalar(value)
	case "refresh_interval":
		duration, err := parseDuration(value)
		if err != nil {
			return err
		}
		cfg.RefreshInterval = duration
	case "request_timeout":
		duration, err := parseDuration(value)
		if err != nil {
			return err
		}
		cfg.RequestTimeout = duration
	case "password":
		cfg.Password = scalar(value)
	default:
		return fmt.Errorf("未知 server 字段 %q", key)
	}
	return nil
}

func applyDiscovery(cfg *DiscoveryConfig, key, value string) error {
	switch key {
	case "region_refresh_interval":
		duration, err := parseDuration(value)
		if err != nil {
			return err
		}
		cfg.RegionRefreshInterval = duration
	case "instance_refresh_interval":
		// 历史字段：当前后台主巡检统一由 server.refresh_interval 控制。
		_, err := parseDuration(value)
		return err
	case "max_concurrency":
		number, err := strconv.Atoi(scalar(value))
		if err != nil {
			return fmt.Errorf("max_concurrency 必须是整数")
		}
		cfg.MaxConcurrency = number
	default:
		return fmt.Errorf("未知 discovery 字段 %q", key)
	}
	return nil
}

func applyTraffic(cfg *TrafficConfig, key, value string) error {
	switch key {
	case "warning_percent":
		number, err := strconv.ParseFloat(scalar(value), 64)
		if err != nil {
			return fmt.Errorf("warning_percent 必须是数字")
		}
		cfg.WarningPercent = number
	default:
		return fmt.Errorf("未知 traffic 字段 %q", key)
	}
	return nil
}

func applyLogging(cfg *LoggingConfig, key, value string) error {
	switch key {
	case "level":
		cfg.Level = strings.ToLower(scalar(value))
	default:
		return fmt.Errorf("未知 logging 字段 %q", key)
	}
	return nil
}

func applyNotification(cfg *NotificationConfig, key, value string) error {
	switch key {
	case "enabled":
		cfg.Enabled = parseBool(value)
	case "corpid":
		cfg.WeChatCorpID = scalar(value)
	case "corpsecret":
		cfg.WeChatCorpSecret = scalar(value)
	case "agentid":
		if scalar(value) == "" {
			cfg.WeChatAgentID = 0
			return nil
		}
		number, err := strconv.Atoi(scalar(value))
		if err != nil {
			return fmt.Errorf("agentid 必须是整数")
		}
		cfg.WeChatAgentID = number
	case "touser", "to_user":
		cfg.WeChatToUser = parseList(value)
	case "notify_events":
		cfg.NotifyEvents = parseList(value)
	default:
		return fmt.Errorf("未知 notification 字段 %q", key)
	}
	return nil
}

func applyKeepAlive(cfg *KeepAliveConfig, key, value string) error {
	switch key {
	case "enabled":
		cfg.Enabled = parseBool(value)
	case "target":
		cfg.Target = scalar(value)
	case "traffic_policy":
		cfg.TrafficPolicy = scalar(value)
	case "start_cooldown":
		duration, err := parseDuration(value)
		if err != nil {
			return err
		}
		cfg.StartCooldown = duration
	case "stop_mode":
		cfg.StopMode = scalar(value)
	case "include_instance_ids":
		cfg.IncludeInstanceIDs = parseList(value)
	default:
		return fmt.Errorf("未知 keep_alive 字段 %q", key)
	}
	return nil
}

func applyAccount(cfg *AccountConfig, key, value string) error {
	switch key {
	case "name":
		cfg.Name = scalar(value)
	case "site":
		cfg.Site = scalar(value)
	case "access_key_id":
		cfg.AccessKeyID = scalar(value)
	case "access_key_secret":
		cfg.AccessKeySecret = scalar(value)
	case "regions":
		cfg.Regions = parseList(value)
	case "mainland_traffic_limit":
		number, err := strconv.ParseFloat(scalar(value), 64)
		if err != nil {
			return fmt.Errorf("mainland_traffic_limit 必须是数字，单位为 GB")
		}
		cfg.MainlandTrafficLimit = number
	case "overseas_traffic_limit":
		number, err := strconv.ParseFloat(scalar(value), 64)
		if err != nil {
			return fmt.Errorf("overseas_traffic_limit 必须是数字，单位为 GB")
		}
		cfg.OverseasTrafficLimit = number
	default:
		return fmt.Errorf("未知账号字段 %q", key)
	}
	return nil
}

func validate(cfg *Config) error {
	if len(cfg.Accounts) == 0 {
		return errors.New("至少需要配置一个阿里云账号")
	}
	if cfg.Server.Listen == "" {
		return errors.New("server.listen 不能为空")
	}
	if cfg.Server.RefreshInterval <= 0 {
		return errors.New("server.refresh_interval 必须大于 0")
	}
	if cfg.Server.RequestTimeout <= 0 {
		return errors.New("server.request_timeout 必须大于 0")
	}
	if cfg.Discovery.RegionRefreshInterval <= 0 {
		return errors.New("discovery.region_refresh_interval 必须大于 0")
	}
	if cfg.Traffic.WarningPercent <= 0 {
		return errors.New("traffic.warning_percent 必须大于 0")
	}
	if cfg.KeepAlive.Target == "" {
		cfg.KeepAlive.Target = "spot_only"
	}
	if cfg.KeepAlive.TrafficPolicy == "" {
		cfg.KeepAlive.TrafficPolicy = "manual_only_when_exceeded"
	}
	if cfg.KeepAlive.StopMode == "" {
		cfg.KeepAlive.StopMode = "StopCharging"
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	switch cfg.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("不支持的 logging.level: %s", cfg.Logging.Level)
	}
	if cfg.Notification.NotifyEvents == nil {
		cfg.Notification.NotifyEvents = []string{"auto_start", "manual_start", "manual_stop", "manual_required", "traffic_exceeded", "error"}
	}
	if err := validateNotifyEvents(cfg.Notification.NotifyEvents); err != nil {
		return err
	}
	switch cfg.KeepAlive.TrafficPolicy {
	case "manual_only_when_exceeded", "ignore_limit", "pause_when_exceeded":
	default:
		return fmt.Errorf("不支持的 traffic_policy: %s", cfg.KeepAlive.TrafficPolicy)
	}
	switch cfg.KeepAlive.StopMode {
	case "StopCharging", "KeepCharging":
	default:
		return fmt.Errorf("不支持的 keep_alive.stop_mode: %s", cfg.KeepAlive.StopMode)
	}
	switch cfg.KeepAlive.Target {
	case "spot_only", "all", "include_list", "disabled":
	default:
		return fmt.Errorf("不支持的 keep_alive.target: %s", cfg.KeepAlive.Target)
	}
	for index := range cfg.Accounts {
		account := &cfg.Accounts[index]
		if account.MainlandTrafficLimit <= 0 {
			account.MainlandTrafficLimit = 20
		}
		if account.OverseasTrafficLimit <= 0 {
			account.OverseasTrafficLimit = 200
		}
		if account.Name == "" {
			account.Name = fmt.Sprintf("account-%d", index+1)
		}
		if account.Site == "" {
			account.Site = SiteChina
		}
		if account.Site != SiteChina && account.Site != SiteInternational {
			return fmt.Errorf("账号 %s 的 site 只能是 china 或 international", account.Name)
		}
		if account.AccessKeyID == "" || account.AccessKeySecret == "" {
			return fmt.Errorf("账号 %s 缺少 access_key_id 或 access_key_secret", account.Name)
		}
		if len(account.Regions) == 0 {
			account.Regions = []string{"auto"}
		}
		if account.MainlandTrafficLimit <= 0 {
			return fmt.Errorf("账号 %s 的 mainland_traffic_limit 必须大于 0，单位为 GB", account.Name)
		}
		if account.OverseasTrafficLimit <= 0 {
			return fmt.Errorf("账号 %s 的 overseas_traffic_limit 必须大于 0，单位为 GB", account.Name)
		}
	}
	return nil
}

func validateNotifyEvents(events []string) error {
	for _, event := range events {
		switch event {
		case "all", "auto_start", "manual_start", "manual_stop", "manual_required", "traffic_exceeded", "error":
		default:
			return fmt.Errorf("不支持的 notification.notify_events: %s", event)
		}
	}
	return nil
}

func applyEnv(cfg *Config, includeGlobal bool) error {
	if includeGlobal {
		if value, ok := lookupEnvString("EC_LISTEN"); ok {
			cfg.Server.Listen = value
		}
		if value, ok, err := lookupEnvDuration("EC_REFRESH_INTERVAL"); err != nil {
			return err
		} else if ok {
			cfg.Server.RefreshInterval = value
		}
		if value, ok, err := lookupEnvDuration("EC_REQUEST_TIMEOUT"); err != nil {
			return err
		} else if ok {
			cfg.Server.RequestTimeout = value
		}
		if value, ok := lookupEnvString("EC_PASSWORD"); ok {
			cfg.Server.Password = value
		}
		if value, ok, err := lookupEnvDuration("EC_REGION_REFRESH_INTERVAL"); err != nil {
			return err
		} else if ok {
			cfg.Discovery.RegionRefreshInterval = value
		}
		if value, ok, err := lookupEnvInt("EC_MAX_CONCURRENCY"); err != nil {
			return err
		} else if ok {
			cfg.Discovery.MaxConcurrency = value
		}
		if value, ok, err := lookupEnvFloat("EC_TRAFFIC_WARNING_PERCENT"); err != nil {
			return err
		} else if ok {
			cfg.Traffic.WarningPercent = value
		}
		if value, ok := lookupEnvString("EC_LOG_LEVEL"); ok {
			cfg.Logging.Level = strings.ToLower(value)
		}
		if value, ok := lookupEnvBool("EC_NOTIFY_ENABLED"); ok {
			cfg.Notification.Enabled = value
		}
		if value, ok := lookupEnvString("EC_WECHAT_CORPID"); ok {
			cfg.Notification.WeChatCorpID = value
		}
		if value, ok := lookupEnvString("EC_WECHAT_CORPSECRET"); ok {
			cfg.Notification.WeChatCorpSecret = value
		}
		if value, ok, err := lookupEnvInt("EC_WECHAT_AGENTID"); err != nil {
			return err
		} else if ok {
			cfg.Notification.WeChatAgentID = value
		}
		if value, ok := lookupEnvList("EC_WECHAT_TOUSER"); ok {
			cfg.Notification.WeChatToUser = value
		}
		if value, ok := lookupEnvList("EC_NOTIFY_EVENTS"); ok {
			cfg.Notification.NotifyEvents = value
		}
		if value, ok := lookupEnvBool("EC_KEEP_ALIVE_ENABLED"); ok {
			cfg.KeepAlive.Enabled = value
		}
		if value, ok := lookupEnvString("EC_KEEP_ALIVE_TARGET"); ok {
			cfg.KeepAlive.Target = value
		}
		if value, ok := lookupEnvString("EC_TRAFFIC_POLICY"); ok {
			cfg.KeepAlive.TrafficPolicy = value
		}
		if value, ok, err := lookupEnvDuration("EC_START_COOLDOWN"); err != nil {
			return err
		} else if ok {
			cfg.KeepAlive.StartCooldown = value
		}
		if value, ok := lookupEnvString("EC_STOP_MODE"); ok {
			cfg.KeepAlive.StopMode = value
		}
		if value, ok := lookupEnvList("EC_INCLUDE_INSTANCE_IDS"); ok {
			cfg.KeepAlive.IncludeInstanceIDs = value
		}
	}
	if aliases, ok := lookupEnvList("EC_ACCOUNTS"); ok {
		accounts, err := accountsFromEnv(aliases)
		if err != nil {
			return err
		}
		cfg.Accounts = accounts
	}
	return nil
}

func accountsFromEnv(aliases []string) ([]AccountConfig, error) {
	accounts := make([]AccountConfig, 0, len(aliases))
	for _, alias := range aliases {
		key := envKey(alias)
		prefix := "EC_ACCOUNT_" + key + "_"
		account := AccountConfig{
			Name:    alias,
			Site:    SiteChina,
			Regions: []string{"auto"},
		}
		if value, ok := lookupEnvString(prefix + "NAME"); ok {
			account.Name = value
		}
		if value, ok := lookupEnvString(prefix + "SITE"); ok {
			account.Site = value
		}
		if value, ok := lookupEnvString(prefix + "ACCESS_KEY_ID"); ok {
			account.AccessKeyID = value
		}
		if value, ok := lookupEnvString(prefix + "ACCESS_KEY_SECRET"); ok {
			account.AccessKeySecret = value
		}
		if value, ok := lookupEnvList(prefix + "REGIONS"); ok {
			account.Regions = value
		}
		if value, ok, err := lookupEnvFloat(prefix + "MAINLAND_TRAFFIC_LIMIT"); err != nil {
			return nil, err
		} else if ok {
			account.MainlandTrafficLimit = value
		}
		if value, ok, err := lookupEnvFloat(prefix + "OVERSEAS_TRAFFIC_LIMIT"); err != nil {
			return nil, err
		} else if ok {
			account.OverseasTrafficLimit = value
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func lookupEnvString(name string) (string, bool) {
	value, ok := os.LookupEnv(name)
	value = strings.TrimSpace(value)
	return value, ok && value != ""
}

func lookupEnvDuration(name string) (time.Duration, bool, error) {
	value, ok := lookupEnvString(name)
	if !ok {
		return 0, false, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, false, fmt.Errorf("%s 必须是时长格式，例如 10m、20s 或 24h", name)
	}
	return duration, true, nil
}

func lookupEnvBool(name string) (bool, bool) {
	value, ok := lookupEnvString(name)
	if !ok {
		return false, false
	}
	return parseBool(value), true
}

func lookupEnvInt(name string) (int, bool, error) {
	value, ok := lookupEnvString(name)
	if !ok {
		return 0, false, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, false, fmt.Errorf("%s 必须是整数", name)
	}
	return number, true, nil
}

func lookupEnvFloat(name string) (float64, bool, error) {
	value, ok := lookupEnvString(name)
	if !ok {
		return 0, false, nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%s 必须是数字", name)
	}
	return number, true, nil
}

func lookupEnvList(name string) ([]string, bool) {
	value, ok := lookupEnvString(name)
	if !ok {
		return nil, false
	}
	values := splitList(value)
	return values, len(values) > 0
}

func splitList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		return parseList(value)
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '|' || r == ';' || r == '\n' || r == '\r' || r == '\t'
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func envKey(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	var builder strings.Builder
	lastUnderscore := false
	for _, char := range value {
		if (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(builder.String(), "_")
}

func parseKeyValue(line string) (string, string, bool) {
	index := strings.Index(line, ":")
	if index < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:index])
	value := strings.TrimSpace(line[index+1:])
	return key, value, key != ""
}

func parseDuration(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(scalar(value))
	if err != nil {
		return 0, fmt.Errorf("duration %q 格式错误", value)
	}
	return duration, nil
}

func parseBool(value string) bool {
	switch strings.ToLower(scalar(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "[]" || value == "" {
		return nil
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		item := scalar(part)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

func scalar(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	return value
}

func stripComment(line string) string {
	inSingle := false
	inDouble := false
	for index, char := range line {
		switch char {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return strings.TrimRight(line[:index], " ")
			}
		}
	}
	return line
}

func lineError(index int, message string) error {
	return fmt.Errorf("第 %d 行: %s", index+1, message)
}
