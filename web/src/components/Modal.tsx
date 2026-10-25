import { useEffect, useId, useRef, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { X } from 'lucide-react';

interface ModalProps {
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  wide?: boolean;
  /** Blokuje zamknięcie (np. w trakcie zapisu). */
  busy?: boolean;
  /** Jeśli podane, okno jest formularzem (Enter zatwierdza, przycisk type="submit" w stopce). */
  onSubmit?: () => void;
}

export function Modal({ title, onClose, children, footer, wide, busy, onSubmit }: ModalProps) {
  const titleId = useId();
  const ref = useRef<HTMLElement | null>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  const busyRef = useRef(busy);
  busyRef.current = busy;

  useEffect(() => {
    const prevFocus = document.activeElement as HTMLElement | null;
    const node = ref.current;
    const first = node?.querySelector<HTMLElement>('input:not([type=hidden]):not([type=checkbox]), select, textarea');
    (first ?? node)?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !busyRef.current) closeRef.current();
    };
    document.addEventListener('keydown', onKey);
    const overflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', onKey);
      document.body.style.overflow = overflow;
      prevFocus?.focus?.();
    };
  }, []);

  const className = `modal${wide ? ' modal-wide' : ''}`;
  const inner = (
    <>
      <div className="modal-head">
        <h2 id={titleId}>{title}</h2>
        <button type="button" className="icon-btn" aria-label="Zamknij" onClick={onClose} disabled={busy}>
          <X size={20} />
        </button>
      </div>
      <div className="modal-body">{children}</div>
      {footer && <div className="modal-foot">{footer}</div>}
    </>
  );

  return createPortal(
    <div
      className="modal-backdrop"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget && !busy) onClose();
      }}
    >
      {onSubmit ? (
        <form
          ref={(el) => {
            ref.current = el;
          }}
          className={className}
          role="dialog"
          aria-modal="true"
          aria-labelledby={titleId}
          tabIndex={-1}
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            if (!busy) onSubmit();
          }}
        >
          {inner}
        </form>
      ) : (
        <div
          ref={(el) => {
            ref.current = el;
          }}
          className={className}
          role="dialog"
          aria-modal="true"
          aria-labelledby={titleId}
          tabIndex={-1}
        >
          {inner}
        </div>
      )}
    </div>,
    document.body,
  );
}
