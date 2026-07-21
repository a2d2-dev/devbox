package apps

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPhaseLegacyState(t *testing.T) {
	cases := map[Phase]string{
		PhaseRunning:   "running",
		PhaseStopped:   "stopped",
		PhaseFailed:    "error",
		PhasePending:   "pending",
		PhaseDeploying: "pending",
		PhaseDegraded:  "pending",
		PhaseRemoving:  "pending",
		PhaseUnknown:   "pending",
	}
	for p, want := range cases {
		assert.Equal(t, want, p.LegacyState(), "phase %s", p)
	}
}

func TestTaskStatusTerminal(t *testing.T) {
	for _, s := range []TaskStatus{TaskSucceeded, TaskFailed, TaskCanceled, TaskSuperseded} {
		assert.True(t, s.IsTerminal(), "%s 应为终态", s)
	}
	for _, s := range []TaskStatus{TaskQueued, TaskRunning} {
		assert.False(t, s.IsTerminal(), "%s 应为非终态", s)
	}
}

func TestErrorKindMapping(t *testing.T) {
	ae, ok := AsError(NotFoundErr("x"))
	assert.True(t, ok)
	assert.Equal(t, ErrKindNotFound, ae.Kind)

	ae, ok = AsError(ConflictErr("revision_mismatch", "m"))
	assert.True(t, ok)
	assert.Equal(t, ErrKindConflict, ae.Kind)
	assert.Equal(t, "revision_mismatch", ae.Reason)

	// 普通错误不识别为领域错误。
	_, ok = AsError(assert.AnError)
	assert.False(t, ok)
}
