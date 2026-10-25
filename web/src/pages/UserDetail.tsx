import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Plus, Printer, Save, Trash } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { Device, PlatformKey, User } from '../api/types';
import { useSession } from '../app/session';
import { useConfirm } from '../components/Confirm';
import { DeviceCard } from '../components/DeviceCard';
import { useToast } from '../components/Toast';
import {
  PlatformSelect,
  RoleSelect,
  TeamSelect,
  defaultDeviceName,
  enabledPlatforms,
} from '../components/UserDialogs';
import { AsyncView, CheckRow, Disclosure, Field, GroupPicker, Led, PageHeader } from '../components/ui';
import { fmtDate } from '../lib/format';
import { useApi } from '../lib/useApi';

export default function UserDetail() {
  const { id } = useParams();
  const userId = Number(id);
  const state = useApi(() => api.user(userId), [userId]);

  return (
    <AsyncView state={state} loadingLabel="Wczytywanie użytkownika…">
      {(u) => <UserEditor key={u.id} user={u} onUser={(nu) => state.setData(nu)} />}
    </AsyncView>
  );
}

function UserEditor({ user, onUser }: { user: User; onUser: (u: User) => void }) {
  const { settings } = useSession();
  const navigate = useNavigate();
  const toast = useToast();
  const confirm = useConfirm();
  const groups = useApi(api.groups, []);
  const groupNames = useMemo(() => {
    const names = (groups.data ?? []).map((g) => g.name);
    // Pokaż też grupy użytkownika, których nie ma na liście (np. usunięte).
    user.groups.forEach((g) => {
      if (!names.includes(g)) names.push(g);
    });
    return names;
  }, [groups.data, user.groups]);

  const [username, setUsername] = useState(user.username);
  const [callsign, setCallsign] = useState(user.callsign);
  const [team, setTeam] = useState(user.team);
  const [role, setRole] = useState(user.role);
  const [userGroups, setUserGroups] = useState<string[]>(user.groups);
  const [disabled, setDisabled] = useState(user.disabled);
  const [password, setPassword] = useState('');
  const [saving, setSaving] = useState(false);

  const platforms = enabledPlatforms(settings);
  const [newPlatform, setNewPlatform] = useState<PlatformKey>(platforms[0]);
  const [newName, setNewName] = useState(defaultDeviceName(platforms[0]));
  const [adding, setAdding] = useState(false);

  useEffect(() => {
    if (!platforms.includes(newPlatform)) setNewPlatform(platforms[0]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settings]);

  const dirty =
    username !== user.username ||
    callsign !== user.callsign ||
    team !== user.team ||
    role !== user.role ||
    disabled !== user.disabled ||
    password !== '' ||
    userGroups.length !== user.groups.length ||
    userGroups.some((g) => !user.groups.includes(g));

  const save = async () => {
    if (!username.trim()) {
      toast.error('Nazwa użytkownika nie może być pusta.');
      return;
    }
    setSaving(true);
    try {
      const updated = await api.updateUser(user.id, {
        username: username.trim(),
        callsign: callsign.trim(),
        team,
        role,
        groups: userGroups,
        disabled,
        ...(password ? { password } : {}),
      });
      setPassword('');
      onUser(updated && typeof updated === 'object' && 'id' in updated ? updated : { ...user, username, callsign, team, role, groups: userGroups, disabled });
      toast.ok('Zapisano zmiany.');
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    const ok = await confirm({
      title: `Usunąć użytkownika ${user.callsign || user.username}?`,
      message: `Certyfikaty wszystkich jego urządzeń (${user.devices.length}) zostaną unieważnione. Tej operacji nie można cofnąć.`,
      confirmLabel: 'Usuń użytkownika',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteUser(user.id);
      toast.ok(`Usunięto użytkownika ${user.username}.`);
      navigate('/users', { replace: true });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  const addDevice = async () => {
    setAdding(true);
    try {
      const d = await api.addDevice(user.id, { platform: newPlatform, name: newName.trim() || defaultDeviceName(newPlatform) });
      onUser({ ...user, devices: [...user.devices, d] });
      setNewName(defaultDeviceName(newPlatform));
      toast.ok(`Dodano urządzenie ${d.name}.`);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setAdding(false);
    }
  };

  const replaceDevice = (d: Device) => onUser({ ...user, devices: user.devices.map((x) => (x.id === d.id ? d : x)) });
  const dropDevice = (id: number) => onUser({ ...user, devices: user.devices.filter((x) => x.id !== id) });

  return (
    <>
      <PageHeader
        back={{ to: '/users', label: 'Użytkownicy' }}
        title={
          <span className="row" style={{ gap: 12 }}>
            <Led state={user.online ? 'on' : 'off'} large label={user.online ? 'W sieci' : 'Poza siecią'} />
            {user.callsign || user.username}
          </span>
        }
        description={`${user.username}, dodany ${fmtDate(user.createdAt)}`}
        actions={
          <button type="button" className="btn" onClick={() => navigate(`/print?users=${user.id}`)}>
            <Printer size={18} /> Drukuj karty
          </button>
        }
      />

      <div className="user-detail">
        <section className="stack">
          <h2 className="section-title">Urządzenia</h2>
          {user.devices.length === 0 && (
            <p className="muted">Ten użytkownik nie ma jeszcze urządzeń. Dodaj telefon, aby dostać kod QR.</p>
          )}
          {user.devices.map((d) => (
            <DeviceCard
              key={d.id}
              device={d}
              user={user}
              settings={settings}
              onChange={replaceDevice}
              onDeleted={() => dropDevice(d.id)}
            />
          ))}
          <form
            className="card add-device"
            onSubmit={(e) => {
              e.preventDefault();
              void addDevice();
            }}
          >
            <div className="strong">Dodaj urządzenie</div>
            <div className="device-input-row">
              <PlatformSelect
                id="nd-platform"
                value={newPlatform}
                platforms={platforms}
                onChange={(p) => {
                  if (newName === defaultDeviceName(newPlatform)) setNewName(defaultDeviceName(p));
                  setNewPlatform(p);
                }}
              />
              <input className="input" aria-label="Nazwa urządzenia" value={newName} onChange={(e) => setNewName(e.target.value)} />
              <button type="submit" className="btn" disabled={adding}>
                <Plus size={18} /> Dodaj
              </button>
            </div>
          </form>
        </section>

        <aside className="stack">
          <form
            className="card stack"
            onSubmit={(e) => {
              e.preventDefault();
              void save();
            }}
          >
            <h2 className="section-title" style={{ marginBottom: 0 }}>
              Dane
            </h2>
            <Field label="Nazwa użytkownika" htmlFor="e-user" hint="Zmiana wymaga ponownej konfiguracji urządzeń.">
              <input
                id="e-user"
                className="input"
                value={username}
                autoCapitalize="none"
                autoCorrect="off"
                onChange={(e) => setUsername(e.target.value)}
              />
            </Field>
            <Field label="Znak wywoławczy" htmlFor="e-cs">
              <input id="e-cs" className="input" value={callsign} onChange={(e) => setCallsign(e.target.value)} />
            </Field>
            <Field label="Zespół (kolor)" htmlFor="e-team">
              <TeamSelect id="e-team" value={team} onChange={setTeam} />
            </Field>
            <Field label="Rola" htmlFor="e-role">
              <RoleSelect id="e-role" value={role} onChange={setRole} />
            </Field>
            <div className="field">
              <span className="field-label">Grupy</span>
              <GroupPicker all={groupNames} value={userGroups} onChange={setUserGroups} />
              {userGroups.length === 0 && groupNames.length > 0 && (
                <span className="field-hint">Bez grupy użytkownik widzi wszystkich spoza grup.</span>
              )}
            </div>
            <CheckRow
              checked={disabled}
              onChange={setDisabled}
              label="Zablokuj konto"
              hint="Urządzenia tego użytkownika nie połączą się z serwerem."
            />
            <Disclosure title="Hasło">
              <Field label="Nowe hasło" htmlFor="e-pass" hint="Zostaw puste, aby nie zmieniać.">
                <input
                  id="e-pass"
                  className="input"
                  type="password"
                  autoComplete="new-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </Field>
            </Disclosure>
            <button type="submit" className="btn btn-primary" disabled={!dirty || saving}>
              <Save size={18} /> Zapisz zmiany
            </button>
          </form>
          <button type="button" className="btn btn-danger" onClick={remove}>
            <Trash size={18} /> Usuń użytkownika
          </button>
        </aside>
      </div>
    </>
  );
}
