import { useEffect } from 'react';
import { Server } from 'lucide-react';
import { api } from '../api/endpoints';
import { currentServer, MAIN_SERVER, setCurrentServer } from '../api/client';
import { useApi } from '../lib/useApi';

/** Zdarzenie wysyłane po dodaniu lub usunięciu serwera. */
export const SERVERS_CHANGED = 'ctak-servers-changed';

/** Przełącza serwer, którym zarządza panel. Widoczny, gdy na urządzeniu działa więcej niż jeden serwer. */
export function ServerSwitcher() {
  const state = useApi(api.servers, []);
  const list = state.data?.servers ?? [];
  const cur = currentServer();
  const { reload } = state;

  useEffect(() => {
    const onChange = () => void reload();
    window.addEventListener(SERVERS_CHANGED, onChange);
    return () => window.removeEventListener(SERVERS_CHANGED, onChange);
  }, [reload]);

  // Wybrany serwer mógł zostać usunięty (np. w innej przeglądarce) — wróć do głównego.
  useEffect(() => {
    if (list.length > 0 && !list.some((s) => s.id === cur)) {
      setCurrentServer(MAIN_SERVER);
      window.location.reload();
    }
  }, [list, cur]);

  if (list.length < 2) return null;
  return (
    <label className="server-switch">
      <Server size={16} aria-hidden />
      <span className="sr-only">Serwer</span>
      <select
        className="select"
        value={cur}
        onChange={(e) => {
          setCurrentServer(e.target.value);
          // Każdy serwer ma własnych użytkowników, mapę i ustawienia — wczytaj panel od nowa.
          window.location.assign('/');
        }}
      >
        {list.map((s) => (
          <option key={s.id} value={s.id}>
            {s.displayName}
            {s.main ? ' (główny)' : ''}
          </option>
        ))}
      </select>
    </label>
  );
}
