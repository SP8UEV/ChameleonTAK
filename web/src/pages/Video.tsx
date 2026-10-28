import { useEffect, useRef, useState } from 'react';
import { Download, Eye, LoaderCircle, Pencil, Plus, Radio, Smartphone, Trash, Users, Video } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { StreamAudience, VideoStream, VideoStreamList } from '../api/types';
import { useSession } from '../app/session';
import { useConfirm } from '../components/Confirm';
import { Modal } from '../components/Modal';
import { QrCode } from '../components/QrCode';
import { useToast } from '../components/Toast';
import { AsyncView, Choice, CopyField, Field, Notice, PageHeader, Switch } from '../components/ui';
import { useApi } from '../lib/useApi';

export default function VideoPage() {
  const state = useApi(api.videoStreams, [], 5000);
  const { settings } = useSession();
  const [editing, setEditing] = useState<VideoStream | 'new' | null>(null);
  return (
    <>
      <PageHeader
        title="Wideo na żywo"
        description="Obraz z kamery iPhone’a albo telefonu z Androidem na żywo u wybranych użytkowników TAK — wszystkich, grup albo pojedynczych osób."
        actions={
          <button type="button" className="btn btn-primary" onClick={() => setEditing('new')} disabled={!state.data?.service.enabled}>
            <Plus size={18} /> Nowy strumień
          </button>
        }
      />
      <AsyncView state={state}>
        {(data) => (
          <div className="stack-lg">
            <ServiceCard data={data} main={settings?.main !== false} reload={state.reload} />
            {data.service.enabled && (
              <>
                {data.streams.length === 0 ? (
                  <Notice>Nie ma jeszcze strumieni. Dodaj pierwszy — np. „Kamera Jan” albo „Dron 1” — i wybierz, kto ma go widzieć.</Notice>
                ) : (
                  <div className="video-grid">
                    {data.streams.map((s) => (
                      <StreamCard key={s.id} s={s} onEdit={() => setEditing(s)} onChanged={() => void state.reload()} />
                    ))}
                  </div>
                )}
                <HowItWorks />
              </>
            )}
          </div>
        )}
      </AsyncView>
      {editing && (
        <StreamDialog
          stream={editing === 'new' ? null : editing}
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

/* ---------- Serwer wideo ---------- */
function ServiceCard({ data, main, reload }: { data: VideoStreamList; main: boolean; reload: () => Promise<void> }) {
  const toast = useToast();
  const sv = data.service;
  const [busy, setBusy] = useState(false);
  const act = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true);
    try {
      await fn();
      toast.ok(ok);
      await reload();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  if (sv.enabled && sv.active && !sv.error) return null;
  if (!main) {
    return sv.enabled ? null : <Notice>Serwer wideo włącza się w panelu serwera głównego.</Notice>;
  }
  return (
    <section className="card card-pad-lg stack">
      <div className="row" style={{ gap: 8 }}>
        <Video size={20} />
        <h2 className="grow">Serwer wideo</h2>
        {sv.installed && (
          <Switch
            checked={sv.enabled}
            label="Serwer wideo włączony"
            disabled={busy}
            onChange={(on) => act(() => api.videoEnable(on), on ? 'Włączono serwer wideo.' : 'Wyłączono serwer wideo.')}
          />
        )}
      </div>
      {!sv.supported ? (
        <Notice>Serwer wideo instaluje się na Raspberry Pi z ChameleonTAK. Na tym komputerze ta funkcja jest niedostępna.</Notice>
      ) : !sv.installed ? (
        <>
          <p className="small muted">
            Odbiera obraz z telefonów i przekazuje go do ATAK/iTAK. To darmowy MediaMTX (ok. 30 MB). Otworzy porty {sv.rtmpPort}{' '}
            (nadawanie) i {sv.rtspPort} (oglądanie).
          </p>
          <button
            type="button"
            className="btn btn-primary"
            style={{ alignSelf: 'flex-start' }}
            disabled={busy}
            onClick={() => act(api.videoInstall, 'Zainstalowano serwer wideo.')}
          >
            {busy ? <LoaderCircle size={18} className="spinner" /> : <Download size={18} />} Zainstaluj serwer wideo
          </button>
        </>
      ) : sv.enabled && !sv.active ? (
        <Notice kind="warn">Serwer wideo jest włączony, ale nie działa. Wyłącz go i włącz ponownie albo sprawdź dziennik.</Notice>
      ) : sv.error ? (
        <Notice kind="warn">{sv.error}</Notice>
      ) : (
        <p className="small muted">Serwer wideo jest wyłączony.</p>
      )}
    </section>
  );
}

/* ---------- Strumienie ---------- */
function audienceText(s: VideoStream) {
  if (s.audience === 'groups') return `grupy: ${s.groups.join(', ')}`;
  if (s.audience === 'users') return `użytkownicy: ${s.users.join(', ')}`;
  return 'wszyscy użytkownicy';
}

function StreamCard({ s, onEdit, onChanged }: { s: VideoStream; onEdit: () => void; onChanged: () => void }) {
  const confirm = useConfirm();
  const toast = useToast();
  const [show, setShow] = useState<'' | 'publish' | 'preview'>('');
  const remove = async () => {
    const ok = await confirm({
      title: `Usunąć strumień „${s.name}”?`,
      message: 'Telefon nadający ten strumień zostanie rozłączony, a widzowie stracą obraz.',
      confirmLabel: 'Usuń strumień',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteStream(s.id);
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  return (
    <article className={`card video-card${s.live ? ' live' : ''}`}>
      <header className="row" style={{ gap: 8 }}>
        <h3 className="grow ellipsis">{s.name}</h3>
        <button type="button" className="icon-btn" onClick={onEdit} aria-label="Edytuj strumień" title="Edytuj">
          <Pencil size={16} />
        </button>
        <button type="button" className="icon-btn" onClick={remove} aria-label="Usuń strumień" title="Usuń">
          <Trash size={16} />
        </button>
      </header>
      <div>
        {s.live ? (
          <span className="badge badge-danger video-live">
            <Radio size={12} /> NA ŻYWO
          </span>
        ) : (
          <span className="badge">czeka na nadawanie</span>
        )}
      </div>
      <div className="small muted row" style={{ gap: 6 }}>
        <Users size={14} /> {audienceText(s)}
      </div>
      {s.live && (
        <div className="small muted row" style={{ gap: 6 }}>
          <Eye size={14} /> {s.viewers} {s.viewers === 1 ? 'widz' : 'widzów'} teraz
        </div>
      )}
      <footer className="row" style={{ gap: 8, flexWrap: 'wrap', marginTop: 'auto' }}>
        <button type="button" className="btn btn-sm btn-primary" onClick={() => setShow('publish')}>
          <Smartphone size={16} /> Nadawaj z telefonu
        </button>
        <button type="button" className="btn btn-sm" onClick={() => setShow('preview')} disabled={!s.live}>
          <Eye size={16} /> Podgląd
        </button>
      </footer>
      {show === 'publish' && <PublishDialog s={s} onClose={() => setShow('')} />}
      {show === 'preview' && <PreviewDialog s={s} onClose={() => setShow('')} />}
    </article>
  );
}

function PublishDialog({ s, onClose }: { s: VideoStream; onClose: () => void }) {
  return (
    <Modal title={`Nadawanie: ${s.name}`} onClose={onClose} wide footer={<button type="button" className="btn" onClick={onClose}>Zamknij</button>}>
      <div className="video-publish">
        <div className="stack-sm" style={{ alignItems: 'center', textAlign: 'center' }}>
          <QrCode value={s.larixUrl} size={220} label="Kod QR dla Larix Broadcaster" />
          <span className="small">Zeskanuj aparatem telefonu z zainstalowanym Larix Broadcaster</span>
        </div>
        <ol className="small stack-sm" style={{ margin: 0, paddingLeft: 18 }}>
          <li>
            Zainstaluj darmową aplikację <strong>Larix Broadcaster</strong> (App Store / Google Play).
          </li>
          <li>Zeskanuj kod aparatem telefonu — Larix sam doda połączenie „TAK: {s.name}”.</li>
          <li>W Larix naciśnij czerwony przycisk nagrywania. Strumień pojawi się tu jako „NA ŻYWO”, a odbiorcy dostaną go w ATAK.</li>
          <li>Telefon musi widzieć serwer: ta sama sieć, ZeroTier albo przekierowany port RTMP.</li>
        </ol>
      </div>
      <CopyField label="Adres RTMP (dla innych aplikacji, np. OBS, Prism, DJI)" value={s.publishUrl} />
      <p className="xsmall faint">Adres zawiera klucz nadawania — nie udostępniaj go nikomu poza osobą z kamerą.</p>
    </Modal>
  );
}

function PreviewDialog({ s, onClose }: { s: VideoStream; onClose: () => void }) {
  const ref = useRef<HTMLVideoElement | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    const video = ref.current;
    if (!video) return;
    const src = api.streamHlsUrl(s.id);
    let destroy = () => {};
    if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = src; // Safari odtwarza HLS sam
    } else {
      void import('hls.js').then(({ default: Hls }) => {
        if (!Hls.isSupported()) {
          setError('Ta przeglądarka nie odtworzy podglądu.');
          return;
        }
        const hls = new Hls({ liveSyncDurationCount: 2, xhrSetup: (xhr) => {
            xhr.withCredentials = true;
          }, });
        hls.on(Hls.Events.ERROR, (_e, d) => {
          if (d.fatal) setError('Nie udało się odtworzyć strumienia — może właśnie przestał nadawać.');
        });
        hls.loadSource(src);
        hls.attachMedia(video);
        destroy = () => hls.destroy();
      });
    }
    return () => destroy();
  }, [s.id]);
  return (
    <Modal title={`Podgląd: ${s.name}`} onClose={onClose} wide footer={<button type="button" className="btn" onClick={onClose}>Zamknij</button>}>
      {error && <Notice kind="danger">{error}</Notice>}
      <video ref={ref} className="video-preview" controls autoPlay muted playsInline />
      <p className="xsmall faint">Podgląd w przeglądarce ma kilka sekund opóźnienia; w ATAK obraz jest szybszy (RTSP).</p>
    </Modal>
  );
}

function StreamDialog({ stream, onClose, onSaved }: { stream: VideoStream | null; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const who = useApi(api.streamAudience, []);
  const [name, setName] = useState(stream?.name ?? '');
  const [audience, setAudience] = useState<StreamAudience>(stream?.audience ?? 'all');
  const [groups, setGroups] = useState<string[]>(stream?.groups ?? []);
  const [users, setUsers] = useState<string[]>(stream?.users ?? []);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    if (!name.trim()) return setError('Podaj nazwę strumienia.');
    if (audience === 'groups' && groups.length === 0) return setError('Wybierz co najmniej jedną grupę.');
    if (audience === 'users' && users.length === 0) return setError('Wybierz co najmniej jednego użytkownika.');
    setError(null);
    setBusy(true);
    try {
      const body = { name: name.trim(), audience, groups, users };
      if (stream) await api.updateStream(stream.id, body);
      else await api.createStream(body);
      toast.ok(stream ? 'Zapisano strumień.' : 'Dodano strumień. Teraz nadawaj z telefonu.');
      onSaved();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const chips = (all: string[], value: string[], set: (v: string[]) => void, empty: string) =>
    all.length === 0 ? (
      <span className="small muted">{empty}</span>
    ) : (
      <div className="chips" role="group">
        {all.map((x) => {
          const on = value.includes(x);
          return (
            <button key={x} type="button" className={`chip${on ? ' selected' : ''}`} aria-pressed={on} onClick={() => set(on ? value.filter((v) => v !== x) : [...value, x])}>
              {x}
            </button>
          );
        })}
      </div>
    );

  return (
    <Modal
      title={stream ? 'Edytuj strumień' : 'Nowy strumień wideo'}
      onClose={onClose}
      busy={busy}
      onSubmit={submit}
      footer={
        <>
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Anuluj
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy}>
            {busy ? <LoaderCircle size={18} className="spinner" /> : <Video size={18} />} {stream ? 'Zapisz' : 'Dodaj'}
          </button>
        </>
      }
    >
      <div className="stack">
        {error && <Notice kind="danger">{error}</Notice>}
        <Field label="Nazwa" htmlFor="vs-name" hint="Widoczna w ATAK na liście wideo.">
          <input id="vs-name" className="input" value={name} placeholder="Kamera Jan" onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Kto ma widzieć obraz">
          <div className="choices">
            <Choice type="radio" name="aud" checked={audience === 'all'} onChange={() => setAudience('all')} title="Wszyscy" description="Każdy użytkownik tego serwera." />
            <Choice type="radio" name="aud" checked={audience === 'groups'} onChange={() => setAudience('groups')} title="Grupy" description="Członkowie wybranych grup." />
            <Choice type="radio" name="aud" checked={audience === 'users'} onChange={() => setAudience('users')} title="Wybrane osoby" description="Tylko wskazani użytkownicy." />
          </div>
        </Field>
        {audience === 'groups' && chips(who.data?.groups ?? [], groups, setGroups, 'Nie ma grup — utwórz je w zakładce Grupy.')}
        {audience === 'users' && chips(who.data?.users ?? [], users, setUsers, 'Nie ma jeszcze użytkowników.')}
      </div>
    </Modal>
  );
}

function HowItWorks() {
  return (
    <p className="xsmall faint">
      Odbiorcy dostają strumień w ATAK (Wideo → lista strumieni) z własnym hasłem, a serwer sprawdza przy każdym połączeniu, czy
      dana osoba może oglądać. Wideo 720p zajmuje ok. 1,5 Mb/s na każdego widza — sprawdź łącze w Ustawieniach → Test sieci.
    </p>
  );
}
