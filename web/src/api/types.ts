// Typy odpowiadające kontraktowi z docs/ARCHITECTURE.md §4.
// Daty są stringami RFC3339.

export type PlatformKey = 'atak' | 'itak' | 'wintak' | 'takaware';

export interface Status {
  setupDone: boolean;
  version: string;
  serverName: string;
  displayName: string;
  now: string;
}

export interface Address {
  iface: string;
  ip: string;
}

export interface Ports {
  cotTls: number;
  cotTcp: number;
  cotTcpEnabled: boolean;
  marti: number;
  enroll: number;
  http: number;
  https: number;
}

export interface PlatformInfo {
  key: PlatformKey;
  label: string;
}

export interface SetupDefaults {
  suggestedServerName: string;
  hostname: string;
  addresses: Address[];
  ports: Ports;
  platforms: PlatformInfo[];
  isPi: boolean;
  wifiAvailable: boolean;
  ntpSynced: boolean;
  serverTime: string;
}

export interface LetsEncrypt {
  enabled: boolean;
  email: string;
}

export type NetworkMode = 'lan' | 'hotspot';

/** Jak telefony docierają do serwera: sieć lokalna, własna domena albo publiczny adres IP. */
export type AccessMode = 'local' | 'domain' | 'ip' | 'zerotier';

export interface PublicCertStatus {
  state: 'off' | 'pending' | 'ok' | 'error';
  host: string;
  notAfter: string | null;
  error: string;
  nextAttempt: string | null;
}

export interface PublicAccess {
  mode: AccessMode;
  host: string;
  letsEncrypt: boolean;
  cert: PublicCertStatus;
  httpPort: number;
  ddns: DdnsStatus;
  main: boolean;
}

/** Dostawca dynamicznego DNS; '' = wyłączone. */
export type DdnsProvider = '' | 'duckdns' | 'cloudflare' | 'url';

export interface DdnsConfig {
  provider: DdnsProvider;
  token: string;
  url: string;
}

export interface DdnsStatus {
  state: 'off' | 'pending' | 'ok' | 'error';
  provider: DdnsProvider;
  domain: string;
  publicIp: string;
  dnsAddrs: string[] | null;
  lastUpdate: string | null;
  checked: string | null;
  message: string;
}

/** Ograniczenie panelu do wybranych adresów IP, sieci i adresów MAC. */
export interface PanelAccessConfig {
  restrict: boolean;
  allow: string[];
}

export interface ClientAddress {
  ip: string;
  mac: string;
}

export interface PortResult {
  port: number;
  external: number;
  name: string;
  ours: boolean;
  listening: boolean;
  firewall: 'open' | 'blocked' | 'unknown';
  internet: 'open' | 'closed' | 'unknown';
  want: 'open' | 'closed' | 'any';
  level: 'ok' | 'info' | 'warn' | 'error';
  suggestion: string;
}

export interface PortCheck {
  mode: AccessMode;
  publicIp: string;
  lanIp: string;
  firewall: 'managed' | 'disabled' | 'unknown';
  externalError: string;
  dns: { host: string; addrs: string[] | null; matches: boolean } | null;
  ports: PortResult[];
  hints: string[] | null;
  checked: string;
}

export interface PublicIP {
  ip: string;
  public: boolean;
}

export interface SetupRequest {
  serverName: string;
  displayName: string;
  organization: string;
  ports: Ports;
  admin: { username: string; password: string };
  platforms: PlatformKey[];
  clientTime: string;
  letsEncrypt: LetsEncrypt;
  network: {
    mode: NetworkMode;
    staticIp: boolean;
    hotspotSsid: string;
    hotspotPassword: string;
  };
  ssh: { enabled: boolean };
}

export type StepState = 'pending' | 'running' | 'done' | 'error' | 'skipped';

export interface SetupStep {
  key: string;
  label: string;
  state: StepState;
  message: string;
}

export interface SetupProgress {
  running: boolean;
  done: boolean;
  error: string;
  panelUrl: string;
  steps: SetupStep[];
}

export interface Me {
  id: number;
  username: string;
}

export type ListenerState = 'running' | 'stopped' | 'error';

export interface Listener {
  name: string;
  port: number;
  state: ListenerState;
  error: string;
}

export interface SystemInfo {
  hostname: string;
  isPi: boolean;
  model: string;
  cpuTempC: number;
  load1: number;
  memTotal: number;
  memUsed: number;
  diskTotal: number;
  diskUsed: number;
  addresses: Address[];
  network: { mode: NetworkMode; ssid: string };
  time: string;
  ntpSynced: boolean;
}

export interface Dashboard {
  online: number;
  users: number;
  devices: number;
  groups: number;
  files: number;
  missions: number;
  uptimeSec: number;
  listeners: Listener[];
  system: SystemInfo;
}

export interface DeviceProfile {
  defaultTeam: string;
  defaultRole: string;
  lockSettings: boolean;
  extraPrefs: Record<string, unknown>;
}

export interface Settings {
  serverName: string;
  displayName: string;
  organization: string;
  ports: Ports;
  platforms: PlatformKey[];
  certValidityDays: number;
  tokenValidityHours: number;
  certPassword: string;
  forceHttps: boolean;
  letsEncrypt: LetsEncrypt;
  profile: DeviceProfile;
  mapTileUrl: string;
  ddns: DdnsConfig;
  panelAccess: PanelAccessConfig;
  /** Serwer, którego dotyczą ustawienia; false = dodatkowy serwer (panel, sieć i system są wspólne). */
  serverId: string;
  main: boolean;
  /** Sieć ZeroTier urządzenia i adresy, które w niej dostało. */
  zerotier: { network: string; ips: string[] };
}

export interface ZeroTierNetwork {
  id: string;
  name: string;
  status: string;
  addrs: string[];
  device: string;
}

export interface ZeroTierStatus {
  supported: boolean;
  installed: boolean;
  running: boolean;
  online: boolean;
  nodeId: string;
  version: string;
  network: string;
  networks: ZeroTierNetwork[];
  error: string;
}

export interface SettingsUpdateResult {
  settings: Settings;
  restarted: boolean;
  certsRegenerated: boolean;
}

export interface NetworkRequest {
  mode: NetworkMode;
  wifiSsid: string;
  wifiPassword: string;
  hotspotSsid: string;
  hotspotPassword: string;
}

export interface Enrollment {
  token: string;
  expiresAt: string;
  used: boolean;
  joinUrl: string;
  qr: { join: string; native: string };
  /** Telefon ufa portowi rejestracji bez instalowania profilu (Let’s Encrypt). */
  publicCert: boolean;
}

export interface Device {
  id: number;
  userId: number;
  name: string;
  platform: PlatformKey;
  clientUid: string;
  online: boolean;
  lastSeen: string;
  certSerial: string;
  certExpires: string;
  enrollment: Enrollment | null;
}

export interface User {
  id: number;
  username: string;
  callsign: string;
  team: string;
  role: string;
  disabled: boolean;
  groups: string[];
  devices: Device[];
  online: boolean;
  createdAt: string;
}

export interface NewDevice {
  platform: PlatformKey;
  name: string;
}

export interface UserInput {
  username: string;
  callsign?: string;
  team?: string;
  role?: string;
  password?: string;
  groups: string[];
  disabled?: boolean;
}

export interface CreateUserRequest extends UserInput {
  devices: NewDevice[];
}

export interface BulkUsersRequest {
  prefix: string;
  count: number;
  startAt: number;
  groups: string[];
  platform: PlatformKey;
  team: string;
}

export interface JoinOption {
  platform: PlatformKey;
  label: string;
  packageUrl: string;
  nativeQr: string;
  manual: {
    host: string;
    port: number;
    enrollPort: number;
    username: string;
    password: string;
  };
}

export type JoinInvalidReason = 'expired' | 'used' | 'unknown';

export interface JoinValid {
  valid: true;
  expiresAt: string;
  displayName: string;
  serverName: string;
  publicCert: boolean;
  /** Serwer, do którego należy kod (kilka serwerów na jednym urządzeniu). */
  server?: string;
  /** Sieć ZeroTier, do której telefon musi dołączyć ("" = niepotrzebna). */
  zerotier?: string;
  /** Nazwa urzędu certyfikacji (widoczna w ustawieniach zaufania iOS). */
  caName: string;
  user: { username: string; callsign: string };
  device: { id: number; name: string; platform: PlatformKey };
  options: JoinOption[];
}

export interface JoinInvalid {
  valid: false;
  reason: JoinInvalidReason;
}

export type JoinInfo = JoinValid | JoinInvalid;

export interface Group {
  id: number;
  name: string;
  description: string;
  members: number;
}

export interface Client {
  id: string;
  uid: string;
  callsign: string;
  username: string;
  deviceId: number | null;
  platform: string;
  version: string;
  ip: string;
  transport: 'tls' | 'tcp';
  connectedAt: string;
  groups: string[];
}

export interface Position {
  uid: string;
  callsign: string;
  username: string;
  kind: 'contact' | 'marker';
  type: string;
  lat: number;
  lon: number;
  hae: number;
  course: number;
  speed: number;
  battery: number;
  team: string;
  role: string;
  online: boolean;
  lastSeen: string;
  remarks: string;
}

export interface ChatMessage {
  id: number | string;
  time: string;
  from: string;
  fromUid: string;
  to: string;
  text: string;
}

export interface FileEntry {
  hash: string;
  name: string;
  mime: string;
  size: number;
  keywords: string[];
  creatorUid: string;
  submitter: string;
  public: boolean;
  createdAt: string;
}

export interface Mission {
  name: string;
  description: string;
  creatorUid: string;
  createdAt: string;
  contents: number;
  uids: number;
  subscribers: number;
  passwordProtected: boolean;
  groups: string[];
}

export interface MissionItem {
  uid: string;
  callsign: string;
  type: string;
  lat: number;
  lon: number;
  createdAt: string;
}

export interface MissionSubscriber {
  clientUid: string;
  username: string;
  role: string;
  createdAt: string;
}

export interface MissionChange {
  type: string;
  time: string;
  creatorUid: string;
  hash: string;
  uid: string;
}

export interface MissionDetail extends Omit<Mission, 'subscribers'> {
  files: FileEntry[];
  items: MissionItem[];
  // W liście misji `subscribers` to liczba, w szczegółach — tablica.
  subscribers: MissionSubscriber[];
  changes: MissionChange[];
}

export interface Cert {
  serial: string;
  cn: string;
  kind: 'ca' | 'server' | 'client';
  userId: number | null;
  deviceId: number | null;
  notBefore: string;
  notAfter: string;
  revoked: boolean;
}

// WebSocket /api/ws
export type WsMessage =
  | { type: 'position'; data: Position }
  | { type: 'remove'; data: { uid: string } }
  | { type: 'client'; data: { event: 'connect' | 'update' | 'disconnect'; client: Client } }
  | { type: 'chat'; data: ChatMessage };

/** Zdarzenie z dziennika serwera (GET /api/logs, najnowsze pierwsze). */
export interface LogEntry {
  time: string;
  level: 'INFO' | 'WARN' | 'ERROR' | string;
  message: string;
  attrs: Record<string, string>;
}

/* ---------- ADS-B ---------- */
export interface AdsbConfig {
  bridge: boolean;
  source: string;
  lat: number;
  lon: number;
  altM: number;
  maxRangeKm: number;
  maxAltM: number;
  groups: string[];
  /** Serwery, które dostają samoloty; pusta lista = serwer główny. */
  servers: string[];
  fr24: boolean;
  fr24Key: string;
  adsbx: boolean;
  adsbxUuid: string;
}

export interface AdsbComponent {
  installed: boolean;
  active: boolean;
  detail: string;
}

export interface AdsbPlane {
  hex: string;
  callsign: string;
  reg: string;
  type: string;
  lat: number;
  lon: number;
  altM: number;
  ground: boolean;
  speedKmh: number;
  track: number;
  category: string;
  squawk: string;
  military: boolean;
  distKm: number;
  seen: string;
}

export interface AdsbJob {
  component: '' | 'readsb' | 'fr24';
  running: boolean;
  error: string;
  started: string | null;
  finished: string | null;
}

export interface AdsbStatus {
  supported: boolean;
  sdrs: { name: string; usb: string }[] | null;
  decoder: AdsbComponent;
  fr24: AdsbComponent;
  adsbx: AdsbComponent;
  tar1090: string;
  source: string;
  readError: string;
  lastRead: string | null;
  messages: number;
  planes: AdsbPlane[];
  sending: boolean;
  sentAt: string | null;
  job: AdsbJob;
}

export interface AdsbInfo {
  config: AdsbConfig;
  status: AdsbStatus;
}

export interface AdsbSaveResult {
  config: AdsbConfig;
  steps: { step: string; ok: boolean; error: string }[];
}

/* ---------- Kilka serwerów TAK na jednym urządzeniu ---------- */
export interface ServerInfo {
  id: string;
  main: boolean;
  displayName: string;
  serverName: string;
  ports: Ports;
  platforms: PlatformKey[];
  clients: number;
  users: number;
  listeners: Listener[] | null;
}

export interface ServerList {
  servers: ServerInfo[];
  suggestedPorts: Ports;
}

export interface NewServer {
  id: string;
  displayName: string;
  serverName: string;
  ports: Pick<Ports, 'cotTls' | 'marti' | 'enroll'>;
  platforms: PlatformKey[];
}

/* ---------- Test sieci ---------- */
export interface InternetSpeed {
  pingMs: number;
  jitterMs: number;
  downMbps: number;
  upMbps: number;
  error: string;
  checked: string;
}

export interface ServerLoad {
  clients: number;
  planes: number;
  cpus: number;
  load1: number;
  memTotal: number;
  memAvail: number;
}

/* ---------- Wideo na żywo ---------- */
export type StreamAudience = 'all' | 'groups' | 'users';

export interface VideoStream {
  id: string;
  name: string;
  audience: StreamAudience;
  groups: string[];
  users: string[];
  publishKey: string;
  createdAt: string;
  path: string;
  live: boolean;
  viewers: number;
  publishUrl: string;
  larixUrl: string;
}

export interface VideoService {
  supported: boolean;
  installed: boolean;
  enabled: boolean;
  active: boolean;
  error: string;
  rtmpPort: number;
  rtspPort: number;
}

export interface VideoStreamList {
  streams: VideoStream[];
  service: VideoService;
}

export interface StreamInput {
  name: string;
  audience: StreamAudience;
  groups: string[];
  users: string[];
}
