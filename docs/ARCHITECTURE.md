# ChameleonTAK — architektura

ChameleonTAK to samodzielny serwer TAK (zgodny z ATAK / iTAK / WinTAK / TAKAware) dostarczany jako
gotowy obraz Raspberry Pi OS (Debian, arm64). Po pierwszym uruchomieniu operator otwiera panel WWW,
wpisuje nazwę serwera i (opcjonalnie) porty — reszta (CA, certyfikaty, usługi, firewall, sieć) robi się sama.

## 1. Komponenty

Jeden plik binarny `chameleontak` (Go, bez CGO, panel WWW wbudowany przez `embed`):

| Usługa | Port domyślny | TLS | Uwierzytelnianie |
|---|---|---|---|
| Strumień CoT (TLS) | 8089/tcp | tak, mTLS | certyfikat klienta (CN = nazwa użytkownika) |
| Strumień CoT (TCP, opcjonalny, domyślnie wyłączony) | 8087/tcp | nie | brak (grupa `__ANON__`) |
| Marti API | 8443/tcp | tak | certyfikat klienta **lub** Basic auth |
| Enrollment certyfikatów (Marti) | 8446/tcp | tak (bez wymogu certu klienta) | Basic auth (hasło lub jednorazowy token) |
| Panel WWW (HTTP) | 80/tcp | nie | sesja (cookie) |
| Panel WWW (HTTPS) | 443/tcp | tak (cert serwera z naszego CA albo Let's Encrypt) | sesja (cookie) |

Moduły Go (`internal/…`):

- `config` — ustawienia serwera (trzymane w SQLite jako JSON pod kluczem `config`).
- `store` — SQLite (`modernc.org/sqlite`, czyste Go): użytkownicy, urządzenia, tokeny, grupy, certyfikaty, pliki, misje, czat, pozycje.
- `pki` — własne CA (RSA), certyfikat serwera (SAN: nazwa serwera + wszystkie IP + hostname), certyfikaty klientów, podpisywanie CSR, PKCS#12 (tryb legacy zgodny z Androidem/iOS), unieważnianie (sprawdzane przy handshake).
- `cot` — parser strumienia XML CoT (ramkowanie zdarzeń `<event>`), model zdarzenia, budowanie zdarzeń (pong, czat, t-x-m-c…).
- `stream` — serwery 8089/8087, sesje klientów, router (grupy, `marti/dest`, ping, cache ostatnich SA).
- `marti` — Marti API: enrollment, wersja, grupy, pliki (Data Package server), misje (Data Sync), profile urządzeń.
- `datapkg` — generatory paczek danych (ATAK, iTAK, WinTAK, TAKAware) i treści kodów QR.
- `panel` — REST API panelu, WebSocket na żywo, kreator pierwszego uruchomienia, strona `/join/<token>`, serwowanie SPA.
- `system` — integracja z systemem (czas, hostname, sieć/hotspot, firewall, restart) przez pomocnika `chameleontak sys …` uruchamianego przez sudo.

Panel WWW: `web/` (React + TypeScript + Vite) → build do `internal/panel/dist` → wbudowany w binarkę.

## 2. Binarka i CLI

```
chameleontak serve   [--data DIR] [--http ADDR] [--https ADDR] [--dev]
chameleontak setup   --server-name X --admin-user U --admin-pass P [--platforms atak,itak] [--cot-port 8089 ...]
chameleontak reset-admin --password P            # nowe hasło administratora panelu
chameleontak factory-reset --yes                 # kasuje dane i wraca do kreatora
chameleontak version
chameleontak sys <akcja> [argumenty]             # pomocnik root (tylko Linux), patrz §5
```

- Katalog danych: `--data` lub `CHAMELEONTAK_DATA`, domyślnie `/var/lib/chameleontak`.
- `--http` / `--https` nadpisują porty panelu (np. `:8080` w trybie deweloperskim na Windows).

Układ katalogu danych:

```
/var/lib/chameleontak/
  chameleontak.db          SQLite
  .setup-done              znacznik zakończonego kreatora (czytany też przez skrypty systemowe)
  pki/ca.crt ca.key server.crt server.key truststore.p12
  files/<sha256>           pliki / paczki danych
  acme/                    cache Let's Encrypt
```

## 3. Pierwsze uruchomienie (Raspberry Pi)

1. Raspberry Pi startuje z obrazu. `chameleontak.service` uruchamia się w trybie kreatora (tylko panel HTTP na :80).
2. Jeśli w ciągu ~60 s nie ma sieci (brak kabla i brak skonfigurowanego Wi-Fi), skrypt `network-fallback`
   tworzy hotspot `ChameleonTAK-Setup` (hasło `chameleontak`) z portalem przechwytującym → telefon/laptop
   po podłączeniu od razu widzi kreator pod `http://10.42.0.1`.
   Z kablem LAN: `http://chameleontak.local` albo IP Raspberry.
3. Kreator: nazwa serwera (podpowiedź = wykryte IP), porty (domyślne wstępnie wypełnione), login/hasło administratora,
   platformy (ATAK/iTAK/WinTAK/TAKAware), tryb sieci (LAN / własny hotspot w terenie), opcjonalnie Let's Encrypt i SSH.
   Przeglądarka wysyła swój czas — jeśli Pi nie ma NTP, zegar jest ustawiany przed wygenerowaniem certyfikatów.
4. Serwer: ustawia czas → generuje CA → certyfikat serwera → zapisuje konfigurację → stosuje firewall/sieć →
   uruchamia nasłuch 8089/8443/8446/443 bez restartu procesu → zapisuje `.setup-done`.

## 4. REST API panelu (kontrakt dla `web/`)

Wszystko pod `/api`, JSON, błędy jako `{"error": "komunikat po polsku"}` z kodem 4xx/5xx.
Sesja: cookie `ctak_session` (HttpOnly, SameSite=Lax). Daty w RFC3339.
Endpointy oznaczone **[publiczny]** nie wymagają logowania; reszta wymaga zalogowanego administratora.

### 4.1 Status, kreator, logowanie

- **[publiczny]** `GET /api/status` →
  `{"setupDone":bool,"version":"0.1.0","serverName":"…","displayName":"…","now":"…"}`
- **[publiczny, tylko przed konfiguracją]** `GET /api/setup/defaults` →
  ```json
  {"suggestedServerName":"192.168.1.50","hostname":"chameleontak",
   "addresses":[{"iface":"eth0","ip":"192.168.1.50"}],
   "ports":{"cotTls":8089,"cotTcp":8087,"cotTcpEnabled":false,"marti":8443,"enroll":8446,"http":80,"https":443},
   "platforms":[{"key":"atak","label":"ATAK (Android)"},{"key":"itak","label":"iTAK (iOS)"},{"key":"wintak","label":"WinTAK (Windows)"},{"key":"takaware","label":"TAKAware (iOS)"}],
   "isPi":true,"wifiAvailable":true,"ntpSynced":false,"serverTime":"…"}
  ```
- **[publiczny, tylko przed konfiguracją]** `POST /api/setup` →
  ```json
  {"serverName":"192.168.1.50","displayName":"ChameleonTAK","organization":"ChameleonTAK",
   "ports":{"cotTls":8089,"cotTcp":8087,"cotTcpEnabled":false,"marti":8443,"enroll":8446,"http":80,"https":443},
   "admin":{"username":"admin","password":"…"},
   "platforms":["atak","itak"],
   "clientTime":"2026-10-25T12:00:00Z",
   "letsEncrypt":{"enabled":false,"email":""},
   "network":{"mode":"lan","staticIp":false,"hotspotSsid":"ChameleonTAK","hotspotPassword":"…"},
   "ssh":{"enabled":false}}
  ```
  → `202 {"started":true}`
- **[publiczny]** `GET /api/setup/progress` →
  `{"running":bool,"done":bool,"error":"","panelUrl":"http://…","steps":[{"key":"time","label":"Ustawienie czasu","state":"pending|running|done|error|skipped","message":""}]}`
- **[publiczny]** `POST /api/login` `{"username","password"}` → `{"id","username"}`; `POST /api/logout`; `GET /api/me` → `{"id","username"}` lub 401.

### 4.2 Pulpit, ustawienia, system

- `GET /api/dashboard` →
  `{"online":3,"users":10,"devices":12,"groups":2,"files":5,"missions":1,"uptimeSec":3600,
    "listeners":[{"name":"CoT TLS","port":8089,"state":"running|stopped|error","error":""}],
    "system":{…jak /api/system…}}`
- `GET /api/settings` → obiekt `Settings`:
  ```json
  {"serverName":"…","displayName":"…","organization":"…",
   "ports":{…jak wyżej…},"platforms":["atak","itak"],
   "certValidityDays":730,"tokenValidityHours":168,"certPassword":"atakatak",
   "forceHttps":false,"letsEncrypt":{"enabled":false,"email":""},
   "profile":{"defaultTeam":"Cyan","defaultRole":"Team Member","lockSettings":false,"extraPrefs":{}},
   "mapTileUrl":"https://tile.openstreetmap.org/{z}/{x}/{y}.png"}
  ```
- `PUT /api/settings` (częściowy obiekt `Settings`) → `{"settings":Settings,"restarted":bool,"certsRegenerated":bool}`
  (zmiana nazwy serwera ⇒ nowy certyfikat serwera; zmiana portów ⇒ restart nasłuchów).
- `GET /api/system` →
  `{"hostname","isPi":bool,"model","cpuTempC":51.2,"load1":0.3,"memTotal","memUsed","diskTotal","diskUsed",
    "addresses":[{"iface","ip"}],"network":{"mode":"lan|hotspot","ssid":""},"time":"…","ntpSynced":bool}`
- `POST /api/system/reboot`; `POST /api/system/network` `{"mode":"lan|hotspot","wifiSsid","wifiPassword","hotspotSsid","hotspotPassword"}`
- `GET /api/system/backup` → `application/gzip` (tar.gz katalogu danych); `POST /api/system/restore` (multipart `file`);
  `POST /api/system/factory-reset` `{"confirm":"RESET"}`.

### 4.3 Użytkownicy, urządzenia, onboarding

Platformy: `atak`, `itak`, `wintak`, `takaware`.

`Device`:
```json
{"id":7,"userId":3,"name":"Telefon","platform":"atak","clientUid":"ANDROID-…","online":true,"lastSeen":"…",
 "certSerial":"…","certExpires":"…",
 "enrollment":{"token":"K7P2QX9M","expiresAt":"…","used":false,
               "joinUrl":"http://192.168.1.50/join/K7P2QX9M",
               "qr":{"join":"http://…/join/K7P2QX9M","native":"tak://com.atakmap.app/enroll?…"}}}
```
`User`:
```json
{"id":3,"username":"jan","callsign":"JAN","team":"Cyan","role":"Team Member","disabled":false,
 "groups":["Alfa"],"devices":[Device],"online":true,"createdAt":"…"}
```

- `GET /api/users` → `[User]`
- `POST /api/users` `{"username","callsign?","team?","role?","password?","groups":["Alfa"],"devices":[{"platform":"atak","name":"Telefon"}]}` → `User`
- `POST /api/users/bulk` `{"prefix":"alfa","count":10,"startAt":1,"groups":[],"platform":"atak","team":"Cyan"}` → `[User]`
- `GET /api/users/{id}`, `PUT /api/users/{id}` (te same pola co POST, bez `devices`), `DELETE /api/users/{id}` (unieważnia certyfikaty)
- `POST /api/users/{id}/devices` `{"platform","name"}` → `Device`
- `DELETE /api/devices/{id}` (unieważnia certyfikat urządzenia)
- `POST /api/devices/{id}/token` → `Device` (nowy jednorazowy token)
- `GET /api/devices/{id}/package` → `application/zip` (paczka dla platformy urządzenia, z certyfikatem)

Strona dołączania (bez logowania, link/QR dla użytkownika końcowego):
- **[publiczny]** `GET /api/join/{token}` →
  ```json
  {"valid":true,"expiresAt":"…","displayName":"…","serverName":"…",
   "user":{"username":"jan","callsign":"JAN"},
   "device":{"id":7,"name":"Telefon","platform":"atak"},
   "options":[{"platform":"atak","label":"ATAK (Android)","packageUrl":"/api/join/K7P2QX9M/package/atak",
               "nativeQr":"tak://…","manual":{"host":"…","port":8089,"enrollPort":8446,"username":"jan","password":"K7P2QX9M"}}]}
  ```
  albo `{"valid":false,"reason":"expired|used|unknown"}`.
- **[publiczny]** `GET /api/join/{token}/package/{platform}` → `application/zip`.

### 4.4 Grupy, klienci, mapa, czat

- `GET /api/groups` → `[{"id","name","description","members":3}]`; `POST /api/groups` `{"name","description"}`;
  `PUT /api/groups/{id}`; `DELETE /api/groups/{id}`. Członkostwo ustawia się przez pole `groups` użytkownika.
  Użytkownik bez grup należy do `__ANON__`.
- `GET /api/clients` → `[{"id","uid","callsign","username","deviceId","platform","version","ip","transport":"tls|tcp","connectedAt","groups":[]}]`;
  `DELETE /api/clients/{id}` (rozłącz).
- `GET /api/positions` →
  `[{"uid","callsign","username","kind":"contact|marker","type","lat","lon","hae","course","speed","battery","team","role","online","lastSeen","remarks"}]`
- `GET /api/chat?limit=100` → `[{"id","time","from","fromUid","to","text"}]`; `POST /api/chat` `{"text","to?"}` (wysyła GeoChat jako serwer).
- `GET /api/ws` (WebSocket) — komunikaty `{"type":"position","data":Position}`, `{"type":"remove","data":{"uid"}}`,
  `{"type":"client","data":{"event":"connect|disconnect","client":Client}}`, `{"type":"chat","data":ChatMessage}`.

### 4.5 Pliki, misje, certyfikaty

- `GET /api/files` → `[{"hash","name","mime","size","keywords":[],"creatorUid","submitter","public":bool,"createdAt"}]`;
  `POST /api/files` (multipart `file`); `PUT /api/files/{hash}` `{"public":bool}`; `DELETE /api/files/{hash}`;
  `GET /api/files/{hash}/download`.
- `GET /api/missions` → `[{"name","description","creatorUid","createdAt","contents":2,"uids":5,"subscribers":3,"passwordProtected":false,"groups":[]}]`;
  `POST /api/missions` `{"name","description","groups":[]}`; `GET /api/missions/{name}` →
  `{…, "files":[File], "items":[{"uid","callsign","type","lat","lon","createdAt"}], "subscribers":[{"clientUid","username","role","createdAt"}], "changes":[{"type","time","creatorUid","hash","uid"}]}`;
  `DELETE /api/missions/{name}`; `POST /api/missions/{name}/files` `{"hash"}`; `DELETE /api/missions/{name}/files/{hash}`.
- `GET /api/certs` → `[{"serial","cn","kind":"ca|server|client","userId","deviceId","notBefore","notAfter","revoked":bool}]`;
  `POST /api/certs/{serial}/revoke`.
- **[publiczny]** `GET /api/ca.pem`, **[publiczny]** `GET /api/truststore.p12`.

## 5. Pomocnik systemowy `chameleontak sys` (Linux, uruchamiany przez sudo)

Usługa działa jako użytkownik `chameleontak` (z `CAP_NET_BIND_SERVICE`). Operacje wymagające roota idą przez
`sudo /usr/local/bin/chameleontak sys <akcja>` (wpis w `/etc/sudoers.d/chameleontak`, tylko ta komenda).
Każda akcja waliduje argumenty. Sekrety (hasła) są czytane ze stdin, nie z argumentów.

| Akcja | Argumenty | Działanie |
|---|---|---|
| `set-time` | `<RFC3339>` | ustawia zegar systemowy (tylko gdy brak synchronizacji NTP) |
| `set-hostname` | `<nazwa>` | `hostnamectl set-hostname` + wpis `127.0.1.1` w `/etc/hosts` |
| `firewall` | `<tcp-porty-CSV>` | zapisuje `/etc/chameleontak/firewall.nft` i stosuje go `nft -f` (podmienia tylko własną tabelę; nic nie robi, gdy istnieje `/etc/chameleontak/firewall.disabled`) |
| `wifi-client` | `<ssid>` (hasło ze stdin) | łączy z siecią Wi-Fi przez NetworkManager |
| `hotspot` | `<ssid>` (hasło ze stdin) | trwały hotspot `chameleontak-hotspot` (10.42.0.1/24) |
| `hotspot-off` | — | usuwa hotspot i hotspot kreatora |
| `setup-hotspot-off` | — | usuwa tylko hotspot kreatora i portal przechwytujący |
| `static-ip` | `<iface> <cidr> <gateway> <dns>` | zamienia bieżący adres DHCP na stały |
| `ssh` | `enable\|disable` | włącza/wyłącza `ssh.service` |
| `set-password` | `<user>` (hasło ze stdin) | `chpasswd` dla konta systemowego |
| `reboot` | — | restart |

## 6. Obraz Raspberry Pi

- Bazą jest Raspberry Pi OS Lite arm64 (Debian Trixie), budowany przez `pi-gen` z własnym etapem `image/stage-chameleontak`.
- Budowanie: GitHub Actions (`.github/workflows/image.yml`) → `chameleontak-<wersja>-arm64.img.xz`.
- Alternatywa dla istniejącego Debiana/Raspberry Pi OS: `scripts/install.sh`.
- Szczegóły w `docs/IMAGE.md`.
