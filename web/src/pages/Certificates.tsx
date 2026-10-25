import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { Ban, Download, ShieldCheck } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { Cert } from '../api/types';
import { useConfirm } from '../components/Confirm';
import { useToast } from '../components/Toast';
import { AsyncView, CheckRow, Empty, PageHeader } from '../components/ui';
import { daysUntil, fmtDate } from '../lib/format';
import { useApi } from '../lib/useApi';

const KIND: Record<Cert['kind'], { label: string; cls: string }> = {
  ca: { label: 'Urząd (CA)', cls: 'badge-accent' },
  server: { label: 'Serwer', cls: 'badge-info' },
  client: { label: 'Urządzenie', cls: '' },
};

function expiryInfo(c: Cert): { text: string; cls: string } {
  const d = daysUntil(c.notAfter);
  if (d === null) return { text: '—', cls: '' };
  if (d < 0) return { text: `wygasł ${fmtDate(c.notAfter)}`, cls: 'text-danger' };
  if (d < 30) return { text: `wygasa za ${d === 0 ? 'mniej niż dzień' : d === 1 ? '1 dzień' : `${d} dni`}`, cls: 'text-warn' };
  return { text: `do ${fmtDate(c.notAfter)}`, cls: '' };
}

export default function Certificates() {
  const state = useApi(api.certs, []);
  const toast = useToast();
  const confirm = useConfirm();
  const [showRevoked, setShowRevoked] = useState(false);

  const revoke = async (c: Cert) => {
    const ok = await confirm({
      title: `Unieważnić certyfikat ${c.cn}?`,
      message: 'Urządzenie z tym certyfikatem nie połączy się już z serwerem. Aby je przywrócić, wygeneruj nowy kod dołączenia.',
      confirmLabel: 'Unieważnij',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.revokeCert(c.serial);
      state.setData((list) => list?.map((x) => (x.serial === c.serial ? { ...x, revoked: true } : x)));
      toast.ok(`Unieważniono certyfikat ${c.cn}.`);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };

  const sorted = useMemo(() => {
    const order = { ca: 0, server: 1, client: 2 } as const;
    return [...(state.data ?? [])]
      .filter((c) => showRevoked || !c.revoked)
      .sort((a, b) => order[a.kind] - order[b.kind] || new Date(a.notAfter).getTime() - new Date(b.notAfter).getTime());
  }, [state.data, showRevoked]);

  const revokedCount = (state.data ?? []).filter((c) => c.revoked).length;
  const expiringCount = (state.data ?? []).filter((c) => {
    const d = daysUntil(c.notAfter);
    return !c.revoked && d !== null && d < 30;
  }).length;

  return (
    <>
      <PageHeader
        title="Certyfikaty"
        description="Serwer sam wydaje certyfikaty urządzeniom podczas dołączania. Tutaj sprawdzisz ich ważność i unieważnisz zgubione."
        actions={
          <>
            <a className="btn" href={api.caPemUrl} download="chameleontak-ca.pem">
              <Download size={18} /> Certyfikat CA (.pem)
            </a>
            <a className="btn" href={api.truststoreUrl} download="truststore.p12">
              <Download size={18} /> Truststore (.p12)
            </a>
          </>
        }
      />
      <AsyncView state={state}>
        {(list) =>
          list.length === 0 ? (
            <Empty icon={ShieldCheck} title="Brak certyfikatów">
              Certyfikaty pojawią się, gdy urządzenia zaczną dołączać.
            </Empty>
          ) : (
            <div className="stack">
              <div className="row-between">
                <span className="small muted">
                  {expiringCount > 0 ? (
                    <span className="text-warn">Wygasające w ciągu 30 dni: {expiringCount}</span>
                  ) : (
                    'Żaden certyfikat nie wygasa w ciągu 30 dni.'
                  )}
                </span>
                {revokedCount > 0 && (
                  <CheckRow checked={showRevoked} onChange={setShowRevoked} label={`Pokaż unieważnione (${revokedCount})`} />
                )}
              </div>
              <div className="list">
                {sorted.map((c) => {
                  const exp = expiryInfo(c);
                  return (
                    <div key={c.serial} className={`list-row${c.revoked ? ' is-revoked' : ''}`}>
                      <div className="list-row-main">
                        <span className="list-row-title">
                          {c.userId ? <Link to={`/users/${c.userId}`}>{c.cn}</Link> : c.cn}
                        </span>
                        <span className="list-row-meta">
                          <span className={`badge ${KIND[c.kind]?.cls ?? ''}`}>{KIND[c.kind]?.label ?? c.kind}</span>
                          {c.revoked ? (
                            <span className="badge badge-danger">unieważniony</span>
                          ) : (
                            <span className={exp.cls}>{exp.text}</span>
                          )}
                          <span className="code xsmall faint" title="Numer seryjny">
                            {c.serial}
                          </span>
                        </span>
                      </div>
                      {c.kind === 'client' && !c.revoked && (
                        <div className="list-row-side">
                          <button type="button" className="btn btn-sm btn-danger" onClick={() => revoke(c)}>
                            <Ban size={16} /> <span className="hide-sm">Unieważnij</span>
                          </button>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          )
        }
      </AsyncView>
    </>
  );
}
