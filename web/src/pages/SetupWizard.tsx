import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react';
import {
  ArrowLeft,
  ArrowRight,
  CircleCheck,
  CircleDashed,
  CircleMinus,
  CircleX,
  Eye,
  EyeOff,
  LoaderCircle,
  Moon,
  Network,
  Power,
  RefreshCw,
  Sun,
  Wifi,
} from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { AccessMode, NetworkMode, PlatformKey, Ports, SetupDefaults, SetupProgress, SetupRequest, StepState } from '../api/types';
import { AccessChoice, DetectIpButton, PortForwarding } from '../components/PublicAccess';
import { BrandMark, Choice, CheckRow, Disclosure, ErrorState, Field, Loading, Notice } from '../components/ui';
import { isPublicIp, looksPublicDomain, validEmail } from '../lib/access';
import { useTheme } from '../lib/theme';
import { fmtTime } from '../lib/format';
import { platformOs } from '../lib/tak';

type StepKey = 'welcome' | 'server' | 'ports' | 'admin' | 'platforms' | 'network' | 'summary';

const STEPS: { key: StepKey; label: string }[] = [
  { key: 'welcome', label: 'Start' },
  { key: 'server', label: 'Adres serwera' },
  { key: 'ports', label: 'Porty' },
  { key: 'admin', label: 'Administrator' },
  { key: 'platforms', label: 'Urządzenia' },
  { key: 'network', label: 'Sieć' },
  { key: 'summary', label: 'Podsumowanie' },
];

const HOTSPOT_IP = '10.42.0.1';

const PORT_FIELDS: { key: keyof Omit<Ports, 'cotTcpEnabled'>; label: string; hint: string }[] = [
  { key: 'cotTls', label: 'Strumień danych (TLS)', hint: 'Pozycje, czat, znaczniki — szyfrowane' },
  { key: 'marti', label: 'API serwera', hint: 'Pliki, misje, paczki danych' },
  { key: 'enroll', label: 'Rejestracja urządzeń', hint: 'Wydawanie certyfikatów telefonom' },
  { key: 'http', label: 'Panel (HTTP)', hint: 'Ten panel i strona dołączania' },
  { key: 'https', label: 'Panel (HTTPS)', hint: 'Szyfrowany panel' },
  { key: 'cotTcp', label: 'Strumień bez szyfrowania', hint: 'Tylko gdy włączony poniżej' },
];

function randomPassword(len = 10): string {
  const alphabet = 'abcdefghjkmnpqrstuvwxyz23456789';
  const buf = new Uint32Array(len);
  try {
    crypto.getRandomValues(buf);
  } catch {
    for (let i = 0; i < len; i++) buf[i] = Math.floor(Math.random() * 1e9);
  }
  return Array.from(buf, (n) => alphabet[n % alphabet.length]).join('');
}

const IPV4 = /^\d{1,3}(\.\d{1,3}){3}$/;

function validServerName(name: string): boolean {
  const n = name.trim();
  if (!n || /\s/.test(n)) return false;
  if (IPV4.test(n)) return n.split('.').every((p) => Number(p) <= 255);
  return /^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$/.test(n);
}

interface FormState {
  access: AccessMode;
  serverName: string; // adres w sieci lokalnej
  domain: string;
  publicIp: string;
  displayName: string;
  organization: string;
  ports: Ports;
  adminUser: string;
  adminPass: string;
  adminPass2: string;
  platforms: PlatformKey[];
  mode: NetworkMode;
  staticIp: boolean;
  hotspotSsid: string;
  hotspotPassword: string;
  leEnabled: boolean;
  leEmail: string;
  ssh: boolean;
}

function stepIcon(state: StepState) {
  switch (state) {
    case 'done':
      return <CircleCheck size={22} className="text-ok" />;
    case 'running':
      return <LoaderCircle size={22} className="spinner progress-running" />;
    case 'error':
      return <CircleX size={22} className="text-danger" />;
    case 'skipped':
      return <CircleMinus size={22} className="faint" />;
    default:
      return <CircleDashed size={22} className="faint" />;
  }
}

export default function SetupWizard() {
  const [theme, toggleTheme] = useTheme();
  const [defaults, setDefaults] = useState<SetupDefaults | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [form, setForm] = useState<FormState | null>(null);
  const [step, setStep] = useState(0);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [phase, setPhase] = useState<'form' | 'progress'>('form');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [showPass, setShowPass] = useState(false);
  const panelRef = useRef<HTMLFormElement>(null);

  const load = async () => {
    setLoadError(null);
    try {
      // Kreator mógł już zostać uruchomiony (np. odświeżenie strony) — wtedy pokaż postęp.
      try {
        const p = await api.setupProgress();
        if (p.running || p.done) {
          setPhase('progress');
          return;
        }
      } catch {
        /* brak postępu — normalny start */
      }
      const d = await api.setupDefaults();
      setDefaults(d);
      setForm({
        access: 'local',
        serverName: d.suggestedServerName || d.addresses[0]?.ip || '',
        domain: '',
        publicIp: '',
        displayName: 'ChameleonTAK',
        organization: 'ChameleonTAK',
        ports: { ...d.ports },
        adminUser: 'admin',
        adminPass: '',
        adminPass2: '',
        platforms: d.platforms.some((p) => p.key === 'atak')
          ? (d.platforms.filter((p) => p.key === 'atak' || p.key === 'itak').map((p) => p.key) as PlatformKey[])
          : d.platforms.map((p) => p.key),
        mode: 'lan',
        staticIp: d.isPi,
        hotspotSsid: 'ChameleonTAK',
        hotspotPassword: randomPassword(),
        leEnabled: true,
        leEmail: '',
        ssh: true,
      });
    } catch (e) {
      setLoadError(errorMessage(e));
    }
  };

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    window.scrollTo(0, 0);
    panelRef.current?.querySelector<HTMLElement>('h2')?.focus({ preventScroll: true });
  }, [step]);

  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((f) => (f ? { ...f, [key]: value } : f));
    setErrors((e) => {
      if (!e[key as string]) return e;
      const copy = { ...e };
      delete copy[key as string];
      return copy;
    });
  };

  const header = (
    <header className="wizard-head">
      <div className="row" style={{ gap: 10 }}>
        <BrandMark className="brand-mark" />
        <span className="brand-name">ChameleonTAK</span>
      </div>
      <button
        type="button"
        className="icon-btn"
        onClick={toggleTheme}
        aria-label={theme === 'dark' ? 'Jasny motyw' : 'Ciemny motyw'}
      >
        {theme === 'dark' ? <Sun size={20} /> : <Moon size={20} />}
      </button>
    </header>
  );

  if (phase === 'progress') {
    return (
      <div className="wizard">
        {header}
        <SetupProgressView
          hotspot={form?.mode === 'hotspot' ? { ssid: form.hotspotSsid, password: form.hotspotPassword } : null}
          onBack={() => {
            if (form) {
              setPhase('form');
              setStep(STEPS.length - 1);
            } else {
              void load().then(() => setPhase('form'));
            }
          }}
        />
      </div>
    );
  }

  if (!defaults || !form) {
    return (
      <div className="wizard">
        {header}
        {loadError ? <ErrorState message={loadError} onRetry={load} /> : <Loading label="Wykrywanie sprzętu i sieci…" />}
      </div>
    );
  }

  const current = STEPS[step].key;
  const internet = form.access !== 'local';
  // Adres, pod którym telefony łączą się z serwerem (trafia do certyfikatu i konfiguracji).
  const address = (form.access === 'domain' ? form.domain : form.access === 'ip' ? form.publicIp : form.serverName).trim();
  const useLe = internet && form.leEnabled;
  const allPorts = PORT_FIELDS.filter((p) => p.key !== 'cotTcp' || form.ports.cotTcpEnabled).map((p) => form.ports[p.key]);

  const validate = (key: StepKey): Record<string, string> => {
    const e: Record<string, string> = {};
    if (key === 'server') {
      if (form.access === 'local') {
        if (!form.serverName.trim()) e.serverName = 'Podaj adres serwera.';
        else if (!validServerName(form.serverName)) e.serverName = 'Użyj adresu IP albo nazwy domenowej (bez spacji i http://).';
      } else if (form.access === 'domain') {
        if (!looksPublicDomain(form.domain)) e.domain = 'Podaj publiczną nazwę domenową, np. tak.twojafirma.pl (bez http://).';
      } else if (!isPublicIp(form.publicIp)) {
        e.publicIp = 'Podaj publiczny adres IP routera (nie adres z sieci lokalnej ani 100.64.x.x).';
      }
      if (useLe && form.leEmail.trim() && !validEmail(form.leEmail)) e.leEmail = 'Niepoprawny adres e-mail.';
      if (!form.displayName.trim()) e.displayName = 'Podaj nazwę wyświetlaną.';
    }
    if (key === 'ports') {
      PORT_FIELDS.forEach((p) => {
        const v = form.ports[p.key];
        if (!Number.isInteger(v) || v < 1 || v > 65535) e[`port_${p.key}`] = 'Port 1–65535.';
      });
      const seen = new Set<number>();
      allPorts.forEach((v) => {
        if (seen.has(v)) e.ports = `Port ${v} jest użyty dwa razy. Każda usługa potrzebuje własnego portu.`;
        seen.add(v);
      });
    }
    if (key === 'admin') {
      if (!/^[a-zA-Z0-9._-]{2,32}$/.test(form.adminUser)) e.adminUser = 'Od 2 do 32 znaków: litery, cyfry, kropka, myślnik.';
      if (form.adminPass.length < 8) e.adminPass = 'Hasło musi mieć co najmniej 8 znaków.';
      else if (form.adminPass !== form.adminPass2) e.adminPass2 = 'Hasła nie są takie same.';
    }
    if (key === 'platforms' && form.platforms.length === 0) e.platforms = 'Wybierz co najmniej jedną aplikację.';
    if (key === 'network') {
      if (form.mode === 'hotspot') {
        if (!form.hotspotSsid.trim() || form.hotspotSsid.length > 32) e.hotspotSsid = 'Nazwa sieci: od 1 do 32 znaków.';
        if (form.hotspotPassword.length < 8 || form.hotspotPassword.length > 63)
          e.hotspotPassword = 'Hasło Wi-Fi: od 8 do 63 znaków.';
      }
      if (internet && defaults.isPi && form.mode === 'hotspot')
        e.mode = 'Hotspot nie ma internetu. Przy dostępie z internetu wybierz istniejącą sieć albo w kroku „Adres serwera” wybierz sieć lokalną.';
    }
    return e;
  };

  const goNext = (e?: FormEvent) => {
    e?.preventDefault();
    const errs = validate(current);
    setErrors(errs);
    if (Object.keys(errs).length > 0) return;
    if (current === 'summary') {
      void submit();
      return;
    }
    setStep((s) => Math.min(s + 1, STEPS.length - 1));
  };

  const goTo = (i: number) => {
    // Można wrócić do dowolnego wcześniejszego kroku; do przodu tylko przez „Dalej”.
    if (i <= step) {
      setErrors({});
      setStep(i);
    }
  };

  const submit = async () => {
    // Ostatnia kontrola wszystkich kroków.
    for (let i = 0; i < STEPS.length; i++) {
      const errs = validate(STEPS[i].key);
      if (Object.keys(errs).length > 0) {
        setErrors(errs);
        setStep(i);
        return;
      }
    }
    const req: SetupRequest = {
      serverName: address,
      displayName: form.displayName.trim(),
      organization: form.organization.trim() || form.displayName.trim(),
      ports: form.ports,
      admin: { username: form.adminUser.trim(), password: form.adminPass },
      platforms: form.platforms,
      clientTime: new Date().toISOString(),
      letsEncrypt: { enabled: useLe, email: useLe ? form.leEmail.trim() : '' },
      network: {
        mode: defaults.isPi ? form.mode : 'lan',
        staticIp: defaults.isPi && form.mode === 'lan' && form.staticIp,
        hotspotSsid: form.hotspotSsid.trim(),
        hotspotPassword: form.mode === 'hotspot' ? form.hotspotPassword : '',
      },
      ssh: { enabled: defaults.isPi && form.ssh },
    };
    setSubmitting(true);
    setSubmitError(null);
    try {
      await api.setup(req);
      setPhase('progress');
    } catch (e) {
      setSubmitError(errorMessage(e));
    } finally {
      setSubmitting(false);
    }
  };

  const err = (k: string) => errors[k] ?? null;
  const portLabel = (k: keyof Ports) => PORT_FIELDS.find((p) => p.key === k)?.label ?? k;

  let body: ReactNode;
  switch (current) {
    case 'welcome':
      body = (
        <div className="stack-lg">
          <img className="wizard-logo" src="/logo-full.png" alt="ChameleonTAK" width={900} height={749} />
          <div className="stack">
            <h2 tabIndex={-1} className="wizard-hero">
              Uruchommy Twój serwer TAK
            </h2>
            <p className="muted measure wizard-lead">
              Kilka pytań i gotowe. Certyfikaty, usługi, zapora i sieć ustawią się same. Całość zajmuje około trzech minut.
            </p>
          </div>
          <dl className="kv">
            <dt>Urządzenie</dt>
            <dd>{defaults.isPi ? 'Raspberry Pi' : 'Komputer'} „{defaults.hostname}”</dd>
            <dt>Wykryte adresy</dt>
            <dd>{defaults.addresses.length ? defaults.addresses.map((a) => a.ip).join(', ') : 'brak — sprawdź kabel lub Wi-Fi'}</dd>
            <dt>Zegar</dt>
            <dd>
              {defaults.ntpSynced
                ? `zsynchronizowany (${fmtTime(defaults.serverTime)})`
                : 'brak synchronizacji — ustawimy go według tego urządzenia'}
            </dd>
          </dl>
        </div>
      );
      break;

    case 'server':
      body = (
        <div className="stack-lg">
          <div className="stack-sm">
            <h2 tabIndex={-1}>Adres serwera</h2>
            <p className="muted measure">Skąd telefony będą łączyć się z serwerem?</p>
          </div>
          <AccessChoice value={form.access} onChange={(m) => set('access', m)} />
          {form.access === 'local' && (
            <Field label="Adres w sieci lokalnej (IP albo nazwa)" htmlFor="s-name" error={err('serverName')}>
              <input
                id="s-name"
                className="input input-code"
                value={form.serverName}
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                aria-invalid={!!err('serverName')}
                onChange={(e) => set('serverName', e.target.value)}
              />
            </Field>
          )}
          {form.access === 'domain' && (
            <Field label="Domena" htmlFor="s-domain" error={err('domain')} hint="Bez http:// i bez portu, np. tak.twojafirma.pl.">
              <input
                id="s-domain"
                className="input input-code"
                value={form.domain}
                placeholder="tak.twojafirma.pl"
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                aria-invalid={!!err('domain')}
                onChange={(e) => set('domain', e.target.value.trim().toLowerCase())}
              />
            </Field>
          )}
          {form.access === 'ip' && (
            <div className="stack-sm">
              <Field label="Publiczny adres IP routera" htmlFor="s-pubip" error={err('publicIp')}>
                <input
                  id="s-pubip"
                  className="input input-code"
                  value={form.publicIp}
                  inputMode="decimal"
                  autoCapitalize="none"
                  autoCorrect="off"
                  spellCheck={false}
                  aria-invalid={!!err('publicIp')}
                  onChange={(e) => set('publicIp', e.target.value.trim())}
                />
              </Field>
              <DetectIpButton
                detect={api.setupPublicIp}
                onFound={(r) => {
                  set('publicIp', r.ip);
                  if (!r.public) setErrors((e) => ({ ...e, publicIp: `Wykryto ${r.ip} — to nie jest publiczny adres (CGNAT?). Użyj domeny albo poproś dostawcę o publiczne IP.` }));
                }}
              />
            </div>
          )}
          {internet && (
            <>
              <PortForwarding mode={form.access} ports={form.ports} lanAddress={form.serverName} />
              <CheckRow
                checked={form.leEnabled}
                onChange={(v) => set('leEnabled', v)}
                label="Certyfikat Let’s Encrypt (zalecane)"
                hint={`Telefony zaufają serwerowi bez ostrzeżeń, a iTAK i TAKAware zarejestrują się kodem QR prosto z aplikacji.${
                  form.access === 'ip' ? ' Certyfikat dla adresu IP jest ważny 6 dni i odnawia się sam.' : ''
                }`}
              />
              {form.leEnabled && (
                <Field label="E-mail dla Let’s Encrypt (opcjonalnie)" htmlFor="s-le" error={err('leEmail')}>
                  <input
                    id="s-le"
                    className="input"
                    type="email"
                    autoComplete="email"
                    value={form.leEmail}
                    onChange={(e) => set('leEmail', e.target.value)}
                  />
                </Field>
              )}
            </>
          )}
          {form.access === 'local' && defaults.addresses.length > 0 && (
            <div className="stack-sm">
              <span className="small muted">Wykryte adresy tego urządzenia — stuknij, aby wybrać:</span>
              <div className="chips">
                {defaults.addresses.map((a) => (
                  <button
                    key={`${a.iface}-${a.ip}`}
                    type="button"
                    className={`chip${form.serverName === a.ip ? ' selected' : ''}`}
                    onClick={() => set('serverName', a.ip)}
                  >
                    <span className="code">{a.ip}</span>
                    <span className="faint xsmall">{a.iface}</span>
                  </button>
                ))}
              </div>
            </div>
          )}
          <Field
            label="Nazwa wyświetlana"
            htmlFor="s-display"
            error={err('displayName')}
            hint="Widoczna dla ludzi na stronie dołączania i w aplikacjach."
          >
            <input
              id="s-display"
              className="input"
              value={form.displayName}
              onChange={(e) => set('displayName', e.target.value)}
            />
          </Field>
          <Disclosure title="Zaawansowane">
            <Field label="Organizacja" htmlFor="s-org" hint="Wpisywana do certyfikatów.">
              <input
                id="s-org"
                className="input"
                value={form.organization}
                onChange={(e) => set('organization', e.target.value)}
              />
            </Field>
          </Disclosure>
        </div>
      );
      break;

    case 'ports':
      body = (
        <div className="stack-lg">
          <div className="stack-sm">
            <h2 tabIndex={-1}>Porty</h2>
            <p className="muted measure">
              Domyślne porty pasują do ATAK, iTAK, WinTAK i TAKAware. Zmieniaj je tylko wtedy, gdy coś innego już ich używa.
            </p>
          </div>
          <dl className="kv">
            {PORT_FIELDS.filter((p) => p.key !== 'cotTcp').map((p) => (
              <FragmentKV key={p.key} k={p.label} v={String(form.ports[p.key])} />
            ))}
            <dt>Strumień bez szyfrowania</dt>
            <dd>{form.ports.cotTcpEnabled ? form.ports.cotTcp : 'wyłączony'}</dd>
          </dl>
          {err('ports') && <Notice kind="danger">{err('ports')}</Notice>}
          <Disclosure title="Zaawansowane" defaultOpen={Object.keys(errors).length > 0}>
            <div className="stack">
              <div className="form-grid-ports">
                {PORT_FIELDS.map((p) => (
                  <Field key={p.key} label={p.label} htmlFor={`p-${p.key}`} hint={p.hint} error={err(`port_${p.key}`)}>
                    <input
                      id={`p-${p.key}`}
                      className="input input-code"
                      type="number"
                      inputMode="numeric"
                      min={1}
                      max={65535}
                      value={Number.isFinite(form.ports[p.key]) ? form.ports[p.key] : ''}
                      disabled={p.key === 'cotTcp' && !form.ports.cotTcpEnabled}
                      onChange={(e) => set('ports', { ...form.ports, [p.key]: parseInt(e.target.value, 10) })}
                    />
                  </Field>
                ))}
              </div>
              <CheckRow
                checked={form.ports.cotTcpEnabled}
                onChange={(v) => set('ports', { ...form.ports, cotTcpEnabled: v })}
                label="Włącz strumień bez szyfrowania (TCP)"
                hint="Niezalecane. Każdy w sieci może się połączyć bez certyfikatu."
              />
              <button type="button" className="btn btn-ghost btn-sm" style={{ alignSelf: 'flex-start' }} onClick={() => set('ports', { ...defaults.ports })}>
                <RefreshCw size={14} /> Przywróć domyślne
              </button>
            </div>
          </Disclosure>
        </div>
      );
      break;

    case 'admin':
      body = (
        <div className="stack-lg">
          <div className="stack-sm">
            <h2 tabIndex={-1}>Konto administratora</h2>
            <p className="muted measure">Tym kontem logujesz się do panelu. Zapisz hasło w bezpiecznym miejscu.</p>
          </div>
          <Field label="Nazwa użytkownika" htmlFor="a-user" error={err('adminUser')}>
            <input
              id="a-user"
              className="input"
              autoComplete="username"
              autoCapitalize="none"
              autoCorrect="off"
              value={form.adminUser}
              onChange={(e) => set('adminUser', e.target.value)}
            />
          </Field>
          <Field label="Hasło" htmlFor="a-pass" error={err('adminPass')} hint="Co najmniej 8 znaków.">
            <div className="input-with-btn">
              <input
                id="a-pass"
                className="input"
                type={showPass ? 'text' : 'password'}
                autoComplete="new-password"
                value={form.adminPass}
                aria-invalid={!!err('adminPass')}
                onChange={(e) => set('adminPass', e.target.value)}
              />
              <button
                type="button"
                className="icon-btn"
                onClick={() => setShowPass((v) => !v)}
                aria-label={showPass ? 'Ukryj hasło' : 'Pokaż hasło'}
              >
                {showPass ? <EyeOff size={18} /> : <Eye size={18} />}
              </button>
            </div>
          </Field>
          <Field label="Powtórz hasło" htmlFor="a-pass2" error={err('adminPass2')}>
            <input
              id="a-pass2"
              className="input"
              type={showPass ? 'text' : 'password'}
              autoComplete="new-password"
              value={form.adminPass2}
              aria-invalid={!!err('adminPass2')}
              onChange={(e) => set('adminPass2', e.target.value)}
            />
          </Field>
        </div>
      );
      break;

    case 'platforms':
      body = (
        <div className="stack-lg">
          <div className="stack-sm">
            <h2 tabIndex={-1}>Z jakich aplikacji korzysta zespół?</h2>
            <p className="muted measure">Dla wybranych aplikacji serwer przygotuje gotowe paczki konfiguracyjne. Możesz to zmienić później.</p>
          </div>
          {err('platforms') && <Notice kind="danger">{err('platforms')}</Notice>}
          <div className="choices">
            {defaults.platforms.map((p) => (
              <Choice
                key={p.key}
                checked={form.platforms.includes(p.key)}
                onChange={(on) =>
                  set('platforms', on ? [...form.platforms, p.key] : form.platforms.filter((x) => x !== p.key))
                }
                title={p.label}
                description={platformOs(p.key)}
              />
            ))}
          </div>
        </div>
      );
      break;

    case 'network':
      body = (
        <div className="stack-lg">
          <div className="stack-sm">
            <h2 tabIndex={-1}>Sieć</h2>
            <p className="muted measure">Jak telefony będą docierać do serwera?</p>
          </div>
          {!defaults.isPi ? (
            <Notice>Ustawienia sieci dotyczą tylko obrazu Raspberry Pi. Na tym komputerze sieć zostaje bez zmian.</Notice>
          ) : (
            <>
              <div className="choices">
                <Choice
                  type="radio"
                  name="netmode"
                  checked={form.mode === 'lan'}
                  onChange={() => set('mode', 'lan')}
                  icon={<Network size={18} />}
                  title="Istniejąca sieć (LAN/Wi-Fi)"
                  description="Serwer i telefony są w tej samej sieci, np. w biurze lub przez router."
                />
                <Choice
                  type="radio"
                  name="netmode"
                  checked={form.mode === 'hotspot'}
                  onChange={() => set('mode', 'hotspot')}
                  disabled={!defaults.wifiAvailable}
                  icon={<Wifi size={18} />}
                  title="Hotspot Raspberry Pi (teren, bez internetu)"
                  description={
                    defaults.wifiAvailable
                      ? 'Raspberry Pi tworzy własną sieć Wi-Fi. Telefony łączą się bezpośrednio z nią.'
                      : 'To urządzenie nie ma karty Wi-Fi.'
                  }
                />
              </div>
              {form.mode === 'lan' ? (
                <CheckRow
                  checked={form.staticIp}
                  onChange={(v) => set('staticIp', v)}
                  label="Zachowaj obecny adres IP na stałe"
                  hint="Zalecane. Adres serwera nie zmieni się po restarcie routera, więc telefony nie stracą połączenia."
                />
              ) : (
                <div className="stack">
                  <div className="form-grid">
                    <Field label="Nazwa sieci Wi-Fi" htmlFor="n-ssid" error={err('hotspotSsid')}>
                      <input id="n-ssid" className="input" value={form.hotspotSsid} onChange={(e) => set('hotspotSsid', e.target.value)} />
                    </Field>
                    <Field label="Hasło Wi-Fi" htmlFor="n-pass" error={err('hotspotPassword')} hint="Od 8 do 63 znaków.">
                      <input
                        id="n-pass"
                        className="input input-code"
                        value={form.hotspotPassword}
                        autoCapitalize="none"
                        autoCorrect="off"
                        onChange={(e) => set('hotspotPassword', e.target.value)}
                      />
                    </Field>
                  </div>
                  {form.serverName !== HOTSPOT_IP && (
                    <Notice kind="warn">
                      <div className="stack-sm">
                        <span>
                          W sieci hotspotu telefony widzą serwer pod adresem <strong className="code">{HOTSPOT_IP}</strong>, a nie{' '}
                          <span className="code">{form.serverName}</span>.
                        </span>
                        <button type="button" className="btn btn-sm" style={{ alignSelf: 'flex-start' }} onClick={() => set('serverName', HOTSPOT_IP)}>
                          Użyj adresu {HOTSPOT_IP}
                        </button>
                      </div>
                    </Notice>
                  )}
                </div>
              )}
            </>
          )}

          {err('mode') && <Notice kind="danger">{err('mode')}</Notice>}

          {defaults.isPi && <hr style={{ margin: 0 }} />}

          {defaults.isPi && (
            <CheckRow
              checked={form.ssh}
              onChange={(v) => set('ssh', v)}
              label="Zdalny dostęp SSH"
              hint="Włączony domyślnie (konto tak). Hasło konta tak zostanie zmienione na hasło administratora."
            />
          )}
        </div>
      );
      break;

    case 'summary': {
      const platformsText = defaults.platforms
        .filter((p) => form.platforms.includes(p.key))
        .map((p) => p.label)
        .join(', ');
      body = (
        <div className="stack-lg">
          <div className="stack-sm">
            <h2 tabIndex={-1}>Wszystko gotowe do uruchomienia</h2>
            <p className="muted measure">Sprawdź ustawienia. Każde z nich możesz później zmienić w panelu.</p>
          </div>
          {submitError && <Notice kind="danger">{submitError}</Notice>}
          <div className="summary-list">
            <SummaryRow label="Adres serwera" onEdit={() => goTo(1)}>
              <span className="code">{address}</span>
              <span className="faint"> — {form.displayName}</span>
              {internet && (
                <>
                  <br />
                  {form.access === 'domain' ? 'dostęp z internetu przez domenę' : 'dostęp z internetu przez publiczny adres IP'}
                  {useLe && `; Let’s Encrypt${form.leEmail.trim() ? ` (${form.leEmail.trim()})` : ''}`}
                </>
              )}
            </SummaryRow>
            <SummaryRow label="Porty" onEdit={() => goTo(2)}>
              {[
                `${portLabel('cotTls')}: ${form.ports.cotTls}`,
                `${portLabel('enroll')}: ${form.ports.enroll}`,
                `${portLabel('http')}: ${form.ports.http}`,
              ].join(', ')}
              {form.ports.cotTcpEnabled && <span className="text-warn">, TCP bez szyfrowania: {form.ports.cotTcp}</span>}
            </SummaryRow>
            <SummaryRow label="Administrator" onEdit={() => goTo(3)}>
              {form.adminUser}
            </SummaryRow>
            <SummaryRow label="Aplikacje" onEdit={() => goTo(4)}>
              {platformsText}
            </SummaryRow>
            <SummaryRow label="Sieć" onEdit={() => goTo(5)}>
              {!defaults.isPi
                ? 'bez zmian'
                : form.mode === 'lan'
                  ? `istniejąca sieć${form.staticIp ? ', stały adres IP' : ''}`
                  : `hotspot „${form.hotspotSsid}”, hasło ${form.hotspotPassword}`}
              {defaults.isPi && form.ssh && '; SSH włączony'}
            </SummaryRow>
          </div>
          {defaults.isPi && form.mode === 'hotspot' && (
            <Notice kind="warn">
              Po uruchomieniu Raspberry Pi przełączy się na własną sieć Wi-Fi „{form.hotspotSsid}”. Połącz z nią to urządzenie,
              żeby dokończyć konfigurację.
            </Notice>
          )}
        </div>
      );
      break;
    }
  }

  const isLast = current === 'summary';

  return (
    <div className="wizard">
      {header}
      <div className="wizard-body">
        <ol className="wizard-rail" aria-label="Kroki kreatora">
          {STEPS.map((s, i) => (
            <li key={s.key}>
              <button
                type="button"
                className={`rail-step${i === step ? ' current' : ''}${i < step ? ' done' : ''}`}
                onClick={() => goTo(i)}
                disabled={i > step}
                aria-current={i === step ? 'step' : undefined}
              >
                <span className="rail-num">{i < step ? <CircleCheck size={16} /> : i + 1}</span>
                <span>{s.label}</span>
              </button>
            </li>
          ))}
        </ol>

        <div className="wizard-mobile-progress" aria-hidden="true">
          <div className="row-between xsmall muted">
            <span>
              Krok {step + 1} z {STEPS.length}
            </span>
            <span>{STEPS[step].label}</span>
          </div>
          <div className="meter">
            <span style={{ width: `${((step + 1) / STEPS.length) * 100}%`, background: 'var(--accent)' }} />
          </div>
        </div>

        <form className="plate wizard-panel" ref={panelRef} onSubmit={goNext} noValidate>
          {body}
          <div className="wizard-nav">
            {step > 0 ? (
              <button type="button" className="btn btn-ghost" onClick={() => goTo(step - 1)} disabled={submitting}>
                <ArrowLeft size={18} /> Wstecz
              </button>
            ) : (
              <span />
            )}
            <button type="submit" className={`btn btn-primary${isLast || step === 0 ? ' btn-lg' : ''}`} disabled={submitting}>
              {submitting ? (
                <LoaderCircle size={20} className="spinner" />
              ) : isLast ? (
                <Power size={20} />
              ) : null}
              {step === 0 ? 'Zaczynamy' : isLast ? 'Uruchom serwer' : 'Dalej'}
              {!isLast && <ArrowRight size={18} />}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function FragmentKV({ k, v }: { k: string; v: string }) {
  return (
    <>
      <dt>{k}</dt>
      <dd className="code">{v}</dd>
    </>
  );
}

function SummaryRow({ label, children, onEdit }: { label: string; children: ReactNode; onEdit: () => void }) {
  return (
    <div className="summary-row">
      <div className="grow">
        <div className="xsmall muted">{label}</div>
        <div>{children}</div>
      </div>
      <button type="button" className="btn btn-ghost btn-sm" onClick={onEdit}>
        Zmień
      </button>
    </div>
  );
}

/* ---------- Postęp konfiguracji ---------- */

function SetupProgressView({
  hotspot,
  onBack,
}: {
  hotspot: { ssid: string; password: string } | null;
  onBack: () => void;
}) {
  const [progress, setProgress] = useState<SetupProgress | null>(null);
  const [failures, setFailures] = useState(0);

  useEffect(() => {
    let alive = true;
    let timer: number | undefined;
    const tick = async () => {
      try {
        const p = await api.setupProgress();
        if (!alive) return;
        setProgress(p);
        setFailures(0);
        if (p.done || (!p.running && p.error)) return; // koniec — przestań odpytywać
      } catch {
        if (!alive) return;
        setFailures((f) => f + 1);
      }
      timer = window.setTimeout(tick, 1000);
    };
    void tick();
    return () => {
      alive = false;
      window.clearTimeout(timer);
    };
  }, []);

  const panelUrl = progress?.panelUrl ?? '';
  const sameOrigin = useMemo(() => {
    if (!panelUrl) return true;
    try {
      return new URL(panelUrl, window.location.href).origin === window.location.origin;
    } catch {
      return true;
    }
  }, [panelUrl]);

  const goLogin = () => {
    if (!sameOrigin && panelUrl) {
      window.location.href = new URL('/login', panelUrl).toString();
    } else {
      // Pełne przeładowanie: status serwera ma już setupDone=true.
      window.location.assign('/login');
    }
  };

  const failed = !!progress && !progress.running && !progress.done && !!progress.error;
  const done = !!progress?.done;
  const steps = progress?.steps ?? [];
  const doneCount = steps.filter((s) => s.state === 'done' || s.state === 'skipped').length;

  return (
    <div className="wizard-body wizard-body-single">
      <div className="plate wizard-panel progress-panel" aria-live="polite">
        <div className="stack-sm">
          <h2>{done ? 'Serwer działa' : failed ? 'Konfiguracja przerwana' : 'Konfigurowanie serwera…'}</h2>
          <p className="muted">
            {done
              ? 'Wszystko gotowe. Zaloguj się do panelu i dodaj pierwszych użytkowników.'
              : failed
                ? 'Jeden z kroków się nie powiódł. Popraw ustawienia i spróbuj ponownie.'
                : 'Nie zamykaj tej strony. To potrwa około minuty.'}
          </p>
        </div>

        {steps.length > 0 && (
          <div className="meter" aria-hidden="true">
            <span
              style={{
                width: `${(doneCount / steps.length) * 100}%`,
                background: failed ? 'var(--danger)' : done ? 'var(--ok)' : 'var(--accent)',
              }}
            />
          </div>
        )}

        {!progress && failures === 0 && <Loading compact label="Łączenie…" />}

        <ol className="progress-steps">
          {steps.map((s) => (
            <li key={s.key} className={`progress-step state-${s.state}`}>
              {stepIcon(s.state)}
              <div className="grow">
                <div className="strong">{s.label}</div>
                {s.message && <div className={`small ${s.state === 'error' ? 'text-danger' : 'muted'}`}>{s.message}</div>}
              </div>
            </li>
          ))}
        </ol>

        {failures >= 3 && !done && (
          <Notice kind="warn">
            {hotspot ? (
              <>
                Utracono połączenie z serwerem — Raspberry Pi prawdopodobnie przełączyło się na hotspot. Połącz to urządzenie z
                siecią Wi-Fi <strong>„{hotspot.ssid}”</strong> (hasło <strong className="code">{hotspot.password}</strong>), a
                następnie otwórz <strong className="code">http://{HOTSPOT_IP}</strong>.
              </>
            ) : (
              <>
                Utracono połączenie z serwerem. Jeśli zmienił się jego adres, otwórz panel pod nowym adresem
                {panelUrl ? (
                  <>
                    {' '}
                    <a href={panelUrl}>{panelUrl}</a>
                  </>
                ) : null}
                . Próbuję dalej…
              </>
            )}
          </Notice>
        )}

        {failed && (
          <>
            <Notice kind="danger">{progress?.error}</Notice>
            <button type="button" className="btn btn-primary btn-lg" onClick={onBack}>
              <ArrowLeft size={18} /> Wróć do ustawień
            </button>
          </>
        )}

        {done && (
          <div className="stack">
            {!sameOrigin && panelUrl && (
              <Notice kind="ok">
                Panel jest teraz dostępny pod adresem <a href={panelUrl}>{panelUrl}</a>.
                {panelUrl.startsWith('https:') && ' Przeglądarka może raz zapytać o zaufanie do certyfikatu serwera.'}
              </Notice>
            )}
            <button type="button" className="btn btn-primary btn-lg" onClick={goLogin}>
              Przejdź do logowania <ArrowRight size={18} />
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
