import { useRef, useState, type ReactNode } from 'react';
import { Archive, Download, LoaderCircle, Network, Power, RotateCcw, Save, TriangleAlert, Upload, Wifi } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { AccessMode, DdnsConfig, NetworkMode, PlatformKey, Ports, Settings } from '../api/types';
import { AccessChoice, CertStatusView, DetectIpButton, PortForwarding } from '../components/PublicAccess';
import { DdnsFields, DdnsStatusView } from '../components/Ddns';
import { PortTest } from '../components/PortTest';
import { ZeroTierPanel } from '../components/ZeroTier';
import { AccessListForm, PasswordForm } from '../components/PanelSecurity';
import { accessModeOf, isPublicIp, looksPublicDomain, validEmail } from '../lib/access';
import { useSession } from '../app/session';
import { useConfirm } from '../components/Confirm';
import { useToast } from '../components/Toast';
import { RoleSelect, TeamSelect } from '../components/UserDialogs';
import { AsyncView, CheckRow, Choice, Disclosure, Field, Notice, PageHeader } from '../components/ui';
import { PLATFORMS } from '../lib/tak';
import { useApi } from '../lib/useApi';

const DEFAULT_TILES = 'https://tile.openstreetmap.org/{z}/{x}/{y}.png';

const SECTIONS = [
  { id: 'serwer', label: 'Serwer i dostęp' },
  { id: 'test-portow', label: 'Test portów' },
  { id: 'bezpieczenstwo', label: 'Bezpieczeństwo panelu' },
  { id: 'porty', label: 'Porty' },
  { id: 'aplikacje', label: 'Aplikacje' },
  { id: 'profil', label: 'Profil urządzeń' },
  { id: 'certyfikaty', label: 'Certyfikaty i kody' },
  { id: 'siec', label: 'Sieć' },
  { id: 'system', label: 'System' },
];

/** Sekcje wspólne dla urządzenia — widoczne tylko przy serwerze głównym. */
const HOST_SECTIONS = ['test-portow', 'bezpieczenstwo', 'siec', 'system'];

type SaveFn = (patch: Partial<Settings>) => Promise<boolean>;

export default function SettingsPage() {
  const state = useApi(api.settings, []);
  const session = useSession();
  const toast = useToast();

  const save: SaveFn = async (patch) => {
    try {
      const res = await api.updateSettings(patch);
      if (res?.settings) {
        state.setData(res.settings);
        session.setSettings(res.settings);
      } else {
        void state.reload();
        void session.reloadSettings();
      }
      toast.ok('Zapisano ustawienia.');
      if (res?.certsRegenerated) toast.warn('Wygenerowano nowy certyfikat serwera. Urządzenia mogą wymagać nowych kodów QR.');
      if (res?.restarted) toast.info('Usługi serwera zostały uruchomione ponownie.');
      return true;
    } catch (e) {
      toast.error(errorMessage(e));
      return false;
    }
  };

  return (
    <>
      <PageHeader title="Ustawienia" />
      <nav className="chips settings-nav" aria-label="Sekcje ustawień">
        {SECTIONS.filter((s) => state.data?.main !== false || !HOST_SECTIONS.includes(s.id)).map((s) => (
          <a key={s.id} href={`#${s.id}`} className="chip">
            {s.label}
          </a>
        ))}
      </nav>
      <AsyncView state={state}>
        {(s) => (
          <div className="stack-lg settings">
            {!s.main && (
              <Notice>
                To ustawienia serwera <strong>{s.displayName}</strong>. Panel, sieć, system, bezpieczeństwo panelu i test portów są
                wspólne dla wszystkich serwerów — znajdziesz je po przełączeniu na serwer główny.
              </Notice>
            )}
            <ServerSection s={s} save={save} />
            {s.main && <PortTestSection />}
            {s.main && <SecuritySection s={s} save={save} />}
            <PortsSection s={s} save={save} />
            <PlatformsSection s={s} save={save} />
            <ProfileSection s={s} save={save} />
            <CertsSection s={s} save={save} />
            {s.main && <NetworkSection />}
            {s.main && <SystemSection />}
          </div>
        )}
      </AsyncView>
    </>
  );
}

function Section({
  id,
  title,
  description,
  children,
  onSave,
  dirty,
  saving,
}: {
  id: string;
  title: string;
  description?: ReactNode;
  children: ReactNode;
  onSave?: () => void;
  dirty?: boolean;
  saving?: boolean;
}) {
  const body = (
    <>
      <div className="settings-head">
        <h2>{title}</h2>
        {description && <p className="muted small measure">{description}</p>}
      </div>
      <div className="stack">{children}</div>
      {onSave && (
        <div className="settings-foot">
          <button type="submit" className="btn btn-primary" disabled={!dirty || saving}>
            {saving ? <LoaderCircle size={18} className="spinner" /> : <Save size={18} />} Zapisz
          </button>
        </div>
      )}
    </>
  );
  return onSave ? (
    <form
      id={id}
      className="card card-pad-lg settings-section"
      onSubmit={(e) => {
        e.preventDefault();
        onSave();
      }}
      noValidate
    >
      {body}
    </form>
  ) : (
    <section id={id} className="card card-pad-lg settings-section">
      {body}
    </section>
  );
}

function useSaver(save: SaveFn) {
  const [saving, setSaving] = useState(false);
  const run = async (patch: Partial<Settings>) => {
    setSaving(true);
    try {
      return await save(patch);
    } finally {
      setSaving(false);
    }
  };
  return { saving, run };
}

/* ---------- Serwer ---------- */
function ServerSection({ s, save }: { s: Settings; save: SaveFn }) {
  const initialMode: AccessMode = s.zerotier.ips.includes(s.serverName) ? 'zerotier' : accessModeOf(s.serverName);
  const [access, setAccess] = useState<AccessMode>(initialMode);
  // Osobny adres dla każdego trybu, żeby przełączanie trybów nie gubiło wpisanych wartości.
  const [names, setNames] = useState<Record<AccessMode, string>>(() => ({
    local: initialMode === 'local' ? s.serverName : '',
    domain: initialMode === 'domain' ? s.serverName : '',
    ip: initialMode === 'ip' ? s.serverName : '',
    zerotier: initialMode === 'zerotier' ? s.serverName : '',
  }));
  const [leEnabled, setLeEnabled] = useState(s.letsEncrypt.enabled || accessModeOf(s.serverName) === 'local');
  const [leEmail, setLeEmail] = useState(s.letsEncrypt.email);
  const [displayName, setDisplayName] = useState(s.displayName);
  const [organization, setOrganization] = useState(s.organization);
  const [ddns, setDdns] = useState<DdnsConfig>({ ...s.ddns });
  const [error, setError] = useState<string | null>(null);
  const status = useApi(api.publicAccess, [s.serverName, s.letsEncrypt.enabled], 15000);
  const sys = useApi(api.system, []);
  const { saving, run } = useSaver(save);
  const confirm = useConfirm();
  const serverName = names[access].trim();
  const internet = access === 'domain' || access === 'ip';
  const le = { enabled: internet && leEnabled, email: internet && leEnabled ? leEmail.trim() : s.letsEncrypt.email };
  const nameChanged = serverName !== s.serverName;
  // Dynamiczny DNS ma sens tylko z domeną; w innych trybach wyłączamy go przy zapisie.
  const ddnsOut: DdnsConfig = access === 'domain' ? ddns : { ...ddns, provider: '' };
  const ddnsChanged =
    ddnsOut.provider !== s.ddns.provider || ddnsOut.token !== s.ddns.token || ddnsOut.url !== s.ddns.url;
  const dirty =
    nameChanged ||
    ddnsChanged ||
    le.enabled !== s.letsEncrypt.enabled ||
    le.email !== s.letsEncrypt.email ||
    displayName !== s.displayName ||
    organization !== s.organization;
  const lanAddress = sys.data?.addresses[0]?.ip;

  const submit = async () => {
    if (access === 'local' && (!serverName || /\s/.test(serverName))) return setError('Podaj adres serwera w sieci lokalnej.');
    if (access === 'domain' && !looksPublicDomain(serverName)) return setError('Podaj publiczną nazwę domenową, np. tak.twojafirma.pl.');
    if (access === 'ip' && !isPublicIp(serverName)) return setError('Podaj publiczny adres IP routera (nie adres z sieci lokalnej ani 100.64.x.x).');
    if (access === 'zerotier' && !serverName) return setError('Dołącz do sieci ZeroTier i wybierz jej adres dla serwera.');
    if (le.enabled && le.email && !validEmail(le.email)) return setError('Niepoprawny adres e-mail dla Let’s Encrypt.');
    if (ddnsOut.provider === 'duckdns' && !/\.duckdns\.org$/i.test(serverName))
      return setError('Dla DuckDNS domena musi kończyć się na .duckdns.org.');
    if ((ddnsOut.provider === 'duckdns' || ddnsOut.provider === 'cloudflare') && !ddnsOut.token)
      return setError('Podaj token dostawcy dynamicznego DNS.');
    if (ddnsOut.provider === 'url' && !/^https?:\/\/\S+$/.test(ddnsOut.url))
      return setError('Adres aktualizacji DNS musi zaczynać się od https://.');
    setError(null);
    if (nameChanged) {
      const ok = await confirm({
        title: 'Zmienić adres serwera?',
        message: (
          <>
            Serwer wygeneruje nowy certyfikat dla adresu <strong className="code">{serverName.trim()}</strong>. Urządzenia
            skonfigurowane na stary adres mogą wymagać ponownej konfiguracji (nowych kodów QR).
          </>
        ),
        confirmLabel: 'Zmień adres',
      });
      if (!ok) return;
    }
    if (
      await run({ serverName, letsEncrypt: le, displayName: displayName.trim(), organization: organization.trim(), ddns: ddnsOut })
    ) {
      void status.reload();
    }
  };

  const setName = (v: string) => setNames({ ...names, [access]: v });

  return (
    <Section
      id="serwer"
      title="Serwer i dostęp"
      description="Skąd telefony łączą się z serwerem. Ten adres trafia do certyfikatu i do konfiguracji urządzeń."
      onSave={submit}
      dirty={dirty}
      saving={saving}
    >
      {error && <Notice kind="danger">{error}</Notice>}
      <AccessChoice value={access} onChange={setAccess} zerotier />
      {access === 'zerotier' && (
        <ZeroTierPanel main={s.main} address={names.zerotier} onAddress={(ip) => setNames({ ...names, zerotier: ip })} />
      )}
      {access === 'local' && (
        <Field label="Adres w sieci lokalnej" htmlFor="st-name" hint="IP albo nazwa, pod którą telefony w sieci lokalnej widzą serwer.">
          <input
            id="st-name"
            className="input input-code"
            value={names.local}
            autoCapitalize="none"
            autoCorrect="off"
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
      )}
      {access === 'domain' && (
        <Field label="Domena" htmlFor="st-domain" hint="Bez http:// i bez portu, np. tak.twojafirma.pl.">
          <input
            id="st-domain"
            className="input input-code"
            value={names.domain}
            placeholder="tak.twojafirma.pl"
            autoCapitalize="none"
            autoCorrect="off"
            onChange={(e) => setName(e.target.value.trim().toLowerCase())}
          />
        </Field>
      )}
      {access === 'ip' && (
        <div className="stack-sm">
          <Field label="Publiczny adres IP routera" htmlFor="st-pubip">
            <input
              id="st-pubip"
              className="input input-code"
              value={names.ip}
              inputMode="decimal"
              autoCapitalize="none"
              autoCorrect="off"
              onChange={(e) => setName(e.target.value.trim())}
            />
          </Field>
          <DetectIpButton
            detect={api.publicIp}
            onFound={(r) => {
              setNames({ ...names, ip: r.ip });
              setError(r.public ? null : `Wykryto ${r.ip} — to nie jest publiczny adres (CGNAT?). Użyj domeny albo poproś dostawcę o publiczne IP.`);
            }}
          />
        </div>
      )}
      {internet && (
        <>
          <PortForwarding mode={access} ports={s.ports} lanAddress={lanAddress} />
          {!s.main && (
            <Notice>
              Certyfikat Let’s Encrypt należy do serwera głównego. Ten serwer korzysta z niego, gdy ma ten sam adres.
            </Notice>
          )}
          {s.main && <CheckRow
            checked={leEnabled}
            onChange={setLeEnabled}
            label="Certyfikat Let’s Encrypt (zalecane)"
            hint={`Telefony zaufają serwerowi bez ostrzeżeń, a iTAK i TAKAware zarejestrują się kodem QR prosto z aplikacji.${
              access === 'ip' ? ' Certyfikat dla adresu IP jest ważny 6 dni i odnawia się sam.' : ''
            }`}
          />}
          {s.main && leEnabled && (
            <Field label="E-mail dla Let’s Encrypt (opcjonalnie)" htmlFor="st-le">
              <input id="st-le" className="input" type="email" value={leEmail} onChange={(e) => setLeEmail(e.target.value)} />
            </Field>
          )}
          {!nameChanged && status.data && <CertStatusView status={status.data.cert} />}
        </>
      )}
      {access === 'domain' && (
        <>
          <DdnsFields value={ddns} onChange={setDdns} domain={serverName} />
          {!nameChanged && !ddnsChanged && status.data && (
            <DdnsStatusView status={status.data.ddns} onChange={(d) => status.setData({ ...status.data!, ddns: d })} />
          )}
        </>
      )}
      {nameChanged && (
        <Notice kind="warn">
          Zmiana adresu wygeneruje nowy certyfikat serwera. Urządzenia skonfigurowane na stary adres mogą wymagać nowych kodów
          QR.
        </Notice>
      )}
      <div className="form-grid">
        <Field label="Nazwa wyświetlana" htmlFor="st-display">
          <input id="st-display" className="input" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </Field>
        <Field label="Organizacja" htmlFor="st-org" hint="Wpisywana do nowych certyfikatów.">
          <input id="st-org" className="input" value={organization} onChange={(e) => setOrganization(e.target.value)} />
        </Field>
      </div>
    </Section>
  );
}

/* ---------- Test portów ---------- */
function PortTestSection() {
  return (
    <Section
      id="test-portow"
      title="Test portów"
      description="Sprawdza, które porty działają, które przepuszcza zapora i które widać z internetu — i podpowiada, co otworzyć, a co zamknąć."
    >
      <PortTest />
    </Section>
  );
}

/* ---------- Bezpieczeństwo panelu ---------- */
function SecuritySection({ s, save }: { s: Settings; save: SaveFn }) {
  return (
    <Section id="bezpieczenstwo" title="Bezpieczeństwo panelu" description="Hasło administratora i urządzenia, z których można otworzyć panel.">
      <PasswordForm />
      <hr />
      <AccessListForm value={s.panelAccess} save={(panelAccess) => save({ panelAccess })} />
    </Section>
  );
}

/* ---------- Porty ---------- */
const PORT_FIELDS: { key: keyof Omit<Ports, 'cotTcpEnabled'>; label: string }[] = [
  { key: 'cotTls', label: 'Strumień danych (TLS)' },
  { key: 'marti', label: 'API serwera' },
  { key: 'enroll', label: 'Rejestracja urządzeń' },
  { key: 'http', label: 'Panel (HTTP)' },
  { key: 'https', label: 'Panel (HTTPS)' },
  { key: 'cotTcp', label: 'Strumień bez szyfrowania' },
];

function PortsSection({ s, save }: { s: Settings; save: SaveFn }) {
  const [ports, setPorts] = useState<Ports>({ ...s.ports });
  const [forceHttps, setForceHttps] = useState(s.forceHttps);
  const [error, setError] = useState<string | null>(null);
  const { saving, run } = useSaver(save);
  const dirty = JSON.stringify(ports) !== JSON.stringify(s.ports) || forceHttps !== s.forceHttps;

  const submit = async () => {
    const active = PORT_FIELDS.filter((p) => p.key !== 'cotTcp' || ports.cotTcpEnabled).map((p) => ports[p.key]);
    if (active.some((v) => !Number.isInteger(v) || v < 1 || v > 65535)) {
      setError('Każdy port musi być liczbą od 1 do 65535.');
      return;
    }
    if (new Set(active).size !== active.length) {
      setError('Ten sam port jest użyty dwa razy.');
      return;
    }
    setError(null);
    await run({ ports, forceHttps });
  };

  return (
    <Section
      id="porty"
      title="Porty"
      description="Zmiana portów uruchamia usługi ponownie, a połączone urządzenia na chwilę się rozłączą. Urządzenia skonfigurowane na stare porty będą potrzebowały nowej konfiguracji."
      onSave={submit}
      dirty={dirty}
      saving={saving}
    >
      {error && <Notice kind="danger">{error}</Notice>}
      <div className="form-grid-ports">
        {PORT_FIELDS.filter((p) => s.main || (p.key !== 'http' && p.key !== 'https')).map((p) => (
          <Field key={p.key} label={p.label} htmlFor={`sp-${p.key}`}>
            <input
              id={`sp-${p.key}`}
              className="input input-code"
              type="number"
              inputMode="numeric"
              min={1}
              max={65535}
              disabled={p.key === 'cotTcp' && !ports.cotTcpEnabled}
              value={Number.isFinite(ports[p.key]) ? ports[p.key] : ''}
              onChange={(e) => setPorts({ ...ports, [p.key]: parseInt(e.target.value, 10) })}
            />
          </Field>
        ))}
      </div>
      <CheckRow
        checked={ports.cotTcpEnabled}
        onChange={(v) => setPorts({ ...ports, cotTcpEnabled: v })}
        label="Strumień bez szyfrowania (TCP)"
        hint="Niezalecane. Każdy w sieci może się połączyć bez certyfikatu i trafia do grupy ogólnej."
      />
      {s.main && (
        <CheckRow
          checked={forceHttps}
          onChange={setForceHttps}
          label="Przekierowuj panel na HTTPS"
          hint="Przeglądarka może ostrzegać o certyfikacie, jeśli nie używasz Let’s Encrypt."
        />
      )}
    </Section>
  );
}

/* ---------- Aplikacje ---------- */
function PlatformsSection({ s, save }: { s: Settings; save: SaveFn }) {
  const [platforms, setPlatforms] = useState<PlatformKey[]>(s.platforms);
  const { saving, run } = useSaver(save);
  const dirty = platforms.length !== s.platforms.length || platforms.some((p) => !s.platforms.includes(p));

  return (
    <Section
      id="aplikacje"
      title="Aplikacje"
      description="Dla których aplikacji przygotowywać konfiguracje. Ogranicza wybór przy dodawaniu urządzeń."
      onSave={() => {
        if (platforms.length > 0) void run({ platforms });
      }}
      dirty={dirty && platforms.length > 0}
      saving={saving}
    >
      {platforms.length === 0 && <Notice kind="danger">Wybierz co najmniej jedną aplikację.</Notice>}
      <div className="choices">
        {PLATFORMS.map((p) => (
          <Choice
            key={p.key}
            checked={platforms.includes(p.key)}
            onChange={(on) => setPlatforms(on ? [...platforms, p.key] : platforms.filter((x) => x !== p.key))}
            title={p.label}
            description={p.os}
          />
        ))}
      </div>
    </Section>
  );
}

/* ---------- Profil urządzeń ---------- */
function ProfileSection({ s, save }: { s: Settings; save: SaveFn }) {
  const [team, setTeam] = useState(s.profile.defaultTeam);
  const [role, setRole] = useState(s.profile.defaultRole);
  const [lock, setLock] = useState(s.profile.lockSettings);
  const [tiles, setTiles] = useState(s.mapTileUrl);
  const { saving, run } = useSaver(save);
  const dirty = team !== s.profile.defaultTeam || role !== s.profile.defaultRole || lock !== s.profile.lockSettings || tiles !== s.mapTileUrl;

  return (
    <Section
      id="profil"
      title="Profil urządzeń"
      description="Ustawienia wysyłane do aplikacji razem z konfiguracją."
      onSave={() =>
        void run({
          profile: { ...s.profile, defaultTeam: team, defaultRole: role, lockSettings: lock },
          mapTileUrl: tiles.trim() || DEFAULT_TILES,
        })
      }
      dirty={dirty}
      saving={saving}
    >
      <div className="form-grid">
        <Field label="Domyślny zespół (kolor)" htmlFor="pf-team" hint="Dla nowych użytkowników.">
          <TeamSelect id="pf-team" value={team} onChange={setTeam} />
        </Field>
        <Field label="Domyślna rola" htmlFor="pf-role">
          <RoleSelect id="pf-role" value={role} onChange={setRole} />
        </Field>
      </div>
      <CheckRow
        checked={lock}
        onChange={setLock}
        label="Zablokuj ustawienia w aplikacjach"
        hint="Użytkownicy nie zmienią serwera, zespołu ani roli na swoich urządzeniach."
      />
      <Field
        label="Podkład mapy w panelu"
        htmlFor="pf-tiles"
        hint={
          <>
            Adres kafelków z {'{z}'}, {'{x}'}, {'{y}'}. Bez internetu mapa w panelu będzie pusta, chyba że podasz lokalny serwer kafelków.{' '}
            {tiles !== DEFAULT_TILES && (
              <button type="button" className="linklike" onClick={() => setTiles(DEFAULT_TILES)}>
                Przywróć OpenStreetMap
              </button>
            )}
          </>
        }
      >
        <input
          id="pf-tiles"
          className="input input-code"
          value={tiles}
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          onChange={(e) => setTiles(e.target.value)}
        />
      </Field>
    </Section>
  );
}

/* ---------- Certyfikaty i kody ---------- */
function CertsSection({ s, save }: { s: Settings; save: SaveFn }) {
  const [certDays, setCertDays] = useState(s.certValidityDays);
  const [tokenHours, setTokenHours] = useState(s.tokenValidityHours);
  const [certPassword, setCertPassword] = useState(s.certPassword);
  const [error, setError] = useState<string | null>(null);
  const { saving, run } = useSaver(save);
  const dirty = certDays !== s.certValidityDays || tokenHours !== s.tokenValidityHours || certPassword !== s.certPassword;

  const submit = () => {
    if (!Number.isInteger(certDays) || certDays < 1 || certDays > 3650) return setError('Ważność certyfikatu: od 1 do 3650 dni.');
    if (!Number.isInteger(tokenHours) || tokenHours < 1 || tokenHours > 8760) return setError('Ważność kodu: od 1 do 8760 godzin.');
    if (certPassword.length < 4) return setError('Hasło paczek certyfikatów musi mieć co najmniej 4 znaki.');
    setError(null);
    void run({ certValidityDays: certDays, tokenValidityHours: tokenHours, certPassword });
  };

  const tokenDays = Number.isFinite(tokenHours) && tokenHours >= 24 ? ` (${Math.round((tokenHours / 24) * 10) / 10} dni)` : '';

  return (
    <Section id="certyfikaty" title="Certyfikaty i kody dołączenia" onSave={submit} dirty={dirty} saving={saving}>
      {error && <Notice kind="danger">{error}</Notice>}
      <div className="form-grid">
        <Field label="Ważność certyfikatu urządzenia (dni)" htmlFor="c-days" hint="Po tym czasie urządzenie trzeba skonfigurować ponownie.">
          <input
            id="c-days"
            className="input"
            type="number"
            inputMode="numeric"
            min={1}
            value={Number.isFinite(certDays) ? certDays : ''}
            onChange={(e) => setCertDays(parseInt(e.target.value, 10))}
          />
        </Field>
        <Field label="Ważność kodu QR (godziny)" htmlFor="c-hours" hint={`Jak długo działa kod dołączenia${tokenDays}.`}>
          <input
            id="c-hours"
            className="input"
            type="number"
            inputMode="numeric"
            min={1}
            value={Number.isFinite(tokenHours) ? tokenHours : ''}
            onChange={(e) => setTokenHours(parseInt(e.target.value, 10))}
          />
        </Field>
      </div>
      <Disclosure title="Zaawansowane">
        <Field
          label="Hasło paczek certyfikatów (.p12)"
          htmlFor="c-pass"
          hint="Aplikacje TAK oczekują zwykle „atakatak”. Zmieniaj tylko, jeśli wiesz, po co."
        >
          <input
            id="c-pass"
            className="input input-code"
            value={certPassword}
            autoCapitalize="none"
            autoCorrect="off"
            onChange={(e) => setCertPassword(e.target.value)}
          />
        </Field>
      </Disclosure>
    </Section>
  );
}

/* ---------- Sieć ---------- */
function NetworkSection() {
  const sys = useApi(api.system, []);
  return (
    <section id="siec" className="card card-pad-lg settings-section">
      <div className="settings-head">
        <h2>Sieć</h2>
        <p className="muted small measure">Jak telefony docierają do serwera. Dotyczy tylko Raspberry Pi.</p>
      </div>
      <AsyncView state={sys}>
        {(info) =>
          info.isPi ? (
            <NetworkForm currentMode={info.network.mode} currentSsid={info.network.ssid} />
          ) : (
            <Notice>Ten serwer nie działa na obrazie Raspberry Pi — siecią zarządza system operacyjny.</Notice>
          )
        }
      </AsyncView>
    </section>
  );
}

function NetworkForm({ currentMode, currentSsid }: { currentMode: NetworkMode; currentSsid: string }) {
  const toast = useToast();
  const confirm = useConfirm();
  const [mode, setMode] = useState<NetworkMode>(currentMode);
  const [wifiSsid, setWifiSsid] = useState(currentMode === 'lan' ? currentSsid : '');
  const [wifiPassword, setWifiPassword] = useState('');
  const [hotspotSsid, setHotspotSsid] = useState(currentMode === 'hotspot' ? currentSsid || 'ChameleonTAK' : 'ChameleonTAK');
  const [hotspotPassword, setHotspotPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    if (mode === 'hotspot') {
      if (!hotspotSsid.trim()) return setError('Podaj nazwę sieci hotspotu.');
      if (hotspotPassword.length < 8 || hotspotPassword.length > 63) return setError('Hasło Wi-Fi: od 8 do 63 znaków.');
    }
    if (mode === 'lan' && wifiSsid.trim() && wifiPassword && wifiPassword.length < 8) {
      return setError('Hasło Wi-Fi musi mieć co najmniej 8 znaków.');
    }
    setError(null);
    const ok = await confirm({
      title: 'Zmienić ustawienia sieci?',
      message:
        mode === 'hotspot'
          ? `Raspberry Pi utworzy sieć „${hotspotSsid.trim()}”. Połączenie z panelem zostanie przerwane — połącz się z tą siecią i otwórz http://10.42.0.1.`
          : 'Raspberry Pi przełączy się na wskazaną sieć. Połączenie z panelem może zostać przerwane, a adres serwera może się zmienić.',
      confirmLabel: 'Zastosuj',
      danger: true,
    });
    if (!ok) return;
    setBusy(true);
    try {
      await api.network({
        mode,
        wifiSsid: mode === 'lan' ? wifiSsid.trim() : '',
        wifiPassword: mode === 'lan' ? wifiPassword : '',
        hotspotSsid: mode === 'hotspot' ? hotspotSsid.trim() : '',
        hotspotPassword: mode === 'hotspot' ? hotspotPassword : '',
      });
      toast.ok('Zastosowano ustawienia sieci.');
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form
      className="stack"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
      noValidate
    >
      <p className="small">
        Teraz:{' '}
        <strong>{currentMode === 'hotspot' ? `hotspot „${currentSsid}”` : currentSsid ? `sieć Wi-Fi „${currentSsid}”` : 'sieć przewodowa / istniejąca'}</strong>
      </p>
      {error && <Notice kind="danger">{error}</Notice>}
      <div className="choices">
        <Choice
          type="radio"
          name="set-net"
          checked={mode === 'lan'}
          onChange={() => setMode('lan')}
          icon={<Network size={18} />}
          title="Istniejąca sieć (LAN/Wi-Fi)"
          description="Kabel Ethernet albo istniejąca sieć Wi-Fi."
        />
        <Choice
          type="radio"
          name="set-net"
          checked={mode === 'hotspot'}
          onChange={() => setMode('hotspot')}
          icon={<Wifi size={18} />}
          title="Hotspot Raspberry Pi"
          description="Własna sieć Wi-Fi w terenie, bez internetu."
        />
      </div>
      {mode === 'lan' ? (
        <div className="form-grid">
          <Field label="Sieć Wi-Fi (opcjonalnie)" htmlFor="nw-ssid" hint="Zostaw puste przy połączeniu kablem.">
            <input id="nw-ssid" className="input" value={wifiSsid} onChange={(e) => setWifiSsid(e.target.value)} />
          </Field>
          <Field label="Hasło Wi-Fi" htmlFor="nw-pass">
            <input
              id="nw-pass"
              className="input"
              type="password"
              autoComplete="off"
              value={wifiPassword}
              onChange={(e) => setWifiPassword(e.target.value)}
            />
          </Field>
        </div>
      ) : (
        <div className="form-grid">
          <Field label="Nazwa sieci hotspotu" htmlFor="nw-hs">
            <input id="nw-hs" className="input" value={hotspotSsid} onChange={(e) => setHotspotSsid(e.target.value)} />
          </Field>
          <Field label="Hasło hotspotu" htmlFor="nw-hp" hint="Od 8 do 63 znaków.">
            <input
              id="nw-hp"
              className="input input-code"
              value={hotspotPassword}
              autoCapitalize="none"
              autoCorrect="off"
              onChange={(e) => setHotspotPassword(e.target.value)}
            />
          </Field>
        </div>
      )}
      <div className="settings-foot">
        <button type="submit" className="btn btn-primary" disabled={busy}>
          {busy ? <LoaderCircle size={18} className="spinner" /> : <Save size={18} />} Zastosuj
        </button>
      </div>
    </form>
  );
}

/* ---------- System ---------- */
function SystemSection() {
  const toast = useToast();
  const confirm = useConfirm();
  const fileRef = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const reboot = async () => {
    const ok = await confirm({
      title: 'Uruchomić serwer ponownie?',
      message: 'Wszystkie urządzenia rozłączą się na około minutę i połączą się same, gdy serwer wstanie.',
      confirmLabel: 'Uruchom ponownie',
      danger: true,
    });
    if (!ok) return;
    setBusy('reboot');
    try {
      await api.reboot();
      toast.info('Serwer uruchamia się ponownie. Odśwież stronę za około minutę.');
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(null);
    }
  };

  const restore = async (file: File) => {
    const ok = await confirm({
      title: 'Przywrócić kopię zapasową?',
      message: (
        <>
          Obecne dane serwera (użytkownicy, certyfikaty, pliki) zostaną zastąpione zawartością pliku <strong>{file.name}</strong>.
          Serwer uruchomi się ponownie.
        </>
      ),
      confirmLabel: 'Przywróć',
      danger: true,
    });
    if (!ok) return;
    setBusy('restore');
    try {
      await api.restore(file);
      toast.ok('Przywrócono kopię zapasową. Za chwilę zaloguj się ponownie.');
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(null);
    }
  };

  const factoryReset = async () => {
    const ok = await confirm({
      title: 'Przywrócić ustawienia fabryczne?',
      message:
        'Wszystkie dane zostaną trwale usunięte: użytkownicy, urządzenia, certyfikaty, pliki i misje. Serwer wróci do kreatora pierwszego uruchomienia. Zrób wcześniej kopię zapasową.',
      confirmLabel: 'Usuń wszystko',
      danger: true,
      requireText: 'RESET',
    });
    if (!ok) return;
    setBusy('reset');
    try {
      await api.factoryReset();
      toast.ok('Przywrócono ustawienia fabryczne.');
      window.setTimeout(() => window.location.assign('/setup'), 1500);
    } catch (e) {
      toast.error(errorMessage(e));
      setBusy(null);
    }
  };

  return (
    <section id="system" className="card card-pad-lg settings-section">
      <div className="settings-head">
        <h2>System</h2>
      </div>
      <div className="system-actions">
        <div className="system-action">
          <div className="grow">
            <div className="strong">Kopia zapasowa</div>
            <div className="small muted">Plik .tar.gz z całą bazą, certyfikatami i plikami. Przechowuj go w bezpiecznym miejscu.</div>
          </div>
          <a className="btn" href={api.backupUrl} download>
            <Download size={18} /> Pobierz kopię
          </a>
        </div>
        <div className="system-action">
          <div className="grow">
            <div className="strong">Przywróć z kopii</div>
            <div className="small muted">Zastępuje wszystkie obecne dane zawartością kopii.</div>
          </div>
          <button type="button" className="btn" onClick={() => fileRef.current?.click()} disabled={!!busy}>
            {busy === 'restore' ? <LoaderCircle size={18} className="spinner" /> : <Upload size={18} />} Wybierz plik
          </button>
          <input
            ref={fileRef}
            type="file"
            accept=".gz,.tgz,application/gzip"
            hidden
            onChange={(e) => {
              const f = e.target.files?.[0];
              e.target.value = '';
              if (f) void restore(f);
            }}
          />
        </div>
        <div className="system-action">
          <div className="grow">
            <div className="strong">Uruchom ponownie</div>
            <div className="small muted">Restart całego urządzenia.</div>
          </div>
          <button type="button" className="btn" onClick={reboot} disabled={!!busy}>
            {busy === 'reboot' ? <LoaderCircle size={18} className="spinner" /> : <Power size={18} />} Uruchom ponownie
          </button>
        </div>
        <div className="system-action danger-zone">
          <div className="grow">
            <div className="strong row" style={{ gap: 6 }}>
              <TriangleAlert size={16} className="text-danger" /> Ustawienia fabryczne
            </div>
            <div className="small muted">Usuwa wszystkie dane i wraca do kreatora. Tego nie da się cofnąć.</div>
          </div>
          <button type="button" className="btn btn-danger" onClick={factoryReset} disabled={!!busy}>
            <RotateCcw size={18} /> Przywróć fabryczne
          </button>
        </div>
      </div>
      <p className="xsmall faint row" style={{ gap: 6 }}>
        <Archive size={14} /> Kopię warto pobrać przed każdą większą zmianą.
      </p>
    </section>
  );
}
