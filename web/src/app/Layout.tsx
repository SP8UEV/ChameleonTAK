import { Suspense, useEffect, useState } from 'react';
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import {
  Ellipsis,
  Files,
  FileText,
  Flag,
  Group,
  LayoutDashboard,
  LogOut,
  Map as MapIcon,
  MessagesSquare,
  MonitorSmartphone,
  Moon,
  Plane,
  Server,
  Settings as SettingsIcon,
  ShieldCheck,
  Sun,
  Users,
  type LucideIcon,
} from 'lucide-react';
import { api } from '../api/endpoints';
import { BrandMark, Led, Loading } from '../components/ui';
import { ErrorBoundary } from '../components/ErrorBoundary';
import { useTheme } from '../lib/theme';
import { useLive, useWsStatus } from '../lib/ws';
import { useSession, useStatus } from './session';
import { ServerSwitcher } from '../components/ServerSwitcher';

interface NavDef {
  to: string;
  label: string;
  icon: LucideIcon;
  end?: boolean;
}

const NAV_MAIN: NavDef[] = [
  { to: '/', label: 'Pulpit', icon: LayoutDashboard, end: true },
  { to: '/map', label: 'Mapa', icon: MapIcon },
  { to: '/users', label: 'Użytkownicy', icon: Users },
  { to: '/groups', label: 'Grupy', icon: Group },
  { to: '/clients', label: 'Połączone urządzenia', icon: MonitorSmartphone },
  { to: '/chat', label: 'Czat', icon: MessagesSquare },
];
const NAV_DATA: NavDef[] = [
  { to: '/files', label: 'Pliki', icon: Files },
  { to: '/missions', label: 'Misje', icon: Flag },
  { to: '/adsb', label: 'ADS-B', icon: Plane },
];
const NAV_SERVER: NavDef[] = [
  { to: '/servers', label: 'Serwery', icon: Server },
  { to: '/certs', label: 'Certyfikaty', icon: ShieldCheck },
  { to: '/logs', label: 'Dziennik', icon: FileText },
  { to: '/settings', label: 'Ustawienia', icon: SettingsIcon },
];
const TABS: NavDef[] = [
  { to: '/', label: 'Pulpit', icon: LayoutDashboard, end: true },
  { to: '/map', label: 'Mapa', icon: MapIcon },
  { to: '/users', label: 'Ludzie', icon: Users },
  { to: '/chat', label: 'Czat', icon: MessagesSquare },
];

function NavItems({ items, onNavigate }: { items: NavDef[]; onNavigate?: () => void }) {
  return (
    <>
      {items.map((n) => (
        <NavLink
          key={n.to}
          to={n.to}
          end={n.end}
          className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
          onClick={onNavigate}
        >
          <n.icon size={20} />
          <span>{n.label}</span>
        </NavLink>
      ))}
    </>
  );
}

function FullNav({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <nav className="nav" aria-label="Menu główne">
      <NavItems items={NAV_MAIN} onNavigate={onNavigate} />
      <div className="nav-group-label">Dane</div>
      <NavItems items={NAV_DATA} onNavigate={onNavigate} />
      <div className="nav-group-label">Serwer</div>
      <NavItems items={NAV_SERVER} onNavigate={onNavigate} />
    </nav>
  );
}

function LiveIndicator() {
  const ws = useWsStatus();
  const on = ws === 'open';
  return (
    <div className="live-indicator" title="Połączenie na żywo z serwerem">
      <Led state={on ? 'on' : ws === 'connecting' ? 'warn' : 'off'} />
      {on ? 'Na żywo' : ws === 'connecting' ? 'Łączenie…' : 'Brak połączenia na żywo'}
    </div>
  );
}

export function Layout() {
  const { status } = useStatus();
  const { settings } = useSession();
  const [theme, toggleTheme] = useTheme();
  const [sheetOpen, setSheetOpen] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();
  const isMap = location.pathname === '/map';

  // Utrzymuje połączenie WebSocket na wszystkich ekranach (wskaźnik „Na żywo”).
  useLive(() => {});

  useEffect(() => {
    setSheetOpen(false);
  }, [location.pathname]);

  const logout = async () => {
    try {
      await api.logout();
    } catch {
      /* i tak przechodzimy do logowania */
    }
    navigate('/login', { replace: true });
  };

  // Nazwa wybranego serwera (przy kilku serwerach na urządzeniu panel pokazuje ten, którym zarządza).
  const shownName = settings?.displayName ?? status.displayName;
  const brandSub = shownName && shownName !== 'ChameleonTAK' ? shownName : (settings?.serverName ?? status.serverName);
  const themeLabel = theme === 'dark' ? 'Jasny motyw' : 'Ciemny motyw';
  const ThemeIcon = theme === 'dark' ? Sun : Moon;
  const moreActive = !TABS.some((t) => (t.end ? location.pathname === t.to : location.pathname.startsWith(t.to)));

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <NavLink to="/" className="brand">
          <BrandMark />
          <span className="brand-text">
            <span className="brand-name">ChameleonTAK</span>
            <span className="brand-sub">{brandSub}</span>
          </span>
        </NavLink>
        <ServerSwitcher />
        <FullNav />
        <div className="sidebar-foot">
          <LiveIndicator />
          <div className="row" style={{ gap: 4 }}>
            <button type="button" className="btn btn-ghost btn-sm grow" onClick={toggleTheme}>
              <ThemeIcon size={16} /> {themeLabel}
            </button>
            <button type="button" className="icon-btn" onClick={logout} aria-label="Wyloguj" title="Wyloguj">
              <LogOut size={18} />
            </button>
          </div>
          <div className="xsmall faint">Wersja {status.version}</div>
        </div>
      </aside>

      <header className="topbar">
        <NavLink to="/" className="brand">
          <BrandMark />
          <span className="brand-text">
            <span className="brand-name">ChameleonTAK</span>
            <span className="brand-sub">{brandSub}</span>
          </span>
        </NavLink>
        <button type="button" className="icon-btn" onClick={toggleTheme} aria-label={themeLabel}>
          <ThemeIcon size={20} />
        </button>
      </header>

      <main className="main">
        <div className={isMap ? 'page page-full' : 'page'}>
          <Suspense fallback={<Loading />}>
            <ErrorBoundary key={location.pathname}>
              <Outlet />
            </ErrorBoundary>
          </Suspense>
        </div>
      </main>

      <nav className="bottombar" aria-label="Menu dolne">
        {TABS.map((t) => (
          <NavLink key={t.to} to={t.to} end={t.end} className={({ isActive }) => `tab${isActive ? ' active' : ''}`}>
            <t.icon size={22} />
            <span>{t.label}</span>
          </NavLink>
        ))}
        <button
          type="button"
          className={`tab${moreActive || sheetOpen ? ' active' : ''}`}
          onClick={() => setSheetOpen(true)}
          aria-expanded={sheetOpen}
        >
          <Ellipsis size={22} />
          <span>Więcej</span>
        </button>
      </nav>

      {sheetOpen && (
        <div
          className="sheet-backdrop"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) setSheetOpen(false);
          }}
        >
          <div className="sheet" role="dialog" aria-label="Menu">
            <ServerSwitcher />
            <FullNav onNavigate={() => setSheetOpen(false)} />
            <div className="sheet-foot">
              <div className="grow" style={{ alignSelf: 'center' }}>
                <LiveIndicator />
              </div>
              <button type="button" className="btn btn-ghost" onClick={logout}>
                <LogOut size={18} /> Wyloguj
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
