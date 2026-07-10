import { T } from '../tokens';

export const DEVICE = {
  name: 'GB10-DEV-01',
  alias: '我的 AI 工作站',
  sn: 'NVIDIA-GB10-2025-DEV-0042',
  site: 'AI 算法实验室',
  dept: '模型研发组',
  os: 'Ubuntu 24.04.1 LTS',
  agent: 'Edge Platform v1.0.0-dev',
  model: 'NVIDIA GB10 · 20-core Arm + Blackwell · 128GB 统一内存',
  uptime: '6 天 14 小时 22 分钟',
  username: 'dev-zhang',
};

export const APPS = [
  // ─ System tools (8) ─
  { id: 'dashboard', kind: 'system', name: '仪表盘',     icon: 'dashboard', color: T.blue,   bg: 'linear-gradient(160deg,#3b82f6,#1d4ed8)' },
  { id: 'store',     kind: 'system', name: '应用商店',   icon: 'store',     color: T.green,  bg: 'linear-gradient(160deg,#34d399,#059669)' },
  { id: 'files',     kind: 'system', name: '文件',       icon: 'folder',    color: '#0891b2', bg: 'linear-gradient(160deg,#22d3ee,#0891b2)' },
  { id: 'models',    kind: 'system', name: '模型仓库',   icon: 'brain',     color: T.violet, bg: 'linear-gradient(160deg,#a78bfa,#7c3aed)' },
  { id: 'processes', kind: 'system', name: '进程',       icon: 'system',    color: '#475569', bg: 'linear-gradient(160deg,#64748b,#1e293b)' },
  { id: 'disks',     kind: 'system', name: '磁盘',       icon: 'hardDrive', color: '#1e40af', bg: 'linear-gradient(160deg,#3b82f6,#1e3a8a)' },
  { id: 'network-connections', kind: 'system', name: '网络连接', icon: 'network', color: '#0e7490', bg: 'linear-gradient(160deg,#06b6d4,#0e7490)' },
  { id: 'alerts',    kind: 'system', name: '告警中心',   icon: 'bell',      color: T.amber,  bg: 'linear-gradient(160deg,#fbbf24,#d97706)', badge: 2 },
  { id: 'settings',  kind: 'system', name: '系统设置',     icon: 'gear',      color: T.slate,  bg: 'linear-gradient(160deg,#64748b,#334155)' },

  // ─ Deployed apps (8) ─
  { id: 'vscode',    kind: 'app', name: 'VS Code',
    icon: 'code', color: '#0078d4', bg: 'linear-gradient(160deg,#3b82f6,#1e40af)',
    state: 'running', version: '4.96.4', preset: 'code-server',
    category: '开发环境', gpu: 'CPU', gpuPct: 0, qps: null, p99: null,
    desc: '浏览器中的 VS Code，远程开发首选',
    deployedAt: '2026-05-20 09:14',
  },
  { id: 'jupyter',   kind: 'app', name: 'JupyterLab',
    icon: 'jupyter', color: '#f37726', bg: 'linear-gradient(160deg,#fb923c,#c2410c)',
    state: 'running', version: '4.3.1', preset: 'jupyter-cuda',
    category: '开发环境', gpu: 'GPU', gpuPct: 12, qps: null, p99: null,
    desc: '交互式 Notebook，CUDA 12.4 + PyTorch 预装',
    deployedAt: '2026-05-22 14:08',
  },
  { id: 'ollama',    kind: 'app', name: 'Ollama',
    icon: 'ollama', color: '#0f172a', bg: 'linear-gradient(160deg,#475569,#0f172a)',
    state: 'running', version: 'v0.4.7', preset: 'ollama',
    category: 'AI 推理', gpu: 'GPU', gpuPct: 28, qps: 22, p99: 620,
    desc: '本地 LLM 推理引擎，OpenAI 兼容 API',
    deployedAt: '2026-05-23 14:02',
  },
  { id: 'vllm',      kind: 'app', name: 'vLLM',
    icon: 'vllm', color: T.red, bg: 'linear-gradient(160deg,#fb7185,#be123c)',
    state: 'error', version: 'v0.6.4', preset: 'vllm-openai',
    category: 'AI 推理', gpu: 'GPU', gpuPct: 0, qps: 0, p99: 0, lastOk: '10:23',
    desc: '高性能 LLM 推理服务，PagedAttention 显存优化',
    deployedAt: '2026-05-25 16:20',
  },
  { id: 'comfyui',   kind: 'app', name: 'ComfyUI',
    icon: 'comfy', color: '#7c3aed', bg: 'linear-gradient(160deg,#a78bfa,#6d28d9)',
    state: 'running', version: 'v0.3.10', preset: 'comfyui',
    category: '图像生成', gpu: 'GPU', gpuPct: 18, qps: null, p99: null,
    desc: '节点式 Stable Diffusion 工作流',
    deployedAt: '2026-05-24 11:30',
  },
  { id: 'sdwebui',   kind: 'app', name: 'SD WebUI',
    icon: 'palette', color: '#db2777', bg: 'linear-gradient(160deg,#f472b6,#9d174d)',
    state: 'running', version: 'v1.10.1', preset: 'sd-webui',
    category: '图像生成', gpu: 'GPU', gpuPct: 8, qps: null, p99: null,
    desc: 'AUTOMATIC1111 · 文生图 + 图生图',
    deployedAt: '2026-05-24 12:00',
  },
  { id: 'openwebui', kind: 'app', name: 'Open WebUI',
    icon: 'openwebui', color: '#0891b2', bg: 'linear-gradient(160deg,#22d3ee,#0891b2)',
    state: 'running', version: 'v0.4.7', preset: 'open-webui',
    category: 'AI 应用', gpu: 'CPU', gpuPct: 0, qps: 3, p99: 48,
    desc: '多模型聊天前端，连接 Ollama / vLLM',
    deployedAt: '2026-05-22 11:48',
  },
  { id: 'training',  kind: 'app', name: 'train-72h',
    icon: 'flame', color: '#ea580c', bg: 'linear-gradient(160deg,#fb923c,#9a3412)',
    state: 'running', version: 'unsloth+axolotl', preset: 'training-job',
    category: '训练任务', gpu: 'GPU', gpuPct: 32, qps: null, p99: null,
    desc: 'LoRA 微调 · llama-3.1-8b-base · Step 14,300 / 30,000',
    deployedAt: '2026-05-25 06:00',
  },
];

export const APP_BY_ID = Object.fromEntries(APPS.map(a => [a.id, a]));

// ─── App Store catalog (RunPod-style) ──────────────────────────
export const STORE_CATEGORIES = [
  { id: 'all',      name: '全部',     icon: 'apps',     count: 32 },
  { id: 'ide',      name: '开发环境', icon: 'code',     count: 5  },
  { id: 'infer',    name: 'AI 推理',  icon: 'sparkle',  count: 7  },
  { id: 'image',    name: '图像生成', icon: 'palette',  count: 5  },
  { id: 'train',    name: '训练框架', icon: 'flame',    count: 4  },
  { id: 'apps',     name: 'AI 应用',  icon: 'apps',     count: 8  },
  { id: 'storage',  name: '存储 / 向量库', icon: 'database', count: 3 },
];

// Featured: big hero cards with image-style backgrounds
export const STORE_FEATURED = [
  {
    id: 'pytorch',
    title: 'PyTorch 2.5.1',
    subtitle: '官方镜像 · CUDA 12.4',
    long: '预装 PyTorch + Transformers + Accelerate，开箱即用的训练 / 推理基础环境。',
    icon: 'flame', bg: 'linear-gradient(135deg,#dc2626,#7f1d1d)', accent: '#fecaca',
    tagline: 'Python 3.11 · CUDA 12.4 · cuDNN 9',
    badges: ['PyTorch 2.5.1', 'CUDA 12.4', '14.2 GB'],
  },
  {
    id: 'vllm-template',
    title: 'vLLM 0.6 推理服务',
    subtitle: '高吞吐量 LLM 服务',
    long: 'PagedAttention + 连续批处理，吞吐量较 HuggingFace 提升 24×。',
    icon: 'vllm', bg: 'linear-gradient(135deg,#0f172a,#020617)', accent: '#94a3b8',
    tagline: '开箱支持 Llama / Qwen / DeepSeek / Mistral',
    badges: ['vLLM 0.6.4', 'CUDA 12.4', '8.1 GB'],
  },
  {
    id: 'comfy-sdxl',
    title: 'ComfyUI + SDXL',
    subtitle: '图像生成全家桶',
    long: 'ComfyUI 节点编辑器 + SDXL Base/Refiner + 常用 LoRA & ControlNet。',
    icon: 'comfy', bg: 'linear-gradient(135deg,#7c3aed,#3b0764)', accent: '#ddd6fe',
    tagline: '内置 30+ 节点扩展和示例工作流',
    badges: ['ComfyUI 0.3.10', 'SDXL 1.0', '22.8 GB'],
  },
];

export const STORE_APPS = [
  // ─ Installed ─
  { id: 'vscode',    name: 'VS Code Server',     cat: '开发环境', ver: 'v4.96.4',  base: 'CUDA 12.4', size: '2.1 GB', icon: 'code',      bg: 'linear-gradient(160deg,#3b82f6,#1e40af)', desc: '浏览器版 VS Code，code-server 4.x，支持远程开发',                                  installed: true,  rating: 4.9, downloads: '46.2k', source: 'coder.com' },
  { id: 'jupyter',   name: 'JupyterLab',         cat: '开发环境', ver: 'v4.3.1',   base: 'CUDA 12.4', size: '3.8 GB', icon: 'jupyter',   bg: 'linear-gradient(160deg,#fb923c,#c2410c)', desc: 'Jupyter Lab 4 · PyTorch / Transformers / pandas 预装',                                installed: true,  rating: 4.8, downloads: '54.1k', source: 'jupyter.org' },
  { id: 'ollama',    name: 'Ollama',             cat: 'AI 推理',  ver: 'v0.4.7',   base: 'CUDA 12.4', size: '0.4 GB', icon: 'ollama',    bg: 'linear-gradient(160deg,#475569,#0f172a)', desc: '本地大模型推理，OpenAI 兼容 API，模型库一键拉取',                                       installed: true,  rating: 4.9, downloads: '38.2k', source: 'ollama.ai' },
  { id: 'vllm',      name: 'vLLM',               cat: 'AI 推理',  ver: 'v0.6.4',   base: 'CUDA 12.4', size: '8.1 GB', icon: 'vllm',      bg: 'linear-gradient(160deg,#fb7185,#be123c)', desc: '高性能 LLM 推理服务 · PagedAttention 显存优化 · 吞吐量提升 24×',                       installed: true,  rating: 4.9, downloads: '24.7k', source: 'vllm.ai' },
  { id: 'comfyui',   name: 'ComfyUI',            cat: '图像生成', ver: 'v0.3.10',  base: 'CUDA 12.4', size: '4.2 GB', icon: 'comfy',     bg: 'linear-gradient(160deg,#a78bfa,#6d28d9)', desc: '节点式 Stable Diffusion 工作流编辑器，30+ 扩展节点',                                  installed: true,  rating: 4.8, downloads: '32.5k', source: 'github.com/comfyanonymous' },
  { id: 'sdwebui',   name: 'SD WebUI',           cat: '图像生成', ver: 'v1.10.1',  base: 'CUDA 12.4', size: '5.6 GB', icon: 'palette',   bg: 'linear-gradient(160deg,#f472b6,#9d174d)', desc: 'AUTOMATIC1111 · 经典 Stable Diffusion Web UI，文生图 / 图生图 / 插件生态最丰富',         installed: true,  rating: 4.7, downloads: '29.8k', source: 'github.com/AUTOMATIC1111' },
  { id: 'openwebui', name: 'Open WebUI',         cat: 'AI 应用',  ver: 'v0.4.7',   base: 'Python 3.11', size: '1.4 GB', icon: 'openwebui', bg: 'linear-gradient(160deg,#22d3ee,#0891b2)', desc: '多模型聊天前端，连 Ollama / vLLM / OpenAI，支持 RAG 与图像生成',                       installed: true,  rating: 4.7, downloads: '21.8k', source: 'open-webui.com' },
  { id: 'training',  name: 'Unsloth 训练镜像',   cat: '训练框架', ver: 'v2024.12', base: 'CUDA 12.4', size: '6.8 GB', icon: 'flame',     bg: 'linear-gradient(160deg,#fb923c,#9a3412)', desc: 'Unsloth + Axolotl + DeepSpeed，LoRA / QLoRA / 全参微调一站搞定',                       installed: true,  rating: 4.8, downloads: '12.6k', source: 'github.com/unslothai' },

  // ─ Available templates ─
  { id: 'pytorch',   name: 'PyTorch 2.5.1',      cat: '训练框架', ver: '2.5.1',    base: 'CUDA 12.4', size: '14.2 GB', icon: 'flame',    bg: 'linear-gradient(160deg,#dc2626,#7f1d1d)', desc: 'PyTorch 官方镜像，Transformers + Accelerate + Datasets 预装',                          installed: false, rating: 5.0, downloads: '89.4k', source: 'pytorch.org' },
  { id: 'tensorflow',name: 'TensorFlow 2.16',    cat: '训练框架', ver: '2.16.1',   base: 'CUDA 12.3', size: '11.6 GB', icon: 'flame',    bg: 'linear-gradient(160deg,#fb923c,#c2410c)', desc: 'TensorFlow + Keras 3 + TensorBoard',                                                  installed: false, rating: 4.7, downloads: '52.3k', source: 'tensorflow.org' },
  { id: 'tgi',       name: 'TGI',                cat: 'AI 推理',  ver: 'v2.4.0',   base: 'CUDA 12.2', size: '9.4 GB',  icon: 'sparkle',  bg: 'linear-gradient(160deg,#fbbf24,#d97706)', desc: 'HuggingFace Text Generation Inference · 生产级 LLM 服务化方案',                        installed: false, rating: 4.7, downloads: '8.6k',  source: 'huggingface.co/tgi' },
  { id: 'sglang',    name: 'SGLang',             cat: 'AI 推理',  ver: 'v0.3.6',   base: 'CUDA 12.4', size: '7.2 GB',  icon: 'sparkle',  bg: 'linear-gradient(160deg,#22d3ee,#0891b2)', desc: '快速结构化 LLM 推理 · RadixAttention 缓存共享，复杂提示快 5×',                          installed: false, rating: 4.6, downloads: '3.4k',  source: 'github.com/sgl-project' },
  { id: 'triton',    name: 'Triton Server',      cat: 'AI 推理',  ver: 'v24.10',   base: 'CUDA 12.6', size: '12.8 GB', icon: 'sparkle',  bg: 'linear-gradient(160deg,#10b981,#064e3b)', desc: 'NVIDIA Triton Inference Server · 多模型 / 多框架统一服务',                              installed: false, rating: 4.6, downloads: '18.2k', source: 'nvidia.com' },
  { id: 'litellm',   name: 'LiteLLM',            cat: 'AI 推理',  ver: 'v1.55.3',  base: 'Python 3.11', size: '0.8 GB', icon: 'sparkle', bg: 'linear-gradient(160deg,#a78bfa,#5b21b6)', desc: '100+ LLM 提供商统一代理，OpenAI 兼容路由 + 配额 + 缓存',                                installed: false, rating: 4.7, downloads: '11.4k', source: 'litellm.ai' },
  { id: 'lmstudio',  name: 'LM Studio Server',   cat: 'AI 推理',  ver: 'v0.3.5',   base: 'CUDA 12.4', size: '1.2 GB',  icon: 'sparkle',  bg: 'linear-gradient(160deg,#1e293b,#020617)', desc: 'GGUF 模型本地推理 · 嵌入式 OpenAI API 网关',                                          installed: false, rating: 4.5, downloads: '6.7k',  source: 'lmstudio.ai' },
  { id: 'fooocus',   name: 'Fooocus',            cat: '图像生成', ver: 'v2.5.5',   base: 'CUDA 12.4', size: '7.4 GB',  icon: 'palette',  bg: 'linear-gradient(160deg,#f472b6,#9f1239)', desc: '极简文生图 · SDXL 默认就出好图，零调参',                                              installed: false, rating: 4.6, downloads: '14.2k', source: 'github.com/lllyasviel' },
  { id: 'invokeai',  name: 'InvokeAI',           cat: '图像生成', ver: 'v5.4.2',   base: 'CUDA 12.4', size: '6.8 GB',  icon: 'palette',  bg: 'linear-gradient(160deg,#fb7185,#831843)', desc: '专业级 SD WebUI，画布工作流 + Canvas Pro',                                              installed: false, rating: 4.7, downloads: '10.1k', source: 'invoke-ai.github.io' },
  { id: 'flux',      name: 'Flux.1 镜像',        cat: '图像生成', ver: 'dev-v1',   base: 'CUDA 12.4', size: '23.4 GB', icon: 'palette',  bg: 'linear-gradient(160deg,#8b5cf6,#3b0764)', desc: 'Black Forest Labs Flux.1 · 当前最强开源文生图模型',                                   installed: false, rating: 4.9, downloads: '7.8k',  source: 'blackforestlabs.ai' },
  { id: 'devbox',    name: 'DevBox',             cat: '开发环境', ver: 'v3.1.0',   base: 'Ubuntu 24.04', size: '1.8 GB', icon: 'code',   bg: 'linear-gradient(160deg,#1e293b,#0f172a)', desc: '云端 IDE，VSCode + 模板预设 + 一键部署，秒级冷启动',                                  installed: false, rating: 4.5, downloads: '6.2k',  source: 'sealos.io' },
  { id: 'cursor',    name: 'Cursor Server',      cat: '开发环境', ver: 'v0.43.6',  base: 'CUDA 12.4', size: '2.4 GB',  icon: 'code',     bg: 'linear-gradient(160deg,#0f172a,#020617)', desc: 'Cursor AI 编辑器服务端 · 远程访问，AI 编程加成',                                       installed: false, rating: 4.8, downloads: '18.4k', source: 'cursor.com' },
  { id: 'jupyterhub',name: 'JupyterHub',         cat: '开发环境', ver: 'v5.2.1',   base: 'Python 3.11', size: '2.6 GB', icon: 'jupyter', bg: 'linear-gradient(160deg,#fb923c,#9a3412)', desc: '多用户 Jupyter，团队共享 GPU 节点首选',                                                  installed: false, rating: 4.6, downloads: '4.1k',  source: 'jupyter.org' },
  { id: 'lobe',      name: 'Lobe Chat',          cat: 'AI 应用',  ver: 'v1.32.0',  base: 'Node 22',     size: '1.1 GB', icon: 'openwebui', bg: 'linear-gradient(160deg,#a78bfa,#7c3aed)', desc: '现代化 ChatGPT/LLMs UI · 多模态 + 插件 + Agent',                                        installed: false, rating: 4.8, downloads: '22.7k', source: 'lobehub.com' },
  { id: 'librechat', name: 'LibreChat',          cat: 'AI 应用',  ver: 'v0.7.5',   base: 'Node 22',     size: '1.4 GB', icon: 'openwebui', bg: 'linear-gradient(160deg,#0ea5e9,#0c4a6e)', desc: '开源 ChatGPT 克隆 · 多账号 / 多模型 / 文件分析',                                        installed: false, rating: 4.6, downloads: '13.8k', source: 'librechat.ai' },
  { id: 'dify',      name: 'Dify',               cat: 'AI 应用',  ver: 'v0.13.1',  base: 'Docker',      size: '3.2 GB', icon: 'sparkle', bg: 'linear-gradient(160deg,#60a5fa,#2563eb)', desc: '开源 LLM 应用开发平台 · 可视化工作流 + RAG 引擎',                                      installed: false, rating: 4.7, downloads: '15.9k', source: 'dify.ai' },
  { id: 'flowise',   name: 'Flowise',            cat: 'AI 应用',  ver: 'v2.2.1',   base: 'Node 22',     size: '1.6 GB', icon: 'wrench',  bg: 'linear-gradient(160deg,#22d3ee,#0e7490)', desc: '拖拽式 LangChain 工作流构建器',                                                        installed: false, rating: 4.5, downloads: '13.6k', source: 'flowiseai.com' },
  { id: 'langflow',  name: 'LangFlow',           cat: 'AI 应用',  ver: 'v1.1.0',   base: 'Python 3.11', size: '1.8 GB', icon: 'wrench', bg: 'linear-gradient(160deg,#34d399,#065f46)', desc: 'LangChain 可视化编排 IDE',                                                              installed: false, rating: 4.5, downloads: '9.4k',  source: 'langflow.org' },
  { id: 'ragflow',   name: 'RAGFlow',            cat: 'AI 应用',  ver: 'v0.15.1',  base: 'CUDA 12.4',   size: '4.1 GB', icon: 'book',    bg: 'linear-gradient(160deg,#fb7185,#be123c)', desc: '深度文档理解 RAG 引擎，OCR + 多格式解析 + 引用追溯',                                    installed: false, rating: 4.7, downloads: '7.6k',  source: 'ragflow.io' },
  { id: 'maxkb',     name: 'MaxKB',              cat: 'AI 应用',  ver: 'v1.10.1',  base: 'Python 3.11', size: '2.1 GB', icon: 'maxkb',  bg: 'linear-gradient(160deg,#a78bfa,#6d28d9)', desc: '企业级智能体平台 · RAG + 工作流 + MCP 工具调用',                                       installed: false, rating: 4.8, downloads: '12.4k', source: 'maxkb.cn' },
  { id: 'openclaw',  name: 'OpenClaw',           cat: 'AI 应用',  ver: 'v2026.5',  base: 'Python 3.11', size: '1.2 GB', icon: 'openclaw', bg: 'linear-gradient(160deg,#fb7185,#be123c)', desc: '自托管个人 AI 助理 · 飞书 / 钉钉 / 企业微信多渠道接入',                                 installed: false, rating: 4.6, downloads: '4.1k',  source: 'openclaw.io' },
  { id: 'n8n',       name: 'n8n',                cat: 'AI 应用',  ver: 'v1.71.0',  base: 'Node 22',     size: '1.8 GB', icon: 'wrench', bg: 'linear-gradient(160deg,#fb7185,#e11d48)', desc: '工作流自动化 · 可视化编排 400+ 集成',                                                  installed: false, rating: 4.7, downloads: '18.4k', source: 'n8n.io' },
  { id: 'axolotl',   name: 'Axolotl',            cat: '训练框架', ver: 'v0.5.2',   base: 'CUDA 12.4',   size: '8.4 GB', icon: 'flame',  bg: 'linear-gradient(160deg,#f59e0b,#92400e)', desc: '一站式 LLM 微调 · YAML 配置 LoRA / QLoRA / 全参',                                       installed: false, rating: 4.7, downloads: '8.9k',  source: 'github.com/axolotl-ai-cloud' },
  { id: 'llamafactory',name: 'LLaMA-Factory',    cat: '训练框架', ver: 'v0.9.1',   base: 'CUDA 12.4',   size: '6.2 GB', icon: 'flame',  bg: 'linear-gradient(160deg,#dc2626,#7f1d1d)', desc: 'Web UI 微调 · 100+ 模型一键 LoRA / DPO / PPO',                                          installed: false, rating: 4.8, downloads: '15.7k', source: 'github.com/hiyouga' },
  { id: 'minio',     name: 'MinIO',              cat: '存储 / 向量库', ver: 'v2026.4.0', base: 'Docker', size: '0.3 GB', icon: 'database', bg: 'linear-gradient(160deg,#fb7185,#e11d48)', desc: 'S3 兼容对象存储 · 数据集 / 模型权重缓存',                                              installed: false, rating: 4.8, downloads: '14.0k', source: 'min.io' },
  { id: 'qdrant',    name: 'Qdrant',             cat: '存储 / 向量库', ver: 'v1.12.4', base: 'Docker', size: '0.5 GB', icon: 'database', bg: 'linear-gradient(160deg,#ef4444,#991b1b)', desc: '高性能向量数据库 · HNSW 索引 + 量化压缩',                                              installed: false, rating: 4.7, downloads: '9.5k',  source: 'qdrant.tech' },
  { id: 'milvus',    name: 'Milvus',             cat: '存储 / 向量库', ver: 'v2.4.13', base: 'Docker', size: '1.4 GB', icon: 'database', bg: 'linear-gradient(160deg,#22d3ee,#0e7490)', desc: '云原生向量数据库 · 万亿级向量检索',                                                    installed: false, rating: 4.7, downloads: '12.6k', source: 'milvus.io' },
  { id: 'kohya',     name: 'Kohya SS',           cat: '图像生成', ver: 'v24.1.6',  base: 'CUDA 12.4',   size: '5.8 GB', icon: 'palette', bg: 'linear-gradient(160deg,#ec4899,#9d174d)', desc: 'SD/SDXL LoRA 训练 Web UI',                                                              installed: false, rating: 4.6, downloads: '11.2k', source: 'github.com/bmaltais' },
];

export const ALERTS = [
  { id: 'a1', sev: 'critical', title: 'vLLM 加载 Llama-3.1-70B 显存溢出 (OOM)',
    target: 'vllm · GPU 0', time: '10:23', state: 'active',
    detail: '加载 llama-3.1-70b-instruct 权重时，GPU 显存峰值达 124.6 GB（含 KV cache 预分配），超出 128 GB 上限。进程已自动退出并尝试 3 次重启，均失败。',
    suggest: ['释放 Ollama 占用（当前 28%）', '改用 70B 量化版 (AWQ / GPTQ)', '降低 max_model_len 至 4096', '切换到 vLLM 0.6.5 的 chunked-prefill'] },
  { id: 'a2', sev: 'warning',  title: '训练任务 train-72h 磁盘空间不足',
    target: 'training · /workspace/checkpoints', time: '09:15', state: 'active',
    detail: '/workspace 当前已用 1.6 TB / 总 2 TB (82.3%)，主要来自训练 checkpoint（每 500 步保存一次，单份 16 GB）。剩余空间预计仅够保存 23 次。',
    suggest: ['启用 save_total_limit=3 自动清理', '将 checkpoint 同步到 MinIO 后删除本地', '降低保存频率到 1000 步一次'] },
  { id: 'a3', sev: 'recovered',  title: '模型 deepseek-r1-distill-32b 下载完成',
    target: 'models / ollama', time: '08:42', state: 'recovered',
    detail: '模型从 Ollama 仓库下载完成，体积 18.4 GB。已自动加入 Ollama 已安装列表，可立即使用。' },
  { id: 'a4', sev: 'info',     title: '端口 7860 冲突 · 已自动切换',
    target: 'sdwebui / gradio', time: '08:00', state: 'recovered',
    detail: 'SD WebUI 启动时端口 7860 被占用（Open WebUI 临时实例），已自动切换到 7861。公网分享链接已更新。' },
];

export const genSeries = (base, amp, seed = 1) => {
  const out = [];
  for (let i = 0; i < 30; i++) {
    const v = base + Math.sin(i * 0.35 + seed) * amp + Math.cos(i * 0.18 + seed * 2) * (amp * 0.4);
    out.push(Math.max(2, Math.min(98, v)));
  }
  return out;
};

export const METRICS = {
  cpu: { used: 18, total: 100, label: 'CPU 使用率', detail: 'NVIDIA GB10 · 20 核 Arm',           color: T.blue,   series: genSeries(18, 6, 1) },
  gpu: { used: 53, total: 100, label: 'GPU 使用率', detail: 'Blackwell · 一体显存',               color: T.indigo, series: genSeries(53, 14, 2) },
  mem: { used: 82, total: 128, label: '统一内存',   detail: '已用 82 GB / 共 128 GB',             color: T.teal,   pct: 64, series: genSeries(64, 6, 3) },
  dsk: { used: 1620,total: 2048,label: '工作区',    detail: '/workspace · 已用 1.6 TB / 共 2 TB',  color: T.amber,  pct: 79, series: genSeries(79, 1.5, 4) },
};

export const GPUS = [
  { id: 0, model: 'NVIDIA GB10 Blackwell', util: 53, memUsed: 68, memTotal: 128, temp: 49, power: 18.4, max: 250, tasks: ['JupyterLab', 'Ollama', 'ComfyUI', 'SD WebUI', '训练任务', 'vLLM (已退出)'] },
];

// Logs used by Ollama detail view
export const DEFECT_LOGS = [
  ['10:14:02.103', 'INFO ', 'router ', 'POST /v1/chat/completions model=qwen2.5:14b'],
  ['10:14:02.318', 'INFO ', 'engine ', 'prompt tokens=842 max_new=1024'],
  ['10:14:02.421', 'DEBUG', 'kvcache', 'page=24 hit=18 miss=6 reuse=75%'],
  ['10:14:03.002', 'INFO ', 'engine ', 'gen first token in 318ms'],
  ['10:14:03.214', 'INFO ', 'engine ', 'gen 28 tok/s · ttft=318ms'],
  ['10:14:03.502', 'INFO ', 'router ', 'POST /v1/embeddings model=bge-m3'],
  ['10:14:03.701', 'INFO ', 'engine ', 'batch=4 latency=82ms'],
  ['10:14:04.118', 'INFO ', 'metric ', 'qps=22.4 ttft.p99=412ms tps=27.8/s'],
  ['10:14:04.320', 'INFO ', 'router ', 'POST /v1/chat/completions model=glm5:9b'],
  ['10:14:04.612', 'INFO ', 'engine ', 'gen first token in 290ms'],
];

export const QPS_SERIES = genSeries(22, 5, 7);
export const LAT_P50    = genSeries(280, 30, 9);
export const LAT_P95    = genSeries(380, 40, 10);
export const LAT_P99    = genSeries(420, 50, 11);

export const RECENT_IDS = ['vscode', 'jupyter', 'ollama', 'store'];

// ─── Exposed ports / public access ──────────────────────────────
export const PORTS = [
  { id: 'vscode',   app: 'VS Code Server', port: 8443,  proto: 'HTTPS',  state: 'public',   url: 'https://gb10-dev-01-vscode.share.edgex.cloud',     traffic: 1240, since: '6天前', auth: 'token' },
  { id: 'jupyter',  app: 'JupyterLab',     port: 8888,  proto: 'HTTPS',  state: 'public',   url: 'https://gb10-dev-01-jupyter.share.edgex.cloud',    traffic: 482,  since: '4天前', auth: 'token' },
  { id: 'ollama',   app: 'Ollama',         port: 11434, proto: 'HTTP',   state: 'lan',      url: 'http://192.168.18.42:11434',                       traffic: 8423, since: '3天前', auth: 'none' },
  { id: 'sdwebui',  app: 'SD WebUI',       port: 7861,  proto: 'HTTPS',  state: 'public',   url: 'https://gb10-dev-01-sdwebui.share.edgex.cloud',    traffic: 67,   since: '2天前', auth: 'token' },
  { id: 'comfyui',  app: 'ComfyUI',        port: 8188,  proto: 'HTTPS',  state: 'public',   url: 'https://gb10-dev-01-comfyui.share.edgex.cloud',    traffic: 124,  since: '2天前', auth: 'token' },
  { id: 'openwebui',app: 'Open WebUI',     port: 3000,  proto: 'HTTP',   state: 'lan',      url: 'http://192.168.18.42:3000',                        traffic: 92,   since: '4天前', auth: 'cookie' },
  { id: 'vllm',     app: 'vLLM',           port: 8000,  proto: 'HTTP',   state: 'offline',  url: '— · 服务异常',                                     traffic: 0,    since: '—',     auth: '—' },
  { id: 'tb',       app: 'TensorBoard',    port: 6006,  proto: 'HTTP',   state: 'lan',      url: 'http://192.168.18.42:6006',                        traffic: 14,   since: '6小时前', auth: 'none' },
];

// ─── Files panel ────────────────────────────────────────────────
export const FILES_TREE = {
  '/workspace': [
    { name: 'datasets',          type: 'dir',  size: '184 GB', count: 12 },
    { name: 'models',            type: 'dir',  size: '326 GB', count: 28 },
    { name: 'checkpoints',       type: 'dir',  size: '892 GB', count: 47 },
    { name: 'notebooks',         type: 'dir',  size: '12 MB',  count: 23 },
    { name: 'projects',          type: 'dir',  size: '4.2 GB', count: 8 },
    { name: 'train.py',          type: 'py',   size: '18 KB',  modified: '10:42' },
    { name: 'requirements.txt',  type: 'txt',  size: '1.2 KB', modified: '昨天' },
    { name: 'config.yaml',       type: 'yaml', size: '3.4 KB', modified: '昨天' },
    { name: 'README.md',         type: 'md',   size: '8.1 KB', modified: '5/22' },
    { name: '.env',              type: 'env',  size: '0.4 KB', modified: '5/20' },
  ],
};

// ─── Local models repo ──────────────────────────────────────────
export const MODELS = [
  { name: 'llama-3.1-8b-instruct',          family: 'Llama',   size: '16 GB',   format: 'safetensors', engine: 'vLLM / Ollama', tags: ['Chat','官方'], pinned: true,  added: '2025-08-20' },
  { name: 'llama-3.1-70b-instruct',         family: 'Llama',   size: '141 GB',  format: 'safetensors', engine: 'vLLM',          tags: ['Chat','大'],  pinned: true,  added: '2025-09-04' },
  { name: 'qwen2.5-14b-instruct',           family: 'Qwen',    size: '29 GB',   format: 'safetensors', engine: 'vLLM / Ollama', tags: ['Chat'],       pinned: true,  added: '2025-10-12' },
  { name: 'qwen2.5-coder-32b',              family: 'Qwen',    size: '65 GB',   format: 'safetensors', engine: 'vLLM',          tags: ['Code'],       added: '2025-11-01' },
  { name: 'deepseek-r1-distill-32b',        family: 'DeepSeek',size: '64 GB',   format: 'safetensors', engine: 'vLLM',          tags: ['Reasoning'],  added: '2026-05-26' },
  { name: 'glm5-9b',                        family: 'GLM',     size: '19 GB',   format: 'safetensors', engine: 'vLLM / Ollama', tags: ['Chat'],       added: '2025-12-08' },
  { name: 'bge-m3',                         family: 'BGE',     size: '1.2 GB',  format: 'safetensors', engine: 'Ollama',        tags: ['Embedding'],  added: '2025-08-01' },
  { name: 'stable-diffusion-xl-base-1.0',   family: 'SD',      size: '6.9 GB',  format: 'safetensors', engine: 'ComfyUI / SD',  tags: ['Image'],      added: '2025-07-14' },
  { name: 'sdxl-refiner-1.0',               family: 'SD',      size: '6.1 GB',  format: 'safetensors', engine: 'ComfyUI / SD',  tags: ['Image'],      added: '2025-07-14' },
  { name: 'flux-1-dev',                     family: 'Flux',    size: '23.4 GB', format: 'safetensors', engine: 'ComfyUI',       tags: ['Image','新'], added: '2026-04-02' },
  { name: 'whisper-large-v3',               family: 'Whisper', size: '3.1 GB',  format: 'safetensors', engine: 'Transformers',  tags: ['Audio'],      added: '2025-09-10' },
  { name: 'kokoro-v0.19',                   family: 'Kokoro',  size: '0.8 GB',  format: 'safetensors', engine: 'Transformers',  tags: ['Audio','TTS'],added: '2026-02-19' },
];
