import { useState, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import {
  ArrowLeft,
  Check,
  ChevronDown,
  CircleAlert,
  Copy,
  Info,
  LoaderCircle,
  RefreshCw,
  TriangleAlert,
  type LucideIcon,
} from 'lucide-react';
import { copyText } from '../lib/clipboard';
import { platformLabel, teamColor, teamLabel } from '../lib/tak';
import { useToast } from './Toast';

/* ---------- Znak firmowy ---------- */
export function BrandMark({ className = 'brand-mark' }: { className?: string }) {
  // Kameleon z logo; maska CSS, więc kolor bierze się z motywu (currentColor).
  return <span className={`logo-mask ${className}`} aria-hidden="true" />;
}

/* ---------- Nagłówek strony ---------- */
export function PageHeader({
  title,
  description,
  actions,
  back,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  back?: { to: string; label: string };
}) {
  return (
    <header className="page-head">
      <div className="page-head-text">
        {back && (
          <Link to={back.to} className="back-link">
            <ArrowLeft size={16} /> {back.label}
          </Link>
        )}
        <h1>{title}</h1>
        {description && <p>{description}</p>}
      </div>
      {actions && <div className="actions">{actions}</div>}
    </header>
  );
}

/* ---------- Stany: ładowanie, błąd, pusto ---------- */
export function Loading({ label = 'Wczytywanie…', compact }: { label?: string; compact?: boolean }) {
  return (
    <div className={`state${compact ? ' compact' : ''}`} role="status">
      <LoaderCircle size={28} className="spinner" />
      <span>{label}</span>
    </div>
  );
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="state" role="alert">
      <div className="state-icon">
        <CircleAlert size={26} className="text-danger" />
      </div>
      <h3>Nie udało się wczytać danych</h3>
      <p>{message}</p>
      {onRetry && (
        <button type="button" className="btn" onClick={onRetry}>
          <RefreshCw size={16} /> Spróbuj ponownie
        </button>
      )}
    </div>
  );
}

export function Empty({
  icon: Icon,
  title,
  children,
  action,
  compact,
}: {
  icon: LucideIcon;
  title: string;
  children?: ReactNode;
  action?: ReactNode;
  compact?: boolean;
}) {
  return (
    <div className={`state${compact ? ' compact' : ''}`}>
      <div className="state-icon">
        <Icon size={26} />
      </div>
      <h3>{title}</h3>
      {children && <p>{children}</p>}
      {action}
    </div>
  );
}

/** Pomocnik: renderuje loading/error/dane. */
export function AsyncView<T>({
  state,
  children,
  loadingLabel,
}: {
  state: { data: T | undefined; error: string | null; loading: boolean; reload: () => void };
  children: (data: T) => ReactNode;
  loadingLabel?: string;
}) {
  if (state.data === undefined) {
    if (state.error) return <ErrorState message={state.error} onRetry={state.reload} />;
    return <Loading label={loadingLabel} />;
  }
  return <>{children(state.data)}</>;
}

/* ---------- Komunikat ---------- */
export function Notice({
  kind = 'info',
  children,
}: {
  kind?: 'info' | 'warn' | 'danger' | 'ok';
  children: ReactNode;
}) {
  const Icon = kind === 'warn' || kind === 'danger' ? TriangleAlert : kind === 'ok' ? Check : Info;
  return (
    <div className={`notice${kind !== 'info' ? ` notice-${kind}` : ''}`}>
      <Icon size={18} />
      <div>{children}</div>
    </div>
  );
}

/* ---------- Pola ---------- */
export function Field({
  label,
  htmlFor,
  hint,
  error,
  children,
}: {
  label: ReactNode;
  htmlFor?: string;
  hint?: ReactNode;
  error?: string | null;
  children: ReactNode;
}) {
  return (
    <div className="field">
      <label htmlFor={htmlFor}>{label}</label>
      {children}
      {error ? <div className="field-error">{error}</div> : hint ? <div className="field-hint">{hint}</div> : null}
    </div>
  );
}

export function Switch({
  checked,
  onChange,
  label,
  disabled,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
  disabled?: boolean;
}) {
  return (
    <span className="switch">
      <input
        type="checkbox"
        role="switch"
        aria-label={label}
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span className="switch-track" />
      <span className="switch-thumb" />
    </span>
  );
}

export function CheckRow({
  checked,
  onChange,
  label,
  hint,
  disabled,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: ReactNode;
  hint?: ReactNode;
  disabled?: boolean;
}) {
  return (
    <label className="check">
      <input type="checkbox" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      <span className="check-text">
        <span className="strong">{label}</span>
        {hint && <span className="field-hint">{hint}</span>}
      </span>
    </label>
  );
}

/** Karta wyboru (checkbox albo radio). */
export function Choice({
  type = 'checkbox',
  name,
  checked,
  onChange,
  title,
  description,
  icon,
  disabled,
}: {
  type?: 'checkbox' | 'radio';
  name?: string;
  checked: boolean;
  onChange: (v: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  icon?: ReactNode;
  disabled?: boolean;
}) {
  return (
    <label className={`choice${type === 'radio' ? ' radio' : ''}${checked ? ' checked' : ''}${disabled ? ' disabled' : ''}`}>
      <input
        type={type}
        name={name}
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(type === 'radio' ? true : e.target.checked)}
      />
      <span className="choice-mark">{type === 'checkbox' ? <Check size={14} strokeWidth={3} /> : null}</span>
      <span className="stack-sm grow" style={{ gap: 2 }}>
        <span className="choice-titlerow">
          {icon}
          <span className="choice-title">{title}</span>
        </span>
        {description && <span className="choice-desc">{description}</span>}
      </span>
    </label>
  );
}

/** Wielokrotny wybór grup jako „chipy”. */
export function GroupPicker({
  all,
  value,
  onChange,
  emptyHint = 'Brak grup. Bez grupy użytkownicy widzą się wszyscy nawzajem.',
}: {
  all: string[];
  value: string[];
  onChange: (v: string[]) => void;
  emptyHint?: string;
}) {
  if (all.length === 0) return <div className="field-hint">{emptyHint}</div>;
  return (
    <div className="chips" role="group">
      {all.map((g) => {
        const on = value.includes(g);
        return (
          <button
            key={g}
            type="button"
            className={`chip${on ? ' selected' : ''}`}
            aria-pressed={on}
            onClick={() => onChange(on ? value.filter((x) => x !== g) : [...value, g])}
          >
            {on && <Check size={14} />}
            {g}
          </button>
        );
      })}
    </div>
  );
}

/* ---------- Drobne znaczniki ---------- */
export function Led({ state, label, large }: { state: 'on' | 'off' | 'warn' | 'err'; label?: string; large?: boolean }) {
  const cls = state === 'on' ? 'led-on' : state === 'warn' ? 'led-warn' : state === 'err' ? 'led-err' : '';
  return (
    <span
      className={`led ${cls}${large ? ' led-lg' : ''}`}
      role={label ? 'img' : undefined}
      aria-label={label}
      title={label}
      aria-hidden={label ? undefined : true}
    />
  );
}

export function TeamTag({ team }: { team: string }) {
  if (!team) return <span className="faint">—</span>;
  return (
    <span className="row nowrap" style={{ gap: 6 }}>
      <span className="swatch" style={{ background: teamColor(team) }} />
      {teamLabel(team)}
    </span>
  );
}

export function PlatformTag({ platform }: { platform: string }) {
  return <span className="badge">{platformLabel(platform)}</span>;
}

export function Disclosure({
  title,
  children,
  defaultOpen,
  icon,
}: {
  title: ReactNode;
  children: ReactNode;
  defaultOpen?: boolean;
  icon?: ReactNode;
}) {
  return (
    <details className="disclosure" open={defaultOpen}>
      <summary>
        {icon}
        {title}
        <ChevronDown size={18} className="chev" />
      </summary>
      <div className="disclosure-body">{children}</div>
    </details>
  );
}

/* ---------- Kopiowanie ---------- */
export function CopyButton({ text, label = 'Kopiuj' }: { text: string; label?: string }) {
  const toast = useToast();
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      className="icon-btn"
      aria-label={label}
      title={label}
      onClick={async () => {
        const ok = await copyText(text);
        if (ok) {
          setDone(true);
          window.setTimeout(() => setDone(false), 1500);
        } else {
          toast.error('Nie udało się skopiować. Zaznacz tekst i skopiuj ręcznie.');
        }
      }}
    >
      {done ? <Check size={18} className="text-ok" /> : <Copy size={18} />}
    </button>
  );
}

export function CopyField({ label, value, big }: { label: string; value: string; big?: boolean }) {
  return (
    <div className="copy-field">
      <span className="copy-field-label">{label}</span>
      <span className={`copy-field-value${big ? ' big' : ''}`}>{value || '—'}</span>
      {value && <CopyButton text={value} label={`Kopiuj: ${label}`} />}
    </div>
  );
}
