import { useState, useEffect, useMemo } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot } from './ui'

// Generate a deterministic-looking 8-char code in XXXX-XXXX form
function genCode() {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789'; // omit ambiguous
  let s = '';
  for (let i = 0; i < 8; i++) s += chars[Math.floor(Math.random() * chars.length)];
  return s;
}

// Generate a faux-QR matrix (25x25) deterministic from the code string
function qrFromString(s) {
  const N = 25;
  // Simple hash chain
  let h = 5381;
  for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) | 0;
  const matrix = [];
  for (let y = 0; y < N; y++) {
    const row = [];
    for (let x = 0; x < N; x++) {
      // Position detection markers in corners (3 squares)
      const inMarker = (mx, my) => x >= mx && x < mx + 7 && y >= my && y < my + 7
        && (x === mx || x === mx + 6 || y === my || y === my + 6
            || (x >= mx + 2 && x <= mx + 4 && y >= my + 2 && y <= my + 4));
      const inMarkerArea = (mx, my) => x >= mx - 1 && x < mx + 8 && y >= my - 1 && y < my + 8;
      if (inMarker(0, 0) || inMarker(N - 7, 0) || inMarker(0, N - 7)) {
        row.push(1); continue;
      }
      if (inMarkerArea(0, 0) || inMarkerArea(N - 7, 0) || inMarkerArea(0, N - 7)) {
        row.push(0); continue;
      }
      // Pseudo-random data
      h = ((h * 1103515245 + 12345) & 0x7fffffff);
      row.push((h >> 16) % 2);
    }
    matrix.push(row);
  }
  return matrix;
}

function QRCode({ code, size = 156 }) {
  const matrix = useMemo(() => qrFromString(code), [code]);
  const N = matrix.length;
  const cell = size / N;
  return (
    <svg width={size} height={size} style={{ display: 'block' }}>
      <rect width={size} height={size} fill="white"/>
      {matrix.map((row, y) => row.map((v, x) =>
        v ? <rect key={`${y}-${x}`} x={x * cell} y={y * cell} width={cell + 0.5} height={cell + 0.5} fill="#0f172a"/> : null
      ))}
    </svg>
  );
}

function Step({ n, children }) {
  return (
    <div style={{ display: 'flex', gap: 7, marginBottom: 3 }}>
      <span style={{
        width: 16, height: 16, borderRadius: '50%', flexShrink: 0,
        background: T.blueSoft, color: T.blueDeep,
        display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
        fontSize: 9.5, fontWeight: 700, marginTop: 1,
      }} className="mono">{n}</span>
      <div style={{ flex: 1 }}>{children}</div>
    </div>
  );
}

function SuccessView() {
  return (
    <div className="edge-fade-in" style={{ padding: '28px 22px 24px', textAlign: 'center' }}>
      <div style={{
        width: 56, height: 56, borderRadius: '50%', margin: '0 auto 12px',
        background: '#ecfdf5', color: T.green,
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        boxShadow: `0 0 0 6px #d1fae5`,
      }}>
        <Icon name="check" size={28} stroke={2.4}/>
      </div>
      <div style={{ fontSize: 15, fontWeight: 700, color: T.ink, marginBottom: 6 }}>已授权</div>
      <div style={{ fontSize: 12.5, color: T.ink3, marginBottom: 14, lineHeight: 1.6 }}>
        操作已写入云端审计 · 正在执行
      </div>
      <div style={{
        display: 'inline-flex', alignItems: 'center', gap: 8,
        padding: '8px 14px', borderRadius: 8,
        background: '#ecfdf5', border: '1px solid #a7f3d0',
        fontSize: 12, color: '#047857',
      }}>
        <span>15 分钟内执行同类操作无需再次确认</span>
      </div>
    </div>
  );
}

export function AuthModal({ reason, onClose, onSuccess }) {
  const [code] = useState(() => genCode());
  const [secondsLeft, setSecondsLeft] = useState(300);
  const [state, setState] = useState('waiting'); // waiting | confirming | success

  // Countdown timer
  useEffect(() => {
    if (state !== 'waiting') return;
    const id = setInterval(() => setSecondsLeft(s => Math.max(0, s - 1)), 1000);
    return () => clearInterval(id);
  }, [state]);

  // Auto-success simulation: ~8 seconds for demo
  useEffect(() => {
    const id = setTimeout(() => {
      if (state === 'waiting') setState('confirming');
    }, 8000);
    return () => clearTimeout(id);
  }, [state]);

  useEffect(() => {
    if (state === 'confirming') {
      const id = setTimeout(() => {
        setState('success');
        setTimeout(() => onSuccess({ until: '14:30' }), 900);
      }, 1400);
      return () => clearTimeout(id);
    }
  }, [state, onSuccess]);

  // ESC to close
  useEffect(() => {
    const h = (e) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  }, [onClose]);

  const mm = String(Math.floor(secondsLeft / 60)).padStart(2, '0');
  const ss = String(secondsLeft % 60).padStart(2, '0');
  const formattedCode = code.slice(0, 4) + '-' + code.slice(4, 8);

  return (
    <div className="edge-backdrop-in" onClick={onClose} style={{
      position: 'fixed', inset: 0, zIndex: 200,
      background: 'rgba(15,23,41,0.45)',
      backdropFilter: 'blur(4px)',
      WebkitBackdropFilter: 'blur(4px)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
    }}>
      <div onClick={(e) => e.stopPropagation()} className="edge-fade-in" style={{
        width: 480, background: 'white', borderRadius: 14,
        boxShadow: '0 28px 64px -12px rgba(15,23,42,0.45), 0 0 0 1px rgba(15,23,42,0.06)',
        overflow: 'hidden',
      }}>
        {/* Header */}
        <div style={{
          padding: '18px 22px 16px',
          background: 'linear-gradient(180deg, #f8fafc, #ffffff)',
          borderBottom: `1px solid ${T.borderSoft}`,
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <div style={{
              width: 32, height: 32, borderRadius: 9,
              background: T.blueSoft, color: T.blueDeep,
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              border: '1px solid #bfdbfe',
            }}>
              <Icon name="shield" size={17} stroke={1.8}/>
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontSize: 14.5, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>
                节点访问授权
              </div>
              <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 2 }}>
                在云端控制台确认授权后继续
              </div>
            </div>
            <div onClick={onClose} className="edge-menu-item" style={{
              width: 28, height: 28, borderRadius: 7, cursor: 'pointer',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              color: T.ink3,
              '--edge-menu-hover-bg': '#f1f5f9',
            }}>
              <Icon name="x" size={15} stroke={2}/>
            </div>
          </div>
        </div>

        {state === 'success' ? (
          <SuccessView/>
        ) : (
          <div style={{ padding: '18px 22px 20px' }}>
            {/* Action being authorized */}
            {reason && (
              <div style={{
                padding: 12, borderRadius: 8, marginBottom: 16,
                background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
              }}>
                <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
                  letterSpacing: '0.04em', textTransform: 'uppercase', marginBottom: 4 }}>
                  即将执行
                </div>
                <div style={{ fontSize: 13, color: T.ink, fontWeight: 600 }}>{reason}</div>
              </div>
            )}

            {/* QR + code */}
            <div style={{ display: 'flex', gap: 18, marginBottom: 14 }}>
              <div style={{
                width: 156, height: 156, flexShrink: 0,
                padding: 8, borderRadius: 10,
                background: 'white', border: `1px solid ${T.border}`,
                boxShadow: '0 2px 6px -2px rgba(15,23,42,0.06)',
              }}>
                <QRCode code={code} size={140}/>
              </div>
              <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
                <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
                  letterSpacing: '0.04em', textTransform: 'uppercase' }}>
                  授权码
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 6 }}>
                  <div className="mono" style={{
                    fontSize: 26, fontWeight: 700, color: T.ink,
                    letterSpacing: '0.08em', lineHeight: 1,
                  }}>
                    {formattedCode.split('').map((c, i) => (
                      <span key={i}>{c}</span>
                    ))}
                  </div>
                  <button title="复制" style={{
                    width: 26, height: 26, borderRadius: 6, border: `1px solid ${T.border}`,
                    background: 'white', color: T.ink3, cursor: 'pointer',
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                  }}>
                    <Icon name="copy" size={12} stroke={1.8}/>
                  </button>
                </div>

                <div style={{ marginTop: 12, fontSize: 11.5, color: T.ink3, lineHeight: 1.6 }}>
                  <Step n={1}>用手机或电脑扫描左侧二维码</Step>
                  <Step n={2}>或访问 <span className="mono" style={{ color: T.ink, fontWeight: 600 }}>edgex.cloud/device</span></Step>
                  <Step n={3}>在云端确认本次操作</Step>
                </div>
              </div>
            </div>

            {/* Status bar */}
            <div style={{
              padding: '10px 14px', borderRadius: 8,
              background: state === 'confirming' ? '#eff4ff' : T.surfaceAlt,
              border: `1px solid ${state === 'confirming' ? '#bfdbfe' : T.borderSoft}`,
              display: 'flex', alignItems: 'center', gap: 10,
            }}>
              {state === 'waiting' && (
                <>
                  <StatusDot tone="blue" size={7} pulse/>
                  <span style={{ fontSize: 12.5, color: T.ink2, fontWeight: 500 }}>
                    等待云端确认…
                  </span>
                  <div style={{ flex: 1 }}/>
                  <span style={{ fontSize: 11.5, color: T.ink3 }}>授权码</span>
                  <span className="mono tnum" style={{ fontSize: 12.5, color: T.ink, fontWeight: 600 }}>
                    {mm}:{ss}
                  </span>
                  <span style={{ fontSize: 11.5, color: T.ink3 }}>后失效</span>
                </>
              )}
              {state === 'confirming' && (
                <>
                  <svg width="14" height="14" viewBox="0 0 24 24" style={{ animation: 'spin 1s linear infinite' }}>
                    <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
                    <circle cx="12" cy="12" r="9" stroke="rgba(37,99,235,0.25)" strokeWidth="2.5" fill="none"/>
                    <path d="M12 3a9 9 0 0 1 9 9" stroke={T.blue} strokeWidth="2.5" strokeLinecap="round" fill="none"/>
                  </svg>
                  <span style={{ fontSize: 12.5, color: T.blueDeep, fontWeight: 600 }}>
                    已收到授权 · 正在确认…
                  </span>
                </>
              )}
            </div>

            {/* Help notice */}
            <div style={{
              marginTop: 12, padding: '8px 10px',
              fontSize: 11, color: T.ink3, lineHeight: 1.55,
              display: 'flex', gap: 7, alignItems: 'flex-start',
            }}>
              <Icon name="info" size={12} stroke={1.8} style={{ color: T.ink4, marginTop: 1, flexShrink: 0 }}/>
              <div>
                授权后 <span style={{ color: T.ink, fontWeight: 600 }}>15 分钟</span>内执行同类操作无需再次确认。
                所有操作记录将同步至云端审计中心。
              </div>
            </div>

            {/* Footer */}
            <div style={{ display: 'flex', gap: 8, marginTop: 16 }}>
              <button onClick={onClose} style={{
                flex: 1, height: 38, borderRadius: 8,
                background: 'white', border: `1px solid ${T.border}`,
                fontSize: 13, fontWeight: 500, color: T.ink2, cursor: 'pointer',
              }}>取消</button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
