import { useEffect, useRef, useState } from 'react';
import { T } from '../tokens';
import { Icon } from '../icons';

const STORAGE_KEY = 'edge-console.memo';
const DEBOUNCE_MS = 1000;

function readMemo() {
  try {
    return localStorage.getItem(STORAGE_KEY) ?? '';
  } catch {
    return '';
  }
}

function writeMemo(value) {
  try {
    localStorage.setItem(STORAGE_KEY, value);
  } catch {
    // quota / privacy mode — silently ignore, AC-3
  }
}

function clearMemo() {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    // ignore
  }
}

export function MemoWidget() {
  const [value, setValue] = useState(readMemo);
  const timerRef = useRef(null);
  const valueRef = useRef(value);

  // Keep the latest value visible to the unmount-flush effect.
  valueRef.current = value;

  useEffect(() => {
    return () => {
      if (timerRef.current != null) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
        writeMemo(valueRef.current);
      }
    };
  }, []);

  const onChange = (e) => {
    const next = e.target.value;
    setValue(next);
    if (timerRef.current != null) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      writeMemo(next);
    }, DEBOUNCE_MS);
  };

  const onClear = () => {
    if (!window.confirm('清空备忘录内容？')) return;
    if (timerRef.current != null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    setValue('');
    clearMemo();
  };

  return (
    <div style={{
      padding: 16,
      background: 'rgba(255,255,255,0.7)',
      backdropFilter: 'blur(12px)',
      WebkitBackdropFilter: 'blur(12px)',
      border: '1px solid rgba(255,255,255,0.9)',
      borderRadius: 14,
      boxShadow: '0 6px 20px -6px rgba(15,23,42,0.10)',
    }}>
      <div style={{
        display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10,
      }}>
        <Icon name="edit" size={14} stroke={1.8} style={{ color: T.blueDeep }}/>
        <div style={{ fontSize: 12, fontWeight: 700, color: T.ink,
          letterSpacing: '0.02em' }}>备忘录</div>
        <div style={{ flex: 1 }}/>
        {value.length > 0 && (
          <button
            type="button"
            onClick={onClear}
            title="清空备忘录"
            aria-label="清空备忘录"
            style={{
              display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
              width: 22, height: 22, borderRadius: 6,
              background: 'transparent', color: T.ink3,
              border: '1px solid transparent', cursor: 'pointer',
              padding: 0,
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.background = T.redSoft;
              e.currentTarget.style.color = T.red;
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.background = 'transparent';
              e.currentTarget.style.color = T.ink3;
            }}
          >
            <Icon name="trash" size={13} stroke={1.8}/>
          </button>
        )}
        <span className="tnum" style={{
          fontSize: 10.5, color: T.ink3, fontWeight: 600,
          fontFamily: T.mono,
        }}>{value.length} 字</span>
      </div>

      <textarea
        value={value}
        onChange={onChange}
        placeholder="临时笔记 · 仅本地保存"
        spellCheck={false}
        style={{
          width: '100%', height: 200, boxSizing: 'border-box',
          resize: 'none',
          background: 'transparent',
          color: T.ink,
          border: `1px solid ${T.borderSoft}`,
          borderRadius: 8,
          padding: 10,
          fontFamily: T.mono,
          fontSize: 12,
          lineHeight: 1.55,
          outline: 'none',
        }}
        onFocus={(e) => { e.currentTarget.style.borderColor = T.blue; }}
        onBlur={(e) => { e.currentTarget.style.borderColor = T.borderSoft; }}
      />
    </div>
  );
}

export default MemoWidget;
