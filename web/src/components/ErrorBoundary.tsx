import { Component, ReactNode } from "react";

interface Props { children: ReactNode; }
interface State { hasError: boolean; error?: Error; }

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false };
  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }
  render() {
    if (this.state.hasError) {
      return (
        <div className="flex min-h-[400px] items-center justify-center">
          <div className="text-center">
            <h2 className="text-lg font-bold text-gray-200">Something went wrong</h2>
            <p className="mt-2 text-sm text-gray-400">{this.state.error?.message}</p>
            <button onClick={() => window.location.reload()} className="mt-4 rounded bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-500">
              Reload
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
