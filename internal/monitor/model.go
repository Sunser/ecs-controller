package monitor

import "time"

type InstanceSnapshot struct {
	AccountName           string    `json:"account_name"`
	AccountSite           string    `json:"account_site"`
	InstanceID            string    `json:"instance_id"`
	InstanceName          string    `json:"instance_name"`
	RegionID              string    `json:"region_id"`
	RegionName            string    `json:"region_name"`
	Status                string    `json:"status"`
	Spot                  bool      `json:"spot"`
	InstanceType          string    `json:"instance_type"`
	InstanceChargeType    string    `json:"instance_charge_type"`
	CPU                   int       `json:"cpu"`
	MemoryMB              int       `json:"memory_mb"`
	InternetBandwidthIn   int       `json:"internet_bandwidth_in"`
	InternetBandwidthOut  int       `json:"internet_bandwidth_out"`
	PublicIP              string    `json:"public_ip"`
	IPv6Addresses         []string  `json:"ipv6_addresses"`
	PrivateIP             string    `json:"private_ip"`
	InstanceTrafficGB     float64   `json:"instance_traffic_gb"`
	InstanceTrafficSource string    `json:"instance_traffic_source"`
	InstanceTrafficMetric string    `json:"instance_traffic_metric,omitempty"`
	InstanceTrafficPoints int       `json:"instance_traffic_points"`
	InstanceTrafficError  string    `json:"instance_traffic_error,omitempty"`
	AccountTrafficScope   string    `json:"account_traffic_scope"`
	AccountTrafficGB      float64   `json:"account_traffic_gb"`
	AccountLimitGB        float64   `json:"account_limit_gb"`
	AccountUsagePercent   float64   `json:"account_usage_percent"`
	KeepAliveDecision     Decision  `json:"keep_alive_decision"`
	ManualPaused          bool      `json:"manual_paused"`
	LastOperation         Operation `json:"last_operation"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type AccountSnapshot struct {
	AccountName      string                  `json:"account_name"`
	AccountSite      string                  `json:"account_site"`
	MonthlyTrafficGB float64                 `json:"monthly_traffic_gb"`
	MonthlyLimitGB   float64                 `json:"monthly_limit_gb"`
	UsagePercent     float64                 `json:"usage_percent"`
	TrafficSource    string                  `json:"traffic_source"`
	TrafficError     string                  `json:"traffic_error,omitempty"`
	TrafficScopes    []TrafficScopeSnapshot  `json:"traffic_scopes,omitempty"`
	TrafficRegions   []TrafficRegionSnapshot `json:"traffic_regions,omitempty"`
	UpdatedAt        time.Time               `json:"updated_at"`
}

type TrafficScopeSnapshot struct {
	Key          string  `json:"key"`
	Name         string  `json:"name"`
	TrafficGB    float64 `json:"traffic_gb"`
	LimitGB      float64 `json:"limit_gb"`
	UsagePercent float64 `json:"usage_percent"`
}

type TrafficRegionSnapshot struct {
	RegionID  string  `json:"region_id"`
	Name      string  `json:"name"`
	Scope     string  `json:"scope"`
	TrafficGB float64 `json:"traffic_gb"`
}

type Operation struct {
	Action     string    `json:"action"`
	Success    bool      `json:"success"`
	Message    string    `json:"message"`
	OccurredAt time.Time `json:"occurred_at"`
}

type Snapshot struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Accounts    []AccountSnapshot  `json:"accounts"`
	Instances   []InstanceSnapshot `json:"instances"`
	Errors      []string           `json:"errors"`
}

type DecisionKind string

const (
	DecisionSkip           DecisionKind = "skip"
	DecisionStart          DecisionKind = "start"
	DecisionManualRequired DecisionKind = "manual_required"
)

type Decision struct {
	Kind   DecisionKind `json:"kind"`
	Reason string       `json:"reason"`
}

type PolicyInput struct {
	Enabled                 bool
	Target                  string
	TrafficPolicy           string
	WarningPercent          float64
	AccountUsagePercent     float64
	AccountTrafficAvailable bool
	IncludeInstanceIDs      []string
	ManualPaused            bool
	StartCooldown           time.Duration
	LastStartAt             time.Time
	Now                     time.Time
	Instance                InstanceSnapshot
}
