import { useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Download, FilePlus, Trash, X } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { FileEntry, MissionDetail as MissionDetailData } from '../api/types';
import { useConfirm } from '../components/Confirm';
import { Modal } from '../components/Modal';
import { useToast } from '../components/Toast';
import { AsyncView, PageHeader } from '../components/ui';
import { countLabel, fmtBytes, fmtDate, fmtDateTime } from '../lib/format';
import { useApi } from '../lib/useApi';

const CHANGE_LABELS: Record<string, string> = {
  ADD_CONTENT: 'dodano',
  REMOVE_CONTENT: 'usunięto',
  CREATE_MISSION: 'utworzono misję',
  DELETE_MISSION: 'usunięto misję',
};

export default function MissionDetail() {
  const { name = '' } = useParams();
  const state = useApi(() => api.mission(name), [name]);
  return (
    <AsyncView state={state} loadingLabel="Wczytywanie misji…">
      {(m) => <MissionView m={m} reload={() => void state.reload()} />}
    </AsyncView>
  );
}

function MissionView({ m, reload }: { m: MissionDetailData; reload: () => void }) {
  const toast = useToast();
  const confirm = useConfirm();
  const navigate = useNavigate();
  const [adding, setAdding] = useState(false);

  const removeFile = async (f: FileEntry) => {
    const ok = await confirm({
      title: `Usunąć „${f.name}” z misji?`,
      message: 'Plik zostanie na serwerze, ale subskrybenci misji przestaną go widzieć.',
      confirmLabel: 'Usuń z misji',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.removeMissionFile(m.name, f.hash);
      toast.ok(`Usunięto ${f.name} z misji.`);
      reload();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  const removeMission = async () => {
    const ok = await confirm({
      title: `Usunąć misję „${m.name}”?`,
      message: 'Subskrybenci stracą dostęp do jej zawartości. Same pliki zostaną na serwerze.',
      confirmLabel: 'Usuń misję',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteMission(m.name);
      toast.ok(`Usunięto misję ${m.name}.`);
      navigate('/missions', { replace: true });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  const fileName = (hash: string) => m.files.find((f) => f.hash === hash)?.name;
  const itemName = (uid: string) => m.items.find((i) => i.uid === uid)?.callsign;

  return (
    <>
      <PageHeader
        back={{ to: '/missions', label: 'Misje' }}
        title={m.name}
        description={m.description || `Utworzona ${fmtDate(m.createdAt)}${m.creatorUid ? ` przez ${m.creatorUid}` : ''}.`}
        actions={
          <>
            <button type="button" className="btn btn-primary" onClick={() => setAdding(true)}>
              <FilePlus size={18} /> Dodaj plik
            </button>
            <button type="button" className="btn btn-danger" onClick={removeMission}>
              <Trash size={18} /> Usuń misję
            </button>
          </>
        }
      />
      {m.groups.length > 0 && (
        <div className="row small muted">
          Widoczna dla grup:
          {m.groups.map((g) => (
            <span key={g} className="chip chip-sm">
              {g}
            </span>
          ))}
        </div>
      )}

      <div className="mission-grid">
        <section className="card">
          <div className="card-head">
            <h2>Pliki</h2>
            <span className="muted small">{m.files.length}</span>
          </div>
          {m.files.length === 0 ? (
            <p className="muted small">Misja nie ma plików. Dodaj plik z listy plików serwera.</p>
          ) : (
            <div className="mini-list">
              {m.files.map((f) => (
                <div key={f.hash} className="mini-row">
                  <div className="grow" style={{ minWidth: 0 }}>
                    <div className="ellipsis strong">{f.name}</div>
                    <div className="xsmall muted">
                      {fmtBytes(f.size)}, {fmtDateTime(f.createdAt)}
                    </div>
                  </div>
                  <a className="icon-btn" href={api.fileDownloadUrl(f.hash)} download={f.name} aria-label={`Pobierz ${f.name}`}>
                    <Download size={18} />
                  </a>
                  <button type="button" className="icon-btn danger" onClick={() => removeFile(f)} aria-label={`Usuń ${f.name} z misji`}>
                    <X size={18} />
                  </button>
                </div>
              ))}
            </div>
          )}
        </section>

        <section className="card">
          <div className="card-head">
            <h2>Znaczniki</h2>
            <span className="muted small">{m.items.length}</span>
          </div>
          {m.items.length === 0 ? (
            <p className="muted small">Brak znaczników. Dodaje się je z aplikacji ATAK.</p>
          ) : (
            <div className="mini-list">
              {m.items.map((i) => (
                <div key={i.uid} className="mini-row">
                  <div className="grow" style={{ minWidth: 0 }}>
                    <div className="ellipsis strong">{i.callsign || i.uid}</div>
                    <div className="xsmall muted">
                      <span className="code">{i.type}</span>, {i.lat.toFixed(5)}, {i.lon.toFixed(5)}
                    </div>
                  </div>
                  <span className="xsmall faint nowrap">{fmtDate(i.createdAt)}</span>
                </div>
              ))}
            </div>
          )}
        </section>

        <section className="card">
          <div className="card-head">
            <h2>Subskrybenci</h2>
            <span className="muted small">{m.subscribers.length}</span>
          </div>
          {m.subscribers.length === 0 ? (
            <p className="muted small">Nikt nie subskrybuje tej misji.</p>
          ) : (
            <div className="mini-list">
              {m.subscribers.map((s) => (
                <div key={s.clientUid} className="mini-row">
                  <div className="grow" style={{ minWidth: 0 }}>
                    <div className="ellipsis strong">{s.username || s.clientUid}</div>
                    <div className="xsmall muted">od {fmtDateTime(s.createdAt)}</div>
                  </div>
                  {s.role && <span className="badge">{s.role}</span>}
                </div>
              ))}
            </div>
          )}
        </section>

        <section className="card">
          <div className="card-head">
            <h2>Historia zmian</h2>
            <span className="muted small">{countLabel(m.changes.length, 'zmiana', 'zmiany', 'zmian')}</span>
          </div>
          {m.changes.length === 0 ? (
            <p className="muted small">Brak zmian.</p>
          ) : (
            <ol className="mini-list changes">
              {[...m.changes]
                .sort((a, b) => new Date(b.time).getTime() - new Date(a.time).getTime())
                .slice(0, 100)
                .map((c, idx) => (
                  <li key={`${c.time}-${idx}`} className="mini-row">
                    <span className="xsmall faint nowrap">{fmtDateTime(c.time)}</span>
                    <span className="grow small" style={{ minWidth: 0 }}>
                      {CHANGE_LABELS[c.type] ?? c.type}{' '}
                      <strong>{c.hash ? fileName(c.hash) || 'plik' : c.uid ? itemName(c.uid) || c.uid : ''}</strong>
                      {c.creatorUid && <span className="muted"> ({c.creatorUid})</span>}
                    </span>
                  </li>
                ))}
            </ol>
          )}
        </section>
      </div>

      {adding && (
        <AddFileDialog
          mission={m}
          onClose={() => setAdding(false)}
          onAdded={() => {
            setAdding(false);
            reload();
          }}
        />
      )}
    </>
  );
}

function AddFileDialog({ mission, onClose, onAdded }: { mission: MissionDetailData; onClose: () => void; onAdded: () => void }) {
  const toast = useToast();
  const files = useApi(api.files, []);
  const [busy, setBusy] = useState<string | null>(null);
  const available = useMemo(
    () => (files.data ?? []).filter((f) => !mission.files.some((x) => x.hash === f.hash)),
    [files.data, mission.files],
  );

  const add = async (f: FileEntry) => {
    setBusy(f.hash);
    try {
      await api.addMissionFile(mission.name, f.hash);
      toast.ok(`Dodano ${f.name} do misji.`);
      onAdded();
    } catch (e) {
      toast.error(errorMessage(e));
      setBusy(null);
    }
  };

  return (
    <Modal title="Dodaj plik do misji" onClose={onClose} busy={!!busy}>
      <AsyncView state={files}>
        {() =>
          available.length === 0 ? (
            <p className="muted">
              Nie ma plików do dodania. Najpierw wyślij plik na stronie <strong>Pliki</strong>.
            </p>
          ) : (
            <div className="mini-list">
              {available.map((f) => (
                <button key={f.hash} type="button" className="mini-row mini-row-btn" onClick={() => add(f)} disabled={!!busy}>
                  <div className="grow" style={{ minWidth: 0, textAlign: 'left' }}>
                    <div className="ellipsis strong">{f.name}</div>
                    <div className="xsmall muted">
                      {fmtBytes(f.size)}, {fmtDateTime(f.createdAt)}
                    </div>
                  </div>
                  <FilePlus size={18} />
                </button>
              ))}
            </div>
          )
        }
      </AsyncView>
    </Modal>
  );
}
