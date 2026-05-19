package monitor

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"ecs-controller/internal/aliyun"
	"ecs-controller/internal/applog"
	"ecs-controller/internal/config"
	"ecs-controller/internal/notify"
)

type Service struct {
	cfg        config.Config
	configPath string
	state      *StateStore

	mu        sync.RWMutex
	snapshot  Snapshot
	locations map[string]instanceLocation
	regions   map[string]regionCacheEntry
}

type instanceLocation struct {
	Account config.AccountConfig
	Region  aliyun.Region
}

type regionCacheEntry struct {
	Regions   []aliyun.Region
	ExpiresAt time.Time
}

type keepAliveCheckSummary struct {
	Checked        int
	Starts         int
	ManualRequired int
	Skipped        int
}

func (s *keepAliveCheckSummary) add(decision Decision) {
	s.Checked++
	switch decision.Kind {
	case DecisionStart:
		s.Starts++
	case DecisionManualRequired:
		s.ManualRequired++
	default:
		s.Skipped++
	}
}

func NewService(cfg config.Config, state *StateStore, configPath ...string) *Service {
	path := ""
	if len(configPath) > 0 {
		path = configPath[0]
	}
	service := &Service{
		cfg:        cfg,
		configPath: path,
		state:      state,
		snapshot:   Snapshot{GeneratedAt: time.Now()},
		locations:  map[string]instanceLocation{},
		regions:    map[string]regionCacheEntry{},
	}
	return service
}

func (s *Service) Config() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyConfig(s.cfg)
}

func (s *Service) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	copySnapshot := s.snapshot
	copySnapshot.Accounts = append([]AccountSnapshot(nil), s.snapshot.Accounts...)
	copySnapshot.Instances = append([]InstanceSnapshot(nil), s.snapshot.Instances...)
	copySnapshot.Errors = append([]string(nil), s.snapshot.Errors...)
	return copySnapshot
}

func (s *Service) RefreshLoop(ctx context.Context) {
	s.refreshWithTimeout(ctx)

	for {
		timer := time.NewTimer(s.Config().Server.RefreshInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.refreshWithTimeout(ctx)
		}
	}
}

func (s *Service) Refresh(ctx context.Context) error {
	cfg := s.Config()
	now := time.Now()
	startedAt := now
	next := Snapshot{GeneratedAt: now}
	locations := map[string]instanceLocation{}
	keepAliveSummary := keepAliveCheckSummary{}

	for _, account := range cfg.Accounts {
		client := aliyun.NewClient(account.AccessKeyID, account.AccessKeySecret, account.Site, cfg.Server.RequestTimeout)
		accountTraffic := s.loadAccountTraffic(ctx, client)
		accountTrafficAvailable := accountTraffic.Source == "cdt"
		accountScopes := trafficScopes(accountTraffic, account.MainlandTrafficLimit, account.OverseasTrafficLimit)
		maxScope, hasMaxScope := maxTrafficScope(accountScopes)
		accountUsagePercent := maxScope.UsagePercent
		accountSnapshot := AccountSnapshot{
			AccountName:      account.Name,
			AccountSite:      account.Site,
			MonthlyTrafficGB: accountTraffic.GB,
			MonthlyLimitGB:   account.OverseasTrafficLimit,
			UsagePercent:     accountUsagePercent,
			TrafficSource:    accountTraffic.Source,
			TrafficError:     trafficErrorText(accountTraffic),
			TrafficScopes:    accountScopes,
			TrafficRegions:   trafficRegions(accountTraffic),
			UpdatedAt:        now,
		}
		next.Accounts = append(next.Accounts, accountSnapshot)
		if accountTrafficAvailable {
			applog.Debug("account", "cdt traffic detail", map[string]string{
				"account":          account.Name,
				"total_traffic":    fmt.Sprintf("%.2fGB", accountTraffic.GB),
				"mainland_traffic": fmt.Sprintf("%.2fGB", accountTraffic.MainlandGB),
				"mainland_limit":   fmt.Sprintf("%.0fGB", account.MainlandTrafficLimit),
				"overseas_traffic": fmt.Sprintf("%.2fGB", accountTraffic.OverseasGB),
				"overseas_limit":   fmt.Sprintf("%.0fGB", account.OverseasTrafficLimit),
			})
			if accountUsagePercent >= cfg.Traffic.WarningPercent {
				applog.Warn("account", "cdt traffic threshold reached", map[string]string{
					"account": account.Name,
					"usage":   fmt.Sprintf("%.2f%%", accountUsagePercent),
				})
				if hasMaxScope {
					s.notifyEvent(ctx, "traffic_exceeded", "流量超阈值", trafficExceededNotificationFields(account.Name, maxScope))
				} else {
					s.notifyEvent(ctx, "traffic_exceeded", "流量超阈值", map[string]string{
						"账号":  account.Name,
						"事件":  "流量超阈值",
						"使用率": fmt.Sprintf("%.2f%%", accountUsagePercent),
					})
				}
			}
		} else {
			applog.Warn("account", "cdt traffic unavailable", map[string]string{
				"account": account.Name,
				"error":   accountTraffic.Metric,
			})
			s.notifyEvent(ctx, "error", "账号流量读取失败", map[string]string{"账号": account.Name, "事件": "账号流量读取失败", "错误": accountTraffic.Metric})
		}

		regions, err := s.resolveRegions(ctx, client, cfg, account)
		if err != nil {
			next.Errors = append(next.Errors, fmt.Sprintf("[%s] describe regions failed: %v", account.Name, err))
			applog.Error("aliyun", "describe regions failed", map[string]string{"account": account.Name, "error": err.Error()})
			s.notifyEvent(ctx, "error", "地域发现失败", map[string]string{"账号": account.Name, "事件": "地域发现失败", "错误": err.Error()})
			continue
		}

		for _, region := range regions {
			instances, err := client.DescribeInstances(ctx, region)
			if err != nil {
				next.Errors = append(next.Errors, fmt.Sprintf("[%s/%s] describe instances failed: %v", account.Name, region.RegionID, err))
				applog.Error("aliyun", "describe instances failed", map[string]string{"account": account.Name, "region": region.RegionID, "error": err.Error()})
				s.notifyEvent(ctx, "error", "实例发现失败", map[string]string{"账号": account.Name, "地域": region.RegionID, "事件": "实例发现失败", "错误": err.Error()})
				continue
			}
			eniIPv6, eniErr := client.DescribeNetworkInterfaceIPv6(ctx, region.RegionID)
			if eniErr != nil {
				next.Errors = append(next.Errors, fmt.Sprintf("[%s/%s] describe network interfaces failed: %v", account.Name, region.RegionID, eniErr))
				applog.Warn("aliyun", "describe network interfaces failed", map[string]string{"account": account.Name, "region": region.RegionID, "error": eniErr.Error()})
			}
			for _, instance := range instances {
				if len(eniIPv6[instance.InstanceID]) > 0 {
					instance.IPv6Addresses = mergeStrings(instance.IPv6Addresses, eniIPv6[instance.InstanceID])
				}
				instanceTraffic := s.loadInstanceTraffic(ctx, client, account.Name, instance, now)
				trafficScope, regionAccountTrafficGB, regionAccountLimitGB, regionAccountUsagePercent := regionTraffic(accountTraffic, instance.RegionID, account.MainlandTrafficLimit, account.OverseasTrafficLimit)

				manualPaused := s.state.IsManualPaused(instance.InstanceID)
				snapshot := InstanceSnapshot{
					AccountName:           account.Name,
					AccountSite:           account.Site,
					InstanceID:            instance.InstanceID,
					InstanceName:          instance.InstanceName,
					RegionID:              instance.RegionID,
					RegionName:            instance.RegionName,
					Status:                instance.Status,
					Spot:                  instance.IsSpot(),
					InstanceType:          instance.InstanceType,
					InstanceChargeType:    instance.InstanceChargeType,
					CPU:                   instance.CPU,
					MemoryMB:              instance.Memory,
					InternetBandwidthIn:   instance.InternetMaxBandwidthIn,
					InternetBandwidthOut:  instance.InternetMaxBandwidthOut,
					PublicIP:              instance.PublicIP,
					IPv6Addresses:         append([]string(nil), instance.IPv6Addresses...),
					PrivateIP:             instance.PrivateIP,
					InstanceTrafficGB:     instanceTraffic.GB,
					InstanceTrafficSource: instanceTraffic.Source,
					InstanceTrafficMetric: instanceTraffic.Metric,
					InstanceTrafficPoints: instanceTraffic.Points,
					InstanceTrafficError:  trafficErrorText(instanceTraffic),
					AccountTrafficScope:   trafficScope,
					AccountTrafficGB:      regionAccountTrafficGB,
					AccountLimitGB:        regionAccountLimitGB,
					AccountUsagePercent:   regionAccountUsagePercent,
					ManualPaused:          manualPaused,
					LastOperation:         s.state.LastOperation(instance.InstanceID),
					UpdatedAt:             now,
				}
				decision := DecideKeepAlive(PolicyInput{
					Enabled:                 cfg.KeepAlive.Enabled,
					Target:                  cfg.KeepAlive.Target,
					TrafficPolicy:           cfg.KeepAlive.TrafficPolicy,
					WarningPercent:          cfg.Traffic.WarningPercent,
					AccountUsagePercent:     regionAccountUsagePercent,
					AccountTrafficAvailable: accountTrafficAvailable,
					IncludeInstanceIDs:      cfg.KeepAlive.IncludeInstanceIDs,
					ManualPaused:            manualPaused,
					StartCooldown:           cfg.KeepAlive.StartCooldown,
					LastStartAt:             s.state.LastStartAt(instance.InstanceID),
					Now:                     now,
					Instance:                snapshot,
				})
				snapshot.KeepAliveDecision = decision
				keepAliveSummary.add(decision)
				s.handleDecision(ctx, account.Name, snapshot, decision)
				if decision.Kind == DecisionStart {
					s.autoStart(ctx, client, snapshot)
					snapshot.LastOperation = s.state.LastOperation(instance.InstanceID)
				}

				next.Instances = append(next.Instances, snapshot)
				locations[instance.InstanceID] = instanceLocation{Account: account, Region: region}
			}
		}
	}

	s.mu.Lock()
	s.snapshot = next
	s.locations = locations
	s.mu.Unlock()
	applog.Info("keepalive", "check finished", map[string]string{
		"checked":         fmt.Sprintf("%d", keepAliveSummary.Checked),
		"starts":          fmt.Sprintf("%d", keepAliveSummary.Starts),
		"manual_required": fmt.Sprintf("%d", keepAliveSummary.ManualRequired),
		"skipped":         fmt.Sprintf("%d", keepAliveSummary.Skipped),
	})
	applog.Info("refresh", "finished", map[string]string{
		"accounts":  fmt.Sprintf("%d", len(next.Accounts)),
		"instances": fmt.Sprintf("%d", len(next.Instances)),
		"errors":    fmt.Sprintf("%d", len(next.Errors)),
		"duration":  time.Since(startedAt).Round(time.Millisecond).String(),
	})
	return nil
}

func (s *Service) ManualStart(ctx context.Context, instanceID string) error {
	location, err := s.location(instanceID)
	if err != nil {
		return err
	}
	cfg := s.Config()
	now := time.Now()
	lastStart := s.state.LastStartAt(instanceID)
	if cfg.KeepAlive.StartCooldown > 0 && !lastStart.IsZero() && now.Sub(lastStart) < cfg.KeepAlive.StartCooldown {
		applog.Warn("keepalive", "manual start blocked by repeat protection", map[string]string{"instance": instanceID})
		return fmt.Errorf("instance is still in start cooldown")
	}

	client := aliyun.NewClient(location.Account.AccessKeyID, location.Account.AccessKeySecret, location.Account.Site, cfg.Server.RequestTimeout)
	instance, _ := s.instanceSnapshot(instanceID)
	if err := client.StartInstance(ctx, location.Region.RegionID, instanceID); err != nil {
		_ = s.state.RecordOperation(instanceID, Operation{Action: "manual_start", Success: false, Message: err.Error(), OccurredAt: now})
		applog.Error("keepalive", "manual start failed", map[string]string{"account": location.Account.Name, "region": location.Region.RegionID, "instance": instanceID, "error": err.Error()})
		fields := instanceNotificationFields(location.Account.Name, location.Region.RegionID, instance, instanceID, "手工启动失败")
		fields["错误"] = err.Error()
		s.notifyEvent(ctx, "error", "手工启动失败", fields)
		return err
	}
	_ = s.state.SetManualPaused(instanceID, false)
	_ = s.state.RecordStart(instanceID, now)
	_ = s.state.RecordOperation(instanceID, Operation{Action: "manual_start", Success: true, Message: "manual start submitted", OccurredAt: now})
	applog.Info("keepalive", "manual start submitted", map[string]string{"account": location.Account.Name, "region": location.Region.RegionID, "instance": instanceID})
	s.notifyEvent(ctx, "manual_start", "手工启动已提交", instanceNotificationFields(location.Account.Name, location.Region.RegionID, instance, instanceID, "手工启动已提交"))
	s.patchInstanceAfterOperation(instanceID, "Starting")
	return nil
}

func (s *Service) ManualStop(ctx context.Context, instanceID, requestedStopMode string) error {
	location, err := s.location(instanceID)
	if err != nil {
		return err
	}
	cfg := s.Config()
	now := time.Now()
	client := aliyun.NewClient(location.Account.AccessKeyID, location.Account.AccessKeySecret, location.Account.Site, cfg.Server.RequestTimeout)
	instance, _ := s.instanceSnapshot(instanceID)
	configuredStopMode := cfg.KeepAlive.StopMode
	if strings.TrimSpace(requestedStopMode) != "" {
		configuredStopMode = strings.TrimSpace(requestedStopMode)
	}
	if configuredStopMode != aliyun.StoppedModeStopCharging && configuredStopMode != aliyun.StoppedModeKeepCharging {
		return fmt.Errorf("unsupported stop mode: %s", configuredStopMode)
	}
	stopMode := effectiveStopMode(configuredStopMode, instance)
	if err := client.StopInstance(ctx, location.Region.RegionID, instanceID, stopMode); err != nil {
		_ = s.state.RecordOperation(instanceID, Operation{Action: "manual_stop", Success: false, Message: err.Error(), OccurredAt: now})
		applog.Error("keepalive", "manual stop failed", map[string]string{"account": location.Account.Name, "region": location.Region.RegionID, "instance": instanceID, "stop_mode": stopMode, "configured_stop_mode": configuredStopMode, "error": err.Error()})
		fields := instanceNotificationFields(location.Account.Name, location.Region.RegionID, instance, instanceID, "手工关机失败")
		fields["错误"] = err.Error()
		s.notifyEvent(ctx, "error", "手工关机失败", fields)
		return err
	}
	_ = s.state.SetManualPaused(instanceID, true)
	_ = s.state.RecordOperation(instanceID, Operation{Action: "manual_stop", Success: true, Message: "manual stop submitted; keep-alive paused; mode=" + stopMode, OccurredAt: now})
	fields := map[string]string{"account": location.Account.Name, "region": location.Region.RegionID, "instance": instanceID, "stop_mode": stopMode}
	if stopMode != configuredStopMode {
		fields["configured_stop_mode"] = configuredStopMode
		fields["reason"] = "prepaid_keep_charging"
	}
	applog.Info("keepalive", "manual stop submitted", fields)
	s.notifyEvent(ctx, "manual_stop", "手工关机已提交", manualStopNotificationFields(location.Account.Name, location.Region.RegionID, instance, instanceID, stopMode))
	s.patchInstanceAfterOperation(instanceID, "Stopping")
	return nil
}

func (s *Service) refreshWithTimeout(parent context.Context) {
	cfg := s.Config()
	ctx, cancel := context.WithTimeout(parent, cfg.Server.RequestTimeout+cfg.Server.RefreshInterval)
	defer cancel()
	if err := s.Refresh(ctx); err != nil {
		applog.Error("refresh", "failed", map[string]string{"error": err.Error()})
	}
}

func (s *Service) resolveRegions(ctx context.Context, client *aliyun.Client, cfg config.Config, account config.AccountConfig) ([]aliyun.Region, error) {
	if len(account.Regions) == 1 && account.Regions[0] == "auto" {
		cacheKey := account.Name + "|" + account.AccessKeyID
		now := time.Now()
		s.mu.RLock()
		cached, ok := s.regions[cacheKey]
		s.mu.RUnlock()
		if ok && now.Before(cached.ExpiresAt) && len(cached.Regions) > 0 {
			return append([]aliyun.Region(nil), cached.Regions...), nil
		}
		regions, err := client.DescribeRegions(ctx)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.regions[cacheKey] = regionCacheEntry{
			Regions:   append([]aliyun.Region(nil), regions...),
			ExpiresAt: now.Add(cfg.Discovery.RegionRefreshInterval),
		}
		s.mu.Unlock()
		return regions, nil
	}
	regions := make([]aliyun.Region, 0, len(account.Regions))
	for _, regionID := range account.Regions {
		regions = append(regions, aliyun.Region{RegionID: regionID, LocalName: regionID})
	}
	return regions, nil
}

func (s *Service) loadAccountTraffic(ctx context.Context, client *aliyun.Client) aliyun.TrafficResult {
	cdt, err := client.CdtAccountTrafficGB(ctx)
	if err == nil {
		return cdt
	}
	return aliyun.TrafficResult{Source: "unknown", Metric: "cdt_failed:" + err.Error()}
}

func (s *Service) loadInstanceTraffic(ctx context.Context, client *aliyun.Client, accountName string, instance aliyun.Instance, now time.Time) aliyun.TrafficResult {
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	monthKey := now.Format("2006-01")
	cms, cmsErr := client.InstanceOutboundTrafficGB(ctx, instance, monthStart, now)
	if cmsErr == nil {
		cms.Source = "cms"
		cms = s.cacheOrRestoreInstanceTraffic(instance.InstanceID, monthKey, cms, now)
		applog.Debug("traffic", "cms instance traffic loaded", map[string]string{
			"account":  accountName,
			"region":   instance.RegionID,
			"instance": instance.InstanceID,
			"used":     fmt.Sprintf("%.2fGB", cms.GB),
			"metric":   cms.Metric,
			"points":   fmt.Sprintf("%d", cms.Points),
		})
		return cms
	}
	if cached, ok := s.state.CachedInstanceTraffic(instance.InstanceID, monthKey); ok {
		applog.Debug("traffic", "cms instance traffic restored from cache", map[string]string{
			"account":  accountName,
			"region":   instance.RegionID,
			"instance": instance.InstanceID,
			"used":     fmt.Sprintf("%.2fGB", cached.GB),
			"metric":   cached.Metric,
		})
		return cachedTrafficResult(cached)
	}
	applog.Warn("traffic", "cms instance traffic unavailable", map[string]string{"account": accountName, "region": instance.RegionID, "instance": instance.InstanceID, "error": cmsErr.Error()})
	return aliyun.TrafficResult{Source: "unknown", Metric: "cms_failed:" + cmsErr.Error()}
}

func (s *Service) cacheOrRestoreInstanceTraffic(instanceID, monthKey string, traffic aliyun.TrafficResult, now time.Time) aliyun.TrafficResult {
	if traffic.Source == "cms" && (traffic.Points > 0 || traffic.GB > 0) {
		_ = s.state.RecordInstanceTraffic(instanceID, monthKey, CachedInstanceTraffic{
			Month:       monthKey,
			GB:          traffic.GB,
			Source:      traffic.Source,
			Metric:      traffic.Metric,
			Points:      traffic.Points,
			UpdatedUnix: now.Unix(),
		})
	}
	cached, ok := s.state.CachedInstanceTraffic(instanceID, monthKey)
	if !ok {
		return traffic
	}
	if traffic.Points == 0 && traffic.GB == 0 {
		return cachedTrafficResult(cached)
	}
	if cached.GB > traffic.GB {
		return cachedTrafficResult(cached)
	}
	return traffic
}

func cachedTrafficResult(cache CachedInstanceTraffic) aliyun.TrafficResult {
	source := cache.Source
	if source == "" {
		source = "cms"
	}
	return aliyun.TrafficResult{
		GB:     cache.GB,
		Source: source,
		Metric: cache.Metric,
		Points: cache.Points,
	}
}

func (s *Service) autoStart(ctx context.Context, client *aliyun.Client, snapshot InstanceSnapshot) {
	now := time.Now()
	err := client.StartInstance(ctx, snapshot.RegionID, snapshot.InstanceID)
	if err != nil {
		_ = s.state.RecordOperation(snapshot.InstanceID, Operation{Action: "auto_start", Success: false, Message: err.Error(), OccurredAt: now})
		applog.Error("keepalive", "auto start failed", map[string]string{"account": snapshot.AccountName, "region": snapshot.RegionID, "instance": snapshot.InstanceID, "error": err.Error()})
		fields := instanceNotificationFields(snapshot.AccountName, snapshot.RegionID, snapshot, snapshot.InstanceID, "后台自动启动失败")
		fields["错误"] = err.Error()
		s.notifyEvent(ctx, "error", "后台自动启动失败", fields)
		return
	}
	_ = s.state.RecordStart(snapshot.InstanceID, now)
	_ = s.state.RecordOperation(snapshot.InstanceID, Operation{Action: "auto_start", Success: true, Message: "auto start submitted", OccurredAt: now})
	applog.Info("keepalive", "auto start submitted", map[string]string{"account": snapshot.AccountName, "region": snapshot.RegionID, "instance": snapshot.InstanceID})
	s.notifyEvent(ctx, "auto_start", "后台自动启动已提交", instanceNotificationFields(snapshot.AccountName, snapshot.RegionID, snapshot, snapshot.InstanceID, "后台自动启动已提交"))
}

func (s *Service) location(instanceID string) (instanceLocation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	location, ok := s.locations[instanceID]
	if !ok {
		return instanceLocation{}, fmt.Errorf("instance %s is not discovered yet", instanceID)
	}
	return location, nil
}

func (s *Service) instanceSnapshot(instanceID string) (InstanceSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, instance := range s.snapshot.Instances {
		if instance.InstanceID == instanceID {
			return instance, true
		}
	}
	return InstanceSnapshot{}, false
}

func effectiveStopMode(configured string, instance InstanceSnapshot) string {
	if configured != aliyun.StoppedModeStopCharging {
		return aliyun.StoppedModeKeepCharging
	}
	if strings.EqualFold(strings.TrimSpace(instance.InstanceChargeType), "PrePaid") {
		return aliyun.StoppedModeKeepCharging
	}
	return aliyun.StoppedModeStopCharging
}

func (s *Service) patchInstanceAfterOperation(instanceID, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.snapshot.Instances {
		if s.snapshot.Instances[index].InstanceID == instanceID {
			s.snapshot.Instances[index].Status = status
			s.snapshot.Instances[index].ManualPaused = s.state.IsManualPaused(instanceID)
			s.snapshot.Instances[index].LastOperation = s.state.LastOperation(instanceID)
			s.snapshot.Instances[index].UpdatedAt = time.Now()
			return
		}
	}
}

func trafficErrorText(traffic aliyun.TrafficResult) string {
	if traffic.Source != "unknown" {
		return ""
	}
	return traffic.Metric
}

func percentage(used, limit float64) float64 {
	if limit <= 0 {
		return 0
	}
	return used / limit * 100
}

func trafficScopes(traffic aliyun.TrafficResult, mainlandLimitGB, overseasLimitGB float64) []TrafficScopeSnapshot {
	if traffic.Source != "cdt" {
		return nil
	}
	return []TrafficScopeSnapshot{
		{
			Key:          aliyun.CDTScopeMainland,
			Name:         "中国内地",
			TrafficGB:    traffic.MainlandGB,
			LimitGB:      mainlandLimitGB,
			UsagePercent: percentage(traffic.MainlandGB, mainlandLimitGB),
		},
		{
			Key:          aliyun.CDTScopeOverseas,
			Name:         "非中国内地",
			TrafficGB:    traffic.OverseasGB,
			LimitGB:      overseasLimitGB,
			UsagePercent: percentage(traffic.OverseasGB, overseasLimitGB),
		},
	}
}

func trafficRegions(traffic aliyun.TrafficResult) []TrafficRegionSnapshot {
	if traffic.Source != "cdt" || len(traffic.RegionUsages) == 0 {
		return nil
	}
	regions := make([]TrafficRegionSnapshot, 0, len(traffic.RegionUsages))
	for _, usage := range traffic.RegionUsages {
		if usage.RegionID == "" || usage.GB <= 0 {
			continue
		}
		regions = append(regions, TrafficRegionSnapshot{
			RegionID:  usage.RegionID,
			Name:      regionDisplayName(usage.RegionID),
			Scope:     usage.Scope,
			TrafficGB: usage.GB,
		})
	}
	sort.Slice(regions, func(i, j int) bool {
		if regions[i].TrafficGB == regions[j].TrafficGB {
			return regions[i].RegionID < regions[j].RegionID
		}
		return regions[i].TrafficGB > regions[j].TrafficGB
	})
	return regions
}

func regionDisplayName(regionID string) string {
	names := map[string]string{
		"cn-hangzhou":    "华东1（杭州）",
		"cn-shanghai":    "华东2（上海）",
		"cn-beijing":     "华北2（北京）",
		"cn-shenzhen":    "华南1（深圳）",
		"cn-hongkong":    "中国香港",
		"ap-southeast-1": "新加坡",
		"ap-northeast-1": "日本",
		"ap-southeast-5": "印尼",
		"us-west-1":      "美国西部",
		"us-east-1":      "美国东部",
		"eu-central-1":   "德国",
	}
	if name := names[regionID]; name != "" {
		return name
	}
	return regionID
}

func maxTrafficScope(scopes []TrafficScopeSnapshot) (TrafficScopeSnapshot, bool) {
	var maxScope TrafficScopeSnapshot
	var found bool
	for _, scope := range scopes {
		if !found || scope.UsagePercent > maxScope.UsagePercent {
			maxScope = scope
			found = true
		}
	}
	return maxScope, found
}

func regionTraffic(traffic aliyun.TrafficResult, regionID string, mainlandLimitGB, overseasLimitGB float64) (string, float64, float64, float64) {
	scope := aliyun.CDTScopeForRegion(regionID)
	limit := overseasLimitGB
	used := traffic.OverseasGB
	if scope == aliyun.CDTScopeMainland {
		limit = mainlandLimitGB
		used = traffic.MainlandGB
	}
	if traffic.Source != "cdt" {
		return scope, 0, limit, 0
	}
	return scope, used, limit, percentage(used, limit)
}

func (s *Service) handleDecision(ctx context.Context, accountName string, snapshot InstanceSnapshot, decision Decision) {
	if decision.Kind == DecisionManualRequired {
		applog.Warn("keepalive", "manual decision required", map[string]string{
			"account":  accountName,
			"instance": snapshot.InstanceID,
			"reason":   decision.Reason,
			"usage":    fmt.Sprintf("%.2f%%", snapshot.AccountUsagePercent),
		})
		fields := instanceNotificationFields(accountName, snapshot.RegionID, snapshot, snapshot.InstanceID, "实例需要人工决策")
		fields["原因"] = decisionReasonText(decision.Reason, snapshot.AccountUsagePercent)
		s.notifyEvent(ctx, "manual_required", "实例需要人工决策", fields)
		return
	}
	if decision.Kind == DecisionStart {
		applog.Info("keepalive", "auto start decision", map[string]string{
			"account":  accountName,
			"instance": snapshot.InstanceID,
			"reason":   decision.Reason,
		})
		return
	}
	applog.Debug("keepalive", "decision skipped", map[string]string{
		"account":  accountName,
		"instance": snapshot.InstanceID,
		"status":   snapshot.Status,
		"reason":   decision.Reason,
	})
}

func (s *Service) notifyEvent(parent context.Context, event, title string, fields map[string]string) {
	cfg := s.Config()
	if !cfg.Notification.Enabled || !notifyEventEnabled(cfg.Notification.NotifyEvents, event) {
		return
	}
	timeout := cfg.Server.RequestTimeout
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	notifyFields := notificationFieldsWithTime(fields, time.Now())
	err := notify.WeChatAppNotifier{
		CorpID:     cfg.Notification.WeChatCorpID,
		CorpSecret: cfg.Notification.WeChatCorpSecret,
		AgentID:    cfg.Notification.WeChatAgentID,
		ToUser:     cfg.Notification.WeChatToUser,
		Client:     &http.Client{Timeout: timeout},
	}.SendText(ctx, title, notifyFields)
	if err != nil {
		logFields := notificationLogFields(cfg, event)
		logFields["error"] = err.Error()
		applog.Warn("notification", "message send failed", logFields)
		return
	}
	applog.Info("notification", "message sent", notificationLogFields(cfg, event))
}

func notificationFieldsWithTime(fields map[string]string, now time.Time) map[string]string {
	normalized := make(map[string]string, len(fields)+1)
	for key, value := range fields {
		normalized[key] = value
	}
	if strings.TrimSpace(normalized["发送时间"]) == "" && strings.TrimSpace(normalized["时间"]) != "" {
		normalized["发送时间"] = strings.TrimSpace(normalized["时间"])
	}
	delete(normalized, "时间")
	if strings.TrimSpace(normalized["发送时间"]) == "" {
		normalized["发送时间"] = now.In(time.Local).Format("2006-01-02 15:04:05")
	}
	return normalized
}

func notificationLogFields(cfg config.Config, event string) map[string]string {
	return map[string]string{
		"channel":   "wechat",
		"event":     event,
		"receivers": notificationReceivers(cfg.Notification.WeChatToUser),
	}
}

func notificationReceivers(values []string) string {
	seen := make(map[string]struct{}, len(values))
	receivers := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ';' || r == '|'
		}) {
			receiver := strings.TrimSpace(part)
			if receiver == "" {
				continue
			}
			if _, ok := seen[receiver]; ok {
				continue
			}
			seen[receiver] = struct{}{}
			receivers = append(receivers, receiver)
		}
	}
	if len(receivers) == 0 {
		return "-"
	}
	return strings.Join(receivers, ",")
}

func notifyEventEnabled(events []string, target string) bool {
	for _, event := range events {
		if event == target || event == "all" {
			return true
		}
	}
	return false
}

func instanceNotificationFields(accountName, regionID string, snapshot InstanceSnapshot, instanceID, action string) map[string]string {
	if instanceID == "" {
		instanceID = snapshot.InstanceID
	}
	if regionID == "" {
		regionID = snapshot.RegionID
	}
	return map[string]string{
		"账号":    accountName,
		"地域":    regionID,
		"实例名称":  snapshot.InstanceName,
		"实例 ID": instanceID,
		"事件":    action,
	}
}

func manualStopNotificationFields(accountName, regionID string, snapshot InstanceSnapshot, instanceID, stopMode string) map[string]string {
	fields := instanceNotificationFields(accountName, regionID, snapshot, instanceID, "手工关机已提交")
	fields["停机模式"] = stopMode
	return fields
}

func trafficExceededNotificationFields(accountName string, scope TrafficScopeSnapshot) map[string]string {
	return map[string]string{
		"账号":   accountName,
		"事件":   "流量超阈值",
		"流量分区": fmt.Sprintf("%s使用率：%.2f%%", scope.Name, scope.UsagePercent),
	}
}

func decisionReasonText(reason string, usagePercent float64) string {
	switch reason {
	case "account_traffic_unknown_manual_required":
		return "账号流量读取失败，需要人工决策"
	case "account_traffic_exceeded_manual_required":
		return fmt.Sprintf("流量已达到阈值 %.2f%%，需要人工决策", usagePercent)
	default:
		return reason
	}
}

func copyConfig(cfg config.Config) config.Config {
	cfg.KeepAlive.IncludeInstanceIDs = append([]string(nil), cfg.KeepAlive.IncludeInstanceIDs...)
	cfg.Notification.WeChatToUser = append([]string(nil), cfg.Notification.WeChatToUser...)
	cfg.Notification.NotifyEvents = append([]string(nil), cfg.Notification.NotifyEvents...)
	cfg.Accounts = append([]config.AccountConfig(nil), cfg.Accounts...)
	for index := range cfg.Accounts {
		cfg.Accounts[index].Regions = append([]string(nil), cfg.Accounts[index].Regions...)
	}
	return cfg
}

func mergeStrings(left, right []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(left)+len(right))
	for _, value := range append(append([]string(nil), left...), right...) {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
