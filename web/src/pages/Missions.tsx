import { useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { ChevronRight, Flag, Lock, Plus } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import { Modal } from '../components/Modal';
import { useToast } from '../components/Toast';
import { AsyncView, Empty, Field, GroupPicker, Notice, PageHeader } from '../components/ui';
import { countLabel, fmtDate } from '../lib/format';
import { useApi } from '../lib/useApi';

export default function Missions() {
  const state = useApi(api.missions, []);
  const [creating, setCreating] = useState(false);
  const navigate = useNavigate();

  return (
    <>
      <PageHeader
        title="Misje"
        description="Wspólne przestrzenie danych (Data Sync): pliki i znaczniki, które aplikacje subskrybujące misję dostają automatycznie."
        actions={
          <button type="button" className="btn btn-primary" onClick={() => setCreating(true)}>
            <Plus size={18} /> Nowa misja
          </button>
        }
      />
      <AsyncView state={state}>
        {(missions) =>
          missions.length === 0 ? (
            <Empty
              icon={Flag}
              title="Brak misji"
              action={
                <button type="button" className="btn btn-primary" onClick={() => setCreating(true)}>
                  <Plus size={18} /> Utwórz misję
                </button>
              }
            >
              Misję można też założyć z aplikacji ATAK (Data Sync). Pojawi się wtedy na tej liście.
            </Empty>
          ) : (
            <div className="list">
              {[...missions]
                .sort((a, b) => a.name.localeCompare(b.name, 'pl'))
                .map((m) => (
                  <Link key={m.name} to={`/missions/${encodeURIComponent(m.name)}`} className="list-row">
                    <div className="list-row-main">
                      <span className="list-row-title">
                        {m.name} {m.passwordProtected && <Lock size={14} style={{ display: 'inline', verticalAlign: '-2px' }} />}
                      </span>
                      <span className="list-row-meta">
                        <span>{countLabel(m.contents, 'plik', 'pliki', 'plików')}</span>
                        <span>{countLabel(m.uids, 'znacznik', 'znaczniki', 'znaczników')}</span>
                        <span>{countLabel(m.subscribers, 'subskrybent', 'subskrybentów', 'subskrybentów')}</span>
                        <span>od {fmtDate(m.createdAt)}</span>
                        {m.groups.length > 0 && <span>grupy: {m.groups.join(', ')}</span>}
                      </span>
                      {m.description && <span className="small muted ellipsis">{m.description}</span>}
                    </div>
                    <ChevronRight size={18} className="faint" />
                  </Link>
                ))}
            </div>
          )
        }
      </AsyncView>
      {creating && (
        <CreateMissionDialog
          onClose={() => setCreating(false)}
          onCreated={(name) => navigate(`/missions/${encodeURIComponent(name)}`)}
        />
      )}
    </>
  );
}

function CreateMissionDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (name: string) => void }) {
  const toast = useToast();
  const groups = useApi(api.groups, []);
  const groupNames = useMemo(() => (groups.data ?? []).map((g) => g.name), [groups.data]);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [selGroups, setSelGroups] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    const n = name.trim();
    if (!n) {
      setError('Podaj nazwę misji.');
      return;
    }
    if (/[/\\?#%]/.test(n)) {
      setError('Nazwa nie może zawierać znaków / \\ ? # %.');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await api.createMission(n, description.trim(), selGroups);
      toast.ok(`Utworzono misję ${n}.`);
      onCreated(n);
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  };

  return (
    <Modal
      title="Nowa misja"
      onClose={onClose}
      onSubmit={submit}
      busy={busy}
      footer={
        <>
          <button type="button" className="btn btn-ghost" onClick={onClose} disabled={busy}>
            Anuluj
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy}>
            Utwórz misję
          </button>
        </>
      }
    >
      <div className="stack">
        {error && <Notice kind="danger">{error}</Notice>}
        <Field label="Nazwa" htmlFor="m-name" hint="Widoczna w aplikacjach na liście misji.">
          <input id="m-name" className="input" value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Opis (opcjonalny)" htmlFor="m-desc">
          <textarea id="m-desc" className="textarea" value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        <div className="field">
          <span className="field-label">Grupy</span>
          <GroupPicker
            all={groupNames}
            value={selGroups}
            onChange={setSelGroups}
            emptyHint="Brak grup — misja będzie widoczna dla wszystkich."
          />
          {groupNames.length > 0 && <span className="field-hint">Bez wybranych grup misja jest widoczna dla wszystkich.</span>}
        </div>
      </div>
    </Modal>
  );
}
