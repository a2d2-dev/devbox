import React, { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Card, Sparkline, useTicker } from './ui'
import { btnSecondary, btnPrimary, btnDanger } from './AppWindow'
import TerminalFace from '../pages/Terminal'
import FilesFace from '../pages/Files'
import PortsFace from '../pages/Ports'
import ModelsFace from '../pages/Models'
import Processes from '../pages/Processes'
import DiskManager from '../pages/DiskManager'
import NetworkConnections from '../pages/NetworkConnections'
import MonitoringApp from '../pages/Monitoring'
import AIActivity from '../pages/AIActivity'

// Reusable face header
function FaceHeader({ accent = T.blue, title, subtitle, version, kb, onMgmt, extra, errorMode }) {
  return (
    <div style={{
      padding: '10px 16px', borderBottom: `1px solid ${T.borderSoft}`,
      background: T.surface, display: 'flex', alignItems: 'center', gap: 10,
      flexShrink: 0,
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <StatusDot tone={errorMode ? 'red' : 'green'} size={7} pulse={errorMode}/>
        <span style={{ fontSize: 12.5, fontWeight: 600, color: T.ink }}>{title}</span>
        {version && <Chip tone="gray"><span className="mono">{version}</span></Chip>}
        {kb && <Chip tone="blue"><Icon name="book" size={10} stroke={2}/>{kb}</Chip>}
        {errorMode && <Chip tone="red">运行异常</Chip>}
      </div>
      <div style={{ fontSize: 11.5, color: T.ink3, marginLeft: 4 }}>{subtitle}</div>
      <div style={{ flex: 1 }}/>
      {extra}
      <button onClick={onMgmt} style={{ ...btnSecondary, height: 28, padding: '0 10px', fontSize: 11.5 }}>
        <Icon name="dashboard" size={12} stroke={1.8}/>应用管理
      </button>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════
// Top-level shell: dispatch by appId
// ═══════════════════════════════════════════════════════════════
// ═══════════════════════════════════════════════════════════════
// IframeFace — generic iframe wrapper for installed apps
// ═══════════════════════════════════════════════════════════════
function IframeFace({ app, onMgmt }) {
  // Resolve the first HostPort from the app's port mappings
  const hostPort = app?.ports?.find(p => p.hostPort > 0)?.hostPort
    || app?.ports?.find(p => p.containerPort > 0)?.containerPort;

  if (!hostPort) {
    return (
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
        <FaceHeader accent={T.blue} title={app?.name || 'App'} version={app?.version}
          subtitle="iframe 嵌入" onMgmt={onMgmt}/>
        <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center',
          color: T.ink3, fontSize: 13 }}>
          <Icon name="info" size={16} stroke={1.8} style={{ marginRight: 8 }}/>
          该应用未暴露 HostPort，无法嵌入
        </div>
      </div>
    );
  }

  const src = `http://${window.location.hostname}:${hostPort}`;
  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
      <FaceHeader accent={T.blue} title={app?.name || 'App'} version={app?.version}
        subtitle={src} onMgmt={onMgmt}
        errorMode={app?.state === 'error'}/>
      <iframe
        src={src}
        style={{ flex: 1, width: '100%', border: 'none' }}
        allow="clipboard-read; clipboard-write"
        title={app?.name}
      />
    </div>
  );
}

function AppShell({ appId, app, authed, onRequireAuth, onOpenManagement }) {
  // Native faces for built-in system tools
  if (appId === 'vscode')    return <VSCodeFace    onMgmt={onOpenManagement}/>;
  if (appId === 'jupyter')   return <JupyterFace   onMgmt={onOpenManagement}/>;
  if (appId === 'ollama')    return <OllamaFace    onMgmt={onOpenManagement}/>;
  if (appId === 'vllm')      return <VLLMErrorFace authed={authed} onRequireAuth={onRequireAuth} onMgmt={onOpenManagement}/>;
  if (appId === 'comfyui')   return <ComfyUIFace   onMgmt={onOpenManagement}/>;
  if (appId === 'sdwebui')   return <SDWebUIFace   onMgmt={onOpenManagement}/>;
  if (appId === 'openwebui') return <OpenWebUIFace onMgmt={onOpenManagement}/>;
  if (appId === 'training')  return <TrainingFace  onMgmt={onOpenManagement}/>;
  // [Story 3.1 Disabled 2026-06-20] Web 终端禁用，桌面图标已移除，
  // AppShell 路由分支保留为注释仅供历史追溯：if (appId === 'terminal') return <TerminalFace/>
  if (appId === 'files')     return <FilesFace/>;
  // [Story 3.3 UI Merged 2026-06-20] Ports 入口已并入 Story 6.2 NetworkConnections，
  // 桌面图标移除，AppShell 分支保留注释仅供历史追溯：
  //   if (appId === 'ports') return <PortsFace authed={authed} onRequireAuth={onRequireAuth}/>;
  // 后端 GET /api/v1/ports 仍可用（参考 handlers_extra.go:101 handlePorts）。
  if (appId === 'models')    return <ModelsFace/>;
  if (appId === 'processes') return <Processes/>;
  if (appId === 'disks')     return <DiskManager/>;
  if (appId === 'network-connections') return <NetworkConnections/>;
  if (appId === 'monitoring') return <MonitoringApp/>;
  if (appId === 'ai-activity') return <AIActivity/>;
  // Generic iframe fallback for any installed app with a HostPort
  if (app) return <IframeFace app={app} onMgmt={onOpenManagement}/>;
  return null;
}

// ═══════════════════════════════════════════════════════════════
// VS Code Server
// ═══════════════════════════════════════════════════════════════
function VSCodeFace({ onMgmt }) {
  const [tab, setTab] = useState('train.py');
  const [showTerm, setShowTerm] = useState(true);

  const fileTree = [
    { type: 'dir', name: 'datasets', open: false },
    { type: 'dir', name: 'models', open: false },
    { type: 'dir', name: 'checkpoints', open: false },
    { type: 'dir', name: 'notebooks', open: true, children: [
      { type: 'ipynb', name: 'eda.ipynb' },
      { type: 'ipynb', name: 'sft_data_prep.ipynb' },
    ]},
    { type: 'dir', name: 'projects', open: true, children: [
      { type: 'dir', name: 'llama-finetune', open: true, children: [
        { type: 'py',  name: 'train.py', active: true },
        { type: 'py',  name: 'dataset.py' },
        { type: 'yaml', name: 'config.yaml' },
      ]},
    ]},
    { type: 'py',   name: 'utils.py' },
    { type: 'txt',  name: 'requirements.txt' },
    { type: 'md',   name: 'README.md' },
    { type: 'env',  name: '.env' },
  ];

  const openTabs = [
    { name: 'train.py',     icon: 'py',   dirty: true  },
    { name: 'config.yaml',  icon: 'yaml', dirty: false },
    { name: 'README.md',    icon: 'md',   dirty: false },
  ];

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: '#1e1e1e', color: '#cccccc', overflow: 'hidden' }}>
      {/* Title bar with menu */}
      <div style={{
        height: 30, background: '#252526', borderBottom: '1px solid #1e1e1e',
        display: 'flex', alignItems: 'center', padding: '0 8px', flexShrink: 0,
        fontSize: 12,
      }}>
        {['文件', '编辑', '选择', '查看', '转到', '运行', '终端', '帮助'].map(m => (
          <div key={m} style={{ padding: '5px 9px', color: '#cccccc', cursor: 'pointer', borderRadius: 3 }}
            onMouseEnter={(e) => e.currentTarget.style.background = '#37373d'}
            onMouseLeave={(e) => e.currentTarget.style.background = 'transparent'}>{m}</div>
        ))}
        <div style={{ flex: 1, textAlign: 'center', color: '#888', fontSize: 11.5 }}>
          llama-finetune — VS Code Server [GB10-DEV-01]
        </div>
        <button onClick={onMgmt} style={{
          padding: '4px 9px', borderRadius: 3, fontSize: 11, color: '#888',
          background: 'transparent', border: 'none', cursor: 'pointer',
          display: 'flex', alignItems: 'center', gap: 4,
        }}
        onMouseEnter={(e) => e.currentTarget.style.background = '#37373d'}
        onMouseLeave={(e) => e.currentTarget.style.background = 'transparent'}>
          <Icon name="dashboard" size={11} stroke={1.8}/>管理
        </button>
      </div>

      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* Activity bar */}
        <div style={{
          width: 48, background: '#333', flexShrink: 0,
          display: 'flex', flexDirection: 'column', alignItems: 'center', padding: '6px 0',
        }}>
          {['folder', 'search', 'history', 'play', 'apps'].map((ic, i) => (
            <div key={ic} style={{
              width: 36, height: 36, display: 'flex', alignItems: 'center', justifyContent: 'center',
              color: i === 0 ? '#ffffff' : '#858585', cursor: 'pointer',
              borderLeft: i === 0 ? '2px solid #0078d4' : '2px solid transparent',
              marginBottom: 2,
            }}>
              <Icon name={ic} size={20} stroke={1.6}/>
            </div>
          ))}
          <div style={{ flex: 1 }}/>
          <div style={{
            width: 36, height: 36, display: 'flex', alignItems: 'center', justifyContent: 'center',
            color: '#858585',
          }}>
            <Icon name="user" size={18} stroke={1.6}/>
          </div>
        </div>

        {/* Explorer panel */}
        <div style={{
          width: 240, background: '#252526', flexShrink: 0,
          borderRight: '1px solid #1e1e1e',
          display: 'flex', flexDirection: 'column',
        }}>
          <div style={{
            padding: '8px 14px 6px', fontSize: 10.5, fontWeight: 700,
            color: '#cccccc', letterSpacing: '0.06em', textTransform: 'uppercase',
          }}>资源管理器</div>
          <div style={{ padding: '0 8px 6px', fontSize: 11, color: '#969696',
            display: 'flex', alignItems: 'center', gap: 4 }}>
            <Icon name="chevDown" size={11} stroke={2}/>LLAMA-FINETUNE
          </div>
          <div style={{ flex: 1, overflow: 'auto', padding: '0 8px 8px', fontSize: 12, lineHeight: 1.6 }}>
            <FileTreeNode nodes={fileTree} depth={0}/>
          </div>
        </div>

        {/* Editor + terminal */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
          {/* Tabs */}
          <div style={{
            display: 'flex', background: '#252526', borderBottom: '1px solid #1e1e1e',
            height: 35, flexShrink: 0, overflowX: 'auto',
          }}>
            {openTabs.map(t2 => {
              const active = tab === t2.name;
              return (
                <div key={t2.name} onClick={() => setTab(t2.name)} style={{
                  padding: '0 14px 0 12px', display: 'flex', alignItems: 'center', gap: 6,
                  background: active ? '#1e1e1e' : 'transparent',
                  borderRight: '1px solid #1e1e1e', cursor: 'pointer',
                  borderTop: active ? '2px solid #0078d4' : '2px solid transparent',
                  marginTop: active ? -2 : 0, fontSize: 12.5,
                  color: active ? '#ffffff' : '#969696',
                }}>
                  <FileIcon type={t2.icon} size={13}/>
                  {t2.name}
                  {t2.dirty && <span style={{ color: '#cccccc', fontSize: 16, lineHeight: 0 }}>●</span>}
                  <span style={{ color: '#858585', marginLeft: 4, fontSize: 14 }}>×</span>
                </div>
              );
            })}
          </div>

          {/* Editor */}
          <div style={{ flex: 1, display: 'flex', overflow: 'hidden', minHeight: 0 }}>
            <div style={{
              width: 40, background: '#1e1e1e', borderRight: '1px solid #1e1e1e',
              fontFamily: 'ui-monospace, monospace', fontSize: 12, color: '#858585',
              padding: '8px 0', textAlign: 'right',
              userSelect: 'none', flexShrink: 0,
            }}>
              {Array.from({ length: 30 }).map((_, i) => (
                <div key={i} style={{ padding: '0 8px', lineHeight: '18px',
                  color: i === 12 ? '#ffffff' : i === 16 ? '#ff5555' : '#858585' }}>{i + 1}</div>
              ))}
            </div>
            <div style={{
              flex: 1, padding: '8px 12px', overflow: 'auto',
              fontFamily: 'ui-monospace, "JetBrains Mono", monospace',
              fontSize: 12.5, lineHeight: '18px', color: '#d4d4d4',
            }}>
              <CodeLine kw="import" rest=" torch"/>
              <CodeLine kw="import" rest=" torch.nn " kw2="as" rest2=" nn"/>
              <CodeLine kw="from" rest=" transformers " kw2="import" rest2=" AutoTokenizer, AutoModelForCausalLM"/>
              <CodeLine kw="from" rest=" datasets " kw2="import" rest2=" load_dataset"/>
              <CodeLine kw="from" rest=" peft " kw2="import" rest2=" LoraConfig, get_peft_model"/>
              <CodeLine kw="from" rest=" trl " kw2="import" rest2=" SFTTrainer, SFTConfig"/>
              <CodeLine/>
              <CodeLine comment="# Configuration"/>
              <CodeLine txt="MODEL_NAME = " str='"meta-llama/Llama-3.1-8B"'/>
              <CodeLine txt="DATASET = " str='"alpaca-cleaned"'/>
              <CodeLine txt="OUTPUT_DIR = " str='"/workspace/checkpoints/llama-finetune-72h"'/>
              <CodeLine/>
              <CodeLine kw="def" fn=" train" rest='():'/>
              <CodeLine highlight indent={1} txt="tokenizer = " kw2="AutoTokenizer" rest='.from_pretrained(MODEL_NAME)'/>
              <CodeLine indent={1} txt="model = " kw2="AutoModelForCausalLM" rest='.from_pretrained('/>
              <CodeLine indent={2} txt="MODEL_NAME,"/>
              <CodeLine error indent={2} txt="torch_dtype=torch.bfloat16,    " comment="# OOM: 改 fp16"/>
              <CodeLine indent={2} txt="device_map=" str='"auto"'/>
              <CodeLine indent={1} txt=")"/>
              <CodeLine/>
              <CodeLine indent={1} txt="peft_config = " kw2="LoraConfig" rest='('/>
              <CodeLine indent={2} txt="r=64, lora_alpha=128,"/>
              <CodeLine indent={2} txt="target_modules=[" str='"q_proj"' txt2=", " str2='"v_proj"' txt2b="],"/>
              <CodeLine indent={2} txt="lora_dropout=0.05,"/>
              <CodeLine indent={1} txt=")"/>
              <CodeLine indent={1} txt="model = " kw2="get_peft_model" rest='(model, peft_config)'/>
              <CodeLine/>
              <CodeLine indent={1} txt="trainer = " kw2="SFTTrainer" rest='(...)'/>
              <CodeLine indent={1} kw="return" rest=" trainer.train()"/>
            </div>
          </div>

          {/* Bottom terminal panel */}
          {showTerm && (
            <div style={{
              height: 220, borderTop: '1px solid #1e1e1e', background: '#181818',
              display: 'flex', flexDirection: 'column', flexShrink: 0,
            }}>
              <div style={{
                display: 'flex', padding: '4px 12px', background: '#252526',
                borderBottom: '1px solid #1e1e1e', fontSize: 11.5,
              }}>
                {['终端', '问题 1', '输出', '调试控制台'].map((tab, i) => (
                  <div key={tab} style={{
                    padding: '4px 10px', color: i === 0 ? '#ffffff' : '#969696',
                    borderBottom: i === 0 ? '2px solid #0078d4' : '2px solid transparent',
                    marginBottom: -1, cursor: 'pointer',
                  }}>{tab}</div>
                ))}
                <div style={{ flex: 1 }}/>
                <button onClick={() => setShowTerm(false)} style={{
                  background: 'transparent', border: 'none', color: '#858585', cursor: 'pointer',
                  padding: '2px 6px', fontSize: 11,
                }}>×</button>
              </div>
              <div style={{
                flex: 1, padding: '8px 14px', fontFamily: 'ui-monospace, monospace',
                fontSize: 12, color: '#cccccc', overflow: 'auto', lineHeight: 1.55,
              }}>
                <div><span style={{ color: '#23d18b' }}>dev-zhang@gb10-dev-01</span>:<span style={{ color: '#3b8eea' }}>~/projects/llama-finetune</span>$ python train.py --resume</div>
                <div style={{ color: '#969696' }}>[2026-05-26 14:08:01] Loading tokenizer from meta-llama/Llama-3.1-8B</div>
                <div style={{ color: '#969696' }}>[2026-05-26 14:08:03] Loading model... (this may take 2-3 minutes)</div>
                <div style={{ color: '#23d18b' }}>[2026-05-26 14:08:18] Model loaded · 16.0 GB GPU memory</div>
                <div style={{ color: '#23d18b' }}>[2026-05-26 14:08:19] LoRA adapter attached · trainable params: 41M (0.5%)</div>
                <div style={{ color: '#969696' }}>[2026-05-26 14:08:20] Resuming from step 14,300 / 30,000</div>
                <div style={{ color: '#cccccc' }}>{'{'}'loss': 1.842, 'grad_norm': 0.412, 'learning_rate': 0.0001, 'epoch': 1.43{'}'}</div>
                <div style={{ color: '#cccccc' }}>{'{'}'loss': 1.821, 'grad_norm': 0.398, 'learning_rate': 0.0001, 'epoch': 1.44{'}'}</div>
                <div style={{ color: '#cccccc' }}>{'{'}'loss': 1.835, 'grad_norm': 0.405, 'learning_rate': 0.0001, 'epoch': 1.44{'}'}</div>
                <div style={{ color: '#ffba2c' }}>[2026-05-26 14:14:22] WARNING: free disk space &lt; 400GB — checkpoints may fail</div>
                <div style={{ display: 'flex' }}>
                  <span style={{ color: '#23d18b' }}>dev-zhang@gb10-dev-01</span>:<span style={{ color: '#3b8eea' }}>~/projects/llama-finetune</span>$&nbsp;
                  <span className="edge-cursor" style={{ color: '#cccccc' }}>▍</span>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Status bar (VS Code style) */}
      <div style={{
        height: 22, background: '#0078d4', color: 'white', fontSize: 11.5,
        display: 'flex', alignItems: 'center', padding: '0 12px', gap: 14, flexShrink: 0,
      }}>
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
          <Icon name="history" size={11} stroke={2}/>main
        </span>
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
          ✗ 0 ⚠ 2
        </span>
        <div style={{ flex: 1 }}/>
        <span>UTF-8</span>
        <span>LF</span>
        <span>Python 3.11.7 (.venv)</span>
        <span>行 13，列 28</span>
        <span>空格: 4</span>
      </div>
    </div>
  );
}

// VS Code helpers
function FileTreeNode({ nodes, depth }) {
  return nodes.map((n, i) => (
    <React.Fragment key={i}>
      <div style={{
        paddingLeft: depth * 14 + 4, display: 'flex', alignItems: 'center', gap: 4,
        cursor: 'pointer', padding: '2px 4px 2px ' + (depth * 14 + 4) + 'px',
        background: n.active ? 'rgba(255,255,255,0.08)' : 'transparent',
        color: n.active ? '#ffffff' : '#cccccc',
      }}>
        {n.type === 'dir'
          ? <Icon name="chevDown" size={10} stroke={2} style={{ color: '#858585', transform: n.open ? '' : 'rotate(-90deg)' }}/>
          : <span style={{ width: 10 }}/>}
        <FileIcon type={n.type} size={12}/>
        <span>{n.name}</span>
      </div>
      {n.open && n.children && <FileTreeNode nodes={n.children} depth={depth + 1}/>}
    </React.Fragment>
  ));
}

function FileIcon({ type, size = 14 }) {
  const map = {
    dir:   { color: '#dcb67a', icon: 'folder' },
    py:    { color: '#3572a5', icon: 'code' },
    yaml:  { color: '#fbc02d', icon: 'code' },
    md:    { color: '#3b82f6', icon: 'book' },
    txt:   { color: '#9e9e9e', icon: 'code' },
    env:   { color: '#737373', icon: 'lock' },
    ipynb: { color: '#f37726', icon: 'jupyter' },
  };
  const m = map[type] || map.txt;
  return <span style={{ color: m.color, display: 'inline-flex' }}><Icon name={m.icon} size={size} stroke={1.7}/></span>;
}

function CodeLine({ kw, kw2, fn, str, str2, comment, txt, txt2, txt2b, rest, rest2, indent = 0, highlight, error }) {
  return (
    <div style={{
      paddingLeft: indent * 24, position: 'relative',
      background: highlight ? 'rgba(33,150,243,0.12)' : error ? 'rgba(255,85,85,0.08)' : 'transparent',
    }}>
      {comment && <span style={{ color: '#6a9955' }}>{comment}</span>}
      {kw && <span style={{ color: '#c586c0' }}>{kw}</span>}
      {fn && <span style={{ color: '#dcdcaa' }}>{fn}</span>}
      {kw2 && <span style={{ color: '#4ec9b0' }}>{kw2}</span>}
      {txt && <span>{txt}</span>}
      {str && <span style={{ color: '#ce9178' }}>{str}</span>}
      {txt2 && <span>{txt2}</span>}
      {str2 && <span style={{ color: '#ce9178' }}>{str2}</span>}
      {txt2b && <span>{txt2b}</span>}
      {rest && <span>{rest}</span>}
      {rest2 && <span>{rest2}</span>}
      {!kw && !fn && !txt && !comment && !rest && '\u00A0'}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════
// JupyterLab
// ═══════════════════════════════════════════════════════════════
function JupyterFace({ onMgmt }) {
  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: '#f6f6f6', overflow: 'hidden' }}>
      {/* Menu bar */}
      <div style={{
        height: 30, background: '#fafafa', borderBottom: '1px solid #d8d8d8',
        display: 'flex', alignItems: 'center', padding: '0 8px', flexShrink: 0,
        fontSize: 12.5,
      }}>
        <div style={{
          display: 'flex', alignItems: 'center', gap: 5,
          padding: '0 10px 0 6px', borderRight: '1px solid #d8d8d8', height: '100%',
        }}>
          <div style={{
            width: 22, height: 22, borderRadius: 4,
            background: 'linear-gradient(140deg, #fb923c, #c2410c)', color: 'white',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
          }}>
            <Icon name="jupyter" size={13} stroke={1.6}/>
          </div>
          <span style={{ fontSize: 12, fontWeight: 600, color: T.ink }}>JupyterLab</span>
        </div>
        {['文件', '编辑', '查看', '运行', '内核', '选项卡', '设置', '帮助'].map(m => (
          <div key={m} style={{ padding: '5px 10px', color: '#333', cursor: 'pointer' }}>{m}</div>
        ))}
        <div style={{ flex: 1 }}/>
        <button onClick={onMgmt} style={{ ...btnSecondary, height: 22, padding: '0 8px', fontSize: 11 }}>
          <Icon name="dashboard" size={11} stroke={1.8}/>管理
        </button>
      </div>

      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* Sidebar */}
        <div style={{ width: 40, background: '#f0f0f0', borderRight: '1px solid #d8d8d8',
          display: 'flex', flexDirection: 'column', alignItems: 'center', padding: '8px 0', flexShrink: 0 }}>
          {[
            { ic: 'folder',  on: true  },
            { ic: 'play',    on: false },
            { ic: 'sparkle', on: false },
            { ic: 'apps',    on: false },
            { ic: 'gear',    on: false },
          ].map((b, i) => (
            <div key={i} style={{
              width: 32, height: 32, display: 'flex', alignItems: 'center', justifyContent: 'center',
              color: b.on ? '#0078d4' : '#666', cursor: 'pointer', marginBottom: 4,
              borderLeft: b.on ? '2px solid #0078d4' : '2px solid transparent',
            }}>
              <Icon name={b.ic} size={17} stroke={1.7}/>
            </div>
          ))}
        </div>

        {/* File browser */}
        <div style={{ width: 220, background: 'white', borderRight: '1px solid #d8d8d8',
          display: 'flex', flexDirection: 'column', flexShrink: 0 }}>
          <div style={{ padding: '8px 12px', borderBottom: '1px solid #ececec',
            display: 'flex', alignItems: 'center', gap: 6, fontSize: 11.5 }}>
            <Icon name="plus" size={13} stroke={1.8} style={{ color: '#666' }}/>
            <Icon name="folder" size={13} stroke={1.8} style={{ color: '#666' }}/>
            <Icon name="refresh" size={13} stroke={1.8} style={{ color: '#666' }}/>
            <div style={{ flex: 1 }}/>
            <span style={{ color: T.ink3 }}>/workspace</span>
          </div>
          <div style={{ flex: 1, overflow: 'auto', padding: '4px 0', fontSize: 12 }}>
            {[
              ['.', 'dir', '..'],
              ['notebooks', 'dir', '23 项'],
              ['datasets', 'dir', '12 项'],
              ['exploration.ipynb',     'ipynb',  '14 分钟前'],
              ['sft_data_prep.ipynb',   'ipynb',  '昨天'],
              ['loss_curves.ipynb',     'ipynb',  '昨天'],
              ['model_eval.ipynb',      'ipynb',  '5/24'],
              ['eda_alpaca.ipynb',      'ipynb',  '5/22'],
            ].map(([n, t, meta], i) => (
              <div key={i} style={{
                padding: '4px 12px 4px 18px', display: 'flex', alignItems: 'center', gap: 6,
                background: i === 3 ? '#e8f0fe' : 'transparent', cursor: 'pointer',
              }}>
                <FileIcon type={t} size={13}/>
                <span style={{ color: '#333', fontSize: 12, fontWeight: i === 3 ? 600 : 400 }}>{n}</span>
                <div style={{ flex: 1 }}/>
                <span style={{ color: T.ink4, fontSize: 10.5 }}>{meta}</span>
              </div>
            ))}
          </div>
        </div>

        {/* Notebook */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
          {/* Tab bar */}
          <div style={{
            display: 'flex', background: '#fafafa', borderBottom: '1px solid #d8d8d8',
            flexShrink: 0,
          }}>
            <div style={{
              display: 'flex', alignItems: 'center', gap: 6, padding: '8px 14px',
              background: 'white', borderBottom: '2px solid #0078d4',
              fontSize: 12.5, color: T.ink, marginBottom: -1,
            }}>
              <FileIcon type="ipynb" size={13}/>
              exploration.ipynb
              <span style={{ marginLeft: 4, color: '#858585' }}>×</span>
            </div>
          </div>

          {/* Notebook toolbar */}
          <div style={{
            padding: '6px 14px', background: 'white', borderBottom: '1px solid #ececec',
            display: 'flex', alignItems: 'center', gap: 6, flexShrink: 0,
          }}>
            {['plus', 'stop', 'play', 'refresh', 'download'].map(ic => (
              <button key={ic} style={{
                width: 26, height: 26, borderRadius: 4, border: '1px solid transparent',
                background: 'transparent', cursor: 'pointer', color: T.ink3,
                display: 'flex', alignItems: 'center', justifyContent: 'center',
              }}
              onMouseEnter={(e) => e.currentTarget.style.background = '#f0f0f0'}
              onMouseLeave={(e) => e.currentTarget.style.background = 'transparent'}>
                <Icon name={ic} size={13} stroke={1.7}/>
              </button>
            ))}
            <div style={{ width: 1, height: 18, background: '#d8d8d8', margin: '0 4px' }}/>
            <select style={{
              fontSize: 12, padding: '2px 6px', borderRadius: 4,
              border: '1px solid #d8d8d8', background: 'white',
            }}>
              <option>代码</option>
              <option>Markdown</option>
            </select>
            <div style={{ flex: 1 }}/>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 11.5, color: T.ink3 }}>
              <StatusDot tone="green" size={6}/>
              <span className="mono">Python 3.11 (CUDA)</span>
            </div>
          </div>

          {/* Cells */}
          <div style={{ flex: 1, overflow: 'auto', padding: '14px 28px', background: 'white' }}>
            <NotebookCell n={1} kind="md">
              <h2 style={{ margin: '0 0 8px', fontSize: 18, color: T.ink }}>Alpaca 数据集 EDA</h2>
              <p style={{ margin: 0, fontSize: 13, lineHeight: 1.7, color: T.ink2 }}>
                探索 alpaca-cleaned 数据集的指令长度分布、主题聚类，并为 SFT 微调做数据准备。
              </p>
            </NotebookCell>

            <NotebookCell n={2} kind="code">
              <div style={{ fontFamily: 'ui-monospace, monospace', fontSize: 12.5, lineHeight: 1.65 }}>
                <div><span style={{ color: '#c586c0' }}>import</span> pandas <span style={{ color: '#c586c0' }}>as</span> pd</div>
                <div><span style={{ color: '#c586c0' }}>import</span> matplotlib.pyplot <span style={{ color: '#c586c0' }}>as</span> plt</div>
                <div><span style={{ color: '#c586c0' }}>from</span> datasets <span style={{ color: '#c586c0' }}>import</span> load_dataset</div>
                <div>&nbsp;</div>
                <div>ds = <span style={{ color: '#4ec9b0' }}>load_dataset</span>(<span style={{ color: '#ce9178' }}>"yahma/alpaca-cleaned"</span>)</div>
                <div>df = ds[<span style={{ color: '#ce9178' }}>"train"</span>].to_pandas()</div>
                <div>df.shape, df.columns.tolist()</div>
              </div>
              <NotebookOutput>
                <div className="mono" style={{ fontSize: 12 }}>{'(51760, 3), [\'instruction\', \'input\', \'output\']'}</div>
              </NotebookOutput>
            </NotebookCell>

            <NotebookCell n={3} kind="code">
              <div style={{ fontFamily: 'ui-monospace, monospace', fontSize: 12.5, lineHeight: 1.65 }}>
                <div>df[<span style={{ color: '#ce9178' }}>"len"</span>] = df[<span style={{ color: '#ce9178' }}>"output"</span>].str.<span style={{ color: '#4ec9b0' }}>split</span>().str.<span style={{ color: '#4ec9b0' }}>len</span>()</div>
                <div>df[<span style={{ color: '#ce9178' }}>"len"</span>].<span style={{ color: '#4ec9b0' }}>hist</span>(bins=<span style={{ color: '#b5cea8' }}>50</span>, figsize=(<span style={{ color: '#b5cea8' }}>10</span>, <span style={{ color: '#b5cea8' }}>4</span>))</div>
                <div>plt.<span style={{ color: '#4ec9b0' }}>title</span>(<span style={{ color: '#ce9178' }}>"Token length distribution"</span>)</div>
                <div>plt.<span style={{ color: '#4ec9b0' }}>show</span>()</div>
              </div>
              <NotebookOutput>
                <PlotPlaceholder/>
              </NotebookOutput>
            </NotebookCell>

            <NotebookCell n={4} kind="code" running>
              <div style={{ fontFamily: 'ui-monospace, monospace', fontSize: 12.5, lineHeight: 1.65 }}>
                <div><span style={{ color: '#6a9955' }}># 长尾样本：超过 512 tokens 的样本占比</span></div>
                <div>(df[<span style={{ color: '#ce9178' }}>"len"</span>] {'>'} <span style={{ color: '#b5cea8' }}>512</span>).<span style={{ color: '#4ec9b0' }}>mean</span>()</div>
              </div>
            </NotebookCell>

            <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 50px',
              fontSize: 12, color: T.ink3, cursor: 'pointer' }}>
              <Icon name="plus" size={13} stroke={1.8}/>新增单元格
            </div>
          </div>

          {/* Status bar */}
          <div style={{
            padding: '4px 14px', background: '#fafafa', borderTop: '1px solid #ececec',
            display: 'flex', alignItems: 'center', gap: 14, fontSize: 11.5, color: T.ink3,
            flexShrink: 0,
          }}>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
              <StatusDot tone="green" size={6}/>已保存
            </span>
            <span>3 / 4 单元格</span>
            <div style={{ flex: 1 }}/>
            <span className="mono">PyTorch 2.5.1 · CUDA 12.4</span>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
              <Icon name="bolt" size={11} stroke={1.8}/>
              <span className="mono">12% GPU</span>
            </span>
            <span className="mono">RAM 3.8 GB</span>
          </div>
        </div>
      </div>
    </div>
  );
}

function NotebookCell({ n, kind, running, children }) {
  const isCode = kind === 'code';
  return (
    <div style={{ display: 'flex', gap: 8, marginBottom: 10 }}>
      <div style={{ width: 50, fontFamily: 'ui-monospace, monospace', fontSize: 11.5,
        color: T.ink4, paddingTop: 8, textAlign: 'right', flexShrink: 0 }}>
        {running ? <span style={{ color: T.amber }}>[*]:</span> : isCode ? `[${n}]:` : ''}
      </div>
      <div style={{
        flex: 1, padding: isCode ? '8px 12px' : '4px 12px',
        background: isCode ? '#f8f8f8' : 'transparent',
        border: isCode ? '1px solid #ececec' : 'none',
        borderLeft: isCode ? '3px solid #0078d4' : 'none',
        borderRadius: 4, minWidth: 0,
      }}>
        {children}
      </div>
    </div>
  );
}

function NotebookOutput({ children }) {
  return (
    <div style={{
      marginTop: 8, paddingTop: 8, borderTop: '1px dashed #ececec',
      color: T.ink2, fontSize: 12,
    }}>{children}</div>
  );
}

function PlotPlaceholder() {
  // SVG histogram
  const bins = [12, 28, 65, 92, 110, 95, 78, 54, 38, 24, 16, 12, 8, 5, 3];
  const max = Math.max(...bins);
  const w = 540, h = 160;
  return (
    <svg width={w} height={h} style={{ display: 'block' }}>
      <line x1="40" x2={w-10} y1={h-22} y2={h-22} stroke="#999" strokeWidth="1"/>
      <line x1="40" x2="40" y1="10" y2={h-22} stroke="#999" strokeWidth="1"/>
      {bins.map((b, i) => {
        const bw = (w - 60) / bins.length;
        const bh = (b / max) * (h - 40);
        return <rect key={i} x={42 + i * bw} y={h - 22 - bh} width={bw - 2} height={bh}
          fill="#3b82f6" opacity="0.85"/>;
      })}
      <text x={w/2} y={12} textAnchor="middle" fontSize="11" fill="#333">Token length distribution</text>
      {[0, 200, 400, 600, 800, 1000, 1200].map((v, i) => (
        <text key={i} x={42 + i * ((w-60) / 6)} y={h - 6} fontSize="10" fill="#666" textAnchor="middle">{v}</text>
      ))}
    </svg>
  );
}

// ═══════════════════════════════════════════════════════════════
// Ollama (kept from previous version, lightly updated)
// ═══════════════════════════════════════════════════════════════
function OllamaFace({ onMgmt }) {
  const [activeModel, setActiveModel] = useState('llama-3.1-8b-instruct');
  const [prompt, setPrompt] = useState('帮我把 RAG 系统的查询流程画一个简单的时序图（用 mermaid 语法）：');
  const [temp, setTemp] = useState(0.7);
  const [maxTok, setMaxTok] = useState(1024);
  const [running, setRunning] = useState(false);
  const [output, setOutput] = useState('```mermaid\nsequenceDiagram\n    participant U as 用户\n    participant W as Open WebUI\n    participant O as Ollama\n    participant V as Qdrant\n\n    U->>W: 提问\n    W->>V: 1. 检索相关文档\n    V-->>W: top-k 片段\n    W->>O: 2. 拼接 Prompt + 检索结果\n    O-->>W: 流式生成回答\n    W-->>U: 渲染答案 + 引用\n```\n\n这个流程图展示了 RAG 系统的核心五步：用户提问 → 向量库检索 → 提示词组装 → LLM 生成 → 流式返回。');

  const models = [
    { name: 'llama-3.1-8b-instruct', size: '4.7 GB',  params: '8B',    family: 'Llama',    loaded: true,  ctx: '128K' },
    { name: 'qwen2.5-14b',           size: '8.7 GB',  params: '14.7B', family: 'Qwen',     loaded: true,  ctx: '128K' },
    { name: 'qwen2.5-coder-32b',     size: '19.0 GB', params: '32.5B', family: 'Qwen',     loaded: false, ctx: '128K' },
    { name: 'deepseek-r1-distill-32b',size: '18.4 GB',params: '32.5B', family: 'DeepSeek', loaded: false, ctx: '64K' },
    { name: 'glm5-9b',               size: '5.4 GB',  params: '9.4B',  family: 'GLM',      loaded: false, ctx: '128K' },
    { name: 'bge-m3',                size: '1.2 GB',  params: '568M',  family: 'BGE',      loaded: true,  ctx: '8K',   embedding: true },
  ];

  const run = () => {
    setRunning(true);
    setOutput('');
    let i = 0;
    const text = '正在调用模型 `' + activeModel + '`（temperature=' + temp + ', max_tokens=' + maxTok + '）… 演示模式，模拟流式输出 …';
    const id = setInterval(() => {
      if (i >= text.length) { clearInterval(id); setRunning(false); return; }
      setOutput(o => o + text[i]); i++;
    }, 18);
  };

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', background: T.surfaceAlt }}>
      <FaceHeader title="Ollama · 本地模型推理" subtitle="OpenAI 兼容 API"
        version="v0.4.7" onMgmt={onMgmt}
        extra={
          <div style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '3px 9px',
            borderRadius: 999, background: '#0f172a', color: '#e2e8f0',
            fontSize: 11, fontFamily: 'ui-monospace, monospace' }}>
            <Icon name="globe" size={11} stroke={1.8}/>
            http://localhost:11434/v1
          </div>
        }/>

      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        <div style={{ width: 280, flexShrink: 0, borderRight: `1px solid ${T.borderSoft}`,
          background: T.surface, overflowY: 'auto', padding: '14px 12px' }}>
          <div style={{ display: 'flex', alignItems: 'center', marginBottom: 10 }}>
            <div style={{ fontSize: 12, fontWeight: 700, color: T.ink }}>已安装模型</div>
            <div style={{ flex: 1 }}/>
            <span style={{ fontSize: 11, color: T.ink3 }}>{models.filter(m => m.loaded).length} / {models.length} 加载</span>
          </div>
          <button style={{
            width: '100%', height: 32, marginBottom: 10,
            borderRadius: 7, background: 'white', border: `1px dashed ${T.border}`,
            color: T.ink2, fontSize: 12, fontWeight: 500, cursor: 'pointer',
            display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 5,
          }}><Icon name="download" size={12} stroke={1.8}/>从 ollama.ai 拉取</button>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            {models.map(m => {
              const on = activeModel === m.name;
              return (
                <div key={m.name} onClick={() => setActiveModel(m.name)} style={{
                  padding: '10px', borderRadius: 8, cursor: 'pointer',
                  background: on ? '#eff4ff' : 'transparent',
                  border: `1px solid ${on ? '#bfdbfe' : 'transparent'}`,
                }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <Icon name="sparkle" size={12} stroke={1.8} style={{ color: on ? T.blueDeep : T.ink3 }}/>
                    <span style={{ fontSize: 12.5, fontWeight: 600, color: on ? T.blueDeep : T.ink }} className="mono">{m.name}</span>
                    <div style={{ flex: 1 }}/>
                    {m.loaded
                      ? <span style={{ fontSize: 9.5, padding: '1px 5px', borderRadius: 3, background: '#ecfdf5', color: '#047857', fontWeight: 600, border: '1px solid #a7f3d0' }}>常驻</span>
                      : <span style={{ fontSize: 9.5, color: T.ink4 }}>未加载</span>}
                  </div>
                  <div style={{ display: 'flex', gap: 8, marginTop: 5, fontSize: 10.5, color: T.ink3 }} className="mono tnum">
                    <span>{m.params}</span><span style={{ color: '#cbd5e1' }}>·</span>
                    <span>{m.size}</span><span style={{ color: '#cbd5e1' }}>·</span>
                    <span>{m.ctx}</span>
                    {m.embedding && <Chip tone="violet" style={{ fontSize: 9, padding: '0 4px', marginLeft: 2 }}>EMB</Chip>}
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        <div style={{ flex: 1, display: 'flex', overflow: 'hidden', minWidth: 0 }}>
          <div style={{ flex: 1, padding: 18, display: 'flex', flexDirection: 'column', gap: 12, overflow: 'auto', minWidth: 0 }}>
            <div>
              <div style={{ fontSize: 11.5, color: T.ink3, fontWeight: 600,
                letterSpacing: '0.04em', textTransform: 'uppercase', marginBottom: 6 }}>提示词</div>
              <textarea value={prompt} onChange={(e) => setPrompt(e.target.value)}
                style={{ width: '100%', height: 86, padding: '10px 12px',
                  border: `1px solid ${T.border}`, borderRadius: 8,
                  fontSize: 12.5, lineHeight: 1.6, color: T.ink,
                  background: 'white', outline: 'none', resize: 'vertical', fontFamily: 'inherit' }}/>
            </div>

            <div>
              <div style={{ display: 'flex', alignItems: 'center', marginBottom: 6 }}>
                <span style={{ fontSize: 11.5, color: T.ink3, fontWeight: 600, letterSpacing: '0.04em', textTransform: 'uppercase' }}>输出</span>
                <div style={{ flex: 1 }}/>
                {running && <Chip tone="blue"><StatusDot tone="blue" size={6} pulse/>生成中 · 28 tok/s</Chip>}
              </div>
              <div style={{
                background: '#0b1020', color: '#e2e8f0', borderRadius: 8,
                padding: '14px 16px', minHeight: 220, fontSize: 12.5, lineHeight: 1.75,
                fontFamily: 'ui-monospace, monospace', whiteSpace: 'pre-wrap',
              }}>
                {output.split(/(`{3}[\s\S]*?`{3}|`[^`]+`)/g).map((part, i) => {
                  if (part.startsWith('```')) {
                    return <div key={i} style={{
                      background: 'rgba(255,255,255,0.04)',
                      borderLeft: `2px solid ${T.green}`,
                      padding: '8px 10px', borderRadius: 4, margin: '6px 0',
                      fontSize: 11.5, color: '#7dd3fc',
                    }}>{part.replace(/^```\w*\n?|```$/g, '')}</div>;
                  }
                  if (part.startsWith('`')) return <span key={i} style={{
                    background: 'rgba(255,255,255,0.06)', padding: '1px 5px',
                    borderRadius: 3, color: '#fbbf24',
                  }}>{part.slice(1, -1)}</span>;
                  return part;
                })}
                {running && <span className="edge-cursor" style={{ color: '#34d399' }}>▍</span>}
              </div>
            </div>

            <button onClick={run} disabled={running} style={{
              height: 38, borderRadius: 8,
              background: running ? '#94a3b8' : T.blue, color: 'white', border: 'none',
              fontSize: 13, fontWeight: 600, cursor: running ? 'wait' : 'pointer',
              display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
            }}><Icon name="play" size={13} stroke={2}/>{running ? '生成中…' : '运行推理'}</button>
          </div>

          <div style={{ width: 240, flexShrink: 0, padding: 16, background: T.surface,
            borderLeft: `1px solid ${T.borderSoft}`, overflowY: 'auto' }}>
            <div style={{ fontSize: 12, fontWeight: 700, color: T.ink, marginBottom: 12 }}>推理参数</div>
            <ParamSlider label="Temperature" value={temp} min={0} max={2} step={0.1} onChange={setTemp}/>
            <ParamSlider label="Max tokens"  value={maxTok} min={64} max={4096} step={64} onChange={setMaxTok}/>
            <ParamSlider label="Top-P"       value={0.95} min={0} max={1} step={0.05} onChange={() => {}}/>
            <ParamSlider label="Frequency"   value={0.0} min={-2} max={2} step={0.1} onChange={() => {}}/>

            <div style={{ marginTop: 16, paddingTop: 12, borderTop: `1px solid ${T.borderSoft}` }}>
              <div style={{ fontSize: 11.5, fontWeight: 600, color: T.ink3, marginBottom: 8,
                letterSpacing: '0.04em', textTransform: 'uppercase' }}>API 示例</div>
              <pre style={{
                margin: 0, padding: 10, background: '#0b1020', color: '#cbd5e1',
                borderRadius: 6, fontSize: 10.5, lineHeight: 1.55, whiteSpace: 'pre-wrap', overflow: 'auto',
              }}>{`curl localhost:11434/v1/chat/completions \\
  -H "Content-Type: application/json" \\
  -d '{"model":"${activeModel}","messages":[...]}'`}</pre>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function ParamSlider({ label, value, min, max, step, onChange }) {
  return (
    <div style={{ marginBottom: 14 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', marginBottom: 6 }}>
        <span style={{ fontSize: 11.5, color: T.ink2, fontWeight: 500 }}>{label}</span>
        <div style={{ flex: 1 }}/>
        <span className="mono tnum" style={{ fontSize: 12, color: T.ink, fontWeight: 600 }}>{value}</span>
      </div>
      <input type="range" min={min} max={max} step={step} value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        style={{ width: '100%', accentColor: T.blue }}/>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════
// vLLM (error / recovery face)
// ═══════════════════════════════════════════════════════════════
function VLLMErrorFace({ authed, onRequireAuth, onMgmt }) {
  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', background: T.surfaceAlt }}>
      <FaceHeader title="vLLM · LLM 推理服务" subtitle="3 次自动重启失败，需人工介入"
        version="v0.6.4" onMgmt={onMgmt} errorMode/>

      <div style={{ flex: 1, overflowY: 'auto', padding: '24px 28px' }}>
        <div style={{ maxWidth: 880, margin: '0 auto' }}>
          {/* Error hero */}
          <div style={{
            background: 'linear-gradient(180deg, #fef2f2 0%, white 100%)',
            border: `1px solid #fecaca`, borderRadius: 14, padding: 24,
            display: 'flex', gap: 18, alignItems: 'flex-start', marginBottom: 14,
          }}>
            <div className="edge-pulse" style={{
              width: 60, height: 60, borderRadius: 16,
              background: 'linear-gradient(140deg, #fb7185, #be123c)', color: 'white',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              flexShrink: 0, boxShadow: '0 8px 22px -4px rgba(239,68,68,0.4)',
            }}>
              <Icon name="alertTri" size={28} stroke={1.7}/>
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontSize: 10.5, color: T.red, fontWeight: 700,
                letterSpacing: '0.06em', textTransform: 'uppercase', marginBottom: 4 }}>
                CUDA OOM · 启动阶段
              </div>
              <div style={{ fontSize: 19, fontWeight: 700, color: T.ink,
                letterSpacing: '-0.01em', lineHeight: 1.3 }}>
                vLLM 无法加载 Llama-3.1-70B-Instruct
              </div>
              <div style={{ fontSize: 12.5, color: T.ink2, marginTop: 8, lineHeight: 1.7 }}>
                加载 <span className="mono" style={{ background: T.surface, padding: '1px 5px',
                  borderRadius: 3, border: `1px solid ${T.border}` }}>llama-3.1-70b-instruct</span> 时，
                GPU 显存峰值达 <strong>124.6 GB</strong>（含 KV cache 预分配），超出 128 GB 上限。
              </div>

              <div style={{ display: 'flex', gap: 8, marginTop: 14, flexWrap: 'wrap' }}>
                <button onClick={() => !authed && onRequireAuth('强制重启 vLLM')} style={{
                  ...btnPrimary, background: T.red, border: 'none',
                  height: 34, padding: '0 14px', fontSize: 12.5,
                }}><Icon name="refresh" size={13} stroke={2}/>{authed ? '强制重启' : '需验证后强制重启'}</button>
                <button style={{ ...btnSecondary, height: 34, padding: '0 14px', fontSize: 12.5 }}>
                  <Icon name="history" size={13} stroke={1.8}/>切换到 70B-AWQ 量化版
                </button>
                <button style={{ ...btnSecondary, height: 34, padding: '0 14px', fontSize: 12.5 }}>
                  <Icon name="terminal" size={13} stroke={1.8}/>查看完整 stderr
                </button>
                <button onClick={onMgmt} style={{ ...btnSecondary, height: 34, padding: '0 14px', fontSize: 12.5 }}>
                  <Icon name="download" size={13} stroke={1.8}/>导出诊断包
                </button>
              </div>
            </div>
          </div>

          {/* Suggested fix */}
          <div style={{ background: T.surface, border: `1px solid ${T.border}`,
            borderRadius: 10, padding: 18, marginBottom: 14 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
              <Icon name="sparkle" size={14} stroke={1.8} style={{ color: T.blueDeep }}/>
              <div style={{ fontSize: 13, fontWeight: 700, color: T.ink }}>建议处置（按推荐顺序）</div>
              <div style={{ flex: 1 }}/>
              <Chip tone="blue">来自社区最佳实践库</Chip>
            </div>
            {[
              { step: '1', title: '使用 AWQ / GPTQ 量化版本',
                detail: '70B 全精度需 ~140GB 显存，AWQ INT4 量化后仅需 ~38GB，质量损失 <2%。推荐 `casperhansen/llama-3.1-70b-instruct-awq`。',
                action: '一键切换模型', risky: false },
              { step: '2', title: '降低 max_model_len 至 4096',
                detail: 'KV cache 预分配按 max_model_len 计算，从 16K 降到 4K 可节约约 28GB 显存，但牺牲长上下文能力。',
                action: '编辑启动参数', risky: true },
              { step: '3', title: '启用 chunked-prefill',
                detail: 'vLLM 0.6.5 引入的分块预填充可降低峰值显存约 20%，对吞吐量影响很小。需升级到 0.6.5+。',
                action: '升级 vLLM 镜像', risky: true },
              { step: '4', title: '改用 8B 版本',
                detail: 'Llama-3.1-8B-Instruct 仅需 16GB 显存，已在 Ollama 中常驻。仅作为兜底方案。',
                action: '切换到 8B', risky: false },
            ].map(s => (
              <div key={s.step} style={{
                display: 'flex', gap: 12, padding: '12px 0',
                borderTop: `1px solid ${T.borderSoft}`,
              }}>
                <div style={{
                  width: 24, height: 24, borderRadius: 6, flexShrink: 0,
                  background: T.blueSoft, color: T.blueDeep,
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  fontSize: 12, fontWeight: 700,
                }} className="mono">{s.step}</div>
                <div style={{ flex: 1 }}>
                  <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>{s.title}</div>
                  <div style={{ fontSize: 12, color: T.ink3, lineHeight: 1.65, marginTop: 4 }}>
                    {s.detail.split(/(`[^`]+`)/g).map((p, i) => p.startsWith('`')
                      ? <span key={i} className="mono" style={{ background: T.surfaceAlt, padding: '1px 5px', borderRadius: 3, fontSize: 11.5 }}>{p.slice(1, -1)}</span>
                      : p)}
                  </div>
                </div>
                <button style={{ ...(s.risky ? btnDanger : btnSecondary),
                  height: 28, padding: '0 12px', fontSize: 11.5, alignSelf: 'flex-start' }}>
                  {s.action}<Icon name="chevRight" size={11} stroke={2}/>
                </button>
              </div>
            ))}
          </div>

          {/* Timeline + Last good */}
          <div style={{ display: 'grid', gridTemplateColumns: '1.5fr 1fr', gap: 12 }}>
            <div style={{ background: T.surface, border: `1px solid ${T.border}`,
              borderRadius: 10, padding: 18 }}>
              <div style={{ fontSize: 13, fontWeight: 700, color: T.ink, marginBottom: 12 }}>故障时间线</div>
              {[
                { t: '10:22:48', tone: 'gray',  title: '收到启动指令', desc: 'docker exec start vllm · 触发模型加载' },
                { t: '10:23:09', tone: 'amber', title: '权重加载中',   desc: 'safetensors → GPU memory · 已加载 87GB' },
                { t: '10:23:12', tone: 'red',   title: 'CUDA OOM',     desc: 'KV cache 预分配失败，显存超 124.6 GB' },
                { t: '10:23:13', tone: 'amber', title: '自动重启 #1',  desc: 'Supervisor 触发重启，10s 后失败' },
                { t: '10:23:24', tone: 'amber', title: '自动重启 #2',  desc: '退避策略升至 20s，仍失败' },
                { t: '10:23:48', tone: 'amber', title: '自动重启 #3',  desc: '退避至 40s，仍失败' },
                { t: '10:24:08', tone: 'red',   title: '进入恢复模式', desc: '需人工介入；告警已推送' },
                { t: '现在',     tone: 'gray',  title: '等待操作',    desc: '运维：dev-zhang · 当前未验证' },
              ].map((e, i, a) => (
                <div key={i} style={{ display: 'flex', gap: 12, paddingBottom: i === a.length - 1 ? 0 : 12 }}>
                  <div style={{ width: 16, display: 'flex', flexDirection: 'column', alignItems: 'center', flexShrink: 0 }}>
                    <StatusDot tone={e.tone} size={8} pulse={i === a.length - 1}/>
                    {i < a.length - 1 && <div style={{ width: 1, flex: 1, background: T.borderSoft, marginTop: 4 }}/>}
                  </div>
                  <div style={{ flex: 1 }}>
                    <div style={{ display: 'flex', gap: 8, alignItems: 'baseline' }}>
                      <span style={{ fontSize: 12, fontWeight: 600, color: T.ink }}>{e.title}</span>
                      <span className="mono tnum" style={{ fontSize: 11, color: T.ink4 }}>{e.t}</span>
                    </div>
                    <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 2 }}>{e.desc}</div>
                  </div>
                </div>
              ))}
            </div>

            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              <div style={{ background: T.surface, border: `1px solid ${T.border}`,
                borderRadius: 10, padding: 16 }}>
                <div style={{ fontSize: 11.5, color: T.ink3, fontWeight: 600,
                  letterSpacing: '0.04em', textTransform: 'uppercase', marginBottom: 8 }}>
                  上次正常运行
                </div>
                <div className="mono tnum" style={{ fontSize: 16, fontWeight: 700, color: T.ink }}>10:22:48</div>
                <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 4 }}>
                  距今 <strong style={{ color: T.ink2 }}>3 小时 45 分</strong>
                </div>
                <div style={{ height: 1, background: T.borderSoft, margin: '12px 0' }}/>
                <div style={{ fontSize: 11.5, color: T.ink3 }}>
                  累计正常运行 <strong style={{ color: T.ink2 }}>2 天 14 小时</strong> · 处理请求 <strong className="mono" style={{ color: T.ink2 }}>18,420</strong>
                </div>
              </div>

              <div style={{ background: T.surface, border: `1px solid ${T.border}`,
                borderRadius: 10, padding: 16 }}>
                <div style={{ fontSize: 11.5, color: T.ink3, fontWeight: 600,
                  letterSpacing: '0.04em', textTransform: 'uppercase', marginBottom: 8 }}>
                  影响面
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: T.ink }}>
                    <StatusDot tone="red" size={6}/>OpenWebUI 70B 路由不可用
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: T.ink }}>
                    <StatusDot tone="amber" size={6}/>3 个测试任务排队等待
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: T.ink }}>
                    <StatusDot tone="green" size={6}/>Ollama 8B 路由正常
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════
// ComfyUI — node canvas
// ═══════════════════════════════════════════════════════════════
function ComfyUIFace({ onMgmt }) {
  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: '#2a2a2a', color: '#e5e5e5', overflow: 'hidden' }}>
      <FaceHeader title="ComfyUI · 节点工作流" subtitle="SDXL Base → Refiner → Upscale"
        version="v0.3.10" onMgmt={onMgmt}/>

      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* Canvas */}
        <div style={{
          flex: 1, position: 'relative',
          background:
            'radial-gradient(circle at 30% 30%, rgba(124,58,237,0.08), transparent 50%),' +
            'linear-gradient(rgba(255,255,255,0.02) 1px, transparent 1px),' +
            'linear-gradient(90deg, rgba(255,255,255,0.02) 1px, transparent 1px),' +
            '#1a1a1a',
          backgroundSize: 'auto, 24px 24px, 24px 24px, auto',
          overflow: 'hidden',
        }}>
          {/* Nodes */}
          <ComfyNode x={28}  y={50}  title="Load Checkpoint"     color="#7c3aed" rows={[['ckpt_name', 'sd_xl_base_1.0']]} outputs={['MODEL', 'CLIP', 'VAE']}/>
          <ComfyNode x={28}  y={250} title="CLIP Text Encode"    color="#0891b2" rows={[['text', 'a futuristic edge AI device, neon glow, 8k']]} outputs={['CONDITIONING']}/>
          <ComfyNode x={28}  y={420} title="CLIP Text Encode"    color="#0891b2" rows={[['text', 'low quality, blurry, text']]} outputs={['CONDITIONING']}/>
          <ComfyNode x={300} y={120} title="Empty Latent Image"  color="#10b981" rows={[['width', '1024'], ['height', '1024'], ['batch', '4']]} outputs={['LATENT']}/>
          <ComfyNode x={300} y={320} title="KSampler"            color="#f59e0b" rows={[['steps', '32'], ['cfg', '7.5'], ['sampler', 'dpmpp_2m'], ['seed', '8421337']]} outputs={['LATENT']} highlight/>
          <ComfyNode x={580} y={250} title="VAE Decode"          color="#ec4899" rows={[]} outputs={['IMAGE']}/>
          <ComfyNode x={830} y={250} title="Save Image"          color="#3b82f6" rows={[['filename', 'edgex_${seed}']]} outputs={[]}/>

          {/* Connections (SVG lines) */}
          <svg style={{ position: 'absolute', inset: 0, pointerEvents: 'none' }} width="100%" height="100%">
            <path d="M 240 100 C 280 100, 290 160, 300 160" stroke="#7c3aed" strokeWidth="2" fill="none"/>
            <path d="M 240 280 C 280 280, 290 360, 300 360" stroke="#0891b2" strokeWidth="2" fill="none"/>
            <path d="M 240 450 C 280 450, 290 390, 300 390" stroke="#0891b2" strokeWidth="2" fill="none"/>
            <path d="M 512 170 C 540 170, 560 350, 580 350" stroke="#10b981" strokeWidth="2" fill="none"/>
            <path d="M 512 410 C 545 410, 555 290, 580 290" stroke="#f59e0b" strokeWidth="2" fill="none"/>
            <path d="M 762 290 C 800 290, 815 290, 830 290" stroke="#ec4899" strokeWidth="2" fill="none"/>
          </svg>

          {/* Mini map */}
          <div style={{
            position: 'absolute', right: 12, bottom: 12,
            width: 160, height: 100, background: 'rgba(0,0,0,0.5)',
            border: '1px solid rgba(255,255,255,0.1)', borderRadius: 6,
            padding: 6,
          }}>
            <div style={{ fontSize: 9, color: '#888', marginBottom: 4 }}>MINIMAP</div>
            <div style={{ position: 'relative', width: '100%', height: '78%' }}>
              {[[8,12,40,18,'#7c3aed'],[8,40,40,18,'#0891b2'],[8,62,40,18,'#0891b2'],[58,22,40,18,'#10b981'],[58,52,40,18,'#f59e0b'],[110,40,30,18,'#ec4899'],[140,40,18,18,'#3b82f6']].map((b, i) => (
                <div key={i} style={{ position: 'absolute', left: b[0], top: b[1], width: b[2], height: b[3], background: b[4], borderRadius: 2, opacity: 0.7 }}/>
              ))}
            </div>
          </div>
        </div>

        {/* Right panel — queue / history */}
        <div style={{
          width: 280, background: '#1a1a1a', flexShrink: 0,
          borderLeft: '1px solid #333', display: 'flex', flexDirection: 'column',
        }}>
          <div style={{ padding: 14, borderBottom: '1px solid #333' }}>
            <button style={{
              width: '100%', height: 38, borderRadius: 8,
              background: 'linear-gradient(140deg, #7c3aed, #4338ca)', color: 'white',
              border: 'none', fontSize: 13, fontWeight: 700, cursor: 'pointer',
              display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
            }}>
              <Icon name="play" size={13} stroke={2.2}/>Queue Prompt
            </button>
            <div style={{ display: 'flex', gap: 6, marginTop: 8 }}>
              <button style={smallDarkBtn}>Save</button>
              <button style={smallDarkBtn}>Load</button>
              <button style={smallDarkBtn}>Clear</button>
            </div>
          </div>

          <div style={{ padding: '12px 14px 6px',
            fontSize: 10.5, fontWeight: 700, color: '#999',
            letterSpacing: '0.06em', textTransform: 'uppercase' }}>
            队列 · 1 运行 / 2 等待
          </div>
          <div style={{ padding: '0 14px' }}>
            <QueueItem state="running" prompt="edge AI device, neon, 8k" progress={68}/>
            <QueueItem state="pending" prompt="cyberpunk robot dog"/>
            <QueueItem state="pending" prompt="datacenter at night, fog"/>
          </div>

          <div style={{ padding: '12px 14px 6px',
            fontSize: 10.5, fontWeight: 700, color: '#999',
            letterSpacing: '0.06em', textTransform: 'uppercase' }}>
            历史
          </div>
          <div style={{ flex: 1, overflowY: 'auto', padding: '0 14px 14px',
            display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 6 }}>
            {Array.from({ length: 12 }).map((_, i) => (
              <div key={i} style={{
                aspectRatio: '1', borderRadius: 6,
                background: `linear-gradient(${135 + i * 17}deg, hsl(${i * 31}, 60%, 40%), hsl(${i * 31 + 60}, 60%, 25%))`,
                position: 'relative', overflow: 'hidden',
              }}>
                <div style={{ position: 'absolute', bottom: 2, right: 4,
                  fontSize: 8.5, color: 'rgba(255,255,255,0.7)' }} className="mono">#{4280 - i}</div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

const smallDarkBtn = {
  flex: 1, height: 26, borderRadius: 5,
  background: '#2a2a2a', color: '#cccccc', border: '1px solid #3a3a3a',
  fontSize: 11, cursor: 'pointer',
};

function ComfyNode({ x, y, title, color, rows, outputs, highlight }) {
  return (
    <div style={{
      position: 'absolute', left: x, top: y, width: 212,
      background: '#2a2a2a', border: `1px solid ${highlight ? color : '#3a3a3a'}`,
      borderRadius: 6, overflow: 'hidden', userSelect: 'none',
      boxShadow: highlight ? `0 0 0 1px ${color}66, 0 4px 12px -4px ${color}66` : '0 2px 8px -2px rgba(0,0,0,0.4)',
    }}>
      <div style={{ padding: '6px 10px', background: color,
        color: 'white', fontSize: 11, fontWeight: 700,
        display: 'flex', alignItems: 'center', gap: 4 }}>
        <div style={{ width: 6, height: 6, borderRadius: '50%', background: 'rgba(255,255,255,0.6)' }}/>
        {title}
      </div>
      <div style={{ padding: '6px 10px' }}>
        {rows.map(([k, v], i) => (
          <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 3 }}>
            <span style={{ fontSize: 10, color: '#999' }}>{k}</span>
            <div style={{
              flex: 1, padding: '2px 6px', borderRadius: 3,
              background: '#1a1a1a', border: '1px solid #333',
              fontSize: 10.5, color: '#e5e5e5', fontFamily: 'ui-monospace, monospace',
              overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
            }}>{v}</div>
          </div>
        ))}
      </div>
      <div style={{ padding: '4px 10px 8px', display: 'flex', gap: 4, flexWrap: 'wrap' }}>
        {outputs.map(o => (
          <span key={o} style={{
            fontSize: 9, padding: '1px 5px', borderRadius: 2,
            background: '#1a1a1a', color: color, border: `1px solid ${color}44`,
            fontFamily: 'ui-monospace, monospace',
          }}>{o}</span>
        ))}
      </div>
    </div>
  );
}

function QueueItem({ state, prompt, progress }) {
  return (
    <div style={{
      padding: 8, borderRadius: 6, background: '#2a2a2a',
      border: '1px solid #333', marginBottom: 6,
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 5 }}>
        <StatusDot tone={state === 'running' ? 'blue' : 'gray'} size={6} pulse={state === 'running'}/>
        <span style={{ fontSize: 11, color: state === 'running' ? '#7dd3fc' : '#888',
          fontWeight: 600 }}>
          {state === 'running' ? '生成中' : '等待中'}
        </span>
        {progress != null && <span style={{ fontSize: 10.5, color: '#888' }} className="mono tnum">{progress}%</span>}
      </div>
      <div style={{ fontSize: 11, color: '#cccccc', overflow: 'hidden',
        textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{prompt}</div>
      {progress != null && (
        <div style={{ height: 3, background: '#1a1a1a', borderRadius: 2, marginTop: 6, overflow: 'hidden' }}>
          <div style={{ width: `${progress}%`, height: '100%', background: '#7c3aed' }}/>
        </div>
      )}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════
// SD WebUI (AUTOMATIC1111 style)
// ═══════════════════════════════════════════════════════════════
function SDWebUIFace({ onMgmt }) {
  const [tab, setTab] = useState('txt2img');
  const [prompt, setPrompt] = useState('a serene mountain lake at dawn, photorealistic, 8k, golden hour lighting');
  const [neg, setNeg] = useState('low quality, blurry, text, watermark');

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: T.surfaceAlt, overflow: 'hidden' }}>
      <FaceHeader title="Stable Diffusion WebUI" subtitle="AUTOMATIC1111"
        version="v1.10.1" onMgmt={onMgmt}/>

      {/* Tabs */}
      <div style={{ display: 'flex', background: T.surface, borderBottom: `1px solid ${T.border}`,
        padding: '0 16px', flexShrink: 0 }}>
        {['txt2img', 'img2img', 'extras', 'PNG Info', 'LoRA', '设置'].map(t2 => (
          <div key={t2} onClick={() => setTab(t2)} style={{
            padding: '10px 14px 12px', fontSize: 12.5,
            color: tab === t2 ? T.ink : T.ink3,
            fontWeight: tab === t2 ? 600 : 500, cursor: 'pointer',
            borderBottom: `2px solid ${tab === t2 ? '#ec4899' : 'transparent'}`,
            marginBottom: -1,
          }}>{t2}</div>
        ))}
      </div>

      {/* Top model selector */}
      <div style={{ padding: '10px 16px', background: T.surface, borderBottom: `1px solid ${T.borderSoft}`,
        display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0 }}>
        <span style={{ fontSize: 11.5, color: T.ink3 }}>Checkpoint</span>
        <div style={{ display: 'flex', alignItems: 'center', gap: 5, padding: '5px 10px',
          background: T.surfaceAlt, border: `1px solid ${T.border}`, borderRadius: 6, fontSize: 12 }}>
          <span className="mono" style={{ color: T.ink, fontWeight: 600 }}>sd_xl_base_1.0.safetensors</span>
          <Icon name="chevDown" size={11} stroke={2} style={{ color: T.ink4 }}/>
        </div>
        <span style={{ fontSize: 11.5, color: T.ink3 }}>VAE</span>
        <div style={{ display: 'flex', alignItems: 'center', gap: 5, padding: '5px 10px',
          background: T.surfaceAlt, border: `1px solid ${T.border}`, borderRadius: 6, fontSize: 12 }}>
          <span className="mono" style={{ color: T.ink, fontWeight: 600 }}>Automatic</span>
          <Icon name="chevDown" size={11} stroke={2} style={{ color: T.ink4 }}/>
        </div>
        <div style={{ flex: 1 }}/>
        <span style={{ fontSize: 11, color: T.ink3, display: 'inline-flex', alignItems: 'center', gap: 4 }}>
          <StatusDot tone="green" size={6}/>已加载 · 显存 6.8 GB
        </span>
      </div>

      <div style={{ flex: 1, display: 'grid', gridTemplateColumns: '1fr 1fr', overflow: 'hidden' }}>
        {/* Left: prompt + params */}
        <div style={{ padding: 16, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div>
            <div style={{ fontSize: 11.5, color: T.ink3, fontWeight: 600,
              letterSpacing: '0.04em', textTransform: 'uppercase', marginBottom: 6 }}>提示词</div>
            <textarea value={prompt} onChange={(e) => setPrompt(e.target.value)} style={{
              width: '100%', height: 80, padding: '10px 12px',
              border: `1px solid ${T.border}`, borderRadius: 8, fontSize: 12.5,
              lineHeight: 1.6, color: T.ink, background: 'white', outline: 'none',
              resize: 'vertical', fontFamily: 'inherit',
            }}/>
            <div style={{ display: 'flex', gap: 4, marginTop: 6, flexWrap: 'wrap' }}>
              {['masterpiece', '8k', 'photorealistic', 'cinematic'].map(t => (
                <span key={t} style={{
                  fontSize: 10.5, padding: '2px 7px', borderRadius: 999,
                  background: '#fdf2f8', color: '#9d174d', border: '1px solid #fbcfe8',
                }}>+ {t}</span>
              ))}
            </div>
          </div>

          <div>
            <div style={{ fontSize: 11.5, color: T.ink3, fontWeight: 600,
              letterSpacing: '0.04em', textTransform: 'uppercase', marginBottom: 6 }}>负面提示词</div>
            <textarea value={neg} onChange={(e) => setNeg(e.target.value)} style={{
              width: '100%', height: 60, padding: '10px 12px',
              border: `1px solid ${T.border}`, borderRadius: 8, fontSize: 12.5,
              color: T.ink, background: 'white', outline: 'none', resize: 'vertical',
              fontFamily: 'inherit',
            }}/>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
            {[
              { label: '采样方法', val: 'DPM++ 2M Karras' },
              { label: '采样步数', val: '32' },
              { label: 'CFG Scale', val: '7.5' },
              { label: 'Seed',     val: '8421337' },
              { label: '宽度',     val: '1024' },
              { label: '高度',     val: '1024' },
            ].map(p => (
              <div key={p.label} style={{
                padding: 10, borderRadius: 7, background: T.surface,
                border: `1px solid ${T.border}`,
              }}>
                <div style={{ fontSize: 10.5, color: T.ink3 }}>{p.label}</div>
                <div className="mono" style={{ fontSize: 13, color: T.ink, fontWeight: 600, marginTop: 3 }}>{p.val}</div>
              </div>
            ))}
          </div>

          <button style={{
            height: 44, borderRadius: 8,
            background: 'linear-gradient(140deg, #ec4899, #9d174d)',
            color: 'white', border: 'none', fontSize: 14, fontWeight: 700,
            cursor: 'pointer',
            display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
            boxShadow: '0 4px 12px -3px rgba(236, 72, 153, 0.5)',
          }}>
            <Icon name="sparkle" size={15} stroke={2.2}/>Generate · 4 张图
          </button>
        </div>

        {/* Right: gallery */}
        <div style={{ background: T.surface, borderLeft: `1px solid ${T.border}`,
          padding: 16, overflowY: 'auto' }}>
          <div style={{ display: 'flex', alignItems: 'center', marginBottom: 10 }}>
            <span style={{ fontSize: 12, fontWeight: 600, color: T.ink }}>预览</span>
            <Chip tone="green" style={{ marginLeft: 8 }}><StatusDot tone="green" size={6} pulse/>生成中 · 步骤 24 / 32</Chip>
            <div style={{ flex: 1 }}/>
            <span className="mono" style={{ fontSize: 11, color: T.ink3 }}>seed 8421337</span>
          </div>

          {/* Big preview */}
          <div style={{
            aspectRatio: '1', borderRadius: 10,
            background: 'linear-gradient(135deg, #fb7185 0%, #ec4899 30%, #8b5cf6 60%, #3b82f6 100%)',
            position: 'relative', overflow: 'hidden', marginBottom: 10,
            boxShadow: '0 8px 30px -10px rgba(15,23,42,0.3)',
          }}>
            {/* "mountain lake" hint via abstract shapes */}
            <div style={{
              position: 'absolute', bottom: 0, left: 0, right: 0, height: '40%',
              background: 'linear-gradient(180deg, rgba(15,23,42,0) 0%, rgba(15,23,42,0.5) 100%)',
            }}/>
            <div style={{
              position: 'absolute', top: '40%', left: 0, right: 0, height: '20%',
              background: 'radial-gradient(ellipse at center, rgba(251,191,36,0.7), transparent 70%)',
            }}/>
            <div style={{
              position: 'absolute', top: 8, left: 10, padding: '3px 8px',
              borderRadius: 4, background: 'rgba(0,0,0,0.45)',
              fontSize: 10, color: 'rgba(255,255,255,0.85)', fontFamily: 'ui-monospace, monospace',
            }}>1024×1024 · 24/32 步</div>
          </div>

          {/* Variant thumbnails */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 6, marginBottom: 14 }}>
            {[0,1,2,3].map(i => (
              <div key={i} style={{
                aspectRatio: '1', borderRadius: 6, cursor: 'pointer',
                background: `linear-gradient(${130 + i * 15}deg, hsl(${320 - i * 8},70%,60%), hsl(${260 - i * 12},60%,40%))`,
                border: i === 0 ? '2px solid #ec4899' : '2px solid transparent',
              }}/>
            ))}
          </div>

          {/* Output bar */}
          <div style={{
            padding: 10, borderRadius: 7, background: T.surfaceAlt,
            border: `1px solid ${T.borderSoft}`,
            display: 'flex', alignItems: 'center', gap: 8,
          }}>
            <span style={{ fontSize: 11, color: T.ink3 }}>输出</span>
            <span className="mono" style={{ fontSize: 11, color: T.ink, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              /workspace/outputs/2026-05-26/00037-8421337.png
            </span>
            <button style={{ ...btnSecondary, height: 24, padding: '0 8px', fontSize: 11 }}>
              <Icon name="folder" size={11} stroke={1.8}/>打开
            </button>
            <button style={{ ...btnSecondary, height: 24, padding: '0 8px', fontSize: 11 }}>
              <Icon name="download" size={11} stroke={1.8}/>下载
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════
// Open WebUI — simple chat
// ═══════════════════════════════════════════════════════════════
function OpenWebUIFace({ onMgmt }) {
  const [model, setModel] = useState('llama-3.1-8b-instruct');
  const suggestions = [
    { icon: 'sparkle', label: '总结这周的训练日志', desc: '从 TensorBoard 抽取 loss / 评估指标' },
    { icon: 'wrench',  label: '排查 vLLM OOM',     desc: '基于当前 GPU 占用给出建议' },
    { icon: 'code',    label: '写一个 LoRA 微调脚本',desc: 'Llama-3.1-8B + alpaca 数据集' },
    { icon: 'book',    label: '解释 PagedAttention', desc: '通俗讲讲 vLLM 的核心机制' },
  ];

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', background: '#fafbfc' }}>
      <FaceHeader title="Open WebUI" subtitle="多模型聊天前端"
        version="v0.4.7" onMgmt={onMgmt}/>

      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        <div style={{ width: 200, flexShrink: 0, borderRight: `1px solid ${T.borderSoft}`,
          background: T.surface, padding: 12, display: 'flex', flexDirection: 'column' }}>
          <button style={{
            height: 34, borderRadius: 8, marginBottom: 10,
            background: '#0f172a', color: 'white', border: 'none',
            fontSize: 12.5, fontWeight: 600, cursor: 'pointer',
            display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 5,
          }}><Icon name="plus" size={13} stroke={2.2}/>新建聊天</button>
          <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
            letterSpacing: '0.06em', textTransform: 'uppercase', padding: '6px 8px' }}>今天</div>
          {['LoRA 训练 vs 全参微调', 'vLLM 显存调优'].map(c => (
            <div key={c} style={{ padding: '7px 10px', borderRadius: 6, fontSize: 12, color: T.ink2, cursor: 'pointer', marginBottom: 2 }}>{c}</div>
          ))}
          <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
            letterSpacing: '0.06em', textTransform: 'uppercase', padding: '10px 8px 6px' }}>昨天</div>
          {['SDXL ControlNet 用法', 'RAG 数据清洗 pipeline'].map(c => (
            <div key={c} style={{ padding: '7px 10px', borderRadius: 6, fontSize: 12, color: T.ink2, cursor: 'pointer', marginBottom: 2 }}>{c}</div>
          ))}
          <div style={{ flex: 1 }}/>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: 8, borderRadius: 7, background: T.surfaceAlt }}>
            <div style={{ width: 28, height: 28, borderRadius: '50%', background: T.blueSoft, color: T.blueDeep,
              display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 11, fontWeight: 700 }}>张</div>
            <div style={{ minWidth: 0 }}>
              <div style={{ fontSize: 12, color: T.ink, fontWeight: 600 }}>dev-zhang</div>
              <div style={{ fontSize: 10.5, color: T.ink3 }}>模型研发组</div>
            </div>
          </div>
        </div>

        <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
          <div style={{
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            padding: '12px 18px', borderBottom: `1px solid ${T.borderSoft}`,
          }}>
            <div style={{
              display: 'flex', alignItems: 'center', gap: 6, padding: '5px 12px',
              borderRadius: 999, background: 'white', border: `1px solid ${T.border}`,
              boxShadow: '0 1px 2px rgba(15,23,42,0.04)', cursor: 'pointer',
            }}>
              <Icon name="sparkle" size={13} stroke={1.8} style={{ color: T.cyan }}/>
              <span style={{ fontSize: 12.5, color: T.ink, fontWeight: 600 }} className="mono">{model}</span>
              <Icon name="chevDown" size={11} stroke={2} style={{ color: T.ink4 }}/>
            </div>
          </div>

          <div style={{ flex: 1, display: 'flex', flexDirection: 'column',
            alignItems: 'center', justifyContent: 'center', padding: 32 }}>
            <div style={{
              width: 56, height: 56, borderRadius: 16,
              background: 'linear-gradient(140deg, #22d3ee, #0891b2)',
              color: 'white', display: 'flex', alignItems: 'center', justifyContent: 'center',
              marginBottom: 16,
              boxShadow: '0 8px 22px -6px rgba(34,211,238,0.5), inset 0 1px 0 rgba(255,255,255,0.3)',
            }}>
              <Icon name="openwebui" size={28} stroke={1.6}/>
            </div>
            <div style={{ fontSize: 18, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>
              你好，dev-zhang
            </div>
            <div style={{ fontSize: 12.5, color: T.ink3, marginTop: 6 }}>
              选一个开始，或直接在下方输入你的问题
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)', gap: 10,
              marginTop: 24, maxWidth: 560 }}>
              {suggestions.map((s, i) => (
                <div key={i} style={{
                  padding: 14, borderRadius: 10, background: T.surface,
                  border: `1px solid ${T.border}`, cursor: 'pointer',
                  display: 'flex', gap: 10, alignItems: 'flex-start',
                }}>
                  <div style={{
                    width: 30, height: 30, borderRadius: 8, flexShrink: 0,
                    background: T.surfaceAlt, color: T.blueDeep,
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                  }}><Icon name={s.icon} size={15} stroke={1.8}/></div>
                  <div>
                    <div style={{ fontSize: 12.5, color: T.ink, fontWeight: 600 }}>{s.label}</div>
                    <div style={{ fontSize: 11, color: T.ink3, marginTop: 3 }}>{s.desc}</div>
                  </div>
                </div>
              ))}
            </div>
          </div>

          <div style={{ padding: 18 }}>
            <div style={{
              maxWidth: 720, margin: '0 auto',
              border: `1.5px solid ${T.border}`, borderRadius: 14, background: 'white',
              boxShadow: '0 4px 14px -4px rgba(15,23,42,0.08)',
              padding: '4px 4px 4px 14px', display: 'flex', alignItems: 'center', gap: 8,
            }}>
              <input placeholder="给 Open WebUI 发送消息…" style={{
                flex: 1, height: 40, border: 'none', outline: 'none',
                fontSize: 13, color: T.ink, background: 'transparent',
              }}/>
              <button style={iconBtn}><Icon name="attach" size={15} stroke={1.8}/></button>
              <button style={iconBtn}><Icon name="mic" size={15} stroke={1.8}/></button>
              <button style={{ ...iconBtn, background: T.cyan, color: 'white' }}>
                <Icon name="send" size={14} stroke={2.2}/>
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

const iconBtn = {
  width: 32, height: 32, borderRadius: 8, border: 'none',
  background: 'transparent', color: T.ink3, cursor: 'pointer',
  display: 'flex', alignItems: 'center', justifyContent: 'center',
};

// ═══════════════════════════════════════════════════════════════
// Training task (running job)
// ═══════════════════════════════════════════════════════════════
function TrainingFace({ onMgmt }) {
  // Generate a smooth descending loss curve with noise
  const lossData = Array.from({ length: 60 }).map((_, i) => {
    return 2.4 - 0.022 * i + Math.sin(i * 0.7) * 0.08 + Math.cos(i * 0.3) * 0.04;
  });
  const gpuData = Array.from({ length: 60 }).map((_, i) =>
    78 + Math.sin(i * 0.4) * 8 + Math.cos(i * 0.7) * 5);

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', background: T.surfaceAlt }}>
      <FaceHeader title="train-72h · Llama-3.1-8B LoRA 微调" subtitle="Unsloth + Axolotl · 数据集 alpaca-cleaned"
        version="Step 14,300 / 30,000" onMgmt={onMgmt}/>

      <div style={{ flex: 1, overflowY: 'auto', padding: 24 }}>
        <div style={{ maxWidth: 1100, margin: '0 auto' }}>
          {/* Top KPIs */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12, marginBottom: 14 }}>
            {[
              { label: '进度',       val: '47.7', unit: '%',  tone: T.blue,  hint: '14,300 / 30,000 step' },
              { label: 'Loss',       val: '1.82',  unit: '',   tone: T.green, hint: '↓ -0.06 (近 1h)' },
              { label: '吞吐量',     val: '8.4',  unit: 'tok/s/GPU', tone: T.indigo, hint: '每 step 平均 1.6s' },
              { label: '预计剩余',   val: '37',   unit: '小时', tone: T.amber, hint: '完成于 2026-05-28 03:14' },
            ].map((k, i) => (
              <div key={i} style={{
                background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
                padding: 16,
              }}>
                <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
                  letterSpacing: '0.04em', textTransform: 'uppercase' }}>{k.label}</div>
                <div style={{ display: 'flex', alignItems: 'baseline', gap: 4, marginTop: 6 }}>
                  <span className="mono tnum" style={{ fontSize: 24, fontWeight: 700, color: k.tone,
                    letterSpacing: '-0.02em' }}>{k.val}</span>
                  <span style={{ fontSize: 12, color: T.ink3 }}>{k.unit}</span>
                </div>
                <div style={{ fontSize: 11, color: T.ink3, marginTop: 4 }}>{k.hint}</div>
              </div>
            ))}
          </div>

          {/* Charts */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 14 }}>
            <Card title="Training Loss" action={<span style={{ fontSize: 11.5, color: T.ink3 }}>最近 60 步</span>}>
              <Sparkline data={lossData} color={T.green} width={460} height={140} fill showAxis max={2.5}/>
              <div style={{ display: 'flex', gap: 16, fontSize: 11.5, marginTop: 8, color: T.ink3 }}>
                <span>当前 <span className="mono tnum" style={{ color: T.ink, fontWeight: 600 }}>{lossData[lossData.length-1].toFixed(3)}</span></span>
                <span>开始时 <span className="mono tnum" style={{ color: T.ink2 }}>2.412</span></span>
                <span style={{ marginLeft: 'auto', color: T.green, fontWeight: 600 }}>下降 24.5%</span>
              </div>
            </Card>
            <Card title="GPU 利用率" action={<span style={{ fontSize: 11.5, color: T.ink3 }}>平均 78%</span>}>
              <Sparkline data={gpuData} color={T.indigo} width={460} height={140} fill showAxis max={100}/>
              <div style={{ display: 'flex', gap: 16, fontSize: 11.5, marginTop: 8, color: T.ink3 }}>
                <span>当前 <span className="mono tnum" style={{ color: T.ink, fontWeight: 600 }}>{gpuData[gpuData.length-1].toFixed(0)}%</span></span>
                <span>峰值 <span className="mono tnum" style={{ color: T.ink2 }}>{Math.max(...gpuData).toFixed(0)}%</span></span>
                <span style={{ marginLeft: 'auto' }}>显存 <span className="mono tnum" style={{ color: T.ink, fontWeight: 600 }}>38.2 / 128 GB</span></span>
              </div>
            </Card>
          </div>

          {/* Config + Recent checkpoints */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <Card title="配置" padding={0}>
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
                <tbody>
                  {[
                    ['基础模型', 'meta-llama/Llama-3.1-8B'],
                    ['数据集',   'yahma/alpaca-cleaned (51,760 条)'],
                    ['方法',     'LoRA · r=64, alpha=128'],
                    ['优化器',   'AdamW (β=0.9, 0.95)'],
                    ['学习率',   '1e-4 · cosine + warmup 100'],
                    ['Batch',    '8 (per device) × 4 (grad accum) = 32'],
                    ['序列长度', '2048'],
                    ['精度',     'bf16'],
                  ].map(([k, v], i) => (
                    <tr key={k} style={{ borderTop: i ? `1px solid ${T.borderSoft}` : 'none' }}>
                      <td style={{ width: 90, padding: '10px 16px', color: T.ink3, fontSize: 12 }}>{k}</td>
                      <td style={{ padding: '10px 16px', color: T.ink }} className="mono">{v}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Card>

            <Card title="最近 checkpoint" padding={0}>
              <div style={{ padding: '4px 0' }}>
                {[
                  ['step-14000', '52 分钟前', '16 GB', 'loss 1.834'],
                  ['step-13500', '1 小时前',  '16 GB', 'loss 1.871'],
                  ['step-13000', '2 小时前',  '16 GB', 'loss 1.892'],
                  ['step-12500', '3 小时前',  '16 GB', 'loss 1.928'],
                  ['step-12000', '4 小时前',  '16 GB', 'loss 1.953'],
                ].map(([s, t, sz, l], i) => (
                  <div key={s} style={{
                    display: 'flex', alignItems: 'center', gap: 10,
                    padding: '10px 16px', borderTop: i ? `1px solid ${T.borderSoft}` : 'none',
                  }}>
                    <Icon name="brain" size={14} stroke={1.8} style={{ color: T.violet }}/>
                    <span className="mono" style={{ fontSize: 12.5, color: T.ink, fontWeight: 600 }}>{s}</span>
                    <span className="mono" style={{ fontSize: 11, color: T.ink3 }}>{l}</span>
                    <div style={{ flex: 1 }}/>
                    <span style={{ fontSize: 11, color: T.ink3 }}>{t}</span>
                    <span className="mono" style={{ fontSize: 11, color: T.ink3 }}>{sz}</span>
                  </div>
                ))}
              </div>
            </Card>
          </div>
        </div>
      </div>
    </div>
  );
}

export { AppShell, FaceHeader, FileIcon }
