import { useState } from 'react';
import { Download, Link2, RefreshCw, ShieldCheck, Trash } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { Device, Settings, User } from '../api/types';
import { fmtDateTime, fmtRelative, isPast } from '../lib/format';
import { platformLabel } from '../lib/tak';
import { useConfirm } from './Confirm';
import { QrCode } from './QrCode';
import { useToast } from './Toast';
import { CopyButton, CopyField, Led, Notice } from './ui';

export function DeviceCard({
  device,
  user,
  settings,
  onChange,
  onDeleted,
}: {
  device: Device;
  user: User;
  settings: Settings | null;
  onChange: (d: Device) => void;
  onDeleted: () => void;
}) {
  const toast = useToast();
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const enr = device.enrollment;
  const expired = !!enr && isPast(enr.expiresAt);
  const active = !!enr && !enr.used && !expired;
  const registered = !!device.certSerial;

  const regenerate = async () => {
    if (active) {
      const ok = await confirm({
        title: 'Wygenerować nowy kod?',
        message: 'Obecny kod QR i hasło jednorazowe przestaną działać. Wydrukowane karty trzeba będzie wymienić.',
        confirmLabel: 'Generuj nowy kod',
      });
      if (!ok) return;
    }
    setBusy(true);
    try {
      const d = await api.newToken(device.id);
      onChange(d);
      toast.ok('Wygenerowano nowy kod.');
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    const ok = await confirm({
      title: `Usunąć urządzenie „${device.name}”?`,
      message: 'Certyfikat urządzenia zostanie unieważniony, a aplikacja na nim straci połączenie z serwerem.',
      confirmLabel: 'Usuń urządzenie',
      danger: true,
    });
    if (!ok) return;
    setBusy(true);
    try {
      await api.deleteDevice(device.id);
      toast.ok(`Usunięto urządzenie ${device.name}.`);
      onDeleted();
    } catch (e) {
      toast.error(errorMessage(e));
      setBusy(false);
    }
  };

  const nativeLabel = `Albo zeskanuj w aplikacji ${platformLabel(device.platform)}`;
  const host = settings?.serverName ?? '';
  const port = settings?.ports.cotTls;
  const enrollPort = settings?.ports.enroll;

  return (
    <article className="device-card">
      <header className="device-head">
        <Led state={device.online ? 'on' : 'off'} large label={device.online ? 'W sieci' : 'Poza siecią'} />
        <div className="grow" style={{ minWidth: 0 }}>
          <h3 className="ellipsis">{device.name}</h3>
          <div className="list-row-meta">
            <span className="badge">{platformLabel(device.platform)}</span>
            <span>{device.online ? 'w sieci teraz' : device.lastSeen ? `ostatnio ${fmtRelative(device.lastSeen)}` : 'jeszcze się nie łączyło'}</span>
            {registered && (
              <span className="row nowrap" style={{ gap: 4 }}>
                <ShieldCheck size={14} /> certyfikat do {fmtDateTime(device.certExpires)}
              </span>
            )}
          </div>
        </div>
      </header>

      {active && enr ? (
        <div className="onboard">
          <div className="onboard-primary">
            <QrCode value={enr.qr.join} size={240} label="Kod QR strony dołączania" />
            <div className="onboard-caption">Zeskanuj aparatem telefonu</div>
            <div className="row xsmall muted" style={{ justifyContent: 'center', gap: 4 }}>
              <Link2 size={14} />
              <a href={enr.joinUrl} target="_blank" rel="noreferrer" className="ellipsis" style={{ maxWidth: 200 }}>
                {enr.joinUrl}
              </a>
              <CopyButton text={enr.joinUrl} label="Kopiuj link" />
            </div>
          </div>
          <div className="onboard-side stack">
            {enr.qr.native && (
              <div className="onboard-native">
                <QrCode value={enr.qr.native} size={132} label="Kod QR dla aplikacji" />
                <div className="small">{nativeLabel}</div>
              </div>
            )}
            <div className="stack-sm">
              <div className="small strong">Albo wpisz ręcznie</div>
              <CopyField label="Serwer" value={host} />
              <CopyField label="Port" value={port ? String(port) : ''} />
              <CopyField label="Użytkownik" value={user.username} />
              <CopyField label="Hasło jednorazowe" value={enr.token} big />
              {enrollPort && enrollPort !== 8446 && (
                <div className="xsmall faint">Port rejestracji certyfikatu: {enrollPort}</div>
              )}
            </div>
            <div className="xsmall muted">
              Kod ważny do {fmtDateTime(enr.expiresAt)} ({fmtRelative(enr.expiresAt)}). Działa tylko raz.
            </div>
          </div>
        </div>
      ) : (
        <div className="onboard-state">
          {enr?.used || (registered && !enr) ? (
            <Notice kind="ok">
              Urządzenie jest zarejestrowane. Jeśli trzeba skonfigurować je od nowa (np. nowy telefon), wygeneruj nowy kod.
            </Notice>
          ) : expired ? (
            <Notice kind="warn">Kod dołączenia wygasł {fmtRelative(enr?.expiresAt)}. Wygeneruj nowy, aby skonfigurować urządzenie.</Notice>
          ) : (
            <Notice>To urządzenie nie ma aktywnego kodu dołączenia.</Notice>
          )}
        </div>
      )}

      <footer className="device-actions">
        <a className="btn btn-sm" href={api.devicePackageUrl(device.id)} download>
          <Download size={16} /> Pobierz paczkę
        </a>
        <button type="button" className="btn btn-sm" onClick={regenerate} disabled={busy}>
          <RefreshCw size={16} /> {active ? 'Nowy kod' : 'Wygeneruj kod'}
        </button>
        <button type="button" className="btn btn-sm btn-danger" onClick={remove} disabled={busy} style={{ marginLeft: 'auto' }}>
          <Trash size={16} /> Usuń urządzenie
        </button>
      </footer>
    </article>
  );
}
