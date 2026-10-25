import { useState } from 'react';
import { Group as GroupIcon, Pencil, Plus, Trash } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { Group } from '../api/types';
import { useConfirm } from '../components/Confirm';
import { Modal } from '../components/Modal';
import { useToast } from '../components/Toast';
import { AsyncView, Empty, Field, Notice, PageHeader } from '../components/ui';
import { countLabel } from '../lib/format';
import { useApi } from '../lib/useApi';

export default function Groups() {
  const state = useApi(api.groups, []);
  const toast = useToast();
  const confirm = useConfirm();
  const [editing, setEditing] = useState<Group | 'new' | null>(null);

  const remove = async (g: Group) => {
    const ok = await confirm({
      title: `Usunąć grupę „${g.name}”?`,
      message:
        g.members > 0
          ? `Należy do niej ${countLabel(g.members, 'osoba', 'osoby', 'osób')}. Kto nie ma innej grupy, trafi do grupy ogólnej.`
          : 'Grupa jest pusta.',
      confirmLabel: 'Usuń grupę',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteGroup(g.id);
      toast.ok(`Usunięto grupę ${g.name}.`);
      void state.reload();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <>
      <PageHeader
        title="Grupy"
        description="Grupy decydują, kto kogo widzi: użytkownicy dostają pozycje, czat i pliki tylko od osób ze wspólnej grupy. Członków przypisujesz na stronie użytkownika."
        actions={
          <button type="button" className="btn btn-primary" onClick={() => setEditing('new')}>
            <Plus size={18} /> Nowa grupa
          </button>
        }
      />
      <AsyncView state={state}>
        {(groups) =>
          groups.length === 0 ? (
            <Empty
              icon={GroupIcon}
              title="Brak grup"
              action={
                <button type="button" className="btn btn-primary" onClick={() => setEditing('new')}>
                  <Plus size={18} /> Utwórz grupę
                </button>
              }
            >
              Bez grup wszyscy widzą wszystkich — to dobre ustawienie dla jednego zespołu. Grupy przydają się, gdy działa kilka
              niezależnych drużyn.
            </Empty>
          ) : (
            <div className="list">
              {groups.map((g) => (
                <div key={g.id} className="list-row">
                  <div className="list-row-main">
                    <span className="list-row-title">{g.name}</span>
                    <span className="list-row-meta">
                      <span>{countLabel(g.members, 'osoba', 'osoby', 'osób')}</span>
                      {g.description && <span>{g.description}</span>}
                    </span>
                  </div>
                  <div className="list-row-side">
                    <button type="button" className="icon-btn" aria-label={`Edytuj ${g.name}`} onClick={() => setEditing(g)}>
                      <Pencil size={18} />
                    </button>
                    <button type="button" className="icon-btn danger" aria-label={`Usuń ${g.name}`} onClick={() => remove(g)}>
                      <Trash size={18} />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )
        }
      </AsyncView>
      {editing && (
        <GroupDialog
          group={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            void state.reload();
          }}
        />
      )}
    </>
  );
}

function GroupDialog({ group, onClose, onSaved }: { group: Group | null; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const [name, setName] = useState(group?.name ?? '');
  const [description, setDescription] = useState(group?.description ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    if (!name.trim()) {
      setError('Podaj nazwę grupy.');
      return;
    }
    if (name.trim() === '__ANON__') {
      setError('Ta nazwa jest zarezerwowana.');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      if (group) await api.updateGroup(group.id, name.trim(), description.trim());
      else await api.createGroup(name.trim(), description.trim());
      toast.ok(group ? 'Zapisano grupę.' : `Utworzono grupę ${name.trim()}.`);
      onSaved();
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  };

  return (
    <Modal
      title={group ? 'Edytuj grupę' : 'Nowa grupa'}
      onClose={onClose}
      onSubmit={submit}
      busy={busy}
      footer={
        <>
          <button type="button" className="btn btn-ghost" onClick={onClose} disabled={busy}>
            Anuluj
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy}>
            {group ? 'Zapisz' : 'Utwórz grupę'}
          </button>
        </>
      }
    >
      <div className="stack">
        {error && <Notice kind="danger">{error}</Notice>}
        {group && group.name !== name.trim() && group.members > 0 && (
          <Notice kind="warn">Zmiana nazwy dotyczy wszystkich członków. Aplikacje odświeżą listę grup przy następnym połączeniu.</Notice>
        )}
        <Field label="Nazwa" htmlFor="g-name" hint="Np. Alfa, Sztab, Medycy.">
          <input id="g-name" className="input" value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Opis (opcjonalny)" htmlFor="g-desc">
          <textarea id="g-desc" className="textarea" value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
      </div>
    </Modal>
  );
}
