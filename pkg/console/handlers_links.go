package console

import (
	"net/http"

	"github.com/a2d2-dev/devbox/pkg/links"
)

// registerLinksRoutes 注册"服务导航"路由。
//
//	GET  /api/v1/links         返回清单 + supervisor 关联状态
//	POST /api/v1/links/reload  强制重读磁盘 YAML（不用重启进程）
func (s *Server) registerLinksRoutes() {
	s.mux.HandleFunc("/api/v1/links", s.handleLinks)
	s.mux.HandleFunc("/api/v1/links/reload", s.requireAdmin(s.handleLinksReload))
}

// handleLinks 返回 links 清单。对于 kind=="supervisor" 的 section，
// 用 supervisor manager 实时查询 items[].supervisor 名字对应的进程状态，
// 覆盖 items[].badge / badgeKind，让"本机服务"段永远显示实时状态而不是配置写死。
func (s *Server) handleLinks(w http.ResponseWriter, r *http.Request) {
	if s.links == nil {
		s.jsonOK(w, links.Snapshot{})
		return
	}
	snap := s.links.Snapshot()

	// 只有 supervisor manager 可用时才尝试关联
	if s.supervisorMgr != nil {
		status, err := s.supervisorMgr.GetStatus()
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

	s.jsonOK(w, snap)
}

func (s *Server) handleLinksReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.links == nil {
		http.Error(w, "links registry not initialised", http.StatusServiceUnavailable)
		return
	}
	s.links.Reload()
	s.jsonOK(w, s.links.Snapshot())
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
