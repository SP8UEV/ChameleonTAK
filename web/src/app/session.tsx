import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { Outlet } from 'react-router-dom';
import { api } from '../api/endpoints';
import { ApiError, errorMessage } from '../api/client';
import type { Me, Settings, Status } from '../api/types';
import { ErrorState, Loading } from '../components/ui';

/* ---------- Status serwera (publiczny) ---------- */
interface StatusCtx {
  status: Status;
  refresh: () => Promise<void>;
}
const StatusContext = createContext<StatusCtx | null>(null);

export function StatusProvider({ status, refresh, children }: StatusCtx & { children: ReactNode }) {
  return <StatusContext.Provider value={{ status, refresh }}>{children}</StatusContext.Provider>;
}

export function useStatus(): StatusCtx {
  const ctx = useContext(StatusContext);
  if (!ctx) throw new Error('useStatus poza StatusProvider');
  return ctx;
}

/* ---------- Zalogowany administrator + ustawienia ---------- */
interface SessionCtx {
  me: Me;
  settings: Settings | null;
  reloadSettings: () => Promise<void>;
  setSettings: (s: Settings) => void;
}
const SessionContext = createContext<SessionCtx | null>(null);

export function useSession(): SessionCtx {
  const ctx = useContext(SessionContext);
  if (!ctx) throw new Error('useSession poza RequireAuth');
  return ctx;
}

/**
 * Wymaga zalogowania. Przy 401 globalny handler (App) przekierowuje do /login.
 * Wczytuje też ustawienia (platformy, domyślny zespół itp.), potrzebne w wielu ekranach.
 */
export function RequireAuth() {
  const [me, setMe] = useState<Me | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let alive = true;
    setError(null);
    api
      .me()
      .then((m) => {
        if (alive) setMe(m);
      })
      .catch((e) => {
        if (!alive) return;
        // 401 obsługuje globalny handler (przekierowanie do logowania).
        if (!(e instanceof ApiError && e.status === 401)) setError(errorMessage(e));
      });
    return () => {
      alive = false;
    };
  }, [attempt]);

  const reloadSettings = useCallback(async () => {
    try {
      setSettings(await api.settings());
    } catch {
      /* ekrany pokażą wartości domyślne */
    }
  }, []);

  useEffect(() => {
    if (me) void reloadSettings();
  }, [me, reloadSettings]);

  if (error) {
    return (
      <div className="page page-narrow">
        <ErrorState message={error} onRetry={() => setAttempt((a) => a + 1)} />
      </div>
    );
  }
  if (!me) return <Loading label="Sprawdzanie sesji…" />;

  return (
    <SessionContext.Provider value={{ me, settings, reloadSettings, setSettings }}>
      <Outlet />
    </SessionContext.Provider>
  );
}
