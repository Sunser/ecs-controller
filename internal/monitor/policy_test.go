package monitor_test

import (
	"testing"
	"time"

	"ecs-controller/internal/monitor"
)

func TestManualOnlyWhenExceededRequiresHumanDecision(t *testing.T) {
	decision := monitor.DecideKeepAlive(monitor.PolicyInput{
		Enabled:                 true,
		Target:                  "spot_only",
		TrafficPolicy:           "manual_only_when_exceeded",
		WarningPercent:          95,
		AccountUsagePercent:     96,
		AccountTrafficAvailable: true,
		Instance: monitor.InstanceSnapshot{
			Status: "Stopped",
			Spot:   true,
		},
	})

	if decision.Kind != monitor.DecisionManualRequired {
		t.Fatalf("decision = %s, want %s, reason=%s", decision.Kind, monitor.DecisionManualRequired, decision.Reason)
	}
}

func TestManualStopPausesAutomaticKeepAlive(t *testing.T) {
	decision := monitor.DecideKeepAlive(monitor.PolicyInput{
		Enabled:                 true,
		Target:                  "spot_only",
		TrafficPolicy:           "ignore_limit",
		WarningPercent:          95,
		AccountUsagePercent:     10,
		AccountTrafficAvailable: true,
		ManualPaused:            true,
		Instance: monitor.InstanceSnapshot{
			InstanceID: "i-1",
			Status:     "Stopped",
			Spot:       true,
		},
	})

	if decision.Kind != monitor.DecisionSkip {
		t.Fatalf("decision = %s, want skip", decision.Kind)
	}
	if decision.Reason != "manual_paused" {
		t.Fatalf("reason = %s, want manual_paused", decision.Reason)
	}
}

func TestStoppedSpotStartsWhenUnderLimitAndNotCoolingDown(t *testing.T) {
	decision := monitor.DecideKeepAlive(monitor.PolicyInput{
		Enabled:                 true,
		Target:                  "spot_only",
		TrafficPolicy:           "manual_only_when_exceeded",
		WarningPercent:          95,
		AccountUsagePercent:     10,
		AccountTrafficAvailable: true,
		StartCooldown:           10 * time.Minute,
		Now:                     time.Unix(1000, 0),
		Instance: monitor.InstanceSnapshot{
			InstanceID: "i-1",
			Status:     "Stopped",
			Spot:       true,
		},
	})

	if decision.Kind != monitor.DecisionStart {
		t.Fatalf("decision = %s, want start, reason=%s", decision.Kind, decision.Reason)
	}
}

func TestUnknownAccountTrafficRequiresHumanDecisionByDefault(t *testing.T) {
	decision := monitor.DecideKeepAlive(monitor.PolicyInput{
		Enabled:        true,
		Target:         "spot_only",
		TrafficPolicy:  "manual_only_when_exceeded",
		WarningPercent: 95,
		Instance: monitor.InstanceSnapshot{
			InstanceID: "i-1",
			Status:     "Stopped",
			Spot:       true,
		},
	})

	if decision.Kind != monitor.DecisionManualRequired {
		t.Fatalf("decision = %s, want manual_required, reason=%s", decision.Kind, decision.Reason)
	}
	if decision.Reason != "account_traffic_unknown_manual_required" {
		t.Fatalf("reason = %s, want account_traffic_unknown_manual_required", decision.Reason)
	}
}
