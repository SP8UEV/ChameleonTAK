import { lazy, Suspense, useCallback, useEffect, useState } from 'react';
import { Navigate, Route, Routes, useNavigate } from 'react-router-dom';
import { api } from './api/endpoints';
import { errorMessage, setUnauthorizedHandler } from './api/client';
import type { Status } from './api/types';
import { Layout } from './app/Layout';
import { RequireAuth, StatusProvider } from './app/session';
import { ErrorState, Loading } from './components/ui';
const SetupWizard = lazy(() => import('./pages/SetupWizard'));
const Login = lazy(() => import('./pages/Login'));
const Dashboard = lazy(() => import('./pages/Dashboard'));
const MapPage = lazy(() => import('./pages/MapPage'));
const Users = lazy(() => import('./pages/Users'));
const UserDetail = lazy(() => import('./pages/UserDetail'));
const PrintCards = lazy(() => import('./pages/PrintCards'));
const Join = lazy(() => import('./pages/Join'));
const Groups = lazy(() => import('./pages/Groups'));
const Clients = lazy(() => import('./pages/Clients'));
const Logs = lazy(() => import('./pages/Logs'));
const Chat = lazy(() => import('./pages/Chat'));
const FilesPage = lazy(() => import('./pages/Files'));
const Missions = lazy(() => import('./pages/Missions'));
const MissionDetail = lazy(() => import('./pages/MissionDetail'));
const Certificates = lazy(() => import('./pages/Certificates'));
const SettingsPage = lazy(() => import('./pages/Settings'));
const AdsbPage = lazy(() => import('./pages/Adsb'));
const ServersPage = lazy(() => import('./pages/Servers'));
const NotFound = lazy(() => import('./pages/NotFound'));

const PUBLIC_PREFIXES = ['/login', '/join/', '/setup'];

/** Każde 401 z API → ekran logowania (z powrotem na bieżącą stronę). */
function UnauthorizedRedirect() {
  const navigate = useNavigate();
  useEffect(() => {
    setUnauthorizedHandler(() => {
      const { pathname, search } = window.location;
      if (PUBLIC_PREFIXES.some((p) => pathname.startsWith(p))) return;
      const next = pathname + search;
      navigate(`/login${next && next !== '/' ? `?next=${encodeURIComponent(next)}` : ''}`, { replace: true });
    });
    return () => setUnauthorizedHandler(null);
  }, [navigate]);
  return null;
}

export default function App() {
  const [status, setStatus] = useState<Status | null>(null);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setStatus(await api.status());
      setError(null);
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  if (!status) {
    if (error) {
      return (
        <div className="page page-narrow">
          <ErrorState message={error} onRetry={() => void refresh()} />
        </div>
      );
    }
    return <Loading label="Łączenie z serwerem…" />;
  }

  return (
    <StatusProvider status={status} refresh={refresh}>
      <UnauthorizedRedirect />
      <Suspense fallback={<Loading />}>
      {!status.setupDone ? (
        <Routes>
          <Route path="/setup" element={<SetupWizard />} />
          <Route path="*" element={<Navigate to="/setup" replace />} />
        </Routes>
      ) : (
        <Routes>
          <Route path="/setup" element={<Navigate to="/" replace />} />
          <Route path="/login" element={<Login />} />
          <Route path="/join/:token" element={<Join />} />
          <Route element={<RequireAuth />}>
            <Route path="/print" element={<PrintCards />} />
            <Route element={<Layout />}>
              <Route index element={<Dashboard />} />
              <Route path="/map" element={<MapPage />} />
              <Route path="/users" element={<Users />} />
              <Route path="/users/:id" element={<UserDetail />} />
              <Route path="/groups" element={<Groups />} />
              <Route path="/clients" element={<Clients />} />
              <Route path="/logs" element={<Logs />} />
              <Route path="/chat" element={<Chat />} />
              <Route path="/files" element={<FilesPage />} />
              <Route path="/missions" element={<Missions />} />
              <Route path="/missions/:name" element={<MissionDetail />} />
              <Route path="/certs" element={<Certificates />} />
              <Route path="/adsb" element={<AdsbPage />} />
              <Route path="/servers" element={<ServersPage />} />
              <Route path="/settings" element={<SettingsPage />} />
              <Route path="*" element={<NotFound />} />
            </Route>
          </Route>
        </Routes>
      )}
      </Suspense>
    </StatusProvider>
  );
}
