import { Component, ReactNode } from "react";
import { useTranslation } from "react-i18next";

interface Props { children: ReactNode; }
interface State { hasError: boolean; error?: Error; }

function ErrorFallback({ error }: { error?: Error }) {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-[400px] items-center justify-center">
      <div className="text-center">
        <h2 className="text-lg font-bold text-gray-200">{t("errorBoundary.title")}</h2>
        <p className="mt-2 text-sm text-gray-400">{error?.message || t("errorBoundary.message")}</p>
        <button onClick={() => window.location.reload()} className="mt-4 rounded bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-500">
          {t("errorBoundary.reload")}
        </button>
      </div>
    </div>
  );
}

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false };
  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }
  render() {
    if (this.state.hasError) {
      return <ErrorFallback error={this.state.error} />;
    }
    return this.props.children;
  }
}
