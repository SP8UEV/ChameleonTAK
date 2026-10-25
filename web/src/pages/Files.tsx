import { useRef, useState } from 'react';
import { Download, FileText, LoaderCircle, Trash, Upload } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { FileEntry } from '../api/types';
import { useConfirm } from '../components/Confirm';
import { useToast } from '../components/Toast';
import { AsyncView, Empty, PageHeader, Switch } from '../components/ui';
import { fmtBytes, fmtDateTime } from '../lib/format';
import { useApi } from '../lib/useApi';

export default function FilesPage() {
  const state = useApi(api.files, []);
  const toast = useToast();
  const confirm = useConfirm();
  const inputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState(false);

  const upload = async (files: FileList | File[]) => {
    const list = Array.from(files);
    for (const f of list) {
      setUploading(f.name);
      try {
        await api.uploadFile(f);
        toast.ok(`Wysłano ${f.name}.`);
      } catch (e) {
        toast.error(`${f.name}: ${errorMessage(e)}`);
      }
    }
    setUploading(null);
    void state.reload();
  };

  const togglePublic = async (f: FileEntry, value: boolean) => {
    state.setData((list) => list?.map((x) => (x.hash === f.hash ? { ...x, public: value } : x)));
    try {
      await api.setFilePublic(f.hash, value);
    } catch (e) {
      state.setData((list) => list?.map((x) => (x.hash === f.hash ? { ...x, public: !value } : x)));
      toast.error(errorMessage(e));
    }
  };

  const remove = async (f: FileEntry) => {
    const ok = await confirm({
      title: `Usunąć plik „${f.name}”?`,
      message: 'Plik zniknie z serwera i ze wszystkich misji, do których był dodany.',
      confirmLabel: 'Usuń plik',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteFile(f.hash);
      state.setData((list) => list?.filter((x) => x.hash !== f.hash));
      toast.ok(`Usunięto ${f.name}.`);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <>
      <PageHeader
        title="Pliki"
        description="Paczki danych, mapy i dokumenty dostępne dla aplikacji TAK. Publiczne pliki widzi każdy użytkownik serwera."
        actions={
          <button type="button" className="btn btn-primary" onClick={() => inputRef.current?.click()} disabled={!!uploading}>
            {uploading ? <LoaderCircle size={18} className="spinner" /> : <Upload size={18} />}
            {uploading ? 'Wysyłanie…' : 'Wyślij plik'}
          </button>
        }
      />
      <input
        ref={inputRef}
        type="file"
        multiple
        hidden
        onChange={(e) => {
          if (e.target.files?.length) void upload(e.target.files);
          e.target.value = '';
        }}
      />

      <div
        className={`dropzone${dragOver ? ' over' : ''}`}
        onDragOver={(e) => {
          e.preventDefault();
          setDragOver(true);
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragOver(false);
          if (e.dataTransfer.files.length) void upload(e.dataTransfer.files);
        }}
      >
        {uploading ? (
          <span className="row">
            <LoaderCircle size={18} className="spinner" /> Wysyłanie „{uploading}”…
          </span>
        ) : (
          <span>Przeciągnij pliki tutaj albo użyj przycisku „Wyślij plik”.</span>
        )}
      </div>

      <AsyncView state={state}>
        {(files) =>
          files.length === 0 ? (
            <Empty icon={FileText} title="Brak plików" compact>
              Pliki wysłane z aplikacji (np. paczki danych) i stąd pojawią się na tej liście.
            </Empty>
          ) : (
            <div className="list">
              {[...files]
                .sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime())
                .map((f) => (
                  <div key={f.hash} className="list-row file-row">
                    <div className="list-row-main">
                      <span className="list-row-title" title={f.name}>
                        {f.name}
                      </span>
                      <span className="list-row-meta">
                        <span>{fmtBytes(f.size)}</span>
                        <span>{fmtDateTime(f.createdAt)}</span>
                        {f.submitter && <span>od {f.submitter}</span>}
                        {f.keywords.length > 0 && <span>{f.keywords.join(', ')}</span>}
                      </span>
                    </div>
                    <div className="list-row-side">
                      <label className="row small muted nowrap" style={{ gap: 8 }}>
                        <span className="hide-sm">Publiczny</span>
                        <Switch checked={f.public} onChange={(v) => void togglePublic(f, v)} label={`Publiczny: ${f.name}`} />
                      </label>
                      <a className="icon-btn" href={api.fileDownloadUrl(f.hash)} download={f.name} aria-label={`Pobierz ${f.name}`}>
                        <Download size={18} />
                      </a>
                      <button type="button" className="icon-btn danger" onClick={() => remove(f)} aria-label={`Usuń ${f.name}`}>
                        <Trash size={18} />
                      </button>
                    </div>
                  </div>
                ))}
            </div>
          )
        }
      </AsyncView>
    </>
  );
}
