import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { useParams } from 'react-router-dom';
import { CircleCheck, Clock, Download, ExternalLink, FolderOpen, KeyRound, LinkIcon, QrCode as QrIcon, Settings, ShieldAlert, type LucideIcon } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { JoinInfo, JoinOption, JoinValid, PlatformKey } from '../api/types';
import { QrCode } from '../components/QrCode';
import { BrandMark, CopyField, Disclosure, ErrorState, Loading } from '../components/ui';
import { fmtDateTime } from '../lib/format';
import { detectPlatform, platformLabel } from '../lib/tak';

interface Step {
  icon: LucideIcon;
  text: ReactNode;
}

function stepsFor(p: PlatformKey): { steps: Step[]; install: string } {
  switch (p) {
    case 'atak':
      return {
        steps: [
          { icon: ExternalLink, text: <>Stuknij <strong>Otwórz w ATAK</strong>. ATAK sam pobierze konfigurację — potwierdź import.</> },
          { icon: CircleCheck, text: <>Gotowe. Po chwili połączenie w ATAK zaświeci się na zielono.</> },
          {
            icon: FolderOpen,
            text: (
              <>
                Starszy ATAK (przed 5.1)? Stuknij <strong>Pobierz plik</strong>, potem w ATAK: menu → <strong>Import</strong> → wskaż pobrany plik ZIP.
              </>
            ),
          },
        ],
        install: 'Nie masz ATAK? Zainstaluj „ATAK-CIV” ze Sklepu Google Play.',
      };
    case 'itak':
      return {
        steps: [
          {
            icon: Download,
            text: (
              <>
                Stuknij <strong>Pobierz konfigurację</strong> i potwierdź <strong>Pobierz</strong>. Plik trafi do aplikacji Pliki, do
                folderu <strong>Pobrane</strong>.
              </>
            ),
          },
          {
            icon: Settings,
            text: (
              <>
                W iTAK stuknij <strong>Settings</strong> (koło zębate) → <strong>Network</strong> → <strong>Servers</strong> →{' '}
                <strong>+</strong> → <strong>Upload Server Package</strong> i wskaż pobrany plik.
              </>
            ),
          },
          { icon: CircleCheck, text: <>iTAK zaimportuje ustawienia, a serwer pojawi się na liście na zielono.</> },
        ],
        install: 'Nie masz iTAK? Zainstaluj go z App Store.',
      };
    case 'takaware':
      return {
        steps: [
          {
            icon: Download,
            text: (
              <>
                Stuknij <strong>Pobierz konfigurację</strong> i potwierdź <strong>Pobierz</strong>. Plik trafi do aplikacji Pliki, do
                folderu <strong>Pobrane</strong>.
              </>
            ),
          },
          {
            icon: Settings,
            text: (
              <>
                W TAKAware otwórz ustawienia, wybierz import paczki danych (<strong>Data Package</strong>) i wskaż pobrany plik.
              </>
            ),
          },
          { icon: CircleCheck, text: <>TAKAware połączy się z serwerem. Gotowe.</> },
        ],
        install: 'Nie masz TAKAware? Zainstaluj go z App Store.',
      };
    case 'wintak':
      return {
        steps: [
          { icon: Download, text: <>Kliknij <strong>Pobierz konfigurację</strong> i zapisz plik ZIP.</> },
          {
            icon: FolderOpen,
            text: (
              <>
                W WinTAK otwórz <strong>Import Manager</strong> i wskaż pobrany plik (albo przeciągnij go na mapę).
              </>
            ),
          },
          { icon: CircleCheck, text: <>Gotowe. Połączenie z serwerem zaświeci się na zielono.</> },
        ],
        install: 'WinTAK pobierzesz ze strony tak.gov.',
      };
  }
}

function pickInitial(info: JoinValid): PlatformKey | null {
  const detected = detectPlatform();
  const has = (p: PlatformKey | null) => !!p && info.options.some((o) => o.platform === p);
  if (has(detected)) return detected;
  // iPhone z TAKAware zamiast iTAK
  if (detected === 'itak' && has('takaware')) return 'takaware';
  if (has(info.device.platform)) return info.device.platform;
  return info.options[0]?.platform ?? null;
}

export default function Join() {
  const { token = '' } = useParams();
  const [info, setInfo] = useState<JoinInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let alive = true;
    setError(null);
    api
      .join(token)
      .then((r) => {
        if (alive) setInfo(r);
      })
      .catch((e) => {
        if (alive) setError(errorMessage(e));
      });
    return () => {
      alive = false;
    };
  }, [token, attempt]);

  useEffect(() => {
    document.documentElement.classList.add('join-root');
    return () => document.documentElement.classList.remove('join-root');
  }, []);

  let content: ReactNode;
  if (!info) {
    content = error ? (
      <ErrorState message={error} onRetry={() => setAttempt((a) => a + 1)} />
    ) : (
      <Loading label="Sprawdzanie kodu…" />
    );
  } else if (!info.valid) {
    content = <InvalidToken reason={info.reason} />;
  } else {
    content = <JoinValidView info={info} />;
  }

  return (
    <div className="join">
      <header className="join-head">
        <BrandMark className="brand-mark" />
        <span className="brand-name">
          {info && info.valid ? info.displayName || 'ChameleonTAK' : 'ChameleonTAK'}
        </span>
      </header>
      <main className="join-main">{content}</main>
    </div>
  );
}

function InvalidToken({ reason }: { reason: 'expired' | 'used' | 'unknown' }) {
  const text = {
    expired: {
      title: 'Ten kod wygasł',
      body: 'Kody dołączenia są ważne przez ograniczony czas. Poproś administratora serwera o nowy.',
      icon: Clock,
    },
    used: {
      title: 'Ten kod został już użyty',
      body: 'Każdy kod działa tylko raz. Jeśli konfigurujesz nowe urządzenie albo coś poszło nie tak, poproś administratora o nowy kod.',
      icon: CircleCheck,
    },
    unknown: {
      title: 'Nie rozpoznajemy tego kodu',
      body: 'Sprawdź, czy link jest kompletny, albo zeskanuj kod QR jeszcze raz. Jeśli to nie pomoże, poproś administratora o nowy.',
      icon: ShieldAlert,
    },
  }[reason] ?? { title: 'Kod jest nieważny', body: 'Poproś administratora serwera o nowy kod.', icon: ShieldAlert };
  const Icon = text.icon;
  return (
    <div className="join-invalid">
      <div className="state-icon">
        <Icon size={28} />
      </div>
      <h1>{text.title}</h1>
      <p className="muted">{text.body}</p>
    </div>
  );
}

function JoinValidView({ info }: { info: JoinValid }) {
  const [platform, setPlatform] = useState<PlatformKey | null>(() => pickInitial(info));
  const option: JoinOption | undefined = useMemo(
    () => info.options.find((o) => o.platform === platform) ?? info.options[0],
    [info.options, platform],
  );
  const guide = option ? stepsFor(option.platform) : null;
  const name = info.user.callsign || info.user.username;

  return (
    <div className="join-content">
      <section className="join-hello">
        <p className="muted">Dołączasz do serwera</p>
        <h1 className="join-title">{info.displayName || info.serverName}</h1>
        <p className="join-who">
          jako <strong>{name}</strong>
          {info.device.name && <span className="muted">, urządzenie „{info.device.name}”</span>}
        </p>
      </section>

      {info.options.length > 1 && (
        <div className="segmented" role="radiogroup" aria-label="Twoja aplikacja">
          {info.options.map((o) => (
            <button
              key={o.platform}
              type="button"
              role="radio"
              aria-checked={o.platform === option?.platform}
              className={`segment${o.platform === option?.platform ? ' active' : ''}`}
              onClick={() => setPlatform(o.platform)}
            >
              {platformLabel(o.platform)}
            </button>
          ))}
        </div>
      )}

      {option && guide ? (
        <>
          {option.platform === 'atak' && option.nativeQr.startsWith('tak://') ? (
            <>
              <a className="btn btn-primary join-btn" href={option.nativeQr}>
                <ExternalLink size={26} /> Otwórz w ATAK
              </a>
              <a className="btn btn-ghost" href={option.packageUrl} download>
                <Download size={18} /> Pobierz plik
              </a>
            </>
          ) : (
            <a className="btn btn-primary join-btn" href={option.packageUrl} download>
              <Download size={26} /> Pobierz konfigurację
            </a>
          )}
          <p className="xsmall muted join-for">{option.label}</p>

          <ol className="join-steps">
            {guide.steps.map((s, i) => (
              <li key={i} className="join-step">
                <span className="join-step-icon">
                  <s.icon size={24} />
                  <span className="join-step-num">{i + 1}</span>
                </span>
                <span className="join-step-text">{s.text}</span>
              </li>
            ))}
          </ol>
          <p className="small muted">{guide.install}</p>

          <div className="stack-sm">
            {option.nativeQr && (
              <Disclosure title="Kod QR do zeskanowania w aplikacji" icon={<QrIcon size={18} />}>
                <div className="stack-sm" style={{ alignItems: 'center', textAlign: 'center' }}>
                  <p className="small muted">
                    {option.platform === 'atak'
                      ? 'Zeskanuj aparatem telefonu z ATAK — otworzy ATAK i zaimportuje konfigurację.'
                      : `Otwórz tę stronę na innym ekranie i zeskanuj kod w ${platformLabel(option.platform)} (dodawanie serwera → skanuj QR).`}
                  </p>
                  <QrCode value={option.nativeQr} size={260} label="Kod QR dla aplikacji" />
                </div>
              </Disclosure>
            )}
            <Disclosure title="Wpisz dane ręcznie" icon={<KeyRound size={18} />}>
              <div className="stack-sm">
                <p className="small muted">
                  W aplikacji dodaj serwer, wybierz połączenie SSL/TLS i rejestrację certyfikatu loginem i hasłem.
                </p>
                <CopyField label="Serwer" value={option.manual.host} />
                <CopyField label="Port" value={String(option.manual.port)} />
                <CopyField label="Użytkownik" value={option.manual.username} />
                <CopyField label="Hasło jednorazowe" value={option.manual.password} big />
                {option.manual.enrollPort !== 8446 && (
                  <CopyField label="Port rejestracji" value={String(option.manual.enrollPort)} />
                )}
              </div>
            </Disclosure>
          </div>
        </>
      ) : (
        <p className="muted">Serwer nie udostępnia konfiguracji dla żadnej aplikacji. Skontaktuj się z administratorem.</p>
      )}

      <p className="xsmall faint join-foot">
        <LinkIcon size={12} style={{ display: 'inline', verticalAlign: '-1px' }} /> Link jest osobisty i działa jednorazowo. Ważny do{' '}
        {fmtDateTime(info.expiresAt)}.
      </p>
    </div>
  );
}
