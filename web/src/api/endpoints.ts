import { http } from './client';
import type {
  BulkUsersRequest,
  Cert,
  ChatMessage,
  Client,
  CreateUserRequest,
  Dashboard,
  Device,
  FileEntry,
  Group,
  JoinInfo,
  Me,
  Mission,
  MissionDetail,
  NetworkRequest,
  NewDevice,
  Position,
  SetupDefaults,
  SetupProgress,
  SetupRequest,
  Settings,
  SettingsUpdateResult,
  Status,
  SystemInfo,
  User,
  UserInput,
} from './types';

const enc = encodeURIComponent;

export const api = {
  // 4.1 Status, kreator, logowanie
  status: () => http.get<Status>('/api/status', { noAuthRedirect: true }),
  setupDefaults: () => http.get<SetupDefaults>('/api/setup/defaults', { noAuthRedirect: true }),
  setup: (req: SetupRequest) => http.post<{ started: boolean }>('/api/setup', req, { noAuthRedirect: true }),
  setupProgress: () => http.get<SetupProgress>('/api/setup/progress', { noAuthRedirect: true }),
  login: (username: string, password: string) =>
    http.post<Me>('/api/login', { username, password }, { noAuthRedirect: true }),
  logout: () => http.post<void>('/api/logout', {}, { noAuthRedirect: true }),
  me: () => http.get<Me>('/api/me'),

  // 4.2 Pulpit, ustawienia, system
  dashboard: () => http.get<Dashboard>('/api/dashboard'),
  settings: () => http.get<Settings>('/api/settings'),
  updateSettings: (patch: Partial<Settings>) => http.put<SettingsUpdateResult>('/api/settings', patch),
  system: () => http.get<SystemInfo>('/api/system'),
  reboot: () => http.post<void>('/api/system/reboot'),
  network: (req: NetworkRequest) => http.post<void>('/api/system/network', req),
  restore: (file: File) => http.upload<void>('/api/system/restore', file),
  factoryReset: () => http.post<void>('/api/system/factory-reset', { confirm: 'RESET' }),
  backupUrl: '/api/system/backup',

  // 4.3 Użytkownicy, urządzenia
  users: () => http.get<User[]>('/api/users'),
  user: (id: number) => http.get<User>(`/api/users/${id}`),
  createUser: (req: CreateUserRequest) => http.post<User>('/api/users', req),
  bulkUsers: (req: BulkUsersRequest) => http.post<User[]>('/api/users/bulk', req),
  updateUser: (id: number, req: UserInput) => http.put<User>(`/api/users/${id}`, req),
  deleteUser: (id: number) => http.del<void>(`/api/users/${id}`),
  addDevice: (userId: number, dev: NewDevice) => http.post<Device>(`/api/users/${userId}/devices`, dev),
  deleteDevice: (id: number) => http.del<void>(`/api/devices/${id}`),
  newToken: (id: number) => http.post<Device>(`/api/devices/${id}/token`),
  devicePackageUrl: (id: number) => `/api/devices/${id}/package`,

  join: (token: string) => http.get<JoinInfo>(`/api/join/${enc(token)}`, { noAuthRedirect: true }),

  // 4.4 Grupy, klienci, mapa, czat
  groups: () => http.get<Group[]>('/api/groups'),
  createGroup: (name: string, description: string) => http.post<Group>('/api/groups', { name, description }),
  updateGroup: (id: number, name: string, description: string) =>
    http.put<Group>(`/api/groups/${id}`, { name, description }),
  deleteGroup: (id: number) => http.del<void>(`/api/groups/${id}`),

  clients: () => http.get<Client[]>('/api/clients'),
  disconnectClient: (id: string) => http.del<void>(`/api/clients/${enc(id)}`),

  positions: () => http.get<Position[]>('/api/positions'),

  chat: (limit = 100) => http.get<ChatMessage[]>(`/api/chat?limit=${limit}`),
  sendChat: (text: string, to?: string) =>
    http.post<ChatMessage | undefined>('/api/chat', to ? { text, to } : { text }),

  // 4.5 Pliki, misje, certyfikaty
  files: () => http.get<FileEntry[]>('/api/files'),
  uploadFile: (file: File) => http.upload<FileEntry>('/api/files', file),
  setFilePublic: (hash: string, isPublic: boolean) => http.put<FileEntry>(`/api/files/${enc(hash)}`, { public: isPublic }),
  deleteFile: (hash: string) => http.del<void>(`/api/files/${enc(hash)}`),
  fileDownloadUrl: (hash: string) => `/api/files/${enc(hash)}/download`,

  missions: () => http.get<Mission[]>('/api/missions'),
  mission: (name: string) => http.get<MissionDetail>(`/api/missions/${enc(name)}`),
  createMission: (name: string, description: string, groups: string[]) =>
    http.post<Mission>('/api/missions', { name, description, groups }),
  deleteMission: (name: string) => http.del<void>(`/api/missions/${enc(name)}`),
  addMissionFile: (name: string, hash: string) => http.post<void>(`/api/missions/${enc(name)}/files`, { hash }),
  removeMissionFile: (name: string, hash: string) => http.del<void>(`/api/missions/${enc(name)}/files/${enc(hash)}`),

  certs: () => http.get<Cert[]>('/api/certs'),
  revokeCert: (serial: string) => http.post<void>(`/api/certs/${enc(serial)}/revoke`),
  caPemUrl: '/api/ca.pem',
  truststoreUrl: '/api/truststore.p12',
};
