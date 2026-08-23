package console

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	onboardingPending   = "pending"
	onboardingCompleted = "completed"
	onboardingSkipped   = "skipped"
)

var onboardingStepIDs = []string{"storage", "recommendedApps", "remoteAccess", "securityContact"}

type onboardingState struct {
	Steps        map[string]string `json:"steps"`
	ContactEmail string            `json:"contactEmail,omitempty"`
	UpdatedAt    time.Time         `json:"updatedAt,omitempty"`
}

type onboardingStore struct {
	path string
	mu   sync.RWMutex
	data onboardingState
}

func newOnboardingStore(path string) *onboardingStore {
	s := &onboardingStore{path: path, data: defaultOnboardingState()}
	s.reload()
	return s
}

func defaultOnboardingState() onboardingState {
	steps := make(map[string]string, len(onboardingStepIDs))
	for _, id := range onboardingStepIDs {
		steps[id] = onboardingPending
	}
	return onboardingState{Steps: steps}
}

func normalizeOnboardingState(state onboardingState) onboardingState {
	if state.Steps == nil {
		state.Steps = make(map[string]string, len(onboardingStepIDs))
	}
	for _, id := range onboardingStepIDs {
		status := state.Steps[id]
		if status != onboardingCompleted && status != onboardingSkipped {
			state.Steps[id] = onboardingPending
		}
	}
	return state
}

func (s *onboardingStore) reload() {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var state onboardingState
	if json.Unmarshal(b, &state) != nil {
		return
	}
	s.mu.Lock()
	s.data = normalizeOnboardingState(state)
	s.mu.Unlock()
}

func (s *onboardingStore) get() onboardingState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := s.data
	state.Steps = make(map[string]string, len(s.data.Steps))
	for id, status := range s.data.Steps {
		state.Steps[id] = status
	}
	return state
}

func (s *onboardingStore) update(step, status string, contactEmail *string) (onboardingState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.data
	next.Steps = make(map[string]string, len(s.data.Steps))
	for id, stepStatus := range s.data.Steps {
		next.Steps[id] = stepStatus
	}
	next.Steps[step] = status
	if contactEmail != nil {
		next.ContactEmail = *contactEmail
	}
	next.UpdatedAt = time.Now().UTC()
	if err := s.saveLocked(next); err != nil {
		return onboardingState{}, err
	}
	s.data = next
	state := s.data
	state.Steps = make(map[string]string, len(s.data.Steps))
	for id, stepStatus := range s.data.Steps {
		state.Steps[id] = stepStatus
	}
	return state, nil
}

func (s *onboardingStore) saveLocked(state onboardingState) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func validOnboardingStep(step string) bool {
	for _, id := range onboardingStepIDs {
		if step == id {
			return true
		}
	}
	return false
}
