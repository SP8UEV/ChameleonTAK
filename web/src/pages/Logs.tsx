import { useMemo, useState } from 'react';
import { FileText } from 'lucide-react';
import { api } from '../api/endpoints';
import type { LogEntry } from '../api/types';
import { AsyncView, Empty, PageHeader } from '../components/ui';
import { fmtTime } from '../lib/format';
import { useApi } from '../lib/useApi';

type Filter = 'all' | 'problems';

const LEVEL_LABEL: Record<string, string> = { INFO: 'info', WARN: 'uwaga', ERROR: 'błąd' };

function levelClass(level: string): string {
  if (level === 'ERROR') return 'text-danger';
  if (level === 'WARN') return 'text-warn';
  return 'muted';
}

function isProblem(e: LogEntry): boolean {
  return e.level !== 'INFO' || /odmow|odrzuc|nieudan|błąd/i.test(e.message);
}

export default function Logs() {
  const state = useApi(api.logs, [], 5000);
  const [filter, setFilter] = useState<Filter>('all');

  const entries = useMemo(
    () => (state.data ?? []).filter((e) => filter === 'all' || isProblem(e)),
    [state.data, filter],
  );

  return (
    <>
      <PageHeader
        title="Dziennik"
        description="Ostatnie zdarzenia serwera: połączenia, nieudane próby TLS, rejestracja certyfikatów, pobrane paczki. Odświeża się co 5 sekund."
        actions={
          <div className="row" style={{ gap: 8 }}>
            <button type="button" className={`btn ${filter === 'all' ? 'btn-primary' : 'btn-ghost'}`} onClick={() => setFilter('all')}>
              Wszystko
            </button>
            <button type="button" className={`btn ${filter === 'problems' ? 'btn-primary' : 'btn-ghost'}`} onClick={() => setFilter('problems')}>
              Tylko problemy
            </button>
          </div>
        }
      />
      <AsyncView state={state}>
        {() =>
          entries.length === 0 ? (
            <Empty icon={FileText} title="Brak zdarzeń">
              Spróbuj połączyć urządzenie albo zaimportować paczkę — zdarzenia pojawią się tutaj.
            </Empty>
          ) : (
            <div className="list">
              {entries.map((e, i) => (
                <div key={`${e.time}-${i}`} className="list-row" style={{ alignItems: 'flex-start' }}>
                  <div className="list-row-main">
                    <span className="list-row-title">
                      <span className={levelClass(e.level)} style={{ fontWeight: 600, marginRight: 8 }}>
                        {LEVEL_LABEL[e.level] ?? e.level.toLowerCase()}
                      </span>
                      {e.message}
                    </span>
                    <span className="list-row-meta" style={{ wordBreak: 'break-word' }}>
                      {fmtTime(e.time, true)}
                      {Object.entries(e.attrs).map(([k, v]) => (
                        <span key={k}>
                          {' · '}
                          <span className="muted">{k}:</span> {v}
                        </span>
                      ))}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          )
        }
      </AsyncView>
    </>
  );
}
