import { Component, type ErrorInfo, type ReactNode } from 'react'
import { AlertCircle, RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface Props {
  children: ReactNode
  fallback?: ReactNode
}

interface State {
  hasError: boolean
  error: Error | null
}

export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[ErrorBoundary]', error, info.componentStack)
  }

  handleRetry = () => {
    this.setState({ hasError: false, error: null })
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) return this.props.fallback

      return (
        <div className="flex flex-col items-center justify-center py-24 gap-4 min-h-[400px]">
          <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center max-w-md">
            <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
            <h2 className="text-lg font-semibold text-text-primary mb-2">
              Something went wrong
            </h2>
            <p className="text-sm text-text-secondary mb-1">
              An unexpected error occurred while rendering this page.
            </p>
            {this.state.error && (
              <p className="text-xs font-mono text-text-tertiary mb-4 break-all">
                {this.state.error.message}
              </p>
            )}
            <div className="flex gap-2 justify-center">
              <Button onClick={this.handleRetry} variant="outline" className="gap-2">
                <RefreshCw className="h-4 w-4" />
                Try Again
              </Button>
              <Button onClick={() => window.location.assign('/')} variant="outline" className="gap-2">
                Go Home
              </Button>
            </div>
          </div>
        </div>
      )
    }

    return this.props.children
  }
}
