import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from 'react';
import { Modal } from './Modal';

export interface ConfirmOptions {
  title: string;
  message?: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
  /** Wymaga przepisania tego tekstu przed potwierdzeniem (np. „RESET”). */
  requireText?: string;
}

type ConfirmFn = (opts: ConfirmOptions) => Promise<boolean>;

const ConfirmContext = createContext<ConfirmFn | null>(null);

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [opts, setOpts] = useState<ConfirmOptions | null>(null);
  const [typed, setTyped] = useState('');
  const resolver = useRef<((v: boolean) => void) | null>(null);

  const confirm = useCallback<ConfirmFn>((o) => {
    setOpts(o);
    setTyped('');
    return new Promise<boolean>((resolve) => {
      resolver.current = resolve;
    });
  }, []);

  const close = (value: boolean) => {
    resolver.current?.(value);
    resolver.current = null;
    setOpts(null);
  };

  const blocked = !!opts?.requireText && typed.trim() !== opts.requireText;

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {opts && (
        <Modal
          title={opts.title}
          onClose={() => close(false)}
          onSubmit={() => {
            if (!blocked) close(true);
          }}
          footer={
            <>
              <button type="button" className="btn btn-ghost" onClick={() => close(false)}>
                {opts.cancelLabel ?? 'Anuluj'}
              </button>
              <button
                type="submit"
                className={`btn ${opts.danger ? 'btn-danger-solid' : 'btn-primary'}`}
                disabled={blocked}
              >
                {opts.confirmLabel ?? 'Potwierdź'}
              </button>
            </>
          }
        >
          <div className="stack">
            {opts.message && <div className="muted measure">{opts.message}</div>}
            {opts.requireText && (
              <div className="field">
                <label htmlFor="confirm-text">
                  Wpisz <strong>{opts.requireText}</strong>, aby potwierdzić
                </label>
                <input
                  id="confirm-text"
                  className="input input-code"
                  value={typed}
                  autoComplete="off"
                  autoCapitalize="characters"
                  onChange={(e) => setTyped(e.target.value)}
                />
              </div>
            )}
          </div>
        </Modal>
      )}
    </ConfirmContext.Provider>
  );
}

export function useConfirm(): ConfirmFn {
  const ctx = useContext(ConfirmContext);
  if (!ctx) throw new Error('useConfirm poza ConfirmProvider');
  return ctx;
}
