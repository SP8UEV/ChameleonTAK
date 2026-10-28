import { useState } from 'react';
import { ArrowRightLeft, Check, LoaderCircle, Plus, Server, Smartphone, Trash, Users } from 'lucide-react';
import { api } from '../api/endpoints';
import { currentServer, errorMessage, setCurrentServer } from '../api/client';
import type { PlatformKey, ServerInfo, ServerList } from '../api/types';
import { useConfirm } from '../components/Confirm';
import { Modal } from '../components/Modal';
import { SERVERS_CHANGED } from '../components/ServerSwitcher';
import { useToast } from '../components/Toast';
import { AsyncView, Choice, Field, Led, Notice, PageHeader } from '../components/ui';
import { PLATFORMS } from '../lib/tak';
import { useApi } from '../lib/useApi';

export default function ServersPage() {
  const state = useApi(api.servers, [], 10000);
  const [adding, setAdding] = useState(false);
  return (
    <>
      <PageHeader
        title="Serwery"
        description="Kilka niezależnych serwerów TAK na jednym urządzeniu. Każdy ma własnych użytkowników, grupy, certyfikaty i porty — panel, sieć i ADS-B są wspólne."
        actions={
          <button type="button" className="btn btn-primary" onClick={() => setAdding(true)} disabled={!state.data}>
            <Plus size={18} /> Dodaj serwer
          </button>
        }
      />
      <AsyncView state={state}>
        {(data) => (
          <div className="server-grid">
            {data.servers.map((s) => (
              <ServerCard
                key={s.id}
                s={s}
                onDeleted={() => {
                  void state.reload();
                  window.dispatchEvent(new Event(SERVERS_CHANGED));
                }}
              />
            ))}
          </div>
        )}
      </AsyncView>
      {adding && state.data && (
        <AddServerDialog
          data={state.data}
          onClose={() => setAdding(false)}
          onCreated={() => {
            setAdding(false);
            void state.reload();
            window.dispatchEvent(new Event(SERVERS_CHANGED));
          }}
        />
      )}
    </>
  );
}

function ServerCard({ s, onDeleted }: { s: ServerInfo; onDeleted: () => void }) {
  const toast = useToast();
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const active = currentServer() === s.id;
  const listeners = s.listeners ?? [];
  const failing = listeners.filter((l) => l.state !== 'running');

  const remove = async () => {
    const ok = await confirm({
      title: `Usunąć serwer „${s.displayName}”?`,
      message: (
        <>
          Serwer przestanie działać, a jego użytkownicy, urządzenia i certyfikaty znikną z panelu. Dane zostaną przeniesione do
          katalogu <span className="code">servers-deleted</span> na urządzeniu.
        </>
      ),
      confirmLabel: 'Usuń serwer',
      danger: true,
      requireText: s.id,
    });
    if (!ok) return;
    setBusy(true);
    try {
      await api.deleteServer(s.id);
      toast.ok(`Usunięto serwer ${s.displayName}.`);
      onDeleted();
    } catch (e) {
      toast.error(errorMessage(e));
      setBusy(false);
    }
  };

  const switchTo = () => {
    setCurrentServer(s.id);
    window.location.assign('/');
  };

  return (
    <article className={`card server-card${active ? ' active' : ''}`}>
      <header className="server-card-head">
        <Led state={failing.length ? 'err' : 'on'} large label={failing.length ? 'Błąd usług' : 'Działa'} />
        <div className="grow" style={{ minWidth: 0 }}>
          <h3 className="ellipsis">{s.displayName}</h3>
          <div className="xsmall muted">
            {s.main ? 'serwer główny' : <span className="code">{s.id}</span>} · <span className="code">{s.serverName}</span>
          </div>
        </div>
        {active && (
          <span className="badge badge-accent">
            <Check size={12} /> w panelu
          </span>
        )}
      </header>
      <dl className="server-ports">
        <div>
          <dt>Strumień TAK</dt>
          <dd className="code">{s.ports.cotTls}</dd>
        </div>
        <div>
          <dt>API</dt>
          <dd className="code">{s.ports.marti}</dd>
        </div>
        <div>
          <dt>Rejestracja</dt>
          <dd className="code">{s.ports.enroll}</dd>
        </div>
      </dl>
      <div className="row small muted" style={{ gap: 16, flexWrap: 'wrap' }}>
        <span className="row" style={{ gap: 4 }}>
          <Users size={14} /> {s.users} {s.users === 1 ? 'użytkownik' : 'użytkowników'}
        </span>
        <span className="row" style={{ gap: 4 }}>
          <Smartphone size={14} /> {s.clients} w sieci
        </span>
      </div>
      {failing.map((l) => (
        <Notice key={l.name} kind="danger">
          {l.name} (port {l.port}): {l.error}
        </Notice>
      ))}
      <footer className="row" style={{ gap: 8, marginTop: 'auto' }}>
        <button type="button" className="btn btn-sm" onClick={switchTo} disabled={active}>
          <ArrowRightLeft size={16} /> Zarządzaj
        </button>
        {!s.main && (
          <button type="button" className="btn btn-sm btn-danger" style={{ marginLeft: 'auto' }} onClick={remove} disabled={busy}>
            <Trash size={16} /> Usuń
          </button>
        )}
      </footer>
    </article>
  );
}

function slugify(name: string) {
  return name
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/ł/g, 'l')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 24);
}

function AddServerDialog({ data, onClose, onCreated }: { data: ServerList; onClose: () => void; onCreated: () => void }) {
  const toast = useToast();
  const main = data.servers.find((s) => s.main);
  const [name, setName] = useState('');
  const [id, setId] = useState('');
  const [idTouched, setIdTouched] = useState(false);
  const [address, setAddress] = useState(main?.serverName ?? '');
  const [ports, setPorts] = useState({ cotTls: data.suggestedPorts.cotTls, marti: data.suggestedPorts.marti, enroll: data.suggestedPorts.enroll });
  const [platforms, setPlatforms] = useState<PlatformKey[]>(main?.platforms ?? ['atak', 'itak']);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const effectiveId = idTouched ? id : slugify(name);

  const submit = async () => {
    if (!name.trim()) return setError('Podaj nazwę serwera.');
    if (!/^[a-z0-9][a-z0-9-]{0,23}$/.test(effectiveId) || effectiveId === 'main') return setError('Identyfikator: małe litery, cyfry i myślniki.');
    if (platforms.length === 0) return setError('Wybierz co najmniej jedną aplikację.');
    setError(null);
    setBusy(true);
    try {
      const s = await api.createServer({ id: effectiveId, displayName: name.trim(), serverName: address.trim(), ports, platforms });
      toast.ok(`Serwer ${s.displayName} działa.`);
      onCreated();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const portField = (key: keyof typeof ports, label: string, hint: string) => (
    <Field label={label} htmlFor={`ns-${key}`} hint={hint}>
      <input
        id={`ns-${key}`}
        className="input input-code"
        inputMode="numeric"
        value={ports[key] || ''}
        onChange={(e) => setPorts({ ...ports, [key]: Number(e.target.value.replace(/\D/g, '')) })}
      />
    </Field>
  );

  return (
    <Modal
      title="Nowy serwer TAK"
      onClose={onClose}
      busy={busy}
      onSubmit={submit}
      wide
      footer={
        <>
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Anuluj
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy}>
            {busy ? <LoaderCircle size={18} className="spinner" /> : <Server size={18} />} Utwórz i uruchom
          </button>
        </>
      }
    >
      <div className="stack">
        {error && <Notice kind="danger">{error}</Notice>}
        <div className="form-grid">
          <Field label="Nazwa" htmlFor="ns-name" hint="Widoczna w panelu i w aplikacjach TAK.">
            <input id="ns-name" className="input" value={name} placeholder="Drużyna B" onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="Identyfikator" htmlFor="ns-id" hint="Używany w nazwach katalogów. Nie da się go później zmienić.">
            <input
              id="ns-id"
              className="input input-code"
              value={effectiveId}
              autoCapitalize="none"
              onChange={(e) => {
                setIdTouched(true);
                setId(e.target.value.toLowerCase());
              }}
            />
          </Field>
        </div>
        <Field
          label="Adres serwera"
          htmlFor="ns-addr"
          hint="Zwykle ten sam co serwera głównego — serwery różnią się portami. Inna domena wskazująca na to urządzenie też zadziała."
        >
          <input id="ns-addr" className="input input-code" value={address} autoCapitalize="none" onChange={(e) => setAddress(e.target.value.trim())} />
        </Field>
        <div className="form-grid server-port-grid">
          {portField('cotTls', 'Strumień TAK (TLS)', 'Połączenie aplikacji.')}
          {portField('marti', 'API serwera', 'Pliki, misje, paczki.')}
          {portField('enroll', 'Rejestracja', 'Kody QR w aplikacjach.')}
        </div>
        <p className="xsmall muted">
          Zapora na urządzeniu otworzy te porty sama. Do dostępu z internetu przekieruj je też na routerze — sprawdzisz to w
          Ustawieniach → Test portów.
        </p>
        <Field label="Aplikacje">
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
        </Field>
      </div>
    </Modal>
  );
}
