package apps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// MED#9：ready=0 不再直接判 failed；按 Deployment conditions 区分。

func TestDeploymentPhaseProgressingNotFailed(t *testing.T) {
	// 新部署/更新初期 ready=0、Progressing=True（滚动中）→ deploying，不是 failed。
	replicas := int32(2)
	d := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 0,
			Conditions: []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue, Reason: "ReplicaSetUpdated"},
			},
		},
	}
	assert.Equal(t, PhaseDeploying, deploymentPhase(d, replicas, 0))
}

func TestDeploymentPhaseProgressDeadlineFailed(t *testing.T) {
	replicas := int32(2)
	d := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 0,
			Conditions: []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded"},
			},
		},
	}
	assert.Equal(t, PhaseFailed, deploymentPhase(d, replicas, 0))
}

func TestDeploymentPhaseReplicaFailure(t *testing.T) {
	replicas := int32(1)
	d := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 0,
			Conditions: []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentReplicaFailure, Status: corev1.ConditionTrue},
			},
		},
	}
	assert.Equal(t, PhaseFailed, deploymentPhase(d, replicas, 0))
}

func TestDeploymentPhaseScaledZero(t *testing.T) {
	zero := int32(0)
	d := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &zero}}
	assert.Equal(t, PhaseStopped, deploymentPhase(d, 0, 0))
}

func TestDeploymentPhaseRunning(t *testing.T) {
	replicas := int32(3)
	d := &appsv1.Deployment{
		Spec:   appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 3},
	}
	assert.Equal(t, PhaseRunning, deploymentPhase(d, replicas, 3))
}
