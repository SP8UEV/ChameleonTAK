import { useMemo } from 'react';
import { Lock, LockOpen, MonitorSmartphone, Unplug } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { Client } from '../api/types';
import { useConfirm } from '../components/Confirm';
import { useToast } from '../components/Toast';
import { AsyncView, Empty, Led, PageHeader } from '../components/ui';
import { fmtRelative, fmtTime } from '../lib/format';
import { platformLabel } from '../lib/tak';
import { useApi } from '../lib/useApi';
import { useLive } from '../lib/ws';

export default function Clients() {
  const state = useApi(api.clients, [], 30000);
  const toast = useToast();
  const confirm = useConfirm();

  useLive((msg) => {
    if (msg.type !== 'client') return;
    const { event, client } = msg.data;
    state.setData((list) => {
      if (!list) return list;
      const rest = list.filter((c) => c.id !== client.id);
      return event === 'connect' ? [...rest, client] : rest;
    });
  });

  const sorted = useMemo(
    () => [...(state.data ?? [])].sort((a, b) => (a.callsign || a.uid).localeCompare(b.callsign || b.uid, 'pl', { numeric: true })),
    [state.data],
  );

  const disconnect = async (c: Client) => {
    const ok = await confirm({
      title: `Rozłączyć ${c.callsign || c.uid}?`,
      message: 'Aplikacja zwykle połączy się ponownie po kilku sekundach. Aby trwale odciąć urządzenie, usuń je albo zablokuj użytkownika.',
      confirmLabel: 'Rozłącz',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.disconnectClient(c.id);
      state.setData((list) => list?.filter((x) => x.id !== c.id));
      toast.ok(`Rozłączono ${c.callsign || c.uid}.`);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <>
      <PageHeader
        title="Połączone urządzenia"
        description="Aplikacje TAK połączone z serwerem w tej chwili. Lista odświeża się na żywo."
      />
      <AsyncView state={state}>
        {() =>
          sorted.length === 0 ? (
            <Empty icon={MonitorSmartphone} title="Nikt nie jest połączony">
              Gdy ktoś uruchomi ATAK, iTAK lub WinTAK skonfigurowany dla tego serwera, pojawi się tutaj.
            </Empty>
          ) : (
            <div className="list">
              {sorted.map((c) => (
                <div key={c.id} className="list-row">
                  <Led state="on" />
                  <div className="list-row-main">
                    <span className="list-row-title">
                      {c.callsign || c.uid}
                      {c.username && (
                        <span className="muted" style={{ fontWeight: 400 }}>
                          {' '}
                          — {c.username}
                        </span>
                      )}
                    </span>
                    <span className="list-row-meta">
                      <span className="row nowrap" style={{ gap: 4 }}>
                        {c.transport === 'tls' ? <Lock size={13} /> : <LockOpen size={13} className="text-warn" />}
                        {c.transport === 'tls' ? 'TLS' : 'TCP bez szyfrowania'}
                      </span>
                      <span className="code">{c.ip}</span>
                      <span>
                        {c.platform ? platformLabel(c.platform.toLowerCase()) : 'nieznana aplikacja'}
                        {c.version && ` ${c.version}`}
                      </span>
                      <span title={fmtTime(c.connectedAt, true)}>połączono {fmtRelative(c.connectedAt)}</span>
                      {c.groups.length > 0 && <span>grupy: {c.groups.join(', ')}</span>}
                    </span>
                  </div>
                  <div className="list-row-side">
                    <button type="button" className="btn btn-sm btn-danger" onClick={() => disconnect(c)}>
                      <Unplug size={16} /> <span className="hide-sm">Rozłącz</span>
                    </button>
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

