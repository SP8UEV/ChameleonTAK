import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { ChevronRight, Printer, Search, UserPlus, Users as UsersIcon, UsersRound } from 'lucide-react';
import { api } from '../api/endpoints';
import type { User } from '../api/types';
import { useSession } from '../app/session';
import { AddUserDialog, BulkAddDialog } from '../components/UserDialogs';
import { AsyncView, Empty, Led, PageHeader } from '../components/ui';
import { countLabel } from '../lib/format';
import { platformLabel, teamColor } from '../lib/tak';
import { useApi } from '../lib/useApi';

export default function Users() {
  const { settings } = useSession();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const users = useApi(api.users, [], 15000);
  const groups = useApi(api.groups, []);
  const [dialog, setDialog] = useState<'add' | 'bulk' | null>(null);
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState<Set<number>>(new Set());

  // /users?add=1 (skrót z pulpitu) otwiera okno dodawania.
  useEffect(() => {
    if (params.get('add') === '1') {
      setDialog('add');
      params.delete('add');
      setParams(params, { replace: true });
    }
  }, [params, setParams]);

  const groupNames = useMemo(() => (groups.data ?? []).map((g) => g.name), [groups.data]);

  const filtered = useMemo(() => {
    const list = users.data ?? [];
    const q = query.trim().toLowerCase();
    const sorted = [...list].sort((a, b) => (a.callsign || a.username).localeCompare(b.callsign || b.username, 'pl', { numeric: true }));
    if (!q) return sorted;
    return sorted.filter(
      (u) =>
        u.username.toLowerCase().includes(q) ||
        u.callsign.toLowerCase().includes(q) ||
        u.groups.some((g) => g.toLowerCase().includes(q)),
    );
  }, [users.data, query]);

  const toggle = (id: number) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });

  const allVisibleSelected = filtered.length > 0 && filtered.every((u) => selected.has(u.id));
  const toggleAll = () =>
    setSelected((s) => {
      const n = new Set(s);
      if (allVisibleSelected) filtered.forEach((u) => n.delete(u.id));
      else filtered.forEach((u) => n.add(u.id));
      return n;
    });

  const printSelected = () => {
    const ids = selected.size > 0 ? Array.from(selected) : filtered.map((u) => u.id);
    navigate(`/print?users=${ids.join(',')}`);
  };

  return (
    <>
      <PageHeader
        title="Użytkownicy"
        description="Każda osoba w zespole i jej urządzenia. Dodaj kogoś, a dostanie kod QR do zeskanowania telefonem."
        actions={
          <>
            <button type="button" className="btn btn-primary" onClick={() => setDialog('add')}>
              <UserPlus size={18} /> Dodaj użytkownika
            </button>
            <button type="button" className="btn" onClick={() => setDialog('bulk')}>
              <UsersRound size={18} /> Dodaj wielu
            </button>
            {(users.data?.length ?? 0) > 0 && (
              <button type="button" className="btn" onClick={printSelected}>
                <Printer size={18} /> {selected.size > 0 ? `Drukuj karty (${selected.size})` : 'Drukuj karty'}
              </button>
            )}
          </>
        }
      />

      <AsyncView state={users}>
        {(list) =>
          list.length === 0 ? (
            <Empty
              icon={UsersIcon}
              title="Nie ma jeszcze użytkowników"
              action={
                <div className="row" style={{ justifyContent: 'center' }}>
                  <button type="button" className="btn btn-primary" onClick={() => setDialog('add')}>
                    <UserPlus size={18} /> Dodaj pierwszego
                  </button>
                  <button type="button" className="btn" onClick={() => setDialog('bulk')}>
                    Dodaj cały zespół
                  </button>
                </div>
              }
            >
              Dodaj osobę, a serwer przygotuje dla jej telefonu kod QR z kompletną konfiguracją.
            </Empty>
          ) : (
            <div className="stack">
              <div className="row">
                <label className="search grow">
                  <Search size={18} />
                  <input
                    className="input"
                    type="search"
                    placeholder="Szukaj po nazwie, znaku lub grupie"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                  />
                </label>
              </div>
              <div className="list">
                <div className="list-head">
                  <input
                    type="checkbox"
                    className="row-check"
                    checked={allVisibleSelected}
                    onChange={toggleAll}
                    aria-label="Zaznacz wszystkich"
                  />
                  <span className="grow">
                    {selected.size > 0
                      ? `Zaznaczono ${selected.size}`
                      : countLabel(filtered.length, 'użytkownik', 'użytkowników', 'użytkowników')}
                  </span>
                  {selected.size > 0 && (
                    <button type="button" className="btn btn-ghost btn-sm" onClick={() => setSelected(new Set())}>
                      Odznacz
                    </button>
                  )}
                </div>
                {filtered.map((u) => (
                  <UserRow key={u.id} u={u} checked={selected.has(u.id)} onToggle={() => toggle(u.id)} />
                ))}
                {filtered.length === 0 && <div className="state compact">Nikt nie pasuje do „{query}”.</div>}
              </div>
            </div>
          )
        }
      </AsyncView>

      {dialog === 'add' && (
        <AddUserDialog
          settings={settings}
          groups={groupNames}
          onClose={() => setDialog(null)}
          onCreated={(u) => {
            setDialog(null);
            navigate(`/users/${u.id}`);
          }}
        />
      )}
      {dialog === 'bulk' && (
        <BulkAddDialog
          settings={settings}
          groups={groupNames}
          onClose={() => setDialog(null)}
          onCreated={() => void users.reload()}
        />
      )}
    </>
  );
}

function UserRow({ u, checked, onToggle }: { u: User; checked: boolean; onToggle: () => void }) {
  const platforms = Array.from(new Set(u.devices.map((d) => platformLabel(d.platform))));
  return (
    <div className={`list-row user-row${u.disabled ? ' is-disabled' : ''}`}>
      <input type="checkbox" className="row-check" checked={checked} onChange={onToggle} aria-label={`Zaznacz ${u.username}`} />
      <Link to={`/users/${u.id}`} className="user-row-link">
        <Led state={u.online ? 'on' : 'off'} label={u.online ? 'W sieci' : 'Poza siecią'} />
        <span className="user-swatch" style={{ background: teamColor(u.team) }} title={u.team} />
        <span className="list-row-main">
          <span className="list-row-title">
            {u.callsign || u.username}
            {u.disabled && <span className="badge badge-danger" style={{ marginLeft: 8 }}>zablokowany</span>}
          </span>
          <span className="list-row-meta">
            <span>{u.username}</span>
            <span>
              {countLabel(u.devices.length, 'urządzenie', 'urządzenia', 'urządzeń')}
              {platforms.length > 0 && ` (${platforms.join(', ')})`}
            </span>
          </span>
        </span>
        <span className="user-row-groups chips">
          {u.groups.map((g) => (
            <span key={g} className="chip chip-sm">
              {g}
            </span>
          ))}
        </span>
        <ChevronRight size={18} className="faint" />
      </Link>
    </div>
  );
}
