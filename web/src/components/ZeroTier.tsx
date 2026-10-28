import { useState } from 'react';
import { Download, LoaderCircle, LogIn, LogOut, RefreshCw } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { ZeroTierStatus } from '../api/types';
import { useApi } from '../lib/useApi';
import { useConfirm } from './Confirm';
import { CopyField, Field, Notice } from './ui';

const STATUS_TEXT: Record<string, string> = {
  OK: 'połączona',
  REQUESTING_CONFIGURATION: 'czeka na autoryzację',
  ACCESS_DENIED: 'brak autoryzacji',
  NOT_FOUND: 'sieć nie istnieje',
  PORT_ERROR: 'błąd interfejsu',
  CLIENT_TOO_OLD: 'za stara wersja ZeroTier',
};

/**
 * ZeroTier w ustawieniach dostępu: instalacja, dołączenie do sieci i wybór adresu serwera.
 * onAddress dostaje adres ZeroTier, gdy sieć jest połączona.
 */
export function ZeroTierPanel({ main, address, onAddress }: { main: boolean; address: string; onAddress: (ip: string) => void }) {
  const state = useApi(api.zerotier, [], 10000);
  const st = state.data;
  if (!main) {
    return <AddressOnly address={address} onAddress={onAddress} />;
  }
  if (state.error && !st) return <Notice kind="danger">{state.error}</Notice>;
  if (!st) {
    return (
      <span className="small muted">
        <LoaderCircle size={14} className="spinner" /> Sprawdzam ZeroTier…
      </span>
    );
  }
  return <ZeroTierManager st={st} setSt={state.setData} address={address} onAddress={onAddress} />;
}

function ZeroTierManager({
  st,
  setSt,
  address,
  onAddress,
}: {
  st: ZeroTierStatus;
  setSt: (s: ZeroTierStatus) => void;
  address: string;
  onAddress: (ip: string) => void;
}) {
  const confirm = useConfirm();
  const [busy, setBusy] = useState<'' | 'install' | 'join' | 'leave'>('');
  const [error, setError] = useState<string | null>(null);
  const [netId, setNetId] = useState(st.network);
  const net = st.networks.find((n) => n.id === st.network);
  const ip = net?.status === 'OK' ? net.addrs[0] : undefined;

  const act = async (kind: 'install' | 'join' | 'leave', fn: () => Promise<ZeroTierStatus>) => {
    setBusy(kind);
    setError(null);
    try {
      setSt(await fn());
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy('');
    }
  };

  if (!st.supported) {
    return <Notice>ZeroTier instaluje się na Raspberry Pi z ChameleonTAK. Na tym komputerze ta opcja jest niedostępna.</Notice>;
  }

  return (
    <div className="stack">
      <Notice>
        <div className="stack-sm">
          <span>
            ZeroTier łączy serwer i telefony w prywatną sieć przez internet — bez publicznego IP, domeny i przekierowania portów.
            Działa też przez sieć komórkową i CGNAT.
          </span>
          <ol className="small" style={{ margin: 0, paddingLeft: 18 }}>
            <li>
              Załóż darmowe konto i sieć na{' '}
              <a href="https://my.zerotier.com" target="_blank" rel="noreferrer">
                my.zerotier.com
              </a>{' '}
              i skopiuj jej identyfikator (Network ID).
            </li>
            <li>Dołącz tu Raspberry Pi, a potem zaznacz je w sekcji „Members” → „Auth”.</li>
            <li>Na telefonach zainstaluj aplikację ZeroTier One, dołącz do tej samej sieci i też je autoryzuj.</li>
          </ol>
        </div>
      </Notice>
      {error && <Notice kind="danger">{error}</Notice>}

      {!st.installed ? (
        <button
          type="button"
          className="btn"
          style={{ alignSelf: 'flex-start' }}
          onClick={() => act('install', api.zerotierInstall)}
          disabled={busy !== ''}
        >
          {busy === 'install' ? <LoaderCircle size={18} className="spinner" /> : <Download size={18} />}
          {busy === 'install' ? 'Instaluję ZeroTier…' : 'Zainstaluj ZeroTier'}
        </button>
      ) : (
        <>
          {st.nodeId && <CopyField label="Node ID tego urządzenia" value={st.nodeId} />}
          <div className="row" style={{ gap: 8, alignItems: 'flex-end', flexWrap: 'wrap' }}>
            <Field label="Sieć ZeroTier (Network ID)" htmlFor="zt-net">
              <input
                id="zt-net"
                className="input input-code"
                value={netId}
                maxLength={16}
                placeholder="8056c2e21c000001"
                autoCapitalize="none"
                autoCorrect="off"
                onChange={(e) => setNetId(e.target.value.trim().toLowerCase())}
              />
            </Field>
            <button
              type="button"
              className="btn"
              onClick={() => act('join', () => api.zerotierJoin(netId))}
              disabled={busy !== '' || !/^[0-9a-f]{16}$/.test(netId) || (netId === st.network && !!net)}
            >
              {busy === 'join' ? <LoaderCircle size={18} className="spinner" /> : <LogIn size={18} />} Dołącz
            </button>
            {st.network && (
              <button
                type="button"
                className="btn btn-ghost"
                disabled={busy !== ''}
                onClick={async () => {
                  const ok = await confirm({
                    title: 'Opuścić sieć ZeroTier?',
                    message: 'Telefony połączone przez ZeroTier stracą dostęp do serwera.',
                    confirmLabel: 'Opuść sieć',
                    danger: true,
                  });
                  if (ok) await act('leave', api.zerotierLeave);
                }}
              >
                <LogOut size={18} /> Opuść
              </button>
            )}
          </div>

          {st.network && (
            <Notice kind={ip ? 'ok' : net?.status === 'ACCESS_DENIED' || net?.status === 'NOT_FOUND' ? 'danger' : 'warn'}>
              <div className="stack-sm">
                <span>
                  Sieć <span className="code">{st.network}</span>
                  {net?.name ? ` („${net.name}”)` : ''}: <strong>{net ? (STATUS_TEXT[net.status] ?? net.status) : 'łączenie…'}</strong>
                  {ip && (
                    <>
                      {' '}
                      · adres serwera <span className="code">{ip}</span>
                    </>
                  )}
                </span>
                {!ip && (
                  <span className="small">
                    Zaloguj się na my.zerotier.com, otwórz sieć i w „Members” zaznacz „Auth” przy urządzeniu{' '}
                    <span className="code">{st.nodeId}</span>. Stan odświeża się sam.
                  </span>
                )}
                {ip && ip !== address && (
                  <button type="button" className="btn btn-sm btn-primary" style={{ alignSelf: 'flex-start' }} onClick={() => onAddress(ip)}>
                    Użyj adresu {ip} dla serwera
                  </button>
                )}
                {ip && ip === address && <span className="small">Serwer używa tego adresu — zapisz ustawienia.</span>}
              </div>
            </Notice>
          )}
          {!st.online && st.running && (
            <span className="small muted">
              <RefreshCw size={14} /> ZeroTier łączy się z internetem…
            </span>
          )}
          {st.error && <span className="small text-danger">{st.error}</span>}
        </>
      )}
    </div>
  );
}

/** Dodatkowy serwer: siecią ZeroTier zarządza serwer główny, tu wybiera się tylko adres. */
function AddressOnly({ address, onAddress }: { address: string; onAddress: (ip: string) => void }) {
  const settings = useApi(api.settings, []);
  const ips = settings.data?.zerotier.ips ?? [];
  if (ips.length === 0) {
    return <Notice>To urządzenie nie jest jeszcze w sieci ZeroTier. Dołącz do niej w ustawieniach serwera głównego.</Notice>;
  }
  return (
    <div className="chips" role="group" aria-label="Adres ZeroTier">
      {ips.map((ip) => (
        <button key={ip} type="button" className={`chip${ip === address ? ' selected' : ''}`} aria-pressed={ip === address} onClick={() => onAddress(ip)}>
          {ip}
        </button>
      ))}
    </div>
  );
}
