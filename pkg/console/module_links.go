package console

import (
	"context"
	"net/http"

	"github.com/a2d2-dev/devbox/pkg/links"
	"github.com/a2d2-dev/devbox/pkg/supervisor"
)

// linksModule 提供"服务导航"功能(吸收自 tkeel-links):
//
//	GET  /api/v1/links         返回清单 + supervisor 关联的实时状态
//	POST /api/v1/links/reload  强制重读磁盘 YAML(无需重启进程)
//
// 对 supervisor 的依赖仅用于把 kind=="supervisor" 段的 badge 覆盖为实时进程状态,
// 通过 Deps 注入而非直接反向依赖 Server——这是 consumer 声明所需能力的最小接线。
type linksModule struct {
	registry   *links.Registry
	supervisor *supervisor.Manager
}

// newLinksModule 构造 links module。registry 为 module 私有;supervisor 为共享依赖。
func newLinksModule(d Deps) *linksModule {
	return &linksModule{
		registry:   links.New(d.Config.LinksPath),
		supervisor: d.Supervisor,
	}
}

func (m *linksModule) Name() string { return "links" }

func (m *linksModule) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/links", m.handleLinks)
	mux.HandleFunc("/api/v1/links/reload", m.handleLinksReload)
}

// links 无后台工作:清单懒加载,并按 reload 请求刷新。
func (m *linksModule) Start(ctx context.Context) error { return nil }
func (m *linksModule) Stop() error                     { return nil }

// handleLinks 返回 links 清单。对于 kind=="supervisor" 的 section,
// 用 supervisor manager 实时查询 items[].supervisor 名字对应的进程状态,
// 覆盖 items[].badge / badgeKind,让"本机服务"段永远显示实时状态而不是配置写死。
func (m *linksModule) handleLinks(w http.ResponseWriter, r *http.Request) {
	if m.registry == nil {
		writeJSON(w, links.Snapshot{})
		return
	}
	snap := m.registry.Snapshot()

	// 只有 supervisor manager 可用时才尝试关联
	if m.supervisor != nil {
		status, err := m.supervisor.GetStatus()
		if err == nil {
			byName := map[string]string{} // supervisor 程序名 → statename
			for _, p := range status.Processes {
				byName[p.Name] = p.StateName
			}
			for i, sec := range snap.Sections {
				if sec.Kind != "supervisor" {
					continue
				}
				for j, item := range sec.Items {
					if item.Supervisor == "" {
						continue
					}
					if st, ok := byName[item.Supervisor]; ok {
						snap.Sections[i].Items[j].Badge = st
						snap.Sections[i].Items[j].BadgeKind = badgeKindForState(st)
					} else {
						snap.Sections[i].Items[j].Badge = "UNKNOWN"
						snap.Sections[i].Items[j].BadgeKind = "muted"
					}
				}
			}
		}
	}

	writeJSON(w, snap)
}

func (m *linksModule) handleLinksReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if m.registry == nil {
		http.Error(w, "links registry not initialised", http.StatusServiceUnavailable)
		return
	}
	m.registry.Reload()
	writeJSON(w, m.registry.Snapshot())
}

func badgeKindForState(state string) string {
	switch state {
	case "RUNNING":
		return "ok"
	case "STARTING", "BACKOFF":
		return "warn"
	case "FATAL", "EXITED":
		return "err"
	case "STOPPED":
		return "muted"
	}
	return "muted"
}
