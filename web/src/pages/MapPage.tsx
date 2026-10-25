import { useEffect, useMemo, useRef, useState } from 'react';
import L from 'leaflet';
import { MapContainer, Marker, Popup, TileLayer, useMap } from 'react-leaflet';
import 'leaflet/dist/leaflet.css';
import { List, LocateFixed, MapPin, X } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { Position } from '../api/types';
import { useSession } from '../app/session';
import { Led } from '../components/ui';
import { useToast } from '../components/Toast';
import { fmtRelative, fmtSpeed } from '../lib/format';
import { OFFLINE_COLOR, roleLabel, teamColor, teamLabel } from '../lib/tak';
import { useLive } from '../lib/ws';

const DEFAULT_TILES = 'https://tile.openstreetmap.org/{z}/{x}/{y}.png';
const DEFAULT_CENTER: [number, number] = [52.07, 19.48]; // środek Polski
const DEFAULT_ZOOM = 6;

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] as string);
}

const iconCache = new Map<string, L.DivIcon>();

function iconFor(p: Position): L.DivIcon {
  const contact = p.kind === 'contact';
  const color = contact ? (p.online ? teamColor(p.team) : OFFLINE_COLOR) : '#e9a93f';
  const moving = contact && p.online && p.speed > 0.5 && Number.isFinite(p.course);
  const course = moving ? Math.round(p.course) : 0;
  const key = `${p.kind}|${color}|${p.online}|${moving ? course : '-'}|${p.callsign}`;
  let icon = iconCache.get(key);
  if (!icon) {
    const label = escapeHtml(p.callsign || p.uid);
    const shape = contact
      ? `<span class="cm-dot" style="background:${color}"></span>${
          moving ? `<span class="cm-heading" style="transform:rotate(${course}deg)"><i style="border-bottom-color:${color}"></i></span>` : ''
        }`
      : `<span class="cm-diamond" style="background:${color}"></span>`;
    icon = L.divIcon({
      className: `ctak-marker${contact && !p.online ? ' is-offline' : ''}`,
      html: `${shape}<span class="cm-label">${label}</span>`,
      iconSize: [22, 22],
      iconAnchor: [11, 11],
      popupAnchor: [0, -12],
    });
    if (iconCache.size > 500) iconCache.clear();
    iconCache.set(key, icon);
  }
  return icon;
}

function MapBridge({ onReady }: { onReady: (m: L.Map) => void }) {
  const map = useMap();
  useEffect(() => {
    onReady(map);
  }, [map, onReady]);
  return null;
}

function sortContacts(a: Position, b: Position) {
  if (a.online !== b.online) return a.online ? -1 : 1;
  return (a.callsign || a.uid).localeCompare(b.callsign || b.uid, 'pl');
}

export default function MapPage() {
  const { settings } = useSession();
  const toast = useToast();
  const [positions, setPositions] = useState<Map<string, Position>>(new Map());
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [map, setMap] = useState<L.Map | null>(null);
  const [listOpen, setListOpen] = useState(false);
  const markerRefs = useRef(new Map<string, L.Marker>());
  const fittedOnce = useRef(false);

  const tileUrl = settings?.mapTileUrl?.trim() || DEFAULT_TILES;

  useEffect(() => {
    let alive = true;
    api
      .positions()
      .then((list) => {
        if (!alive) return;
        setPositions(new Map(list.map((p) => [p.uid, p])));
        setLoaded(true);
      })
      .catch((e) => {
        if (alive) setError(errorMessage(e));
      });
    return () => {
      alive = false;
    };
  }, []);

  useLive((msg) => {
    if (msg.type === 'position') {
      setPositions((prev) => {
        const next = new Map(prev);
        next.set(msg.data.uid, msg.data);
        return next;
      });
    } else if (msg.type === 'remove') {
      setPositions((prev) => {
        if (!prev.has(msg.data.uid)) return prev;
        const next = new Map(prev);
        next.delete(msg.data.uid);
        return next;
      });
    }
  });

  const all = useMemo(() => Array.from(positions.values()).filter((p) => Number.isFinite(p.lat) && Number.isFinite(p.lon)), [positions]);
  const contacts = useMemo(() => all.filter((p) => p.kind === 'contact').sort(sortContacts), [all]);
  const onlineCount = contacts.filter((c) => c.online).length;

  const fitAll = (quiet = false) => {
    if (!map) return;
    if (all.length === 0) {
      if (!quiet) toast.info('Na mapie nie ma jeszcze żadnych pozycji.');
      return;
    }
    if (all.length === 1) {
      map.setView([all[0].lat, all[0].lon], 15);
      return;
    }
    const bounds = L.latLngBounds(all.map((p) => [p.lat, p.lon] as [number, number]));
    map.fitBounds(bounds, { padding: [48, 48], maxZoom: 16 });
  };

  // Pierwsze dopasowanie widoku po wczytaniu pozycji.
  useEffect(() => {
    if (map && loaded && !fittedOnce.current && all.length > 0) {
      fittedOnce.current = true;
      fitAll(true);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [map, loaded, all.length]);

  const focus = (p: Position) => {
    if (!map) return;
    map.flyTo([p.lat, p.lon], Math.max(map.getZoom(), 15), { duration: 0.6 });
    window.setTimeout(() => markerRefs.current.get(p.uid)?.openPopup(), 650);
    setListOpen(false);
  };

  return (
    <div className="map-layout">
      <div className="map-wrap">
        <MapContainer center={DEFAULT_CENTER} zoom={DEFAULT_ZOOM} className="map" worldCopyJump>
          <TileLayer
            key={tileUrl}
            url={tileUrl}
            maxZoom={19}
            attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>'
          />
          <MapBridge onReady={setMap} />
          {all.map((p) => (
            <Marker
              key={p.uid}
              position={[p.lat, p.lon]}
              icon={iconFor(p)}
              zIndexOffset={p.online ? 1000 : 0}
              ref={(m) => {
                if (m) markerRefs.current.set(p.uid, m);
                else markerRefs.current.delete(p.uid);
              }}
            >
              <Popup>
                <PositionPopup p={p} />
              </Popup>
            </Marker>
          ))}
        </MapContainer>

        <div className="map-tools">
          <button type="button" className="btn map-btn" onClick={() => fitAll()} title="Pokaż wszystkich">
            <LocateFixed size={18} /> <span className="map-btn-text">Pokaż wszystkich</span>
          </button>
          <button type="button" className="btn map-btn map-list-toggle" onClick={() => setListOpen((v) => !v)} aria-expanded={listOpen}>
            <List size={18} /> Lista ({contacts.length})
          </button>
        </div>

        {error && (
          <div className="map-overlay-msg">
            <div className="notice notice-danger">Nie udało się wczytać pozycji: {error}</div>
          </div>
        )}
      </div>

      <aside className={`map-side${listOpen ? ' open' : ''}`} aria-label="Kontakty">
        <div className="map-side-head">
          <div>
            <h2>Kontakty</h2>
            <div className="xsmall muted">
              {onlineCount} w sieci z {contacts.length}
            </div>
          </div>
          <button type="button" className="icon-btn map-side-close" onClick={() => setListOpen(false)} aria-label="Zamknij listę">
            <X size={20} />
          </button>
        </div>
        <div className="map-side-list">
          {!loaded && !error && <div className="muted small" style={{ padding: 16 }}>Wczytywanie…</div>}
          {loaded && contacts.length === 0 && (
            <div className="state compact">
              <div className="state-icon">
                <MapPin size={24} />
              </div>
              <p className="small">Nikt jeszcze nie wysłał pozycji. Kontakty pojawią się tu, gdy telefony połączą się z serwerem.</p>
            </div>
          )}
          {contacts.map((c) => (
            <button key={c.uid} type="button" className="contact-row" onClick={() => focus(c)}>
              <span
                className="contact-swatch"
                style={{ background: c.online ? teamColor(c.team) : 'transparent', borderColor: c.online ? 'rgba(0,0,0,.35)' : OFFLINE_COLOR }}
              />
              <span className="grow" style={{ minWidth: 0 }}>
                <span className={`contact-name${c.online ? '' : ' faint'}`}>{c.callsign || c.uid}</span>
                <span className="contact-meta">
                  {c.username && <span>{c.username}</span>}
                  <span>{c.online ? 'w sieci' : fmtRelative(c.lastSeen)}</span>
                </span>
              </span>
              <Led state={c.online ? 'on' : 'off'} />
            </button>
          ))}
        </div>
      </aside>
    </div>
  );
}

function PositionPopup({ p }: { p: Position }) {
  return (
    <div className="pos-popup">
      <div className="pos-popup-title">
        <span className="swatch" style={{ background: p.kind === 'contact' ? (p.online ? teamColor(p.team) : OFFLINE_COLOR) : '#e9a93f' }} />
        <strong>{p.callsign || p.uid}</strong>
      </div>
      <dl className="kv">
        {p.kind === 'contact' ? (
          <>
            <dt>Użytkownik</dt>
            <dd>{p.username || '—'}</dd>
            <dt>Zespół</dt>
            <dd>
              {teamLabel(p.team)}
              {p.role ? `, ${roleLabel(p.role)}` : ''}
            </dd>
            <dt>Bateria</dt>
            <dd>{p.battery > 0 ? `${Math.round(p.battery)}%` : '—'}</dd>
            <dt>Prędkość</dt>
            <dd>{fmtSpeed(p.speed)}</dd>
          </>
        ) : (
          <>
            <dt>Znacznik</dt>
            <dd className="code">{p.type}</dd>
          </>
        )}
        <dt>Ostatnio</dt>
        <dd>{p.online ? 'teraz w sieci' : fmtRelative(p.lastSeen)}</dd>
        <dt>Pozycja</dt>
        <dd className="code">
          {p.lat.toFixed(5)}, {p.lon.toFixed(5)}
        </dd>
        {p.remarks && (
          <>
            <dt>Uwagi</dt>
            <dd>{p.remarks}</dd>
          </>
        )}
      </dl>
    </div>
  );
}
