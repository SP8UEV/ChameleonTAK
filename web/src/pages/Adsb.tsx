import { useEffect, useMemo, useState, type ReactNode } from 'react';
import L from 'leaflet';
import { Circle, MapContainer, Marker, Popup, TileLayer, useMap, useMapEvents } from 'react-leaflet';
import 'leaflet/dist/leaflet.css';
import { Antenna, Cpu, Download, ExternalLink, Globe, LoaderCircle, Plane, Radio, Save, Send } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { AdsbConfig, AdsbInfo, AdsbPlane, AdsbStatus } from '../api/types';
import { useSession } from '../app/session';
import { useToast } from '../components/Toast';
import { AsyncView, CheckRow, Disclosure, Field, GroupPicker, Led, Notice, PageHeader } from '../components/ui';
import { fmtRelative } from '../lib/format';
import { useApi } from '../lib/useApi';

const DEFAULT_TILES = 'https://tile.openstreetmap.org/{z}/{x}/{y}.png';
const POLAND: [number, number] = [52.07, 19.48];

export default function AdsbPage() {
  const state = useApi(api.adsb, [], 5000);
  return (
    <>
      <PageHeader
        title="ADS-B"
        description="Samoloty z odbiornika 1090 MHz na Raspberry Pi: na mapie aplikacji TAK oraz w serwisach Flightradar24 i ADS-B Exchange."
      />
      <AsyncView state={state}>{(info) => <AdsbView info={info} reload={state.reload} />}</AsyncView>
    </>
  );
}

function AdsbView({ info, reload }: { info: AdsbInfo; reload: () => Promise<void> }) {
  const [cfg, setCfg] = useState<AdsbConfig>(info.config);
  const st = info.status;
  return (
    <div className="stack-lg">
      {!st.supported && (
        <Notice>
          Instalacja dekodera i feederów działa na Raspberry Pi z ChameleonTAK. Tutaj możesz przekazywać do TAK samoloty z innego
          odbiornika — podaj jego adres w sekcji „Zaawansowane”.
        </Notice>
      )}
      <StatusCards st={st} cfg={info.config} reload={reload} />
      <section className="card adsb-map-card">
        <AdsbMap cfg={cfg} planes={st.planes} onPick={(lat, lon) => setCfg({ ...cfg, lat, lon })} />
        <div className="adsb-map-foot small muted">
          <span>
            <Plane size={14} /> {st.planes.length} {planesWord(st.planes.length)} w zasięgu
            {cfg.maxRangeKm > 0 ? ` ${cfg.maxRangeKm} km` : ''}
          </span>
          <span>Kliknij mapę, aby ustawić położenie anteny.</span>
          {st.tar1090 && (
            <a href={st.tar1090} target="_blank" rel="noreferrer">
              Pełna mapa tar1090 <ExternalLink size={14} />
            </a>
          )}
        </div>
      </section>
      <ConfigForm cfg={cfg} setCfg={setCfg} saved={info.config} status={st} />
    </div>
  );
}

function planesWord(n: number) {
  if (n === 1) return 'samolot';
  const d = n % 10;
  const t = n % 100;
  return d >= 2 && d <= 4 && (t < 12 || t > 14) ? 'samoloty' : 'samolotów';
}

/* ---------- Karty stanu ---------- */
function StatusCard({ icon, title, state, children }: { icon: ReactNode; title: string; state: 'on' | 'off' | 'warn' | 'err'; children: ReactNode }) {
  return (
    <div className="card adsb-status">
      <div className="adsb-status-head">
        <span className="adsb-status-icon">{icon}</span>
        <span className="strong grow">{title}</span>
        <Led state={state} />
      </div>
      <div className="small stack-sm">{children}</div>
    </div>
  );
}

function InstallButton({ component, label, st, reload }: { component: 'readsb' | 'fr24'; label: string; st: AdsbStatus; reload: () => Promise<void> }) {
  const toast = useToast();
  const [starting, setStarting] = useState(false);
  const mine = st.job.component === component;
  const running = mine && st.job.running;
  const start = async () => {
    setStarting(true);
    try {
      await api.adsbInstall(component);
      await reload();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setStarting(false);
    }
  };
  return (
    <>
      <button type="button" className="btn btn-sm" style={{ alignSelf: 'flex-start' }} onClick={start} disabled={!st.supported || running || starting || st.job.running}>
        {running || starting ? <LoaderCircle size={16} className="spinner" /> : <Download size={16} />} {running ? 'Instaluję…' : label}
      </button>
      {running && (
        <span className="xsmall muted">
          {component === 'readsb' ? 'Kompilacja na Raspberry Pi trwa zwykle 5–20 minut.' : 'To potrwa minutę lub dwie.'} Możesz zamknąć tę
          stronę.
        </span>
      )}
      {mine && !st.job.running && st.job.error && <span className="xsmall text-danger" style={{ wordBreak: 'break-word' }}>{st.job.error}</span>}
    </>
  );
}

function StatusCards({ st, cfg, reload }: { st: AdsbStatus; cfg: AdsbConfig; reload: () => Promise<void> }) {
  const sdr = st.sdrs?.[0];
  return (
    <div className="adsb-status-grid">
      <StatusCard icon={<Antenna size={18} />} title="Odbiornik SDR" state={sdr ? 'on' : st.supported ? 'err' : 'off'}>
        {sdr ? <span>{sdr.name}</span> : <span>Nie wykryto klucza RTL-SDR. Podłącz go do portu USB Raspberry Pi.</span>}
      </StatusCard>

      <StatusCard
        icon={<Cpu size={18} />}
        title="Dekoder readsb"
        state={st.lastRead && !st.readError ? 'on' : st.decoder.installed ? 'warn' : 'off'}
      >
        {st.lastRead && !st.readError ? (
          <span>
            Działa · {st.messages.toLocaleString('pl-PL')} wiadomości · odczyt {fmtRelative(st.lastRead)}
          </span>
        ) : st.decoder.installed ? (
          <span>{st.decoder.active ? st.readError || 'Uruchamia się…' : 'Zainstalowany, ale nie działa. Sprawdź, czy klucz SDR jest podłączony.'}</span>
        ) : (
          <>
            <span>Odbiera sygnał 1090 MHz i dekoduje pozycje samolotów.</span>
            <InstallButton component="readsb" label="Zainstaluj readsb" st={st} reload={reload} />
          </>
        )}
      </StatusCard>

      <StatusCard icon={<Radio size={18} />} title="Flightradar24" state={st.fr24.active ? 'on' : st.fr24.installed && cfg.fr24 ? 'warn' : 'off'}>
        {st.fr24.active ? (
          <span>{st.fr24.detail || 'Wysyła dane.'}</span>
        ) : st.fr24.installed ? (
          <span>{cfg.fr24 ? 'Włączony, ale usługa nie działa — sprawdź klucz.' : 'Zainstalowany, wyłączony.'}</span>
        ) : (
          <>
            <span>Za udostępnianie danych FR24 daje darmowe konto Business.</span>
            <InstallButton component="fr24" label="Zainstaluj fr24feed" st={st} reload={reload} />
          </>
        )}
      </StatusCard>

      <StatusCard icon={<Globe size={18} />} title="ADS-B Exchange" state={st.adsbx.active ? 'on' : cfg.adsbx ? 'warn' : 'off'}>
        {st.adsbx.active ? (
          <span>
            Wysyła dane.{' '}
            <a href="https://www.adsbexchange.com/myip/" target="_blank" rel="noreferrer">
              Sprawdź na adsbexchange.com
            </a>
          </span>
        ) : (
          <span>{cfg.adsbx ? 'Włączony, ale połączenie nie działa.' : 'Wyłączony.'}</span>
        )}
      </StatusCard>

      <StatusCard icon={<Send size={18} />} title="Do aplikacji TAK" state={st.sending ? 'on' : cfg.bridge ? 'warn' : 'off'}>
        {cfg.bridge ? (
          <span>
            {st.sending ? `Wysyła ${st.planes.length} ${planesWord(st.planes.length)} co 5 s` : 'Włączone — czeka na samoloty.'}
            {cfg.groups.length > 0 ? ` · grupy: ${cfg.groups.join(', ')}` : ' · wszyscy użytkownicy'}
          </span>
        ) : (
          <span>Wyłączone.</span>
        )}
      </StatusCard>
    </div>
  );
}

/* ---------- Mapa ---------- */
const antennaIcon = L.divIcon({
  className: 'adsb-antenna',
  html: '<span class="adsb-antenna-dot"></span>',
  iconSize: [22, 22],
  iconAnchor: [11, 11],
});

const planeIcons = new Map<string, L.DivIcon>();
function planeIcon(p: AdsbPlane): L.DivIcon {
  const rot = Math.round(p.track / 5) * 5;
  const kind = p.military ? 'mil' : p.ground ? 'gnd' : 'civ';
  const key = `${kind}|${rot}|${p.callsign || p.hex}`;
  let icon = planeIcons.get(key);
  if (!icon) {
    const label = (p.callsign || p.reg || p.hex).replace(/[<>&"]/g, '');
    icon = L.divIcon({
      className: `adsb-plane adsb-${kind}`,
      html:
        `<svg viewBox="0 0 24 24" width="22" height="22" style="transform:rotate(${rot}deg)"><path d="M12 2c.8 0 1.3.8 1.3 1.8v5.4l7.7 4.6v2l-7.7-2.3v4.6l2.2 1.7v1.6L12 20.3l-3.5 1.1v-1.6l2.2-1.7v-4.6L3 15.8v-2l7.7-4.6V3.8C10.7 2.8 11.2 2 12 2z"/></svg>` +
        `<span class="adsb-plane-label">${label}</span>`,
      iconSize: [22, 22],
      iconAnchor: [11, 11],
      popupAnchor: [0, -10],
    });
    if (planeIcons.size > 2000) planeIcons.clear();
    planeIcons.set(key, icon);
  }
  return icon;
}

function ClickToPick({ onPick }: { onPick: (lat: number, lon: number) => void }) {
  useMapEvents({ click: (e) => onPick(Number(e.latlng.lat.toFixed(5)), Number(e.latlng.lng.toFixed(5))) });
  return null;
}

function Recenter({ lat, lon, has }: { lat: number; lon: number; has: boolean }) {
  const map = useMap();
  const [done, setDone] = useState(false);
  useEffect(() => {
    if (has && !done) {
      map.setView([lat, lon], 8);
      setDone(true);
    }
  }, [has, done, lat, lon, map]);
  return null;
}

function AdsbMap({ cfg, planes, onPick }: { cfg: AdsbConfig; planes: AdsbPlane[]; onPick: (lat: number, lon: number) => void }) {
  const { settings } = useSession();
  const tileUrl = settings?.mapTileUrl?.trim() || DEFAULT_TILES;
  const has = cfg.lat !== 0 || cfg.lon !== 0;
  const center = useMemo<[number, number]>(() => (has ? [cfg.lat, cfg.lon] : POLAND), [has, cfg.lat, cfg.lon]);
  return (
    <MapContainer center={center} zoom={has ? 8 : 6} className="adsb-map" scrollWheelZoom={false}>
      <TileLayer url={tileUrl} attribution="&copy; OpenStreetMap" />
      <ClickToPick onPick={onPick} />
      <Recenter lat={cfg.lat} lon={cfg.lon} has={has} />
      {has && (
        <>
          <Marker position={[cfg.lat, cfg.lon]} icon={antennaIcon} />
          {cfg.maxRangeKm > 0 && (
            <Circle center={[cfg.lat, cfg.lon]} radius={cfg.maxRangeKm * 1000} pathOptions={{ color: '#3fb6a8', weight: 1, fillOpacity: 0.04, dashArray: '6 6' }} />
          )}
        </>
      )}
      {planes.map((p) => (
        <Marker key={p.hex} position={[p.lat, p.lon]} icon={planeIcon(p)}>
          <Popup>
            <div className="stack-sm" style={{ gap: 2 }}>
              <strong>{p.callsign || p.reg || p.hex}</strong>
              <span>
                {[p.reg, p.type].filter(Boolean).join(' · ')}
                {p.military ? ' · wojskowy' : ''}
              </span>
              <span>
                {p.ground ? 'na ziemi' : `${Math.round(p.altFt).toLocaleString('pl-PL')} ft`} · {Math.round(p.speedKt)} kt · kurs{' '}
                {Math.round(p.track)}°
              </span>
              {p.distKm > 0 && <span>{p.distKm.toFixed(0)} km od anteny</span>}
              <span className="code">ICAO {p.hex}{p.squawk ? ` · squawk ${p.squawk}` : ''}</span>
            </div>
          </Popup>
        </Marker>
      ))}
    </MapContainer>
  );
}

/* ---------- Konfiguracja ---------- */
function ConfigForm({ cfg, setCfg, saved, status }: { cfg: AdsbConfig; setCfg: (c: AdsbConfig) => void; saved: AdsbConfig; status: AdsbStatus }) {
  const toast = useToast();
  const groups = useApi(api.groups, []);
  const servers = useApi(api.servers, []);
  const serverList = servers.data?.servers ?? [];
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [steps, setSteps] = useState<{ step: string; ok: boolean; error: string }[]>([]);
  const set = (patch: Partial<AdsbConfig>) => setCfg({ ...cfg, ...patch });
  const dirty = JSON.stringify(cfg) !== JSON.stringify(saved);
  const num = (v: string) => (v.trim() === '' ? 0 : Number(v.replace(',', '.')));

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if ((cfg.fr24 || cfg.adsbx) && cfg.lat === 0 && cfg.lon === 0) return setError('Ustaw położenie anteny (kliknij mapę).');
    if (cfg.fr24 && !/^[A-Za-z0-9]{8,40}$/.test(cfg.fr24Key)) return setError('Podaj poprawny klucz Flightradar24 (litery i cyfry).');
    setError(null);
    setBusy(true);
    try {
      const res = await api.saveAdsb(cfg);
      setCfg(res.config);
      setSteps(res.steps);
      if (res.steps.some((s) => !s.ok)) toast.warn('Zapisano, ale nie wszystko udało się zastosować.');
      else toast.ok('Zapisano ustawienia ADS-B.');
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="card card-pad-lg stack" onSubmit={submit} noValidate>
      <h2>Konfiguracja</h2>
      {error && <Notice kind="danger">{error}</Notice>}

      <h3 className="strong">Antena</h3>
      <div className="form-grid adsb-coords">
        <Field label="Szerokość geograficzna" htmlFor="adsb-lat">
          <input id="adsb-lat" className="input input-code" inputMode="decimal" value={cfg.lat || ''} onChange={(e) => set({ lat: num(e.target.value) })} />
        </Field>
        <Field label="Długość geograficzna" htmlFor="adsb-lon">
          <input id="adsb-lon" className="input input-code" inputMode="decimal" value={cfg.lon || ''} onChange={(e) => set({ lon: num(e.target.value) })} />
        </Field>
        <Field label="Wysokość anteny (m n.p.m.)" htmlFor="adsb-alt" hint="Potrzebna do MLAT we Flightradar24.">
          <input id="adsb-alt" className="input input-code" inputMode="decimal" value={cfg.altM || ''} onChange={(e) => set({ altM: num(e.target.value) })} />
        </Field>
      </div>

      <hr />
      <h3 className="strong">Samoloty w aplikacjach TAK</h3>
      <CheckRow
        checked={cfg.bridge}
        onChange={(v) => set({ bridge: v })}
        label="Pokazuj samoloty na mapie ATAK / iTAK"
        hint="Serwer wysyła je co 5 sekund jako neutralne obiekty powietrzne. Znikają same, gdy samolot wyleci z zasięgu."
      />
      {cfg.bridge && (
        <>
          <div className="form-grid">
            <Field label={`Zasięg: ${cfg.maxRangeKm > 0 ? `${cfg.maxRangeKm} km` : 'bez limitu'}`} htmlFor="adsb-range" hint="Liczony od anteny. Mniejszy zasięg = mniej danych na telefonach.">
              <input
                id="adsb-range"
                type="range"
                min={0}
                max={400}
                step={10}
                value={cfg.maxRangeKm}
                onChange={(e) => set({ maxRangeKm: Number(e.target.value) })}
              />
            </Field>
            <Field label="Maksymalna wysokość (ft)" htmlFor="adsb-maxalt" hint="0 = bez limitu. Np. 10 000 ft pokaże tylko lotnictwo nisko latające.">
              <input
                id="adsb-maxalt"
                className="input input-code"
                inputMode="numeric"
                value={cfg.maxAltFt || ''}
                placeholder="0"
                onChange={(e) => set({ maxAltFt: Math.max(0, Math.round(num(e.target.value))) })}
              />
            </Field>
          </div>
          {serverList.length > 1 && (
            <Field label="Na których serwerach" hint="Bez wyboru samoloty trafiają tylko na serwer główny.">
              <div className="chips" role="group">
                {serverList.map((sv) => {
                  const on = cfg.servers.includes(sv.id);
                  return (
                    <button
                      key={sv.id}
                      type="button"
                      className={`chip${on ? ' selected' : ''}`}
                      aria-pressed={on}
                      onClick={() => set({ servers: on ? cfg.servers.filter((x) => x !== sv.id) : [...cfg.servers, sv.id] })}
                    >
                      {sv.displayName}
                    </button>
                  );
                })}
              </div>
            </Field>
          )}
          <Field
            label="Komu pokazywać"
            hint={cfg.groups.length === 0 && (groups.data?.length ?? 0) > 0 ? 'Bez wybranych grup samoloty widzą wszyscy.' : undefined}
          >
            <GroupPicker all={(groups.data ?? []).map((g) => g.name)} value={cfg.groups} onChange={(v) => set({ groups: v })} emptyHint="Brak grup — samoloty zobaczą wszyscy." />
          </Field>
        </>
      )}

      <hr />
      <h3 className="strong">Udostępnianie danych</h3>
      <CheckRow
        checked={cfg.fr24}
        onChange={(v) => set({ fr24: v })}
        label="Flightradar24"
        hint={status.fr24.installed ? 'fr24feed czyta dane z readsb — klucz SDR obsługuje jeden program.' : 'Najpierw zainstaluj fr24feed (karta powyżej).'}
      />
      {cfg.fr24 && (
        <Field
          label="Klucz udostępniania (sharing key)"
          htmlFor="adsb-fr24key"
          hint={
            <>
              Masz już radar FR24? Klucz jest w ustawieniach konta (Data sharing). Nowy klucz uzyskasz w konsoli Raspberry Pi
              poleceniem <span className="code">sudo fr24feed --signup</span> — podaj e-mail i współrzędne anteny, a resztę pytań
              zostaw domyślnie.
            </>
          }
        >
          <input
            id="adsb-fr24key"
            className="input input-code"
            autoComplete="off"
            autoCapitalize="none"
            value={cfg.fr24Key}
            onChange={(e) => set({ fr24Key: e.target.value.trim() })}
          />
        </Field>
      )}
      <CheckRow
        checked={cfg.adsbx}
        onChange={(v) => set({ adsbx: v })}
        label="ADS-B Exchange"
        hint="Niefiltrowane dane, także samoloty wojskowe. Rejestracja nie jest potrzebna — serwer nada stacji własny identyfikator."
      />

      <Disclosure title="Zaawansowane">
        <Field
          label="Źródło danych"
          htmlFor="adsb-src"
          hint={
            <>
              Puste = wykryj automatycznie ({status.source ? <span className="code">{status.source}</span> : 'nie wykryto'}). Możesz
              podać plik aircraft.json albo adres innego odbiornika, np. <span className="code">http://192.168.1.50/tar1090/data/aircraft.json</span>.
            </>
          }
        >
          <input
            id="adsb-src"
            className="input input-code"
            autoCapitalize="none"
            autoCorrect="off"
            value={cfg.source}
            onChange={(e) => set({ source: e.target.value.trim() })}
          />
        </Field>
      </Disclosure>

      {steps.length > 0 && (
        <ul className="adsb-steps small">
          {steps.map((s) => (
            <li key={s.step} className={s.ok ? 'text-ok' : 'text-danger'}>
              {s.ok ? '✓' : '✗'} {s.step}
              {s.error ? `: ${s.error}` : ''}
            </li>
          ))}
        </ul>
      )}
      <div className="settings-foot">
        <button type="submit" className="btn btn-primary" disabled={!dirty || busy}>
          {busy ? <LoaderCircle size={18} className="spinner" /> : <Save size={18} />} Zapisz
        </button>
      </div>
    </form>
  );
}
