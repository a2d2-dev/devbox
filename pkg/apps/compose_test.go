package apps

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComposeLogsSelectsService(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/containers/json":
			_ = json.NewEncoder(w).Encode([]engineContainer{
				{ID: "web-id", Labels: map[string]string{"com.docker.compose.service": "web"}},
				{ID: "worker-id", Labels: map[string]string{"com.docker.compose.service": "worker"}},
			})
		case "/containers/worker-id/logs":
			payload := []byte("worker line\n")
			header := make([]byte, 8)
			header[0] = 1
			binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
			_, _ = w.Write(append(header, payload...))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runtime := &composeRuntime{engine: &dockerEngine{baseURL: server.URL, client: server.Client()}}
	page, err := runtime.Logs(context.Background(), Application{ID: "demo"}, LogOptions{Tail: 20, Service: "worker"})
	assert.NoError(t, err)
	assert.Equal(t, "worker line\n", page.Logs)
	_, err = runtime.Logs(context.Background(), Application{ID: "demo"}, LogOptions{Service: "missing"})
	assert.Error(t, err)
}

func TestAggregatePhase(t *testing.T) {
	cases := []struct {
		name string
		svcs []ServiceStatus
		want Phase
	}{
		{"all running healthy", []ServiceStatus{{State: "running", Health: "healthy"}, {State: "running", Health: "healthy"}}, PhaseRunning},
		{"one unhealthy", []ServiceStatus{{State: "running", Health: "healthy"}, {State: "running", Health: "unhealthy"}}, PhaseDegraded},
		{"all exited", []ServiceStatus{{State: "exited"}, {State: "exited"}}, PhaseStopped},
		{"all dead failed", []ServiceStatus{{State: "dead"}, {State: "dead"}}, PhaseFailed},
		{"exited and dead failed", []ServiceStatus{{State: "exited"}, {State: "dead"}}, PhaseFailed},
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
