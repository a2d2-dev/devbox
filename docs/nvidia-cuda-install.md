# NVIDIA 驱动 + CUDA 12.4 + cuDNN 安装流程

面向 devbox（Ubuntu 22.04.5 LTS · Kernel 5.15.x），验证机器：GTX 1080 (Pascal, sm_61)。

## 版本决策链

先定框架 → 定 CUDA → 定驱动，不能反过来。

| 层 | 版本 | 依据 |
|---|---|---|
| CUDA Toolkit | **12.4** | PyTorch 2.4/2.5、vLLM、ComfyUI、SD WebUI 主流 wheel 目标 |
| NVIDIA 驱动 | **550.x** | CUDA 12.4 要求 ≥ 550；550 是 LTS 起点，长期支持 Pascal |
| cuDNN | **9.x for CUDA 12** | 与 CUDA 12.4 匹配的默认 |

**兼容性说明**：Pascal (GTX 10 系) 已进 NVIDIA legacy 分支，CUDA 12.x 是最后支持代（CUDA 13 已砍 Pascal）。这个组合是 GTX 1080 的最优终态。

## 前置检查

```bash
# 内核头文件 (DKMS 编译需要)
dpkg -l | grep "linux-headers-$(uname -r)"

# 磁盘空间 (整套下来 6-8 GB)
df -h /usr/local /var/cache/apt

# 现有 nvidia 包 (应为空)
dpkg -l | grep -iE "^ii  (nvidia|cuda|libnvidia)"

# GPU 确认存在
lspci -nn | grep -iE "vga|3d|display"
```

## 步骤 1 · 加 NVIDIA CUDA 官方 repo

**不要**只用 Ubuntu universe 里的 `nvidia-driver-XXX`。CUDA toolkit 版本要跟 NVIDIA 官方 repo 拿，能精确锁 12.4。

```bash
wget https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/x86_64/cuda-keyring_1.1-1_all.deb -O /tmp/cuda-keyring.deb
sudo dpkg -i /tmp/cuda-keyring.deb
sudo apt-get update
```

## 步骤 2 · 装驱动 + CUDA Toolkit 12.4

NVIDIA repo 里 `cuda-toolkit-12-4` 元包会自动带上匹配版本的驱动依赖，但**更稳的做法是显式装驱动**，避免元包升级把驱动带飞。

```bash
# 显式装 550 系列驱动 (含 DKMS 模块)
sudo apt-get install -y nvidia-driver-550
```

**CUDA toolkit 建议装最小集，不要用 `cuda-toolkit-12-4` 元包**——它会拖进：
- `cuda-nsight-compute-12-4` (1.4 GB, GUI profiler, 服务器无用)
- `cuda-nsight-systems-12-4` (770 MB, 同上)
- `cuda-documentation-12-4` / `cuda-demo-suite-12-4` (数百 MB)

最小集覆盖 PyTorch/vLLM/ComfyUI 需要的全部（nvcc + cuBLAS + cuFFT + cuSPARSE + cuRAND + cuSOLVER + NPP + NVTX + NVRTC）：

```bash
sudo apt-get install -y \
    cuda-minimal-build-12-4 \    # nvcc + cudart-dev + cccl + profiler-api
    cuda-libraries-12-4 \        # 运行时: cuBLAS/cuFFT/cuSPARSE/cuRAND/cuSOLVER/NPP/OpenCL...
    cuda-libraries-dev-12-4 \    # dev 版本 (含头文件 + 静态库)
    cuda-nvtx-12-4 \             # PyTorch / vLLM profiling 需要
    cuda-nvrtc-dev-12-4          # JIT 编译
```

装完 `/usr/local/cuda-12.4/` 出现（4.3 GB），同时 `/usr/local/cuda` 通过 alternatives 软链到它。

### 空间受限：把 CUDA 装到 /data 上

如果 `/` 分区紧张、`/data` 有独立大分区（devbox 通常是这样），**在装之前**预置软链，让 apt 直接把 4.3 GB 落到 /data：

```bash
sudo mkdir -p /data/_system/cuda-12.4
sudo ln -s /data/_system/cuda-12.4 /usr/local/cuda-12.4
# 然后执行上面的 apt install，文件全落到 /data
```

链路：`/usr/local/cuda` → `/etc/alternatives/cuda` → `/usr/local/cuda-12.4` → `/data/_system/cuda-12.4`
alternatives 由 apt 自动维护，一次做完不用管。

> **⚠ 教训 (2026-07-05)**：不要在 `apt purge` cuda 子包后紧跟 `apt-get autoremove -y`。
> autoremove 会把 `cuda-toolkit-12-4` 元包判成"孤儿"一起拆掉，需要重装。要清理孤儿包时先 `apt-mark manual cuda-toolkit-12-4`（或改用最小集就不需要元包，天然免疫）。

## 步骤 3 · cuDNN 9 for CUDA 12

```bash
sudo apt-get install -y cudnn9-cuda-12
```

装完在 `/usr/lib/x86_64-linux-gnu/libcudnn*.so.9*`。

## 步骤 4 · 环境变量

写到 `/etc/profile.d/cuda.sh`，所有 shell 用户都生效：

```bash
sudo tee /etc/profile.d/cuda.sh > /dev/null <<'EOF'
export PATH=/usr/local/cuda/bin:$PATH
export LD_LIBRARY_PATH=/usr/local/cuda/lib64:$LD_LIBRARY_PATH
EOF
sudo chmod 0644 /etc/profile.d/cuda.sh
```

新登录 shell 会自动加载。当前 shell 手动 `source /etc/profile.d/cuda.sh`。

## 步骤 5 · 重启 (必需)

驱动模块要卸掉 nouveau、加载 nvidia，必须重启：

```bash
sudo reboot
```

**重启前必看**：devbox 这台机器上 supervisor 管着 32 个进程（rise-cloud、edge-ota-agent-dev 等），reboot 全会中断。选合适时间。

## 步骤 6 · 验证 (重启后)

```bash
# 1. 驱动 + GPU 识别
nvidia-smi
# 期望输出：
#   Driver Version: 550.xxx
#   CUDA Version:   12.4  (显示的是 driver 支持的 max，不是装的 toolkit)
#   GPU 0: NVIDIA GeForce GTX 1080  8 GiB

# 2. CUDA 编译器
nvcc --version
# Cuda compilation tools, release 12.4, Vxx.xx.xxx

# 3. cuDNN 加载
ldconfig -p | grep cudnn
# libcudnn.so.9 → /usr/lib/.../libcudnn.so.9.x.x

# 4. 端到端 (PyTorch)
python3 -c "
import torch
print('CUDA available:', torch.cuda.is_available())
print('Device:', torch.cuda.get_device_name(0))
print('Compute capability:', torch.cuda.get_device_capability(0))
print('torch built with CUDA:', torch.version.cuda)
"
# 期望：CUDA available: True · Device: NVIDIA GeForce GTX 1080 · CC: (6, 1)
```

如果 `nvidia-smi` 报 `NVIDIA-SMI has failed because it couldn't communicate with the NVIDIA driver`：
1. `sudo dkms status` 看模块编译状态
2. `dmesg | grep -i nvidia | tail` 找加载失败原因
3. `lsmod | grep -iE "nvidia|nouveau"` 应有 nvidia 无 nouveau

## 步骤 7 · devbox 硬件中心确认

浏览器打开 http://10.126.126.12:9092/ → 硬件中心 → 显卡：
- 驱动应显示 `nvidia`（不再是 `nouveau`）
- 检查异常段的 "nouveau" 告警消失
- PCIe 协商如果起了负载会自动升到 Gen3 x16（当前 idle 会继续 Gen1）

传感器页 GPU 段：
- 温度继续可见（走同一个 hwmon 通道）
- **功耗列现在会有数值**（nouveau 不给功耗，nvidia 官方驱动给）

如果要功耗数据出现，devbox 后端还得改一版：新增 `nvidia-smi --query-gpu=power.draw` 采集分支，nouveau 走 sysfs、nvidia 走 nvidia-smi。TODO。

## 可选 · Docker/K8s GPU 支持

如果要在容器里跑 CUDA workload：

```bash
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | \
  sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
  sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
  sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list
sudo apt-get update
sudo apt-get install -y nvidia-container-toolkit
sudo nvidia-ctk runtime configure --runtime=docker
sudo systemctl restart docker
# 验证
sudo docker run --rm --gpus all nvidia/cuda:12.4.1-base-ubuntu22.04 nvidia-smi
```

## 回滚

如果新驱动出问题：

```bash
sudo apt-get purge -y '^nvidia-.*' '^libnvidia-.*' '^cuda-.*' '^cudnn.*'
sudo apt-get autoremove -y
sudo rm -f /etc/profile.d/cuda.sh
sudo reboot
# 重启后回到 nouveau
```
