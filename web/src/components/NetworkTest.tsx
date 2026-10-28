import { useState, type ReactNode } from 'react';
import { Cpu, Gauge, Globe, LoaderCircle, Users, Video, Wifi } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { InternetSpeed } from '../api/types';
import { fmtBytes } from '../lib/format';
import { useApi } from '../lib/useApi';
import { Notice } from './ui';

/* ---------- Model obciążenia ----------
 * Każdy klient TAK wysyła swoją pozycję mniej więcej co 5 s (~600 B), a serwer rozsyła ją do
 * pozostałych; samoloty z ADS-B idą do każdego klienta co 5 s (~700 B na samolot). Wideo 720p
 * to ok. 1,5 Mb/s na widza. Liczymy z zapasem 50% łącza na pliki, mapy i skoki ruchu. */
const SA_BYTES_PER_SEC = 600 / 5;
const PLANE_BYTES_PER_SEC = 700 / 5;
const VIDEO_MBPS = 1.5;

export function maxTakUsers(mbps: number, planes: number): number {
  const budget = (mbps * 1e6 * 0.5) / 8;
  let n = 0;
  while (n < 5000 && (n + 1) * (n * SA_BYTES_PER_SEC + planes * PLANE_BYTES_PER_SEC) <= budget) n++;
  return n;
}

export function maxVideoViewers(mbps: number): number {
  return Math.max(0, Math.floor((mbps * 0.7) / VIDEO_MBPS));
}

interface LocalResult {
  pingMs: number;
  downMbps: number;
  upMbps: number;
}

const now = () => performance.now();

async function localPing(): Promise<number> {
  const times: number[] = [];
  for (let i = 0; i < 8; i++) {
    const t = now();
    await fetch('/api/system/speedtest/ping', { cache: 'no-store', credentials: 'same-origin' });
    if (i > 0) times.push(now() - t);
  }
  times.sort((a, b) => a - b);
  return times[Math.floor(times.length / 2)];
}

/** Pobiera coraz większe porcje, aż test potrwa ok. 4 s. */
async function localDown(): Promise<number> {
  let size = 4 << 20;
  let bytes = 0;
  const start = now();
  while (now() - start < 4000) {
    const res = await fetch(`/api/system/speedtest/down?bytes=${size}`, { cache: 'no-store', credentials: 'same-origin' });
    if (!res.ok) throw new Error(`Pobieranie: odpowiedź ${res.status}`);
    bytes += (await res.arrayBuffer()).byteLength;
    if (now() - start < 1000) size = Math.min(size * 2, 64 << 20);
  }
  return (bytes * 8) / ((now() - start) / 1000) / 1e6;
}

function testBlob(size: number): Blob {
  const chunk = new Uint8Array(new ArrayBuffer(64 << 10));
  crypto.getRandomValues(chunk);
  const parts: BlobPart[] = [];
  for (let n = 0; n < size; n += chunk.length) parts.push(chunk);
  return new Blob(parts);
}

async function localUp(): Promise<number> {
  let size = 4 << 20;
  let bytes = 0;
  const start = now();
  while (now() - start < 4000) {
    const blob = testBlob(size);
    const res = await fetch('/api/system/speedtest/up', {
      method: 'POST',
      body: blob,
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/octet-stream' },
    });
    if (!res.ok) throw new Error(`Wysyłanie: odpowiedź ${res.status}`);
    bytes += blob.size;
    if (now() - start < 1000) size = Math.min(size * 2, 32 << 20);
  }
  return (bytes * 8) / ((now() - start) / 1000) / 1e6;
}

const mbps = (v: number) => `${v >= 100 ? Math.round(v) : v.toFixed(1)} Mb/s`;

function pingLevel(ms: number): 'ok' | 'warn' | 'danger' {
  return ms < 80 ? 'ok' : ms < 250 ? 'warn' : 'danger';
}

/** Test sieci: łącze internetowe Raspberry Pi i sieć lokalna między tym urządzeniem a Pi. */
export function NetworkTest() {
  const load = useApi(api.speedLoad, [], 15000);
  const [inet, setInet] = useState<InternetSpeed | null>(null);
  const [local, setLocal] = useState<LocalResult | null>(null);
  const [busy, setBusy] = useState<'' | 'internet' | 'local'>('');
  const [phase, setPhase] = useState('');
  const [error, setError] = useState<string | null>(null);
  const l = load.data;
  const planes = l?.planes ?? 0;

  const runInternet = async () => {
    setBusy('internet');
    setError(null);
    try {
      const res = await api.speedInternet();
      setInet(res.internet);
      load.setData(res.load);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy('');
    }
  };

  const runLocal = async () => {
    setBusy('local');
    setError(null);
    try {
      setPhase('opóźnienie…');
      const pingMs = await localPing();
      setPhase('pobieranie…');
      const downMbps = await localDown();
      setPhase('wysyłanie…');
      const upMbps = await localUp();
      setLocal({ pingMs, downMbps, upMbps });
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy('');
      setPhase('');
    }
  };

  return (
    <div className="stack">
      <div className="net-test-grid">
        <div className="card net-test-card">
          <div className="net-test-head">
            <Globe size={18} />
            <span className="strong grow">Łącze internetowe Raspberry Pi</span>
          </div>
          <p className="small muted">
            Dla użytkowników łączących się z internetu (domena, publiczne IP, ZeroTier). Mierzone z Pi do speed.cloudflare.com — trwa
            ok. 15 s.
          </p>
          <button type="button" className="btn btn-sm" style={{ alignSelf: 'flex-start' }} onClick={runInternet} disabled={busy !== ''}>
            {busy === 'internet' ? <LoaderCircle size={16} className="spinner" /> : <Gauge size={16} />}{' '}
            {busy === 'internet' ? 'Mierzę…' : inet ? 'Zmierz ponownie' : 'Zmierz'}
          </button>
          {inet?.error && <Notice kind="danger">{inet.error}</Notice>}
          {inet && !inet.error && (
            <>
              <Metrics ping={inet.pingMs} down={inet.downMbps} up={inet.upMbps} upLabel="wysyłanie z Pi" downLabel="pobieranie na Pi" />
              <Capacity egress={inet.upMbps} planes={planes} clients={l?.clients ?? 0} where="z internetu" />
            </>
          )}
        </div>

        <div className="card net-test-card">
          <div className="net-test-head">
            <Wifi size={18} />
            <span className="strong grow">Sieć lokalna: to urządzenie ↔ Pi</span>
          </div>
          <p className="small muted">
            Uruchom na telefonie w tym miejscu, gdzie będzie używany — zmierzysz zasięg i prędkość Wi-Fi albo hotspotu Pi.
          </p>
          <button type="button" className="btn btn-sm" style={{ alignSelf: 'flex-start' }} onClick={runLocal} disabled={busy !== ''}>
            {busy === 'local' ? <LoaderCircle size={16} className="spinner" /> : <Gauge size={16} />}{' '}
            {busy === 'local' ? `Mierzę ${phase}` : local ? 'Zmierz ponownie' : 'Zmierz'}
          </button>
          {local && (
            <>
              <Metrics ping={local.pingMs} down={local.downMbps} up={local.upMbps} downLabel="z Pi do urządzenia" upLabel="z urządzenia do Pi" />
              <Capacity egress={local.downMbps} planes={planes} clients={l?.clients ?? 0} where="w tej sieci" />
            </>
          )}
        </div>
      </div>
      {error && <Notice kind="danger">{error}</Notice>}

      {l && (
        <div className="row small muted" style={{ gap: 16, flexWrap: 'wrap' }}>
          <span className="row" style={{ gap: 4 }}>
            <Cpu size={14} /> obciążenie {l.load1.toFixed(2)} / {l.cpus} rdzenie
            {l.load1 > l.cpus ? ' — procesor jest przeciążony' : ''}
          </span>
          {l.memTotal > 0 && (
            <span>
              wolna pamięć {fmtBytes(l.memAvail)} z {fmtBytes(l.memTotal)}
            </span>
          )}
          <span>
            teraz: {l.clients} {l.clients === 1 ? 'klient' : 'klientów'} TAK{planes ? `, ${planes} samolotów ADS-B` : ''}
          </span>
        </div>
      )}
      <p className="xsmall faint">
        Szacunek zakłada pozycję od każdego użytkownika co ok. 5 s, samoloty ADS-B co 5 s i wideo 720p ok. 1,5 Mb/s na widza, z
        zapasem połowy łącza. Procesor Raspberry Pi 4/5 obsłuży kilkaset klientów — zwykle ogranicza łącze.
      </p>
    </div>
  );
}

function Metrics({ ping, down, up, downLabel, upLabel }: { ping: number; down: number; up: number; downLabel: string; upLabel: string }) {
  return (
    <dl className="net-metrics">
      <div className={`net-${pingLevel(ping)}`}>
        <dt>opóźnienie</dt>
        <dd>{Math.round(ping)} ms</dd>
      </div>
      <div>
        <dt>{downLabel}</dt>
        <dd>{mbps(down)}</dd>
      </div>
      <div>
        <dt>{upLabel}</dt>
        <dd>{mbps(up)}</dd>
      </div>
    </dl>
  );
}

function Capacity({ egress, planes, clients, where }: { egress: number; planes: number; clients: number; where: string }) {
  const users = maxTakUsers(egress, planes);
  const viewers = maxVideoViewers(egress);
  const kind = users === 0 || users < clients ? 'danger' : users < clients * 1.5 + 5 ? 'warn' : 'ok';
  return (
    <div className="stack-sm">
      <Verdict kind={kind} icon={<Users size={16} />}>
        Użytkownicy TAK {where}: <strong>do ok. {users >= 5000 ? '5000+' : users}</strong> jednocześnie
        {clients > 0 ? ` (teraz ${clients})` : ''}
      </Verdict>
      <Verdict kind={viewers >= 2 ? 'ok' : viewers === 1 ? 'warn' : 'danger'} icon={<Video size={16} />}>
        Wideo na żywo 720p: <strong>{viewers === 0 ? 'łącze za słabe' : `ok. ${viewers} ${viewers === 1 ? 'widz' : 'widzów'}`}</strong>
      </Verdict>
    </div>
  );
}

function Verdict({ kind, icon, children }: { kind: 'ok' | 'warn' | 'danger'; icon: ReactNode; children: ReactNode }) {
  return (
    <div className={`net-verdict net-${kind}`}>
      {icon}
      <span>{children}</span>
    </div>
  );
}
