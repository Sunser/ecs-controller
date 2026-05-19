package monitor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type StateStore struct {
	path string
	mu   sync.Mutex
	data PersistentState
}

const (
	PauseReasonManualStop               = "manual_stop"
	PauseReasonManualStopUntilNextMonth = "manual_stop_until_next_month"
)

type PersistentState struct {
	ManualPauses                  map[string]PauseState            `json:"manual_pauses,omitempty"`
	LastStartUnix                 map[string]int64                 `json:"last_start_unix"`
	LastOperations                map[string]Operation             `json:"last_operations"`
	InstanceTraffic               map[string]CachedInstanceTraffic `json:"instance_traffic,omitempty"`
	LastManualRequiredNotifyUnix  map[string]int64                 `json:"last_manual_required_notify_unix,omitempty"`
	LastTrafficExceededNotifyUnix map[string]int64                 `json:"last_traffic_exceeded_notify_unix,omitempty"`
}

type PauseState struct {
	Reason          string `json:"reason"`
	ResumeAfterUnix int64  `json:"resume_after_unix,omitempty"`
}

type CachedInstanceTraffic struct {
	Month       string  `json:"month"`
	GB          float64 `json:"gb"`
	Source      string  `json:"source"`
	Metric      string  `json:"metric,omitempty"`
	Points      int     `json:"points,omitempty"`
	UpdatedUnix int64   `json:"updated_unix"`
}

func OpenStateStore(path string) (*StateStore, error) {
	store := &StateStore{path: path, data: emptyState()}
	if path == "" {
		return store, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store.data); err != nil {
		return nil, err
	}
	store.ensureMaps()
	return store, nil
}

func (s *StateStore) IsManualPaused(instanceID string) bool {
	return s.IsManualPausedAt(instanceID, time.Now())
}

func (s *StateStore) IsManualPausedAt(instanceID string, at time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if at.IsZero() {
		at = time.Now()
	}
	return s.manualPauseLocked(instanceID, at)
}

func (s *StateStore) SetManualPaused(instanceID string, paused bool) error {
	if !paused {
		return s.ClearManualPause(instanceID)
	}
	return s.SetManualPause(instanceID, PauseState{Reason: PauseReasonManualStop})
}

func (s *StateStore) SetManualPause(instanceID string, pause PauseState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMaps()
	if pause.Reason == "" {
		pause.Reason = PauseReasonManualStop
	}
	s.data.ManualPauses[instanceID] = pause
	return s.saveLocked()
}

func (s *StateStore) ClearManualPause(instanceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMaps()
	delete(s.data.ManualPauses, instanceID)
	return s.saveLocked()
}

func (s *StateStore) LastStartAt(instanceID string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	unix := s.data.LastStartUnix[instanceID]
	if unix <= 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0)
}

func (s *StateStore) RecordStart(instanceID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMaps()
	s.data.LastStartUnix[instanceID] = at.Unix()
	return s.saveLocked()
}

func (s *StateStore) LastOperation(instanceID string) Operation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.LastOperations[instanceID]
}

func (s *StateStore) RecordOperation(instanceID string, operation Operation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMaps()
	s.data.LastOperations[instanceID] = operation
	return s.saveLocked()
}

func (s *StateStore) AllowManualRequiredNotification(instanceID, reason string, at time.Time, interval time.Duration) (bool, error) {
	if at.IsZero() {
		at = time.Now()
	}
	key := instanceID + "|" + reason
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMaps()
	lastUnix := s.data.LastManualRequiredNotifyUnix[key]
	if lastUnix > 0 && interval > 0 && at.Sub(time.Unix(lastUnix, 0)) < interval {
		return false, nil
	}
	s.data.LastManualRequiredNotifyUnix[key] = at.Unix()
	return true, s.saveLocked()
}

func (s *StateStore) AllowTrafficExceededNotification(accountName, scope string, at time.Time, interval time.Duration) (bool, error) {
	if at.IsZero() {
		at = time.Now()
	}
	key := accountName + "|" + scope
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMaps()
	lastUnix := s.data.LastTrafficExceededNotifyUnix[key]
	if lastUnix > 0 && interval > 0 && at.Sub(time.Unix(lastUnix, 0)) < interval {
		return false, nil
	}
	s.data.LastTrafficExceededNotifyUnix[key] = at.Unix()
	return true, s.saveLocked()
}

func (s *StateStore) CachedInstanceTraffic(instanceID, month string) (CachedInstanceTraffic, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cache, ok := s.data.InstanceTraffic[instanceID]
	if !ok || cache.Month != month {
		return CachedInstanceTraffic{}, false
	}
	return cache, true
}

func (s *StateStore) RecordInstanceTraffic(instanceID, month string, cache CachedInstanceTraffic) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMaps()
	cache.Month = month
	if cache.UpdatedUnix <= 0 {
		cache.UpdatedUnix = time.Now().Unix()
	}
	if existing, ok := s.data.InstanceTraffic[instanceID]; ok && existing.Month == month && existing.GB > cache.GB {
		cache.GB = existing.GB
		if cache.Metric == "" {
			cache.Metric = existing.Metric
		}
		if cache.Points == 0 {
			cache.Points = existing.Points
		}
	}
	s.data.InstanceTraffic[instanceID] = cache
	return s.saveLocked()
}

func (s *StateStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

func (s *StateStore) ensureMaps() {
	if s.data.ManualPauses == nil {
		s.data.ManualPauses = map[string]PauseState{}
	}
	if s.data.LastStartUnix == nil {
		s.data.LastStartUnix = map[string]int64{}
	}
	if s.data.LastOperations == nil {
		s.data.LastOperations = map[string]Operation{}
	}
	if s.data.InstanceTraffic == nil {
		s.data.InstanceTraffic = map[string]CachedInstanceTraffic{}
	}
	if s.data.LastManualRequiredNotifyUnix == nil {
		s.data.LastManualRequiredNotifyUnix = map[string]int64{}
	}
	if s.data.LastTrafficExceededNotifyUnix == nil {
		s.data.LastTrafficExceededNotifyUnix = map[string]int64{}
	}
}

func emptyState() PersistentState {
	return PersistentState{
		ManualPauses:                  map[string]PauseState{},
		LastStartUnix:                 map[string]int64{},
		LastOperations:                map[string]Operation{},
		InstanceTraffic:               map[string]CachedInstanceTraffic{},
		LastManualRequiredNotifyUnix:  map[string]int64{},
		LastTrafficExceededNotifyUnix: map[string]int64{},
	}
}

func (s *StateStore) manualPauseLocked(instanceID string, at time.Time) bool {
	s.ensureMaps()
	pause, ok := s.data.ManualPauses[instanceID]
	if !ok {
		return false
	}
	if pause.ResumeAfterUnix > 0 && !at.Before(time.Unix(pause.ResumeAfterUnix, 0)) {
		delete(s.data.ManualPauses, instanceID)
		_ = s.saveLocked()
		return false
	}
	return true
}

func nextMonthStart(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
}
