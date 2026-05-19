package monitor

import "time"

func DecideKeepAlive(input PolicyInput) Decision {
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	if !input.Enabled {
		return Decision{Kind: DecisionSkip, Reason: "keep_alive_disabled"}
	}
	if input.Target == "disabled" {
		return Decision{Kind: DecisionSkip, Reason: "target_disabled"}
	}
	if input.Instance.Status != "Stopped" {
		return Decision{Kind: DecisionSkip, Reason: "instance_not_stopped"}
	}
	if input.ManualPaused {
		return Decision{Kind: DecisionSkip, Reason: "manual_paused"}
	}
	if !targetMatched(input) {
		return Decision{Kind: DecisionSkip, Reason: "target_not_matched"}
	}
	if !input.AccountTrafficAvailable && input.TrafficPolicy == "manual_only_when_exceeded" {
		return Decision{Kind: DecisionManualRequired, Reason: "account_traffic_unknown_manual_required"}
	}
	if !input.AccountTrafficAvailable && input.TrafficPolicy == "pause_when_exceeded" {
		return Decision{Kind: DecisionSkip, Reason: "account_traffic_unknown_paused"}
	}
	if input.TrafficPolicy == "manual_only_when_exceeded" && input.AccountUsagePercent >= input.WarningPercent {
		return Decision{Kind: DecisionManualRequired, Reason: "account_traffic_exceeded_manual_required"}
	}
	if input.TrafficPolicy == "pause_when_exceeded" && input.AccountUsagePercent >= input.WarningPercent {
		return Decision{Kind: DecisionSkip, Reason: "account_traffic_exceeded_paused"}
	}
	if input.OperationCooldown > 0 && !input.LastStartAt.IsZero() && now.Sub(input.LastStartAt) < input.OperationCooldown {
		return Decision{Kind: DecisionSkip, Reason: "operation_cooldown"}
	}
	return Decision{Kind: DecisionStart, Reason: "stopped_target"}
}

func targetMatched(input PolicyInput) bool {
	switch input.Target {
	case "", "spot_only":
		return input.Instance.Spot
	case "all":
		return true
	case "include_list":
		for _, id := range input.IncludeInstanceIDs {
			if id == input.Instance.InstanceID {
				return true
			}
		}
		return false
	default:
		return false
	}
}
