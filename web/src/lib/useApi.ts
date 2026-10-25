import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react';
import { errorMessage } from '../api/client';

export interface ApiState<T> {
  data: T | undefined;
  error: string | null;
  loading: boolean;
  reload: () => Promise<void>;
  setData: Dispatch<SetStateAction<T | undefined>>;
}

/**
 * Wczytuje dane przy montowaniu (i przy zmianie `deps`).
 * `intervalMs` — opcjonalne cykliczne odświeżanie (ciche, bez migania stanu ładowania).
 */
export function useApi<T>(fn: () => Promise<T>, deps: unknown[] = [], intervalMs?: number): ApiState<T> {
  const [data, setData] = useState<T | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const fnRef = useRef(fn);
  fnRef.current = fn;
  const alive = useRef(true);

  const run = useCallback(async (silent: boolean) => {
    if (!silent) setLoading(true);
    try {
      const d = await fnRef.current();
      if (!alive.current) return;
      setData(d);
      setError(null);
    } catch (e) {
      if (!alive.current) return;
      setError(errorMessage(e));
    } finally {
      if (alive.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    alive.current = true;
    void run(false);
    let timer: number | undefined;
    if (intervalMs) {
      timer = window.setInterval(() => {
        if (document.visibilityState === 'visible') void run(true);
      }, intervalMs);
    }
    return () => {
      alive.current = false;
      if (timer) window.clearInterval(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  const reload = useCallback(() => run(true), [run]);
  return { data, error, loading, reload, setData };
}
