import { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Sparkline } from '../components/ui'

export default function TerminalFace() {
  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: '#0b1020', overflow: 'hidden' }}>
      <iframe
        src="/terminal.html"
        style={{ flex: 1, border: 'none', width: '100%', height: '100%' }}
      />
    </div>
  );
}
