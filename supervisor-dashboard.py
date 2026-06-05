#!/usr/bin/env python3
"""Supervisor Dashboard — reads supervisor XML-RPC API via unix socket."""

import http.client
import json
import re
import socket
import subprocess
import xmlrpc.client
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import urlparse, parse_qs

SUPERVISOR_SOCK = "/var/run/supervisor.sock"
LISTEN_PORT = 3802

# --- Service Groups ---
SERVICE_GROUPS = [
    ("rise-global",  ("Rise Global", 1)),
    ("rise-cloud",   ("Rise Cloud", 2)),
    ("edge-",        ("Edge Platform", 3)),
    ("camp-",        ("CAMP", 4)),
    ("frpc",         ("Network", 5)),
    ("gost",         ("Network", 5)),
    ("easytier",     ("Network", 5)),
    ("sing-box",     ("Network", 5)),
    ("kcn",          ("Network", 5)),
    ("routa",        ("Network", 5)),
    ("browsidian",   ("Dev Tools", 6)),
    ("claudecodeui", ("Dev Tools", 6)),
    ("opencode",     ("Dev Tools", 6)),
    ("opcode",       ("Dev Tools", 6)),
    ("crush",        ("Dev Tools", 6)),
    ("happy",        ("Dev Tools", 6)),
    ("paperclip",    ("Dev Tools", 6)),
    ("github-",      ("Infra", 7)),
    ("supervisor-",  ("Infra", 7)),
]


def classify_group(name):
    for prefix, (label, order) in SERVICE_GROUPS:
        if name.startswith(prefix):
            return label, order
    return "Other", 99


# --- Supervisor XML-RPC via Unix Socket ---

class UnixHTTPConnection(http.client.HTTPConnection):
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(SUPERVISOR_SOCK)


class UnixTransport(xmlrpc.client.Transport):
    def make_connection(self, host):
        return UnixHTTPConnection(host)


def get_supervisor_proxy():
    return xmlrpc.client.ServerProxy(
        "http://localhost", transport=UnixTransport()
    )


def fetch_all_process_info():
    return get_supervisor_proxy().supervisor.getAllProcessInfo()


def read_log_tail(name, offset=0, length=8192):
    try:
        return get_supervisor_proxy().supervisor.tailProcessStdoutLog(name, offset, length)
    except Exception as e:
        return [str(e), 0, False]


# --- HTTP Handler ---

HTML_PAGE = r"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Neov Dev Server</title>
<style>
  :root {
    --bg: #0f1117; --surface: #1a1d27; --surfaceHover: #222632;
    --border: #2a2d3a; --borderSoft: #232636;
    --text: #e1e4ed; --text2: #b0b4c3; --text3: #8b8fa3; --text4: #5e6275;
    --accent: #6c7ee1; --accentSoft: rgba(108,126,225,.12);
    --green: #34d399; --greenSoft: rgba(52,211,153,.12); --greenBorder: rgba(52,211,153,.25);
    --red: #f87171; --redSoft: rgba(248,113,113,.12); --redBorder: rgba(248,113,113,.25);
    --amber: #fbbf24; --amberSoft: rgba(251,191,36,.12);
    --gray: #64748b; --graySoft: rgba(100,116,139,.12);
    --blue: #60a5fa; --blueSoft: rgba(96,165,250,.12);
  }
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, 'SF Pro Display', 'PingFang SC', 'Noto Sans SC', sans-serif; background: var(--bg); color: var(--text); min-height: 100vh; }
  ::-webkit-scrollbar { width: 6px; } ::-webkit-scrollbar-thumb { background: #3a3d4a; border-radius: 3px; }

  /* Header */
  .header { padding: 20px 32px 14px; display: flex; align-items: center; gap: 14; border-bottom: 1px solid var(--border); }
  .header-logo { width: 28px; height: 28px; border-radius: 8px; background: linear-gradient(135deg, #6c7ee1, #818cf8); display: flex; align-items: center; justify-content: center; font-size: 14px; font-weight: 700; color: white; }
  .header h1 { font-size: 16px; font-weight: 600; letter-spacing: -0.2px; }
  .header-meta { font-size: 12px; color: var(--text3); display: flex; align-items: center; gap: 6; }
  .header-meta .sep { color: var(--text4); }
  .header-stats { margin-left: auto; display: flex; gap: 6; }
  .stat-pill { display: inline-flex; align-items: center; gap: 5px; padding: 3px 10px; border-radius: 999px; font-size: 11px; font-weight: 600; border: 1px solid; }
  .stat-pill.green { color: var(--green); background: var(--greenSoft); border-color: var(--greenBorder); }
  .stat-pill.red { color: var(--red); background: var(--redSoft); border-color: var(--redBorder); }
  .stat-pill.gray { color: var(--text3); background: var(--graySoft); border-color: var(--border); }
  .stat-dot { width: 6px; height: 6px; border-radius: 50%; }
  .stat-dot.green { background: var(--green); }
  .stat-dot.red { background: var(--red); animation: pulse 2s infinite; }
  .stat-dot.gray { background: var(--gray); }

  /* Filter bar */
  .toolbar { padding: 10px 32px; display: flex; gap: 6; }
  .chip { padding: 4px 11px; border-radius: 6px; font-size: 11px; font-weight: 500; cursor: pointer; border: 1px solid var(--border); background: transparent; color: var(--text3); transition: all .15s; user-select: none; }
  .chip:hover { border-color: var(--text4); color: var(--text2); }
  .chip.active { border-color: var(--accent); color: var(--accent); background: var(--accentSoft); }
  .chip .n { margin-left: 3px; opacity: .55; }

  /* Content */
  .content { padding: 6px 32px 40px; }

  /* Group section */
  .group { margin-bottom: 22px; }
  .group-head { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
  .group-label { font-size: 11.5px; font-weight: 700; color: var(--text3); text-transform: uppercase; letter-spacing: 0.6px; }
  .group-line { flex: 1; height: 1px; background: var(--borderSoft); }
  .group-n { font-size: 10px; color: var(--text4); font-weight: 500; }

  /* Card grid */
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 8px; }

  /* Service card */
  .card { background: var(--surface); border: 1px solid var(--border); border-radius: 10px; padding: 12px 14px; display: flex; flex-direction: column; gap: 6px; transition: border-color .15s, background .15s; position: relative; }
  .card.clickable { cursor: pointer; }
  .card.clickable:hover { border-color: var(--accent); background: var(--surfaceHover); }
  .card.clickable:hover .card-open { opacity: 1; }

  .card-row1 { display: flex; align-items: center; gap: 8px; }
  .card-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
  .card-dot.RUNNING { background: var(--green); box-shadow: 0 0 6px var(--green); }
  .card-dot.STOPPED { background: var(--gray); }
  .card-dot.FATAL { background: var(--red); box-shadow: 0 0 6px var(--red); animation: pulse 2s infinite; }
  .card-dot.STARTING, .card-dot.BACKOFF { background: var(--amber); }
  @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:.35} }

  .card-name { font-size: 13px; font-weight: 500; flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .card-badge { font-size: 10px; padding: 1px 7px; border-radius: 4px; font-weight: 600; flex-shrink: 0; }
  .card-badge.RUNNING { background: var(--greenSoft); color: var(--green); }
  .card-badge.STOPPED { background: var(--graySoft); color: var(--text3); }
  .card-badge.FATAL { background: var(--redSoft); color: var(--red); }
  .card-badge.STARTING, .card-badge.BACKOFF { background: var(--amberSoft); color: var(--amber); }

  .card-row2 { display: flex; align-items: center; gap: 10px; font-size: 11px; color: var(--text3); min-height: 16px; }
  .card-row2 span { white-space: nowrap; }
  .card-port { color: var(--blue); font-weight: 600; font-family: ui-monospace, monospace; }

  /* Open arrow indicator */
  .card-open { position: absolute; right: 12px; top: 50%; transform: translateY(-50%); opacity: 0; transition: opacity .15s; color: var(--text4); font-size: 16px; }

  /* Log button on card */
  .card-log { position: absolute; right: 10px; bottom: 10px; width: 22px; height: 22px; border-radius: 5px; border: 1px solid var(--border); background: var(--bg); color: var(--text4); display: flex; align-items: center; justify-content: center; cursor: pointer; font-size: 11px; opacity: 0; transition: opacity .15s; z-index: 2; }
  .card:hover .card-log { opacity: 1; }
  .card-log:hover { border-color: var(--accent); color: var(--accent); background: var(--accentSoft); }

  /* Log panel */
  .overlay { display: none; position: fixed; inset: 0; background: rgba(0,0,0,.55); z-index: 100; backdrop-filter: blur(2px); }
  .overlay.open { display: flex; justify-content: center; align-items: center; }
  .log-panel { background: var(--surface); border: 1px solid var(--border); border-radius: 12px; width: min(90vw, 960px); max-height: 80vh; display: flex; flex-direction: column; box-shadow: 0 24px 48px -12px rgba(0,0,0,.4); }
  .log-header { padding: 14px 18px; border-bottom: 1px solid var(--border); display: flex; align-items: center; gap: 10px; }
  .log-header h2 { font-size: 14px; font-weight: 500; flex: 1; }
  .log-btn { background: none; border: 1px solid var(--border); color: var(--text3); border-radius: 6px; padding: 3px 10px; cursor: pointer; font-size: 11px; font-weight: 500; }
  .log-btn:hover { border-color: var(--accent); color: var(--text); }
  .log-body { padding: 12px 16px; overflow: auto; flex: 1; }
  .log-body pre { font-family: 'SF Mono', 'Fira Code', ui-monospace, monospace; font-size: 11.5px; line-height: 1.55; white-space: pre-wrap; word-break: break-all; color: var(--text3); }

  .footer { text-align: center; padding: 6px; font-size: 10px; color: var(--text4); }
</style>
</head>
<body>

<div class="header">
  <div class="header-logo">N</div>
  <h1 style="margin-left:10px">Neov Dev Server</h1>
  <div class="header-meta" style="margin-left:14px">
    <span id="host-info"></span>
    <span class="sep">&middot;</span>
    <span id="refresh-ts"></span>
  </div>
  <div class="header-stats" id="stats"></div>
</div>

<div class="toolbar" id="toolbar"></div>
<div class="content" id="content"></div>
<div class="footer">Auto-refresh 5s &middot; Click service to open &middot; Hover for logs</div>

<div class="overlay" id="overlay" onclick="if(event.target===this)closeLogs()">
  <div class="log-panel">
    <div class="log-header">
      <div class="card-dot" id="log-dot" style="width:8px;height:8px"></div>
      <h2 id="log-title"></h2>
      <button class="log-btn" onclick="refreshLog()">Refresh</button>
      <button class="log-btn" onclick="closeLogs()">Close</button>
    </div>
    <div class="log-body"><pre id="log-content">Loading...</pre></div>
  </div>
</div>

<script>
const HOST = location.hostname;
let allProcs = [];
let filter = 'ALL';
let currentLogName = null;

async function fetchData() {
  const r = await fetch('/api/status');
  const data = await r.json();
  allProcs = data.processes;
  document.getElementById('host-info').textContent = data.hostname + ' \u00b7 ' + data.ip;
  document.getElementById('refresh-ts').textContent = new Date().toLocaleTimeString();
  render();
}

function render() {
  const counts = { ALL: allProcs.length, RUNNING: 0, STOPPED: 0, FATAL: 0, OTHER: 0 };
  allProcs.forEach(p => {
    if (counts[p.statename] !== undefined) counts[p.statename]++;
    else counts.OTHER++;
  });

  // Stats pills
  const stats = document.getElementById('stats');
  stats.innerHTML =
    '<div class="stat-pill green"><div class="stat-dot green"></div>' + counts.RUNNING + ' Running</div>' +
    (counts.FATAL ? '<div class="stat-pill red"><div class="stat-dot red"></div>' + counts.FATAL + ' Fatal</div>' : '') +
    (counts.STOPPED ? '<div class="stat-pill gray"><div class="stat-dot gray"></div>' + counts.STOPPED + ' Stopped</div>' : '');

  // Toolbar
  const toolbar = document.getElementById('toolbar');
  toolbar.innerHTML = '';
  for (const [key, val] of Object.entries(counts)) {
    if (val === 0 && key !== 'ALL') continue;
    const el = document.createElement('div');
    el.className = 'chip' + (filter === key ? ' active' : '');
    el.innerHTML = key + '<span class="n">' + val + '</span>';
    el.onclick = () => { filter = key; render(); };
    toolbar.appendChild(el);
  }

  // Filter
  const filtered = filter === 'ALL' ? allProcs :
    filter === 'OTHER' ? allProcs.filter(p => !['RUNNING','STOPPED','FATAL'].includes(p.statename)) :
    allProcs.filter(p => p.statename === filter);

  // Group
  const groups = {};
  filtered.forEach(p => {
    const g = p.group || 'Other';
    if (!groups[g]) groups[g] = { order: p.groupOrder || 99, items: [] };
    groups[g].items.push(p);
  });
  const sorted = Object.entries(groups).sort((a, b) => a[1].order - b[1].order);

  // Render
  const content = document.getElementById('content');
  content.innerHTML = '';

  sorted.forEach(([label, { items }]) => {
    const sec = document.createElement('div');
    sec.className = 'group';
    sec.innerHTML =
      '<div class="group-head">' +
        '<span class="group-label">' + esc(label) + '</span>' +
        '<div class="group-line"></div>' +
        '<span class="group-n">' + items.length + '</span>' +
      '</div>';

    const grid = document.createElement('div');
    grid.className = 'grid';

    items.forEach(p => {
      const hasPort = p.port && p.statename === 'RUNNING';
      const url = hasPort ? 'http://' + HOST + ':' + p.port : null;

      const card = document.createElement('div');
      card.className = 'card' + (hasPort ? ' clickable' : '');

      if (hasPort) {
        card.onclick = (e) => {
          if (e.target.closest('.card-log')) return;
          window.open(url, '_blank');
        };
      }

      const uptime = p.statename === 'RUNNING' ? formatUptime(p.now - p.start) : '';
      const pid = p.pid ? 'PID ' + p.pid : '';

      let row2 = '';
      if (p.port) row2 += '<span class="card-port">:' + p.port + '</span>';
      if (pid) row2 += '<span>' + pid + '</span>';
      if (uptime) row2 += '<span>' + uptime + '</span>';
      if (p.description && p.statename !== 'RUNNING') row2 += '<span>' + esc(p.description) + '</span>';

      card.innerHTML =
        '<div class="card-row1">' +
          '<div class="card-dot ' + p.statename + '"></div>' +
          '<span class="card-name">' + esc(p.name) + '</span>' +
          '<span class="card-badge ' + p.statename + '">' + p.statename + '</span>' +
        '</div>' +
        '<div class="card-row2">' + row2 + '</div>' +
        (hasPort ? '<div class="card-open">\u2197</div>' : '') +
        '<div class="card-log" onclick="event.stopPropagation();openLogs(\'' + esc(p.name) + '\',\'' + p.statename + '\')" title="View logs">\u2261</div>';

      grid.appendChild(card);
    });

    sec.appendChild(grid);
    content.appendChild(sec);
  });
}

function formatUptime(s) {
  if (s < 60) return s + 's';
  if (s < 3600) return Math.floor(s/60) + 'm';
  if (s < 86400) return Math.floor(s/3600) + 'h ' + Math.floor((s%3600)/60) + 'm';
  return Math.floor(s/86400) + 'd ' + Math.floor((s%86400)/3600) + 'h';
}

function esc(s) { const d = document.createElement('div'); d.textContent = s; return d.innerHTML; }

function openLogs(name, state) {
  currentLogName = name;
  document.getElementById('overlay').classList.add('open');
  document.getElementById('log-title').textContent = name;
  document.getElementById('log-dot').className = 'card-dot ' + state;
  document.getElementById('log-content').textContent = 'Loading...';
  refreshLog();
}

async function refreshLog() {
  if (!currentLogName) return;
  try {
    const r = await fetch('/api/log?name=' + encodeURIComponent(currentLogName));
    const data = await r.json();
    const pre = document.getElementById('log-content');
    pre.textContent = data.log || '(empty)';
    pre.parentElement.scrollTop = pre.parentElement.scrollHeight;
  } catch(e) {
    document.getElementById('log-content').textContent = 'Error: ' + e.message;
  }
}

function closeLogs() {
  document.getElementById('overlay').classList.remove('open');
  currentLogName = null;
}

document.addEventListener('keydown', e => { if (e.key === 'Escape') closeLogs(); });

fetchData();
setInterval(fetchData, 5000);
</script>
</body>
</html>
"""


class Handler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        pass

    def do_GET(self):
        if self.path == "/api/status":
            self._json_response(self._build_status())
        elif self.path.startswith("/api/log"):
            name = self._query_param("name")
            if name:
                self._json_response(self._build_log(name))
            else:
                self._json_response({"error": "missing name"}, 400)
        else:
            self._html_response(HTML_PAGE)

    def _build_status(self):
        procs = fetch_all_process_info()
        hostname = socket.gethostname()
        try:
            ip = subprocess.check_output(
                "hostname -I 2>/dev/null | awk '{print $1}'",
                shell=True, timeout=2
            ).decode().strip()
        except Exception:
            ip = "unknown"

        result = []
        for p in procs:
            port = None
            conf_path = f"/etc/supervisor/conf.d/{p['name']}.conf"
            try:
                with open(conf_path) as f:
                    content = f.read()
                m = re.search(r'(?:--port[= ]|[ ]-p[ ])(\d{4,5})', content)
                if m:
                    port = m.group(1)
            except FileNotFoundError:
                pass

            group_label, group_order = classify_group(p["name"])

            result.append({
                "name": p["name"],
                "group": group_label,
                "groupOrder": group_order,
                "statename": p["statename"],
                "pid": p["pid"],
                "start": p["start"],
                "now": p["now"],
                "description": p["description"],
                "port": port,
            })

        return {"hostname": hostname, "ip": ip, "processes": result}

    def _build_log(self, name):
        try:
            log_data = read_log_tail(name, 0, 32768)
            return {"name": name, "log": log_data[0]}
        except Exception as e:
            return {"name": name, "log": str(e)}

    def _query_param(self, key):
        q = parse_qs(urlparse(self.path).query)
        vals = q.get(key, [])
        return vals[0] if vals else None

    def _json_response(self, data, code=200):
        body = json.dumps(data).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _html_response(self, html):
        body = html.encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    server = HTTPServer(("0.0.0.0", LISTEN_PORT), Handler)
    print(f"Supervisor Dashboard listening on http://127.0.0.1:{LISTEN_PORT}")
    server.serve_forever()
