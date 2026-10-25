import { useState } from 'react';
import { Plus, Printer, Trash } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { NewDevice, PlatformKey, Settings, User } from '../api/types';
import { PLATFORMS, ROLES, TEAMS, platformLabel } from '../lib/tak';
import { countLabel } from '../lib/format';
import { Modal } from './Modal';
import { useToast } from './Toast';
import { Disclosure, Field, GroupPicker, Notice } from './ui';

export function defaultDeviceName(p: PlatformKey): string {
  switch (p) {
    case 'atak':
      return 'Telefon';
    case 'itak':
    case 'takaware':
      return 'iPhone';
    case 'wintak':
      return 'Komputer';
  }
}

export function enabledPlatforms(settings: Settings | null): PlatformKey[] {
  const list = settings?.platforms?.length ? settings.platforms : PLATFORMS.map((p) => p.key);
  return PLATFORMS.map((p) => p.key).filter((k) => list.includes(k));
}

const USERNAME_RE = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/;

export function TeamSelect({ id, value, onChange }: { id: string; value: string; onChange: (v: string) => void }) {
  return (
    <select id={id} className="select" value={value} onChange={(e) => onChange(e.target.value)}>
      {!TEAMS.some((t) => t.value === value) && <option value={value}>{value || '—'}</option>}
      {TEAMS.map((t) => (
        <option key={t.value} value={t.value}>
          {t.label} ({t.value})
        </option>
      ))}
    </select>
  );
}

export function RoleSelect({ id, value, onChange }: { id: string; value: string; onChange: (v: string) => void }) {
  return (
    <select id={id} className="select" value={value} onChange={(e) => onChange(e.target.value)}>
      {!ROLES.some((r) => r.value === value) && <option value={value}>{value || '—'}</option>}
      {ROLES.map((r) => (
        <option key={r.value} value={r.value}>
          {r.label}
        </option>
      ))}
    </select>
  );
}

export function PlatformSelect({
  id,
  value,
  onChange,
  platforms,
}: {
  id: string;
  value: PlatformKey;
  onChange: (v: PlatformKey) => void;
  platforms: PlatformKey[];
}) {
  return (
    <select id={id} className="select" value={value} onChange={(e) => onChange(e.target.value as PlatformKey)}>
      {platforms.map((p) => {
        const def = PLATFORMS.find((x) => x.key === p);
        return (
          <option key={p} value={p}>
            {platformLabel(p)} ({def?.os})
          </option>
        );
      })}
    </select>
  );
}

/* ---------- Dodaj użytkownika ---------- */
export function AddUserDialog({
  settings,
  groups,
  onClose,
  onCreated,
}: {
  settings: Settings | null;
  groups: string[];
  onClose: () => void;
  onCreated: (u: User) => void;
}) {
  const toast = useToast();
  const platforms = enabledPlatforms(settings);
  const [username, setUsername] = useState('');
  const [callsign, setCallsign] = useState('');
  const [team, setTeam] = useState(settings?.profile.defaultTeam || 'Cyan');
  const [role, setRole] = useState(settings?.profile.defaultRole || 'Team Member');
  const [password, setPassword] = useState('');
  const [selGroups, setSelGroups] = useState<string[]>([]);
  const [devices, setDevices] = useState<NewDevice[]>([{ platform: platforms[0], name: defaultDeviceName(platforms[0]) }]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [userErr, setUserErr] = useState<string | null>(null);

  const updateDevice = (i: number, patch: Partial<NewDevice>) => {
    setDevices((list) =>
      list.map((d, idx) => {
        if (idx !== i) return d;
        const next = { ...d, ...patch };
        // Nazwa domyślna podąża za platformą, dopóki nie zmieniono jej ręcznie.
        if (patch.platform && d.name === defaultDeviceName(d.platform)) next.name = defaultDeviceName(patch.platform);
        return next;
      }),
    );
  };

  const submit = async () => {
    const u = username.trim();
    if (!USERNAME_RE.test(u)) {
      setUserErr('Litery, cyfry, kropka, podkreślnik lub myślnik — bez spacji i polskich znaków.');
      return;
    }
    setUserErr(null);
    setBusy(true);
    setError(null);
    try {
      const created = await api.createUser({
        username: u,
        callsign: callsign.trim() || undefined,
        team,
        role,
        password: password || undefined,
        groups: selGroups,
        devices: devices.map((d) => ({ platform: d.platform, name: d.name.trim() || defaultDeviceName(d.platform) })),
      });
      toast.ok(`Dodano użytkownika ${created.callsign || created.username}.`);
      onCreated(created);
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  };

  return (
    <Modal
      title="Dodaj użytkownika"
      onClose={onClose}
      busy={busy}
      onSubmit={submit}
      wide
      footer={
        <>
          <button type="button" className="btn btn-ghost" onClick={onClose} disabled={busy}>
            Anuluj
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy}>
            Dodaj użytkownika
          </button>
        </>
      }
    >
      <div className="stack">
        {error && <Notice kind="danger">{error}</Notice>}
        <div className="form-grid">
          <Field label="Nazwa użytkownika" htmlFor="u-name" error={userErr} hint="Np. jan.kowalski">
            <input
              id="u-name"
              className="input"
              value={username}
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
              aria-invalid={!!userErr}
              onChange={(e) => setUsername(e.target.value)}
            />
          </Field>
          <Field label="Znak wywoławczy" htmlFor="u-cs" hint="Nazwa widoczna na mapie. Puste = jak nazwa użytkownika.">
            <input
              id="u-cs"
              className="input"
              value={callsign}
              placeholder={username.trim().toUpperCase()}
              onChange={(e) => setCallsign(e.target.value)}
            />
          </Field>
          <Field label="Zespół (kolor)" htmlFor="u-team">
            <TeamSelect id="u-team" value={team} onChange={setTeam} />
          </Field>
          <Field label="Rola" htmlFor="u-role">
            <RoleSelect id="u-role" value={role} onChange={setRole} />
          </Field>
        </div>

        <div className="field">
          <span className="field-label">Grupy</span>
          <GroupPicker all={groups} value={selGroups} onChange={setSelGroups} />
        </div>

        <div className="stack-sm">
          <span className="field-label">Urządzenia</span>
          {devices.map((d, i) => (
            <div key={i} className="device-input-row">
              <PlatformSelect id={`d-p-${i}`} value={d.platform} platforms={platforms} onChange={(p) => updateDevice(i, { platform: p })} />
              <input
                className="input"
                aria-label="Nazwa urządzenia"
                value={d.name}
                onChange={(e) => updateDevice(i, { name: e.target.value })}
              />
              <button
                type="button"
                className="icon-btn danger"
                aria-label="Usuń urządzenie"
                disabled={devices.length <= 1}
                onClick={() => setDevices((list) => list.filter((_, idx) => idx !== i))}
              >
                <Trash size={18} />
              </button>
            </div>
          ))}
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            style={{ alignSelf: 'flex-start' }}
            onClick={() => setDevices((list) => [...list, { platform: platforms[0], name: defaultDeviceName(platforms[0]) }])}
          >
            <Plus size={16} /> Kolejne urządzenie
          </button>
          <span className="field-hint">Każde urządzenie dostaje własny kod QR i certyfikat.</span>
        </div>

        <Disclosure title="Zaawansowane">
          <Field
            label="Hasło (opcjonalne)"
            htmlFor="u-pass"
            hint="Pozwala połączyć aplikację ręcznie loginem i hasłem zamiast kodem jednorazowym."
          >
            <input
              id="u-pass"
              className="input"
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
        </Disclosure>
      </div>
    </Modal>
  );
}

/* ---------- Dodaj wielu ---------- */
export function BulkAddDialog({
  settings,
  groups,
  onClose,
  onCreated,
}: {
  settings: Settings | null;
  groups: string[];
  onClose: () => void;
  onCreated: (users: User[]) => void;
}) {
  const toast = useToast();
  const navigate = useNavigate();
  const platforms = enabledPlatforms(settings);
  const [prefix, setPrefix] = useState('');
  const [count, setCount] = useState(10);
  const [startAt, setStartAt] = useState(1);
  const [platform, setPlatform] = useState<PlatformKey>(platforms[0]);
  const [team, setTeam] = useState(settings?.profile.defaultTeam || 'Cyan');
  const [selGroups, setSelGroups] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [created, setCreated] = useState<User[] | null>(null);

  const prefixOk = /^[a-zA-Z][a-zA-Z0-9._-]{0,30}$/.test(prefix.trim());
  const countOk = Number.isInteger(count) && count >= 1 && count <= 200;
  const startOk = Number.isInteger(startAt) && startAt >= 0;

  const submit = async () => {
    if (!prefixOk || !countOk || !startOk) {
      setError('Sprawdź pola: przedrostek (litery i cyfry), liczba od 1 do 200, numer początkowy od 0.');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const users = await api.bulkUsers({ prefix: prefix.trim(), count, startAt, groups: selGroups, platform, team });
      toast.ok(`Utworzono ${countLabel(users.length, 'użytkownika', 'użytkowników', 'użytkowników')}.`);
      onCreated(users);
      setCreated(users);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  if (created) {
    return (
      <Modal
        title="Użytkownicy utworzeni"
        onClose={onClose}
        footer={
          <>
            <button type="button" className="btn btn-ghost" onClick={onClose}>
              Zamknij
            </button>
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => navigate(`/print?users=${created.map((u) => u.id).join(',')}`)}
            >
              <Printer size={18} /> Drukuj karty
            </button>
          </>
        }
      >
        <div className="stack">
          <p>
            Utworzono {countLabel(created.length, 'użytkownika', 'użytkowników', 'użytkowników')}. Wydrukuj karty z kodami QR i
            rozdaj je zespołowi — każdy zeskanuje swój kod aparatem telefonu.
          </p>
          <div className="chips">
            {created.slice(0, 40).map((u) => (
              <span key={u.id} className="chip chip-sm">
                {u.callsign || u.username}
              </span>
            ))}
            {created.length > 40 && <span className="small muted">i {created.length - 40} więcej</span>}
          </div>
        </div>
      </Modal>
    );
  }

  const last = startAt + Math.max(count, 1) - 1;
  const preview = prefixOk && countOk && startOk ? `${prefix.trim()}${startAt}${count > 1 ? ` … ${prefix.trim()}${last}` : ''}` : '';

  return (
    <Modal
      title="Dodaj wielu użytkowników"
      onClose={onClose}
      busy={busy}
      onSubmit={submit}
      footer={
        <>
          <button type="button" className="btn btn-ghost" onClick={onClose} disabled={busy}>
            Anuluj
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy}>
            Utwórz {countOk ? countLabel(count, 'użytkownika', 'użytkowników', 'użytkowników') : 'użytkowników'}
          </button>
        </>
      }
    >
      <div className="stack">
        <p className="muted small">
          Szybki sposób na cały zespół: każdy dostaje jedno urządzenie i własny kod QR. Nazwy powstają z przedrostka i numeru.
        </p>
        {error && <Notice kind="danger">{error}</Notice>}
        <div className="form-grid">
          <Field label="Przedrostek" htmlFor="b-prefix" hint={preview ? `Powstaną: ${preview}` : 'Np. alfa'}>
            <input
              id="b-prefix"
              className="input"
              value={prefix}
              autoCapitalize="none"
              autoCorrect="off"
              onChange={(e) => setPrefix(e.target.value)}
            />
          </Field>
          <Field label="Liczba" htmlFor="b-count">
            <input
              id="b-count"
              className="input"
              type="number"
              inputMode="numeric"
              min={1}
              max={200}
              value={Number.isFinite(count) ? count : ''}
              onChange={(e) => setCount(parseInt(e.target.value, 10))}
            />
          </Field>
          <Field label="Numer początkowy" htmlFor="b-start">
            <input
              id="b-start"
              className="input"
              type="number"
              inputMode="numeric"
              min={0}
              value={Number.isFinite(startAt) ? startAt : ''}
              onChange={(e) => setStartAt(parseInt(e.target.value, 10))}
            />
          </Field>
          <Field label="Aplikacja" htmlFor="b-platform">
            <PlatformSelect id="b-platform" value={platform} platforms={platforms} onChange={setPlatform} />
          </Field>
          <Field label="Zespół (kolor)" htmlFor="b-team">
            <TeamSelect id="b-team" value={team} onChange={setTeam} />
          </Field>
        </div>
        <div className="field">
          <span className="field-label">Grupy</span>
          <GroupPicker all={groups} value={selGroups} onChange={setSelGroups} />
        </div>
      </div>
    </Modal>
  );
}
