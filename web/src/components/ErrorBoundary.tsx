import { Component, type ErrorInfo, type ReactNode } from 'react';
import { RefreshCw, TriangleAlert } from 'lucide-react';

interface Props {
  children: ReactNode;
}
interface State {
  error: Error | null;
}

/** Łapie błędy renderowania, żeby jeden ekran nie wyłączał całego panelu. */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div className="state" role="alert">
        <div className="state-icon">
          <TriangleAlert size={26} className="text-danger" />
        </div>
        <h3>Ten ekran napotkał błąd</h3>
        <p>Odśwież stronę. Jeśli problem się powtarza, zgłoś go razem z wersją serwera.</p>
        <p className="xsmall faint code">{this.state.error.message}</p>
        <button type="button" className="btn" onClick={() => window.location.reload()}>
          <RefreshCw size={16} /> Odśwież
        </button>
      </div>
    );
  }
}
