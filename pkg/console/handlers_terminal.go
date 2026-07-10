package console

import (
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleTerminalExec WebSocket 终端 — 完整 PTY 交互
func (s *Server) handleTerminalExec(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	// 启动 bash PTY
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		s.logger.Error("PTY start failed", zap.Error(err))
		conn.WriteMessage(websocket.TextMessage, []byte("PTY start failed: "+err.Error()))
		return
	}
	defer ptmx.Close()

	// 设置初始终端大小
	pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120})

	var wg sync.WaitGroup

	// PTY → WebSocket（stdout）
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	// WebSocket → PTY（stdin + resize）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				cmd.Process.Kill()
				return
			}
			if msgType == websocket.TextMessage && len(msg) > 0 && msg[0] == '{' {
				// JSON 消息：resize {"type":"resize","cols":120,"rows":40}
				// 简单解析 resize JSON
				var cols, rows uint16
				for i := 0; i < len(msg)-5; i++ {
					if string(msg[i:i+4]) == "cols" {
						for j := i + 5; j < len(msg); j++ {
							if msg[j] >= '0' && msg[j] <= '9' {
								cols = cols*10 + uint16(msg[j]-'0')
							} else if cols > 0 {
								break
							}
						}
					}
					if string(msg[i:i+4]) == "rows" {
						for j := i + 5; j < len(msg); j++ {
							if msg[j] >= '0' && msg[j] <= '9' {
								rows = rows*10 + uint16(msg[j]-'0')
							} else if rows > 0 {
								break
							}
						}
					}
				}
				if cols > 0 && rows > 0 {
					pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols})
				}
				continue
			}
			// 普通输入
			ptmx.Write(msg)
		}
	}()

	// 等待进程退出
	cmd.Wait()
	wg.Wait()
}
