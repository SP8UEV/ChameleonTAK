import { useState } from 'react';
import { Globe, House, LoaderCircle, Network, Radar, Router } from 'lucide-react';
import { errorMessage } from '../api/client';
import type { AccessMode, Ports, PublicCertStatus, PublicIP } from '../api/types';
import { fmtDateTime, fmtRelative } from '../lib/format';
import { Choice, Notice } from './ui';

/** Wybór: sieć lokalna / własna domena / publiczny adres IP / ZeroTier (tylko w ustawieniach). */
export function AccessChoice({ value, onChange, zerotier }: { value: AccessMode; onChange: (m: AccessMode) => void; zerotier?: boolean }) {
  return (
    <div className="choices">
      <Choice
        type="radio"
        name="access"
        checked={value === 'local'}
        onChange={() => onChange('local')}
        icon={<House size={18} />}
        title="Sieć lokalna"
        description="Telefony są w tej samej sieci co serwer (LAN, Wi-Fi albo hotspot Raspberry Pi)."
      />
      <Choice
        type="radio"
        name="access"
        checked={value === 'domain'}
        onChange={() => onChange('domain')}
        icon={<Globe size={18} />}
        title="Z internetu — własna domena"
        description="Np. tak.twojafirma.pl, wskazująca na publiczny adres routera."
      />
      <Choice
        type="radio"
        name="access"
        checked={value === 'ip'}
        onChange={() => onChange('ip')}
        icon={<Router size={18} />}
        title="Z internetu — publiczny adres IP"
        description="Bez domeny. Router musi mieć stały, publiczny adres IP."
      />
      {zerotier && (
        <Choice
          type="radio"
          name="access"
          checked={value === 'zerotier'}
          onChange={() => onChange('zerotier')}
          icon={<Network size={18} />}
          title="Z internetu — przez ZeroTier"
          description="Bez publicznego IP i domeny. Telefony łączą się przez prywatną sieć ZeroTier."
        />
      )}
    </div>
  );
}

/** Przycisk „Wykryj” publicznego adresu IP. */
export function DetectIpButton({ detect, onFound }: { detect: () => Promise<PublicIP>; onFound: (r: PublicIP) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const run = async () => {
    setBusy(true);
    setError(null);
    try {
      onFound(await detect());
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="stack-sm">
      <button type="button" className="btn btn-sm" style={{ alignSelf: 'flex-start' }} onClick={run} disabled={busy}>
        {busy ? <LoaderCircle size={16} className="spinner" /> : <Radar size={16} />} Wykryj mój publiczny adres
      </button>
      {error && <span className="small text-danger">{error}</span>}
    </div>
  );
}

/** Co trzeba ustawić na routerze, żeby serwer był osiągalny z internetu. */
export function PortForwarding({ mode, ports, lanAddress }: { mode: AccessMode; ports: Ports; lanAddress?: string }) {
  if (mode === 'local') return null;
  const rows: { ext: number; int: number; what: string }[] = [
    { ext: 80, int: ports.http, what: 'Let’s Encrypt i strona dołączania' },
    { ext: 443, int: ports.https, what: 'panel i pobieranie konfiguracji' },
    { ext: ports.cotTls, int: ports.cotTls, what: 'strumień danych TAK' },
    { ext: ports.marti, int: ports.marti, what: 'API serwera (pliki, misje)' },
    { ext: ports.enroll, int: ports.enroll, what: 'rejestracja urządzeń' },
  ];
  return (
    <Notice>
      <div className="stack-sm">
        <span>
          Na routerze przekieruj porty TCP na Raspberry Pi
          {lanAddress ? (
            <>
              {' '}
              (<span className="code">{lanAddress}</span>)
            </>
          ) : null}
          :
        </span>
        <ul className="small" style={{ margin: 0, paddingLeft: 18 }}>
          {rows.map((r) => (
            <li key={r.ext}>
              <span className="code">{r.ext === r.int ? r.ext : `${r.ext} → ${r.int}`}</span> — {r.what}
            </li>
          ))}
        </ul>
        {mode === 'domain' ? (
          <span className="small">Rekord DNS typu A domeny musi wskazywać publiczny adres IP routera.</span>
        ) : (
          <span className="small">
            Adres musi być stały. Jeśli dostawca internetu go zmienia albo daje adres za CGNAT (100.64.x.x), użyj domeny z
            dynamicznym DNS.
          </span>
        )}
        <span className="small muted">
          Telefony w tej samej sieci co serwer mogą nie połączyć się przez adres publiczny, jeśli router nie obsługuje
          „NAT loopback”. Wtedy kod QR otwarty w sieci lokalnej i tak pobierze konfigurację przez adres lokalny.
        </span>
      </div>
    </Notice>
  );
}

/** Stan certyfikatu Let's Encrypt. */
export function CertStatusView({ status }: { status: PublicCertStatus }) {
  switch (status.state) {
    case 'ok':
      return (
        <Notice kind="ok">
          Certyfikat Let’s Encrypt dla <span className="code">{status.host}</span> jest ważny do {fmtDateTime(status.notAfter)}.
          Odnawia się sam.
        </Notice>
      );
    case 'pending':
      return (
        <Notice>
          Trwa uzyskiwanie certyfikatu Let’s Encrypt dla <span className="code">{status.host}</span>. To zwykle zajmuje kilkanaście
          sekund.
        </Notice>
      );
    case 'error':
      return (
        <Notice kind={status.notAfter ? 'warn' : 'danger'}>
          <div className="stack-sm">
            <span>
              Nie udało się uzyskać certyfikatu Let’s Encrypt dla <span className="code">{status.host}</span>
              {status.nextAttempt ? `. Kolejna próba ${fmtRelative(status.nextAttempt)}.` : '.'}
            </span>
            <span className="small code" style={{ wordBreak: 'break-word' }}>
              {status.error}
            </span>
            {status.notAfter && <span className="small">Obecny certyfikat działa do {fmtDateTime(status.notAfter)}.</span>}
          </div>
        </Notice>
      );
    default:
      return null;
  }
}
