import { Component } from 'react'

export default class ErrorBoundary extends Component {
  constructor(props) {
    super(props)
    this.state = { error: null }
  }

  static getDerivedStateFromError(error) {
    return { error }
  }

  componentDidCatch(error, info) {
    console.error('UI error:', error, info)
  }

  render() {
    if (this.state.error) {
      return (
        <div className="empty" style={{ margin: '2rem 0' }}>
          <h2 style={{ marginTop: 0 }}>Something went wrong</h2>
          <p className="muted mono" style={{ whiteSpace: 'pre-wrap' }}>
            {String(this.state.error?.message || this.state.error)}
          </p>
          <button className="small" onClick={() => this.setState({ error: null })}>
            Try again
          </button>
          <button className="ghost small" style={{ marginLeft: 8 }} onClick={() => window.location.reload()}>
            Reload
          </button>
        </div>
      )
    }
    return this.props.children
  }
}
