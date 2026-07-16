# 虚拟机管理工具

DevBox 控制台内置「虚拟机」系统工具，用于管理本机 libvirt domain。

## API

- `GET /api/v1/vms`：列出本机所有 libvirt 虚拟机，包含状态、vCPU、内存、磁盘、租约 IP 和 guest agent 快照。
- `GET /api/v1/vms/{name}`：读取单台虚拟机详情。
- `POST /api/v1/vms/{name}/control`：执行固定白名单动作。
- `POST /api/v1/vms/{name}/config`：更新虚拟机持久配置。

控制动作只接受：

- `start`
- `shutdown`
- `reboot`
- `destroy`

后端会先用 `virsh list --all --name` 校验 domain 名称，避免任意命令透传。

配置项支持：

- `vcpus`：写入 inactive XML，重启后完整生效。
- `memoryMiB`：写入 inactive XML，重启后完整生效。
- `autostart`：调用 `virsh autostart`，立即生效。

## UI

桌面系统工具中新增「虚拟机」入口。页面包含：

- 虚拟机列表与运行/关机汇总
- 单台虚拟机状态、vCPU、内存、load、IP
- 块设备路径、容量、已分配空间和读写累计
- 共享挂载：从 libvirt domain XML 的 `<filesystem>` 读取 host path / virtiofs tag；VM 运行且 guest agent 可用时，再用 guest `findmnt` 补齐实际 guest 挂载点（例如 `data3 -> /mnt/data3`）。
- guest agent 返回的内存压力
- 启动、重启、关机、强制断电操作
- vCPU、内存、开机自启配置
