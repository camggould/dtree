import { Component, type ReactNode, type ErrorInfo } from "react";
import { Card, CardBody, CardHeader, Button, Code } from "@heroui/react";
import { AlertTriangle } from "lucide-react";

interface Props {
  children: ReactNode;
  fallback?: (error: Error, reset: () => void) => ReactNode;
}
interface State {
  error: Error | null;
}

/** Top-level error boundary. Without this, any render-time throw produces a
 *  blank screen and back-navigation can't recover until a hard refresh.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // eslint-disable-next-line no-console
    console.error("UI ErrorBoundary caught:", error, info);
  }

  reset = () => this.setState({ error: null });

  render() {
    if (!this.state.error) return this.props.children;
    if (this.props.fallback)
      return this.props.fallback(this.state.error, this.reset);

    return (
      <div className="p-8 max-w-3xl mx-auto">
        <Card>
          <CardHeader className="flex items-center gap-2">
            <AlertTriangle className="text-danger" size={20} />
            <h2 className="text-lg font-semibold">Something went wrong</h2>
          </CardHeader>
          <CardBody className="gap-3">
            <p className="text-sm text-default-600">
              The view crashed. Recovering local UI state — your data is safe.
            </p>
            <Code size="sm" className="overflow-auto whitespace-pre-wrap">
              {this.state.error.message}
            </Code>
            {this.state.error.stack && (
              <details className="text-xs text-default-500">
                <summary className="cursor-pointer">Stack trace</summary>
                <pre className="mt-2 overflow-auto">
                  {this.state.error.stack}
                </pre>
              </details>
            )}
            <div className="flex gap-2 mt-2">
              <Button color="primary" onPress={this.reset}>
                Retry
              </Button>
              <Button
                variant="flat"
                onPress={() => {
                  this.reset();
                  window.history.back();
                }}
              >
                Go back
              </Button>
              <Button
                variant="light"
                onPress={() => window.location.reload()}
              >
                Hard reload
              </Button>
            </div>
          </CardBody>
        </Card>
      </div>
    );
  }
}
