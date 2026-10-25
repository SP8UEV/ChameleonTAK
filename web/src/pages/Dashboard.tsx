import { Link, useNavigate } from 'react-router-dom';
import { Clock, Cpu, HardDrive, MemoryStick, Network, Thermometer, UserPlus } from 'lucide-react';
import { api } from '../api/endpoints';
import type { Dashboard as DashboardData, Listener } from '../api/types';
import { AsyncView, Led, PageHeader } from '../components/ui';
import { fmtBytes, fmtDateTime, fmtNumber, fmtUptime, percent, plural } from '../lib/format';
import { useApi } from '../lib/useApi';

function listenerLed(l: Listener) {
  return l.state === 'running' ? 'on' : l.state === 'error' ? 'err' : 'off';
}

function listenerText(l: Listener) {
  if (l.state === 'running') return 'działa';
  if (l.state === 'error') return 'błąd';
  return 'zatrzymany';
}

function Meter({ value }: { value: number }) {
  const cls = value >= 90 ? ' danger' : value >= 75 ? ' warn' : '';
  return (
    <div className={`meter${cls}`} role="progressbar" aria-valuenow={Math.round(value)} aria-valuemin={0} aria-valuemax={100}>
      <span style={{ width: `${value}%` }} />
    </div>
  );
}

export default function Dashboard() {
  const state = useApi(api.dashboard, [], 10000);
  const navigate = useNavigate();

  return (
    <>
      <PageHeader
        title="Pulpit"
        actions={
          <button type="button" className="btn btn-primary" onClick={() => navigate('/users?add=1')}>
            <UserPlus size={18} /> Dodaj użytkownika
          </button>
        }
      />
      <AsyncView state={state}>{(d) => <DashboardBody d={d} />}</AsyncView>
    </>
  );
}

function DashboardBody({ d }: { d: DashboardData }) {
  const sys = d.system;
  const mem = percent(sys.memUsed, sys.memTotal);
  const disk = percent(sys.diskUsed, sys.diskTotal);
  const listenersOk = d.listeners.every((l) => l.state === 'running');
  const temp = sys.cpuTempC;
  const tempClass = temp >= 80 ? 'text-danger' : temp >= 70 ? 'text-warn' : '';

  return (
    <div className="stack-lg">
      <section className="dash-hero">
        <Link to="/clients" className="plate dash-online">
          <div className="row" style={{ gap: 10 }}>
            <Led state={d.online > 0 ? 'on' : 'off'} large />
            <span className="muted">Teraz w sieci</span>
          </div>
          <div className="dash-online-value">{d.online}</div>
          <div className="muted small">{plural(d.online, 'urządzenie połączone', 'urządzenia połączone', 'urządzeń połączonych')}</div>
        </Link>
        <div className="dash-counts">
          <CountTile to="/users" value={d.users} label={plural(d.users, 'użytkownik', 'użytkownicy', 'użytkowników')} />
          <CountTile to="/users" value={d.devices} label={plural(d.devices, 'urządzenie', 'urządzenia', 'urządzeń')} />
          <CountTile to="/groups" value={d.groups} label={plural(d.groups, 'grupa', 'grupy', 'grup')} />
          <CountTile to="/files" value={d.files} label={plural(d.files, 'plik', 'pliki', 'plików')} />
          <CountTile to="/missions" value={d.missions} label={plural(d.missions, 'misja', 'misje', 'misji')} />
          <div className="count-tile count-tile-static">
            <span className="count-value">{fmtUptime(d.uptimeSec)}</span>
            <span className="count-label">bez przerwy w działaniu</span>
          </div>
        </div>
      </section>

      <div className="dash-grid">
        <section className="card">
          <div className="card-head">
            <h2>Usługi serwera</h2>
            <span className={`badge ${listenersOk ? 'badge-ok' : 'badge-danger'}`}>
              {listenersOk ? 'wszystkie działają' : 'wymaga uwagi'}
            </span>
          </div>
          <div className="stack-sm">
            {d.listeners.map((l) => (
              <div key={`${l.name}-${l.port}`} className="listener-row">
                <Led state={listenerLed(l)} />
                <div className="grow">
                  <div className="strong">{l.name}</div>
                  {l.error && <div className="small text-danger">{l.error}</div>}
                </div>
                <span className="code small muted">:{l.port}</span>
                <span className={`small ${l.state === 'error' ? 'text-danger' : 'muted'}`}>{listenerText(l)}</span>
              </div>
            ))}
            {d.listeners.length === 0 && <p className="muted small">Brak uruchomionych usług.</p>}
          </div>
        </section>

        <section className="card">
          <div className="card-head">
            <h2>{sys.isPi ? sys.model || 'Raspberry Pi' : 'System'}</h2>
            <span className="muted small">{sys.hostname}</span>
          </div>
          <div className="sys-tiles">
            <div className="sys-tile">
              <Thermometer size={18} />
              <div className="grow">
                <div className="xsmall muted">Temperatura CPU</div>
                <div className={`sys-value ${tempClass}`}>{temp > 0 ? `${fmtNumber(temp)} °C` : '—'}</div>
              </div>
            </div>
            <div className="sys-tile">
              <Cpu size={18} />
              <div className="grow">
                <div className="xsmall muted">Obciążenie</div>
                <div className="sys-value">{fmtNumber(sys.load1, 2)}</div>
              </div>
            </div>
            <div className="sys-tile sys-tile-wide">
              <MemoryStick size={18} />
              <div className="grow stack-sm" style={{ gap: 4 }}>
                <div className="row-between xsmall muted">
                  <span>Pamięć</span>
                  <span>
                    {fmtBytes(sys.memUsed)} z {fmtBytes(sys.memTotal)}
                  </span>
                </div>
                <Meter value={mem} />
              </div>
            </div>
            <div className="sys-tile sys-tile-wide">
              <HardDrive size={18} />
              <div className="grow stack-sm" style={{ gap: 4 }}>
                <div className="row-between xsmall muted">
                  <span>Karta / dysk</span>
                  <span>
                    {fmtBytes(sys.diskUsed)} z {fmtBytes(sys.diskTotal)}
                  </span>
                </div>
                <Meter value={disk} />
              </div>
            </div>
            <div className="sys-tile sys-tile-wide">
              <Network size={18} />
              <div className="grow">
                <div className="xsmall muted">
                  Adresy {sys.network.mode === 'hotspot' ? `(hotspot „${sys.network.ssid}”)` : ''}
                </div>
                <div className="chips" style={{ marginTop: 4 }}>
                  {sys.addresses.map((a) => (
                    <span key={`${a.iface}-${a.ip}`} className="chip chip-sm">
                      <span className="code">{a.ip}</span>
                      <span className="faint">{a.iface}</span>
                    </span>
                  ))}
                  {sys.addresses.length === 0 && <span className="small text-warn">brak adresu sieciowego</span>}
                </div>
              </div>
            </div>
            <div className="sys-tile sys-tile-wide">
              <Clock size={18} />
              <div className="grow">
                <div className="xsmall muted">Czas serwera</div>
                <div className="row" style={{ gap: 10 }}>
                  <span className="sys-value">{fmtDateTime(sys.time)}</span>
                  <span className={`badge ${sys.ntpSynced ? 'badge-ok' : 'badge-warn'}`}>
                    {sys.ntpSynced ? 'NTP zsynchronizowany' : 'bez NTP'}
                  </span>
                </div>
                {!sys.ntpSynced && (
                  <div className="xsmall faint" style={{ marginTop: 4 }}>
                    Bez internetu zegar może się spóźniać, a certyfikaty wymagają poprawnej daty.
                  </div>
                )}
              </div>
            </div>
          </div>
        </section>
      </div>
      <p className="xsmall faint">Dane odświeżają się co 10 sekund.</p>
    </div>
  );
}

function CountTile({ to, value, label }: { to: string; value: number; label: string }) {
  return (
    <Link to={to} className="count-tile">
      <span className="count-value">{value}</span>
      <span className="count-label">{label}</span>
    </Link>
  );
}
