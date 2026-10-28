import { useState } from 'react';
import { KeyRound, LoaderCircle, Plus, Save, X } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { PanelAccessConfig } from '../api/types';
import { useApi } from '../lib/useApi';
import { useToast } from './Toast';
import { CheckRow, Field, Notice } from './ui';

/** Zmiana hasła zalogowanego administratora. */
export function PasswordForm() {
  const toast = useToast();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [repeat, setRepeat] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (next.length < 8) return setError('Nowe hasło musi mieć co najmniej 8 znaków.');
    if (next !== repeat) return setError('Powtórzone hasło się nie zgadza.');
    setError(null);
    setBusy(true);
    try {
      await api.changePassword(current, next);
      setCurrent('');
      setNext('');
      setRepeat('');
      toast.ok('Zmieniono hasło. Inne zalogowane przeglądarki zostały wylogowane.');
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="stack-sm" onSubmit={submit} noValidate>
      <h3 className="strong">Zmiana hasła</h3>
      {error && <Notice kind="danger">{error}</Notice>}
      <div className="form-grid">
        <Field label="Obecne hasło" htmlFor="pw-current">
          <input
            id="pw-current"
            className="input"
            type="password"
            autoComplete="current-password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
          />
        </Field>
        <span />
        <Field label="Nowe hasło" htmlFor="pw-new" hint="Co najmniej 8 znaków.">
          <input id="pw-new" className="input" type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
        </Field>
        <Field label="Powtórz nowe hasło" htmlFor="pw-repeat">
          <input
            id="pw-repeat"
            className="input"
            type="password"
            autoComplete="new-password"
            value={repeat}
            onChange={(e) => setRepeat(e.target.value)}
          />
        </Field>
      </div>
      <button type="submit" className="btn" style={{ alignSelf: 'flex-start' }} disabled={busy || !current || !next}>
        {busy ? <LoaderCircle size={18} className="spinner" /> : <KeyRound size={18} />} Zmień hasło
      </button>
    </form>
  );
}

const MAC_RE = /^([0-9a-f]{2}[:-]){5}[0-9a-f]{2}$/i;
const IPV4_RE = /^(\d{1,3}\.){3}\d{1,3}(\/\d{1,2})?$/;

function looksLikeEntry(s: string) {
  return MAC_RE.test(s) || IPV4_RE.test(s) || (s.includes(':') && /^[0-9a-f:./]+$/i.test(s));
}

/** Lista adresów, z których wolno otworzyć panel. */
export function AccessListForm({ value, save }: { value: PanelAccessConfig; save: (v: PanelAccessConfig) => Promise<boolean> }) {
  const me = useApi(api.clientAddress, []);
  const [restrict, setRestrict] = useState(value.restrict);
  const [allow, setAllow] = useState<string[]>(value.allow ?? []);
  const [entry, setEntry] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const dirty = restrict !== value.restrict || allow.join('|') !== (value.allow ?? []).join('|');

  const add = (v: string) => {
    v = v.trim();
    if (!v) return;
    if (!looksLikeEntry(v)) return setError(`„${v}” nie wygląda na adres IP, sieć (np. 192.168.1.0/24) ani adres MAC.`);
    setError(null);
    const norm = MAC_RE.test(v) ? v.toLowerCase().replace(/-/g, ':') : v;
    if (!allow.includes(norm)) setAllow([...allow, norm]);
    setEntry('');
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (restrict && allow.length === 0) return setError('Dodaj co najmniej jeden adres.');
    setError(null);
    setBusy(true);
    try {
      await save({ restrict, allow });
    } finally {
      setBusy(false);
    }
  };

  const mine = me.data;
  const meCovered = mine && (allow.includes(mine.ip) || (mine.mac !== '' && allow.includes(mine.mac)));

  return (
    <form className="stack-sm" onSubmit={submit} noValidate>
      <h3 className="strong">Dostęp do panelu</h3>
      <CheckRow
        checked={restrict}
        onChange={setRestrict}
        label="Panel tylko z wybranych adresów"
        hint="Inne urządzenia zobaczą komunikat o braku dostępu. Telefony nadal otworzą stronę dołączania z kodu QR, a aplikacje TAK łączą się normalnie."
      />
      {restrict && (
        <>
          {error && <Notice kind="danger">{error}</Notice>}
          {allow.length > 0 ? (
            <ul className="access-list">
              {allow.map((a) => (
                <li key={a}>
                  <span className="code">{a}</span>
                  <span className="xsmall muted grow">
                    {MAC_RE.test(a) ? 'adres MAC' : a.includes('/') ? 'sieć' : 'adres IP'}
                    {mine && (a === mine.ip || a === mine.mac) ? ' · to urządzenie' : ''}
                  </span>
                  <button type="button" className="btn btn-sm btn-ghost" aria-label={`Usuń ${a}`} onClick={() => setAllow(allow.filter((x) => x !== a))}>
                    <X size={16} />
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="small muted">Lista jest pusta.</p>
          )}
          <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
            <input
              className="input input-code grow"
              style={{ minWidth: 0, flexBasis: 220 }}
              value={entry}
              placeholder="192.168.1.20, 192.168.1.0/24 albo aa:bb:cc:dd:ee:ff"
              aria-label="Adres do dodania"
              autoCapitalize="none"
              autoCorrect="off"
              onChange={(e) => setEntry(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  add(entry);
                }
              }}
            />
            <button type="button" className="btn" onClick={() => add(entry)} disabled={!entry.trim()}>
              <Plus size={18} /> Dodaj
            </button>
          </div>
          {mine && !meCovered && (
            <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
              <button type="button" className="btn btn-sm" onClick={() => add(mine.ip)}>
                <Plus size={16} /> Mój adres IP ({mine.ip})
              </button>
              {mine.mac && (
                <button type="button" className="btn btn-sm" onClick={() => add(mine.mac)}>
                  <Plus size={16} /> Mój adres MAC ({mine.mac})
                </button>
              )}
            </div>
          )}
          <p className="xsmall muted">
            Adres MAC działa tylko dla urządzeń w tej samej sieci lokalnej co serwer (z internetu widać tylko adres IP). Jeśli
            stracisz dostęp, wyłącz ograniczenie w konsoli Raspberry Pi: <span className="code">sudo -u chameleontak chameleontak panel-access off</span>{' '}
            i <span className="code">sudo systemctl restart chameleontak</span>.
          </p>
        </>
      )}
      <button type="submit" className="btn btn-primary" style={{ alignSelf: 'flex-end' }} disabled={!dirty || busy}>
        {busy ? <LoaderCircle size={18} className="spinner" /> : <Save size={18} />} Zapisz
      </button>
    </form>
  );
}
