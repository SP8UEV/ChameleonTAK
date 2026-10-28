import { useState } from 'react';
import { LoaderCircle, RefreshCw } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { DdnsConfig, DdnsProvider, DdnsStatus } from '../api/types';
import { fmtDateTime, fmtRelative } from '../lib/format';
import { Field, Notice } from './ui';

const PROVIDERS: { key: DdnsProvider; label: string }[] = [
  { key: '', label: 'Wyłączona — rekord DNS ustawiam sam' },
  { key: 'duckdns', label: 'DuckDNS (darmowa domena *.duckdns.org)' },
  { key: 'cloudflare', label: 'Cloudflare' },
  { key: 'url', label: 'Inny dostawca (adres aktualizacji)' },
];

/** Ustawienia automatycznej aktualizacji rekordu A domeny (dynamiczny DNS). */
export function DdnsFields({ value, onChange, domain }: { value: DdnsConfig; onChange: (v: DdnsConfig) => void; domain: string }) {
  const set = (patch: Partial<DdnsConfig>) => onChange({ ...value, ...patch });
  const duckWrong = value.provider === 'duckdns' && domain !== '' && !/\.duckdns\.org$/i.test(domain);
  return (
    <div className="stack-sm">
      <Field
        label="Automatyczna aktualizacja adresu domeny"
        htmlFor="ddns-provider"
        hint="Gdy dostawca internetu zmieni publiczny adres routera, serwer sam przestawi domenę na nowy adres (sprawdza co 5 minut)."
      >
        <select
          id="ddns-provider"
          className="select"
          value={value.provider}
          onChange={(e) => set({ provider: e.target.value as DdnsProvider })}
        >
          {PROVIDERS.map((p) => (
            <option key={p.key} value={p.key}>
              {p.label}
            </option>
          ))}
        </select>
      </Field>
      {value.provider === 'duckdns' && (
        <>
          {duckWrong && <Notice kind="warn">Dla DuckDNS domena musi kończyć się na .duckdns.org, np. mojtak.duckdns.org.</Notice>}
          <Field label="Token DuckDNS" htmlFor="ddns-token" hint="Widoczny na duckdns.org po zalogowaniu, nad listą domen.">
            <input
              id="ddns-token"
              className="input input-code"
              type="password"
              autoComplete="off"
              value={value.token}
              onChange={(e) => set({ token: e.target.value.trim() })}
            />
          </Field>
        </>
      )}
      {value.provider === 'cloudflare' && (
        <Field
          label="Token API Cloudflare"
          htmlFor="ddns-token"
          hint="Utwórz go w My Profile → API Tokens z szablonu „Edit zone DNS” dla strefy tej domeny. Rekord będzie miał wyłączone proxy (pomarańczową chmurkę), bo TAK przez nie nie działa."
        >
          <input
            id="ddns-token"
            className="input input-code"
            type="password"
            autoComplete="off"
            value={value.token}
            onChange={(e) => set({ token: e.target.value.trim() })}
          />
        </Field>
      )}
      {value.provider === 'url' && (
        <Field
          label="Adres aktualizacji"
          htmlFor="ddns-url"
          hint={
            <>
              Adres podany przez dostawcę DNS. Wpisz <span className="code">{'{ip}'}</span> w miejscu nowego adresu i{' '}
              <span className="code">{'{domain}'}</span> w miejscu domeny. Np. No-IP:{' '}
              <span className="code">{'https://login:hasło@dynupdate.no-ip.com/nic/update?hostname={domain}&myip={ip}'}</span>
            </>
          }
        >
          <input
            id="ddns-url"
            className="input input-code"
            autoComplete="off"
            autoCapitalize="none"
            autoCorrect="off"
            value={value.url}
            placeholder="https://…"
            onChange={(e) => set({ url: e.target.value.trim() })}
          />
        </Field>
      )}
    </div>
  );
}

/** Stan dynamicznego DNS z przyciskiem „Aktualizuj teraz”. */
export function DdnsStatusView({ status, onChange }: { status: DdnsStatus; onChange: (s: DdnsStatus) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  if (status.state === 'off') return null;
  const update = async () => {
    setBusy(true);
    setError(null);
    try {
      onChange(await api.ddnsUpdate());
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const dns = status.dnsAddrs?.length ? status.dnsAddrs.join(', ') : 'brak rekordu A';
  const kind = status.state === 'ok' ? 'ok' : status.state === 'error' ? 'danger' : 'info';
  return (
    <Notice kind={kind}>
      <div className="stack-sm">
        <span>
          {status.state === 'ok' && (
            <>
              Domena <span className="code">{status.domain}</span> wskazuje na publiczny adres routera{' '}
              <span className="code">{status.publicIp}</span>.
            </>
          )}
          {status.state === 'pending' && (
            <>
              Domena <span className="code">{status.domain}</span> wskazuje jeszcze na {dns}, a publiczny adres to{' '}
              <span className="code">{status.publicIp}</span>.
            </>
          )}
          {status.state === 'error' && <>Nie udało się zaktualizować adresu domeny.</>}
        </span>
        {status.message && <span className="small">{status.message}</span>}
        <span className="small muted">
          {status.lastUpdate ? `Ostatnia aktualizacja: ${fmtDateTime(status.lastUpdate)}. ` : ''}
          {status.checked ? `Sprawdzono ${fmtRelative(status.checked)}.` : ''}
        </span>
        <button type="button" className="btn btn-sm" style={{ alignSelf: 'flex-start' }} onClick={update} disabled={busy}>
          {busy ? <LoaderCircle size={16} className="spinner" /> : <RefreshCw size={16} />} Aktualizuj teraz
        </button>
        {error && <span className="small text-danger">{error}</span>}
      </div>
    </Notice>
  );
}
