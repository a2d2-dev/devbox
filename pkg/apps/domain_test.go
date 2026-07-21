package apps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// HIGH#1：RiskBlockedErr 必须携带 findings，供调用方/HTTP 取得具体阻断项。
func TestRiskBlockedErrCarriesFindings(t *testing.T) {
	findings := []RiskFinding{
		{Level: RiskBlocked, Service: "web", Field: "privileged", Message: "privileged:true"},
		{Level: RiskConfirmation, Service: "web", Field: "cap_add", Message: "SYS_ADMIN"},
	}
	ae, ok := AsError(RiskBlockedErr("存在阻断级风险", findings))
	require.True(t, ok)
	assert.Equal(t, ErrKindRiskBlocked, ae.Kind)
	assert.Equal(t, findings, ae.Findings, "findings 必须被保存并返回")

	// findings 仅含字段名/描述，不含 secret/compose 正文。
	for _, f := range ae.Findings {
		assert.NotEmpty(t, f.Message)
	}
}
