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
