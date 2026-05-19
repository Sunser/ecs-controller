package monitor

import (
	"testing"
	"time"
)

func TestStateStoreKeepsMonthlyInstanceTrafficCache(t *testing.T) {
	path := t.TempDir() + "/state.json"
	store, err := OpenStateStore(path)
	if err != nil {
		t.Fatalf("OpenStateStore() error = %v", err)
	}
	at := time.Date(2026, 5, 19, 2, 0, 0, 0, time.UTC)
	if err := store.RecordInstanceTraffic("i-stopped", "2026-05", CachedInstanceTraffic{
		Month:       "2026-05",
		GB:          0.12,
		Source:      "cms",
		Metric:      "VPC_PublicIP_InternetOutRate",
		Points:      3,
		UpdatedUnix: at.Unix(),
	}); err != nil {
		t.Fatalf("RecordInstanceTraffic() error = %v", err)
	}

	reopened, err := OpenStateStore(path)
	if err != nil {
		t.Fatalf("OpenStateStore() reopen error = %v", err)
	}
	got, ok := reopened.CachedInstanceTraffic("i-stopped", "2026-05")
	if !ok {
		t.Fatal("CachedInstanceTraffic() ok = false, want true")
	}
	if got.GB != 0.12 || got.Metric != "VPC_PublicIP_InternetOutRate" || got.Points != 3 {
		t.Fatalf("cached traffic = %#v", got)
	}
	if _, ok := reopened.CachedInstanceTraffic("i-stopped", "2026-06"); ok {
		t.Fatal("CachedInstanceTraffic() returned previous month cache")
	}
}

func TestStateStoreThrottlesManualRequiredNotificationsByInstanceAndReason(t *testing.T) {
	path := t.TempDir() + "/state.json"
	store, err := OpenStateStore(path)
	if err != nil {
		t.Fatalf("OpenStateStore() error = %v", err)
	}
	now := time.Date(2026, 5, 19, 8, 0, 0, 0, time.UTC)

	allowed, err := store.AllowManualRequiredNotification("i-1", "account_traffic_exceeded_manual_required", now, time.Hour)
	if err != nil {
		t.Fatalf("AllowManualRequiredNotification() error = %v", err)
	}
	if !allowed {
		t.Fatal("first notification allowed = false, want true")
	}

	allowed, err = store.AllowManualRequiredNotification("i-1", "account_traffic_exceeded_manual_required", now.Add(30*time.Minute), time.Hour)
	if err != nil {
		t.Fatalf("AllowManualRequiredNotification() second error = %v", err)
	}
	if allowed {
		t.Fatal("same instance and reason inside interval allowed = true, want false")
	}

	allowed, err = store.AllowManualRequiredNotification("i-1", "account_traffic_unknown_manual_required", now.Add(30*time.Minute), time.Hour)
	if err != nil {
		t.Fatalf("AllowManualRequiredNotification() different reason error = %v", err)
	}
	if !allowed {
		t.Fatal("different reason allowed = false, want true")
	}

	reopened, err := OpenStateStore(path)
	if err != nil {
		t.Fatalf("OpenStateStore() reopen error = %v", err)
	}
	allowed, err = reopened.AllowManualRequiredNotification("i-1", "account_traffic_exceeded_manual_required", now.Add(61*time.Minute), time.Hour)
	if err != nil {
		t.Fatalf("AllowManualRequiredNotification() after interval error = %v", err)
	}
	if !allowed {
		t.Fatal("same instance and reason after interval allowed = false, want true")
	}
}
