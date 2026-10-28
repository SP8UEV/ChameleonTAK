import { CircleAlert, CircleCheck, Info, LoaderCircle, RefreshCw, TriangleAlert } from 'lucide-react';
import { api } from '../api/endpoints';
import type { PortCheck, PortResult } from '../api/types';
import { fmtRelative } from '../lib/format';
import { useApi } from '../lib/useApi';
import { Notice } from './ui';

const LEVEL_ICON = {
  ok: <CircleCheck size={18} />,
  info: <Info size={18} />,
  warn: <TriangleAlert size={18} />,
  error: <CircleAlert size={18} />,
};

/** Samotest portów: co działa, co przepuszcza zapora, co widać z internetu — i co z tym zrobić. */
export function PortTest() {
  const state = useApi(() => api.portCheck(), []);
  const r = state.data;
  const rerun = async () => {
    state.setData(undefined);
    try {
      state.setData(await api.portCheck(true));
    } catch {
      void state.reload();
    }
  };
  const busy = state.loading || (!r && !state.error);

  return (
    <div className="stack">
      <div className="row" style={{ justifyContent: 'space-between', flexWrap: 'wrap', gap: 8 }}>
        <span className="small muted">
          {r ? (
            <>
              Publiczny adres: <span className="code">{r.publicIp || '—'}</span>
              {r.lanIp && (
                <>
                  {' '}
                  · adres w sieci lokalnej: <span className="code">{r.lanIp}</span>
                </>
              )}{' '}
              · sprawdzono {fmtRelative(r.checked)}
            </>
          ) : busy ? (
            'Sprawdzam porty z internetu — to trwa do 30 sekund…'
          ) : null}
        </span>
        <button type="button" className="btn btn-sm" onClick={rerun} disabled={busy}>
          {busy ? <LoaderCircle size={16} className="spinner" /> : <RefreshCw size={16} />} Sprawdź ponownie
        </button>
      </div>
      {state.error && <Notice kind="danger">{state.error}</Notice>}
      {r && <PortReport r={r} />}
    </div>
  );
}

function PortReport({ r }: { r: PortCheck }) {
  const problems = r.ports.filter((p) => p.level === 'error' || p.level === 'warn').length;
  return (
    <>
      {r.externalError && <Notice kind="warn">{r.externalError}</Notice>}
      {r.dns?.matches && (
        <Notice kind="ok">
          Domena <span className="code">{r.dns.host}</span> wskazuje na publiczny adres routera{' '}
          <span className="code">{r.publicIp}</span>.
        </Notice>
      )}
      {r.hints?.map((h) => (
        <Notice key={h} kind="danger">
          {h}
        </Notice>
      ))}
      {!r.externalError && problems === 0 && (
        <Notice kind="ok">
          {r.mode === 'local'
            ? 'Serwer działa w sieci lokalnej i żaden zbędny port nie jest otwarty z internetu.'
            : 'Wszystkie potrzebne porty są osiągalne z internetu, a zbędne są zamknięte.'}
        </Notice>
      )}
      <ul className="port-list">
        {r.ports.map((p) => (
          <PortRow key={p.port} p={p} />
        ))}
      </ul>
      <p className="xsmall faint">
        Dostępność z internetu sprawdza zewnętrzny serwis portchecker.io, łącząc się z publicznym adresem routera.
        {r.firewall === 'disabled' && ' Zapora ChameleonTAK jest wyłączona — ruch filtruje Twoja własna zapora.'}
      </p>
    </>
  );
}

function PortRow({ p }: { p: PortResult }) {
  const portLabel = p.external !== p.port ? `${p.external} → ${p.port}` : String(p.port);
  return (
    <li className={`port-row port-${p.level}`}>
      <span className="port-icon">{LEVEL_ICON[p.level]}</span>
      <div className="port-main">
        <div className="port-head">
          <span className="code strong">{portLabel}</span>
          <span className="strong">{p.name}</span>
        </div>
        <div className="port-badges">
          <span className={`badge ${p.listening ? 'badge-ok' : 'badge-danger'}`}>{p.listening ? 'działa' : 'nie działa'}</span>
          {p.firewall !== 'unknown' && (
            <span className={`badge ${p.firewall === 'open' ? 'badge-ok' : ''}`}>
              zapora: {p.firewall === 'open' ? 'przepuszcza' : 'blokuje'}
            </span>
          )}
          {p.internet !== 'unknown' && (
            <span className={`badge ${p.internet === 'open' ? (p.want === 'closed' ? 'badge-warn' : 'badge-ok') : p.want === 'open' ? 'badge-danger' : ''}`}>
              z internetu: {p.internet === 'open' ? 'otwarty' : 'zamknięty'}
            </span>
          )}
          <span className="badge badge-info">
            {p.want === 'open' ? 'powinien być otwarty' : p.want === 'closed' ? 'powinien być zamknięty' : 'opcjonalny'}
          </span>
        </div>
        <div className="small">{p.suggestion}</div>
      </div>
    </li>
  );
}
