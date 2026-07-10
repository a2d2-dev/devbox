# 用 supervisor 部署 devbox

devbox 定位是**无本地显示的 Linux Server 远程管理工具**，本机自身需要一个稳定的托管方式：**用 supervisor 管**，不用 `nohup /tmp/xxx &`（`/tmp` 会被 systemd-tmpfiles 在重启时清空）。同一台机器上其它常驻服务（`rise-cloud` / `edge-ota-agent-dev` / `browsidian` 等 30+）也是这个模式，devbox 跟着约定走。

如果要走容器路线，等 `deploy/docker-compose.yaml` 就位后再加一节。

## 路径约定

跟这台机器上已有 supervisor 服务保持一致：

| 项 | 路径 |
|---|---|
| 二进制 | `<项目源码根>/bin/devbox`（`make build` 产物） |
| 运行时 config | `/etc/devbox/config.yaml` |
| supervisor conf | `/etc/supervisor/conf.d/devbox.conf` |
| stdout log | `/data/log/supervisord/devbox-stdout.log` |
| stderr log | `/data/log/supervisord/devbox-stderr.log` |

`/etc/devbox/` 是 `pkg/config` 的第一优先查找路径（见 `Load()`），命令行只传 `--config /etc/devbox/config.yaml` 即可。

## 一次性初始化

```bash
# 1. 编译，产物在 bin/devbox
cd /data/src/github.com/a2d2-dev/devbox
make build

# 2. 目录 & 日志
sudo mkdir -p /etc/devbox /data/log/supervisord

# 3. config：从 config.yaml.example 复制过来改
sudo cp config.yaml.example /etc/devbox/config.yaml
# 必改的两处：
#   console.port       —— 挑一个不冲突的端口 (见"端口选择")
#   auth.password      —— 本地认证密码
sudo vi /etc/devbox/config.yaml

# 4. supervisor conf：直接软链到 repo 里的模板，
#    以后改模板 = 改本机（避免两处漂移）
sudo ln -s /data/src/github.com/a2d2-dev/devbox/deploy/supervisor/devbox.conf \
           /etc/supervisor/conf.d/devbox.conf

# 5. supervisor 加载 + 启动
sudo supervisorctl reread
sudo supervisorctl add devbox
sudo supervisorctl status devbox   # 应显示 RUNNING
```

## 日常运维

```bash
sudo supervisorctl status devbox
sudo supervisorctl restart devbox
sudo supervisorctl stop devbox
sudo supervisorctl tail devbox stdout           # 实时看日志
sudo supervisorctl tail -f devbox stderr        # zap 日志走 stderr
```

改了 conf 之后：

```bash
sudo supervisorctl reread     # 重读磁盘 conf
sudo supervisorctl update     # 应用变更
```

改了二进制之后：

```bash
cd /data/src/github.com/a2d2-dev/devbox && make build
sudo supervisorctl restart devbox
```

## 端口选择

`console.port` 常见候选：

| 端口 | 说明 |
|---|---|
| **9092** | 当前 devbox 落点。9090 被 mihomo (127.0.0.1) 占，9091 被 edge-ota dev agent 占 |
| 80   | ⚠ 有已知冲突，见下节 |
| 8080 / 8090 | 无冲突且直观，如果不想走 80 又想要"整数端口"，选这两个 |

**别绑 `127.0.0.1` / `localhost`**：LF 从 SSH 外网访问只能走 tun0 (`10.126.126.12`)，绑 loopback 就看不到了。默认 `":<port>"` 绑到 `0.0.0.0` 即可。

配完访问 URL 只用 `http://10.126.126.12:<port>/` 形式（LAN IP 会飘）。

## 已知端口冲突

| 端口 | 占用者 | 类型 | 位置 | 备注 |
|---|---|---|---|---|
| 80 | **tkeel-links** | supervisor 管的 python 静态目录 | `/opt/tkeel-links/index.html` · conf `/etc/supervisor/conf.d/tkeel-links.conf` | `python3 -m http.server 80 --bind 0.0.0.0 --directory /opt/tkeel-links`。开机自启，2026-07-05 观察 uptime 1h15m+ 常驻 |
| 9090 (127.0.0.1) | mihomo (VPN client) | 系统服务 | pid 从 `ss -tlnp \| grep 9090` 查 | 只绑 loopback，devbox 绑 `0.0.0.0:9090` 会冲突（Linux 双栈行为） |
| 9091 | edge-ota agent (dev) | supervisor 管 | edge-platform 工作空间 | dev 状态，偶尔停 |

**要把 devbox 迁到 80 的正规做法**：

1. 决定 tkeel-links 是不是还需要（问 tkeel-links owner，或看 `journalctl` / 静态页 index.html 内容判断是不是历史遗留）
2. 需要留：把它 `edit conf` 改成 8180 或类似闲置端口，`supervisorctl reread && supervisorctl update tkeel-links`
3. 不需要了：`supervisorctl stop tkeel-links` + 在它 conf 里 `autostart=false`（或直接删 conf 归档到 `/etc/supervisor/conf.d.archived/`）
4. 确认 `ss -tlnp \| grep :80` 空
5. 改 `/etc/devbox/config.yaml` 里 `console.port: 80`
6. `supervisorctl restart devbox`
7. 访问 `http://10.126.126.12/`（省掉 `:80`）

**只在 owner 明确同意后动 tkeel-links**。CLAUDE.md 里说过"服务持久化前先跟 LF 对齐"，不擅自 stop 别人的服务。

## 重启后为什么"不用手工重编"

- supervisor 是 systemd 服务，机器重启会自动拉起 supervisord
- supervisord 读 `/etc/supervisor/conf.d/*.conf`，把 `autostart=true` 的都拉起来
- devbox 二进制在 `/data/src/.../bin/devbox`（`/data` 是独立分区，不会被 tmpfiles 清）
- config 在 `/etc/devbox/`（同上）

三处都持久，重启后 devbox 自动回来，跟 supervisor 里其它服务的行为一致。

## 常见问题

**Q: 起来了但 30 秒后 supervisor 报 `Exited too quickly (process log may have details)`**
A: 看 `/data/log/supervisord/devbox-stderr.log` 尾巴。常见：`bind: address already in use` (端口冲突) / `permission denied` (config 权限) / config 里 `auth.password` 类型不对。

**Q: 硬件中心页面里能看到 devbox 自己吗？**
A: 能。devbox 的"进程守护"页面读的是本机 supervisord socket，`devbox` 也是其中一个 `[program:xxx]`。可以从自己的页面里 restart 自己。

**Q: DKMS / CUDA 相关命令 (nvcc / nvidia-smi) 在 devbox 进程里能跑吗？**
A: conf 里 `environment=PATH=` 已经把 `/usr/local/cuda/bin` 加进来。GPU 传感器 (nvidia-smi 分支) 会自动用到。
