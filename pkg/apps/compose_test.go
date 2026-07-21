package apps

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAggregatePhase(t *testing.T) {
	cases := []struct {
		name string
		svcs []ServiceStatus
		want Phase
	}{
		{"all running healthy", []ServiceStatus{{State: "running", Health: "healthy"}, {State: "running", Health: "healthy"}}, PhaseRunning},
		{"one unhealthy", []ServiceStatus{{State: "running", Health: "healthy"}, {State: "running", Health: "unhealthy"}}, PhaseDegraded},
		{"all exited", []ServiceStatus{{State: "exited"}, {State: "exited"}}, PhaseStopped},
		{"partial running", []ServiceStatus{{State: "running", Health: "healthy"}, {State: "exited"}}, PhaseDegraded},
		{"created deploying", []ServiceStatus{{State: "created"}, {State: "running", Health: "healthy"}}, PhaseDeploying},
		{"empty unknown", []ServiceStatus{}, PhaseUnknown},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, aggregatePhase(c.svcs), c.name)
	}
}

func TestParseHealth(t *testing.T) {
	assert.Equal(t, "healthy", parseHealth("Up 5 minutes (healthy)"))
	assert.Equal(t, "unhealthy", parseHealth("Up 5 minutes (unhealthy)"))
	assert.Equal(t, "none", parseHealth("Up 5 minutes"))
}

func TestServiceFromContainer(t *testing.T) {
	ct := engineContainer{
		ID:     "abcdef1234567890",
		Image:  "nginx:1.27",
		State:  "running",
		Status: "Up (healthy)",
		Labels: map[string]string{"com.docker.compose.service": "web"},
		Ports: []enginePort{
			{PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
			{PrivatePort: 443, Type: "tcp"},
		},
	}
	svc := serviceFromContainer(ct)
	assert.Equal(t, "web", svc.Name)
	assert.Equal(t, "nginx:1.27", svc.Image)
	assert.Equal(t, "healthy", svc.Health)
	assert.Equal(t, "abcdef123456", svc.ContainerID)
	assert.Len(t, svc.Ports, 2)
	assert.Equal(t, int32(8080), svc.Ports[0].HostPort)
	assert.Equal(t, int32(80), svc.Ports[0].ContainerPort)
	assert.Equal(t, int32(0), svc.Ports[1].HostPort) // 仅容器端口
}
