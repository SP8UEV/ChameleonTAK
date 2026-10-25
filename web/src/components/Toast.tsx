import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react';
import { CircleAlert, CircleCheck, Info, TriangleAlert, X } from 'lucide-react';

type ToastKind = 'ok' | 'error' | 'info' | 'warn';
interface ToastItem {
  id: number;
  kind: ToastKind;
  text: string;
}

interface ToastApi {
  ok: (text: string) => void;
  error: (text: string) => void;
  info: (text: string) => void;
  warn: (text: string) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const nextId = useRef(1);

  const remove = useCallback((id: number) => {
    setItems((list) => list.filter((t) => t.id !== id));
  }, []);

  const push = useCallback(
    (kind: ToastKind, text: string) => {
      const id = nextId.current++;
      setItems((list) => [...list.slice(-3), { id, kind, text }]);
      window.setTimeout(() => remove(id), kind === 'error' ? 7000 : 4000);
    },
    [remove],
  );

  const api = useMemo<ToastApi>(
    () => ({
      ok: (t) => push('ok', t),
      error: (t) => push('error', t),
      info: (t) => push('info', t),
      warn: (t) => push('warn', t),
    }),
    [push],
  );

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {items.map((t) => (
          <div key={t.id} className={`toast toast-${t.kind}`}>
            {t.kind === 'ok' && <CircleCheck size={18} className="text-ok" />}
            {t.kind === 'error' && <CircleAlert size={18} className="text-danger" />}
            {t.kind === 'warn' && <TriangleAlert size={18} className="text-warn" />}
            {t.kind === 'info' && <Info size={18} />}
            <div className="toast-text">{t.text}</div>
            <button type="button" className="icon-btn" aria-label="Zamknij" onClick={() => remove(t.id)}>
              <X size={16} />
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastApi {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error('useToast poza ToastProvider');
  return ctx;
}
