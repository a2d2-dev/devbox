import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './styles.css'

class ErrorBoundary extends React.Component {
  constructor(props) { super(props); this.state = { error: null }; }
  static getDerivedStateFromError(error) { return { error }; }
  render() {
    if (this.state.error) {
      return React.createElement('pre', {
        style: { position: 'fixed', inset: 0, background: '#2a1215', color: '#ff8a80', font: '13px monospace', padding: 20, overflow: 'auto', zIndex: 99999, whiteSpace: 'pre-wrap' }
      }, '[React Error]\n\n' + this.state.error.message + '\n\n' + (this.state.error.stack || ''));
    }
    return this.props.children;
  }
}

ReactDOM.createRoot(document.getElementById('root')).render(
  <ErrorBoundary><App /></ErrorBoundary>
)
