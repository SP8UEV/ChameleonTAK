import { useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { ArrowLeft, IdCard, Printer } from 'lucide-react';
import { api } from '../api/endpoints';
import type { Device, User } from '../api/types';
import { useSession, useStatus } from '../app/session';
import { QrCode } from '../components/QrCode';
import { AsyncView, CheckRow, Empty } from '../components/ui';
import { countLabel, fmtDateTime, isPast } from '../lib/format';
import { platformLabel } from '../lib/tak';
import { useApi } from '../lib/useApi';

interface CardData {
  user: User;
  device: Device;
}

function hasActiveCode(d: Device): boolean {
  return !!d.enrollment && !d.enrollment.used && !isPast(d.enrollment.expiresAt);
}

export default function PrintCards() {
  const [params] = useSearchParams();
  const users = useApi(api.users, []);
  const ids = useMemo(
    () =>
      (params.get('users') ?? '')
        .split(',')
        .map((s) => Number(s.trim()))
        .filter((n) => Number.isInteger(n) && n > 0),
    [params],
  );

  return (
    <div className="print-page">
      <AsyncView state={users} loadingLabel="Przygotowywanie kart…">
        {(list) => <CardsSheet users={ids.length ? list.filter((u) => ids.includes(u.id)) : list} />}
      </AsyncView>
    </div>
  );
}

function CardsSheet({ users }: { users: User[] }) {
  const { settings } = useSession();
  const { status } = useStatus();
  const [onlyActive, setOnlyActive] = useState(true);

  const all: CardData[] = useMemo(
    () => users.flatMap((user) => user.devices.map((device) => ({ user, device }))),
    [users],
  );
  const cards = onlyActive ? all.filter((c) => hasActiveCode(c.device)) : all;
  const skipped = all.length - cards.length;
  const serverTitle = status.displayName || settings?.displayName || 'ChameleonTAK';
  const backTo = users.length === 1 ? `/users/${users[0].id}` : '/users';

  return (
    <>
      <div className="print-toolbar no-print">
        <Link to={backTo} className="btn btn-ghost">
          <ArrowLeft size={18} /> Wróć
        </Link>
        <div className="grow">
          <div className="strong">Karty dołączenia</div>
          <div className="small muted">
            {countLabel(cards.length, 'karta', 'karty', 'kart')} dla {countLabel(users.length, 'użytkownika', 'użytkowników', 'użytkowników')}
            {skipped > 0 && onlyActive && `, pominięto ${skipped} bez aktywnego kodu`}
          </div>
        </div>
        <CheckRow checked={onlyActive} onChange={setOnlyActive} label="Tylko aktywne kody" />
        <button type="button" className="btn btn-primary" onClick={() => window.print()} disabled={cards.length === 0}>
          <Printer size={18} /> Drukuj
        </button>
      </div>

      {cards.length === 0 ? (
        <Empty icon={IdCard} title="Brak kart do wydruku">
          Wybrani użytkownicy nie mają urządzeń z aktywnym kodem. Wygeneruj nowe kody na stronie użytkownika.
        </Empty>
      ) : (
        <div className="print-sheet">
          {cards.map(({ user, device }) => (
            <article key={device.id} className="print-card">
              <header className="print-card-head">
                <div>
                  <div className="print-callsign">{user.callsign || user.username}</div>
                  <div className="print-sub">
                    {device.name}, {platformLabel(device.platform)}
                  </div>
                </div>
                <div className="print-server">{serverTitle}</div>
              </header>
              <div className="print-card-body">
                {device.enrollment ? (
                  <QrCode value={device.enrollment.qr.join} size={150} label={`Kod QR dla ${user.username}`} />
                ) : (
                  <div className="qr" style={{ width: 150 }}>
                    <div className="qr-error">Brak kodu</div>
                  </div>
                )}
                <div className="print-creds">
                  <div className="print-step">Zeskanuj kod aparatem telefonu (nie w aplikacji TAK) i postępuj według strony, która się otworzy.</div>
                  <div className="print-kv">
                    <div className="print-kv-wide">
                      <span>Serwer</span>
                      <strong>{settings?.serverName ?? '—'}</strong>
                    </div>
                    <div>
                      <span>Port</span>
                      <strong>{settings?.ports.cotTls ?? '—'}</strong>
                    </div>
                    <div>
                      <span>Użytkownik</span>
                      <strong>{user.username}</strong>
                    </div>
                    <div className="print-kv-wide">
                      <span>Hasło jednorazowe</span>
                      <strong className="print-token">{device.enrollment?.token ?? '—'}</strong>
                    </div>
                  </div>
                  {device.enrollment && (
                    <div className="print-exp">
                      {hasActiveCode(device)
                        ? `Ważny do ${fmtDateTime(device.enrollment.expiresAt)}`
                        : device.enrollment.used
                          ? 'Kod już wykorzystany'
                          : 'Kod wygasł'}
                    </div>
                  )}
                </div>
              </div>
            </article>
          ))}
        </div>
      )}
    </>
  );
}
