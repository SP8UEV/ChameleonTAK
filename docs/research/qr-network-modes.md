# QR onboarding across network modes: ATAK, iTAK, TAKAware

Research notes for ChameleonTAK. The goal: a phone scans one QR code and ends up connected, in each of the network modes we support (LAN, own Wi-Fi hotspot `10.42.0.1` offline, public IP with port forwarding, user-owned domain, ZeroTier).

This builds on `docs/research/enrollment-certs-datapackages.md` (enrollment API, package layouts, `.pref` keys). That document is not repeated here.

Researched on 2026-10-26. Tags have the same meaning as in the companion document:

| Tag | Meaning |
|---|---|
| **[SRC]** | Read in primary source code or official docs (file path or URL given). |
| **[SRC-2]** | Secondary source: third-party code or docs that work with the closed-source client. |
| **[INFERRED]** | Deduced from several primary sources. Not seen directly. |
| **UNVERIFIED** | No primary source found. Test on real devices. |

## Sources and versions read

| Source | Version / commit | Used for |
|---|---|---|
| ATAK-CIV `deptofdefense/AndroidTacticalAssaultKit-CIV` | `main` 889eee292c = ATAK **4.6.0.5** (newest public source; 5.x is closed) | Manifest intent filters, remote import download path, trust managers, hostname verifier |
| AOSP `platform/frameworks/base` `core/java/org/apache/http/conn/ssl/AbstractVerifier.java` | `main` | The legacy Apache hostname verifier ATAK uses on Android |
| qrtak (`joshuafuller/qrtak`) `docs/qr-formats.md`, issue #240 | `main`, 2026 | `tak://` URI formats (says it was checked against ATAK CIV SDK 5.5) **[SRC-2]** |
| mytecknet "QR Code Registrations with TAK", `joshuafuller/ATAK-Maps` README | 2025–2026 | `tak://` formats, "ATAK 5.1 introduced `tak://`" **[SRC-2]** |
| OpenTAKServer docs (`brian7704/OpenTAKServer-docs`) | 4691a55, 2026-10-24 | `certificate_enrollment.md`, `lets_encrypt.md` |
| TAK Portal (`geiman/TAK-Portal`) | 1e5cd5c, 2026-10-20 | Live generators for the ATAK `tak://` QR, the **iTAK JSON QR**, the `preference` URL; per-client scan instructions |
| TAKAware (`flighttactics/TAKAware`) | `main` b11532b, 2025-06-08 (≈1.4.2; newer versions are not public) | QR parser, enrollment flow, Info.plist |
| TAKAware App Store listing (id6738631659), GitHub issues #176, #220 | current version **2.1** (Oct 2026) | Release notes: `tak://` enrollment (1.5), **8446 cert validation (2.0.2)** |
| iTAK App Store listing (id1561656396) | current version **2.12.3** | Release notes: "QR Code enrollment using predefined JSON string format" |
| Let's Encrypt: blog 2026-01-15, `/docs/profiles/`, `/docs/challenge-types/`, `/docs/rate-limits/`, `/certificates/`, community post 243873 | fetched 2026-10-26 | IP certificates, profiles, challenges, rate limits, Gen Y chain |
| `golang.org/x/crypto` | **v0.58.0** (the version in ChameleonTAK's `go.mod`) and HEAD 9f2b28d (2026-10-20) | `acme`, `acme/autocert` |
| `github.com/mholt/acmez/v3` | v3.1.7 (2026-09-25), HEAD 8c3d90a | ACME profiles, IP identifiers, ARI |
| `github.com/caddyserver/certmagic` | v0.25.6 (2026-09-30) | `ACMEIssuer.Profile`, IP certificate policy, empty-SNI handling |
| Public Suffix List | VERSION 2026-10-22_08-36-41_UTC | sslip.io / nip.io status |
| `cunnie/sslip.io` issue #108 | Feb–Mar 2026 | Shared Let's Encrypt quota exhaustion |
| ZeroTier: `install.zerotier.com`, `download.zerotier.com/debian/trixie`, `ZeroTierOne` `dev` (`one.cpp`, `service/OneService.cpp`, `osdep/LinuxEthernetTap.cpp`), `docs.zerotier.com` (`/openapi/central/v1.json`, `/api-central-examples/`, `/controller/`, `/corporate-firewalls/`) | fetched 2026-10-26; package `zerotier-one` **1.16.2** arm64 | Install, CLI JSON, local API, Central API, controller, firewall |

iTAK and iTAK's QR behaviour are closed source; everything about iTAK is **[SRC-2]**, **[INFERRED]** or **UNVERIFIED**.

---

## 0. Summary

1. **ATAK ≥ 5.1 handles three `tak://` links:** `enroll`, `import`, `preference` **[SRC-2]**. ATAK 4.6 (public source) registers no `tak` scheme at all **[SRC]**.
2. **No link can make ATAK trust a private CA.** There is no "trust this server" prompt (the default prompt returns `false` and the app never installs another), and the `preference` link only writes SharedPreferences. Trust comes only from a `.p12` truststore inside an imported data package **[SRC]**.
3. **But `tak://com.atakmap.app/import?url=…` sidesteps the trust problem.** ATAK's remote-file download **verifies no hostname**, trusts Android's **system** CA store, and **allows plain `http://`** (`usesCleartextTraffic="true"`) **[SRC, 4.6 code path; that the 5.x `import` link uses the same downloader is UNVERIFIED]**. So one QR can deliver a full connection package (truststore + client `.p12` + `.pref`) in every network mode, with no typing and no enrollment.
4. **The ATAK `enroll` QR needs a publicly trusted certificate on 8446 whose CN or DNS SAN equals the QR host.** ATAK checks the hostname with the legacy Apache `STRICT_HOSTNAME_VERIFIER`, which reads **only the CN and dNSName SANs and ignores iPAddress SANs** **[SRC]**. Let's Encrypt IP certificates have **no CN** and only an IP SAN, so **an IP certificate will not work for ATAK QR enrollment**. Use it for the HTTPS download instead (point 3).
5. **iTAK** has two QR formats: CSV `name,host,port,ssl` (asks for credentials) and, since 2.12.3, a **JSON registration** format with credentials. **[SRC-2]** TAK Portal emits that JSON:
   ```json
   {"passphrase":"false","type":"registration","serverCredentials":{"connectionString":"host:8089:ssl"},"userCredentials":{"username":"u","password":"token","registrationId":"<uuid>"}}
   ```
   QR enrollment needs a publicly trusted certificate on 8446 (OTS docs). We found **no iTAK URL scheme** and no import-from-URL feature. For a private CA, the path is a **data package** downloaded in Safari and imported with "Upload Server Package".
6. **TAKAware** accepts CSV, JSON (same keys as iTAK) and, since 1.5, `tak://com.atakmap.app/enroll?…` from its QR screen and via "Open in App". **Since 2.0.2 it "Validate[s] server certs on 8446 and allow[s] for custom trust stores".** The "accepts any cert" finding in the companion document is therefore **outdated for current TAKAware**. `tak://…/import` is not supported yet (issue #176 open).
7. **Let's Encrypt IP certificates are GA (2026-01-15):**
   - Profile **`shortlived`** is required. Validity is **160 h**, there is **no CN**, and the profile allows IP identifiers.
   - Validation is **HTTP-01 (port 80) or TLS-ALPN-01 (port 443)**. DNS-01 cannot validate IPs.
   - `x/crypto/acme` v0.58.0 can do it at low level (`acme.IPIDs`, `acme.WithOrderIssuanceProfile("shortlived")`), but it has no ARI. **`autocert` cannot** (it requires SNI and uses `DomainIDs` only).
   - **Recommended library: `certmagic` v0.25.6 (or `acmez/v3`)**, which has `Profile`, IP support for LE, and ARI.
8. **sslip.io / nip.io are not on the Public Suffix List.** Every user shares one Let's Encrypt bucket, and that bucket ran dry repeatedly in Feb 2026. Do not make them the default.
9. **ZeroTier:** install `zerotier-one` from `download.zerotier.com/debian/trixie` (arm64, 1.16.2). Read the IP from `zerotier-cli -j listnetworks` → `assignedAddresses`, interface `portDeviceName` (`zt…`). Authorize members with `POST https://api.zerotier.com/api/v1/network/{nwid}/member/{nodeId}` `{"config":{"authorized":true}}` and `Authorization: token …`. Private networks require authorization.

### Recommendation matrix (detail in §6)

| Mode | ATAK (Android) | iTAK (iOS) | TAKAware (iOS) |
|---|---|---|---|
| **LAN / hotspot** (private CA) | QR = `tak://com.atakmap.app/import?url=http%3A%2F%2F<ip>%2Fjoin%2F<token>%2Fatak.zip` → full connection package over HTTP | QR = `http://<ip>/join/<token>` → Safari downloads the iTAK package → iTAK › Servers › + › Upload Server Package | Same as iTAK (TAKAware package). Also try the JSON QR (works on ≤2.0.1; on ≥2.0.2 only if the private CA is in its custom trust store, UNVERIFIED) |
| **Public IP** (no domain) | LE **IP** certificate on 443 → `tak://…/import?url=https%3A%2F%2F<public-ip>%2Fjoin%2F…` | `https://<public-ip>/join/<token>` → package (LE IP cert, trusted by iOS); JSON QR against an LE IP cert on 8446 is plausible but UNVERIFIED | JSON QR with LE IP cert on 8446 (iOS matches IP SANs) or package |
| **Own domain** | LE cert (HTTP-01/TLS-ALPN-01) on 443 **and 8446** → native `tak://com.atakmap.app/enroll?host=<domain>:8089&username=…&token=…`; import link as fallback | JSON registration QR (2.12.3+) or CSV QR | JSON QR or `tak://…/enroll` |
| **ZeroTier** | Two QRs: ZeroTier network ID (typed into the ZeroTier app), then the LAN-style `tak://…/import?url=http%3A%2F%2F<zt-ip>…` (ZeroTier encrypts the link) | ZeroTier ID, then the LAN-style package page | ZeroTier ID, then package or JSON QR |

In every mode, **8089 and 8443 keep the private CA**, and the enrollment response's `caN` returns the private CA (companion doc §1.5).

---

## 1. ATAK deep links and QR codes

### 1.1 What ATAK 4.6 registers [SRC]

From `atak/ATAK/app/src/main/AndroidManifest.xml` (4.6.0.5), `com.atakmap.android.maps.ImportFileActivity` is the only component with VIEW/SEND filters:

- **SEND** (`android.intent.action.SEND`, category DEFAULT): `mimeType="*/*" host="*"` with `pathPattern` for `.zip .kml .kmz .lpt .drw .shp .shpz .xml .txt .pref .cot .sqlite .gpx .csv .inf .tif .ntf .nitf .sid .dpkg`.
- **VIEW** (`android.intent.action.VIEW`, category DEFAULT): `scheme="content"` + `mimeType="application/octet-stream"`.
- **No `tak` scheme, no `http(s)` scheme, no host filter.** The `tak://` handlers are a 5.x addition.
- `<application android:usesCleartextTraffic="true" …>`, `targetSdkVersion 30`, `minSdkVersion 21`. There is no `networkSecurityConfig`, so user-installed Android CAs are **not** trusted by the platform default trust manager (Android ≥ 7 rule for targetSdk ≥ 24).

### 1.2 The `tak://` links (ATAK ≥ 5.1) [SRC-2]

| Purpose | Exact syntax |
|---|---|
| Enroll (Quick Connect) | `tak://com.atakmap.app/enroll?host=<host>[:<port>[:quic]]&username=<user>&token=<password-or-token>` |
| Import a file / data package from a URL | `tak://com.atakmap.app/import?url=<percent-encoded absolute http(s) URL>` |
| Set preferences | `tak://com.atakmap.app/preference?key1=<k>&type1=<string\|boolean\|long\|int>&value1=<v>&key2=…&type2=…&value2=…` |

- **Minimum version:** ATAK **5.1** ("ATAK 5.1 introduced the ability to handle URLS with a prefix tak://", mytecknet; "Requires ATAK 5.1+ (the version that added the `tak://` import scheme)", ATAK-Maps README). The ATAK 5.x source is not public, so whether all three paths shipped in 5.1 is UNVERIFIED.
- **`enroll`**: details are in the companion doc §2.1.
  - `host` is the **streaming** host:port (default 8089, protocol `ssl`). The enrollment port always comes from the pref `apiCertEnrollmentPort` (default `8446`).
  - `token` is sent as the HTTP Basic password.
  - Percent-encode `username` and `token`.
- **`import`**: qrtak says *"ATAK asks the user to confirm before it starts the import"* and that the URL must be absolute `http` or `https`. Example from ATAK-Maps: `tak://com.atakmap.app/import?url=https%3A%2F%2Fjoshuafuller.github.io%2FATAK-Maps%2Fpack%2Fatak-maps-all.zip`.
- **`preference`**: TAK Portal uses it live (`services/qr.service.js` `buildPreferenceUrl`) with keys:
  - `locationCallsign`, `locationTeam`, `atakRoleType` (type `string`)
  - `appMgmtEnableUpdateServer`, `repoStartupSync` (`boolean`)
  - `atakUpdateServerUrl` (`string`)

  Whether ATAK blocks any keys (for example certificate locations) is UNVERIFIED.
- **How the user gets there:**
  - OTS docs: *"use your EUD's camera app or any QR code scanning app. Once scanned, opening the link will automatically open ATAK and start the certificate registration process."*
  - TAK Portal's ATAK instructions: open the camera, *"select the notification to open the link"*, then *"Select Yes to perform quick enrollment inside ATAK"*.
  - A `tak://` link can also be a tappable button on a web page opened on the phone ("Open ATAK" in OTS, "Open In App" in TAK Portal).

### 1.3 How ATAK downloads a remote file, and why that matters [SRC, 4.6]

`atak/.../importfiles/http/ImportFileDownloader.java` `ndl()` (the Import Manager remote-resource downloader):

- **Transport choice:**
  - If the URL host equals a *connected* TAK server's host **and** the scheme is `https` **and** the port is **8443**, ATAK uses `TakHttpClient`, which brings the client cert and the per-server truststore.
  - **Otherwise** it uses `new URL(urlStr).openConnection()` with `User-Agent: TAK`, 10 s connect timeout and 30 s read timeout, wrapped by `AtakAuthenticationHandlerHTTP.makeAuthenticatedConnection(conn, 3)`.
- **TLS on the second path** (`takkernel/engine/src/main/java/com/atakmap/net/AtakAuthenticationHandlerHTTP.java` `setAcceptAllVerifier`):
  - It sets `CertificateManager.ALL_TRUSTING_HOSTNAME_VERIFIER`, so **no hostname check**.
  - It uses the socket factory of `CertificateManager.SelfSignedAcceptingTrustManager`. Despite the name, that is a plain `new CentralTrustManager()`: `iss=false` and the default prompt.
  - `CentralTrustManager.checkServerTrusted` with `iss=false` first runs `cert.checkValidity()`, then tries the **local** trust manager: the embedded DoD and DigiCert roots, **ISRG Root X1**, and every CA ATAK already imported.
  - If that fails, it tries the **Android system** trust manager. If that fails too, it calls `promptToAccept`, which returns `false` (*"self signed server cert detected, without anchor, blocked by default"*).
  - **Net effect: a Let's Encrypt (or any public) certificate works, including an IP certificate. A private-CA certificate is rejected unless that CA was imported earlier.** A grep of the app and the takkernel sources finds **no call to `setSystemCertificatePrompt`**, so no interactive trust prompt exists in 4.6.
- **Plain HTTP works:** `HttpURLConnection` plus `usesCleartextTraffic="true"`. In 5.x this is UNVERIFIED, so test it first.
- **File naming:** the file name comes from the request. If the response `Content-Type` maps to a known type (`ResourceFile.MIMEType.ZIP("application/zip","zip",…)`), ATAK appends `.zip`. **So serve the package as `Content-Type: application/zip` and make the URL path end in `.zip`.** The file is then sorted like a local import (`ImportRemoteFileTask`), so a connection package (`.pref` + `.p12`) is applied exactly as in companion doc §3.1.
- That the 5.x `tak://…/import` handler goes through this same `ImportFileDownloader` is **[INFERRED]**: it is the only remote-import path in 4.6. This is the single most important thing to test on ATAK 5.x.

### 1.4 Can a link make ATAK trust a private CA? No [SRC]

- Trust anchors live in ATAK's encrypted certificate database. They are added only by:
  - importing a `.p12` through a data package (`ImportCertSort`), or
  - the manual "Import Trust Store" button in the server dialog.
- The `preference` link writes SharedPreferences values (string/boolean/long/int). It cannot carry certificate bytes. Pointing `caLocation` at a file that doesn't exist does nothing.
- There is no "trust this server" UI (§1.3).
- **The only one-QR path for a private CA is the `import` link pointing at a package that contains the truststore.** That package can be:
  - a **full connection package** (truststore + client `.p12` + `.pref`; no enrollment), or
  - a **bootstrap package** (truststore + `.pref` with `enrollForCertificateWithTrust0=true`). ATAK then starts enrollment and shows the credentials dialog, and the user has to type the username and token (companion doc §2.5).

### 1.5 Hostname verification in the `enroll` flow [SRC]

- The companion doc established that ATAK verifies the hostname strictly when there is no truststore for the host (`allowAllHostnames = truststore != null`).
- The verifier is `org.apache.http.conn.ssl.SSLSocketFactory.STRICT_HOSTNAME_VERIFIER` (`CertificateManager.ExtendedSSLSocketFactory.createSocket`).
- On Android this class comes from the **platform legacy Apache library**:
  - `atak/ATAK/app/build.gradle` has `useLibrary 'org.apache.http.legacy'`.
  - The `httpclient:4.5.13` dependency in `takkernel/engine/build.gradle` is only in the `isJavaKernelBuild` (desktop) branch.
- AOSP `AbstractVerifier.verify(String host, X509Certificate cert)` builds its name list from `getCNs(cert)` (first CN only) and `getDNSSubjectAlts(cert)`. That second method keeps only `type == 2` (dNSName). **iPAddress SANs (type 7) are ignored.** If neither is present, it throws `"Certificate for <host> doesn't contain CN or DNS subjectAlt"`.

Consequences:

- **Let's Encrypt `shortlived` / `tlsserver` certificates have no CN** (LE profiles page). An LE **IP** certificate therefore always fails ATAK's enroll hostname check. A **DNS-name** LE certificate passes (dNSName SAN).
- ChameleonTAK's own private-CA server certificate is safe in two ways:
  - It is normally used with a truststore present, so the hostname is not checked.
  - Still, put the **QR host string in the CN** (for example `CN=10.42.0.1` or `CN=tak.example.com`). The legacy verifier then matches even when ATAK checks hostnames.
- These conclusions are from 4.6. ATAK 5.x may have switched verifiers (UNVERIFIED).

---

## 2. iTAK

### 2.1 QR formats

| Format | Exact content | Source |
|---|---|---|
| CSV (Quick Connect) | `<description>,<host>,<port>,<protocol>`, for example `ChameleonTAK,tak.example.com,8089,ssl` | mytecknet, OTS `/api/itak_qr_string`, goatak (companion doc §2.2) **[SRC-2]**. iTAK then *"prompt[s] for credentials"*. |
| JSON registration (iTAK **2.12.3+**: *"adds support for QR Code enrollment using predefined JSON string format"*) | `{"passphrase":"false","type":"registration","serverCredentials":{"connectionString":"<host>:<port>:ssl"},"userCredentials":{"username":"<user>","password":"<token>","registrationId":"<uuid>"}}` | TAK Portal `services/qr.service.js` `buildItakEnrollPayload` (comment: *"plain-text JSON scanned by iTAK"*) **[SRC-2]**; release note from the App Store listing |

- Scan path inside iTAK (TAK Portal instructions): **Settings (gear) › Network › Servers › + › "Scan QR"**.
- The iOS Camera app does not hand plain text to iTAK. The QR must be scanned **inside iTAK**.
- **No URL scheme:** we found no `itak://` scheme, no "import from URL" for server packages, and no trust prompt documented anywhere (App Store notes, TAK Portal, OTS, mytecknet). **UNVERIFIED** (closed source).
  - iTAK can download *overlays* from URLs ("Adds functionality to import an Overlay from a network link"), but nothing says that applies to connection packages.
- The enrollment port iTAK uses with these QRs is not in the QR. 8446 is **[INFERRED]**.

### 2.2 Private CA

- OTS docs (`certificate_enrollment.md`): *"In order to use a QR code for iTAK enrollment, your server must be configured with a signed certificate from Let's Encrypt or a similar trusted certificate authority."*
- Whether iTAK would trust a private CA that was installed as an iOS configuration profile **and** enabled under Settings › General › About › Certificate Trust Settings is UNVERIFIED. That path also needs too many taps to count as "just works".
- **Working path for a private CA:** a full iTAK connection package (companion doc §3.7), imported with **Network › Servers › + › "Upload Server Package"** after downloading it in Safari to Files (argustak.com instructions **[SRC-2]**). Expect iTAK to ask for the `.p12` password(s). We always use `atakatak`.

---

## 3. TAKAware

### 3.1 Accepted QR formats [SRC, public v1.4 `TAKAware/Parsers/QRCodeParser.swift`]

The scanned string is trimmed, then:

1. **Exactly 4 comma-separated parts → iTAK CSV** `serverName,serverURL,serverPort,serverProtocol`.
   - `serverPort` must parse as `Int`.
   - `serverProtocol` is read but ignored.
   - The description must not contain commas.
   - It fills host and port only. The user types the username and password and taps save.
2. **Starts with `{` → JSON**, parsed with `JSONSerialization`:
   - `json["serverCredentials"] as? [String:String]` must succeed, so **every value inside must be a string**. Then `connectionString` is split on `:`.
   - Element `[0]` is the host and element `[1]` the port. **A `connectionString` without `:port` indexes out of range and crashes.** Always include the port.
   - Element `[2]` (`ssl`) is ignored.
   - `json["userCredentials"] as? [String:String]` gives `username` and `password`. Again, **all values must be strings**; iTAK's `registrationId` string is fine.
   - If both are non-empty, `shouldAutoSubmit = true` and enrollment starts immediately.
   - Extra top-level keys (`passphrase`, `type`) are ignored. **The iTAK JSON in §2.1 is therefore valid for TAKAware too.**
3. **`tak://com.atakmap.app/enroll?host=…&username=…&token=…`**: supported *"from the QR code screen"* since **1.5** (App Store release note). It also works through "Open in App" from a web page (TAK Portal issue #220, TAKAware 2.0.2).
4. Anything else is shown as an invalid QR.

### 3.2 What happens after the scan [SRC]

`ConnectionOptions.swift` `processQRCode` → `submitCertEnrollmentForm` → `CSRRequestor.beginEnrollment()`:

1. `GET https://<host>:<csrPort>/Marti/api/tls/config`, with `csrPort` default **8446** (editable in the form).
2. `POST https://<host>:<csrPort>/Marti/api/tls/signClient/v2?clientUid=…&version=…`
3. Stream TLS to `<host>:<port>` (the port from the QR, normally 8089). The secure API runs on 8443.

TAKAware does not fetch the enrollment profile. TAK Portal tells users to *"scan the QR until you see 'Status: Succeeded'"*.

### 3.3 Server-certificate validation changed in 2.0.2

- Public v1.4 accepts any certificate on 8446.
- The App Store notes for **2.0.2** (and 2.1) list ***"Validate server certs on 8446 and allow for custom trust stores"***.
- So **current TAKAware will reject a private-CA certificate on 8446 unless that CA is in its custom trust store**.
- How a custom trust store is added (settings, a data package with `caLocation`, or a prompt) is **UNVERIFIED**. Test it.
- Data-package import still works: an in-app `.fileImporter` accepting `.zip` **[SRC]**. Packages carry the identity, so no enrollment is needed.
- The public v1.4 Info.plist declares **no URL types and no document types** (keys: `NSAppTransportSecurity`/`NSAllowsArbitraryLoads`, `NSBonjourServices`, `UIBackgroundModes`, `ITSAppUsesNonExemptEncryption`). Packages must be picked from inside the app.
- `tak://…/import` is **not supported yet**: issue #176 *"Support downloading packages / importing through the tak URL"* is open, with milestone "2.0 App Store Release".

---

## 4. Let's Encrypt without and with a domain

### 4.1 IP-address certificates (no domain) [SRC]

- **Status:** generally available since **2026-01-15** (*"6-day and IP Address Certificates are Generally Available"*). IPv4 and IPv6 are both supported.
- **Profile:** IP certificates **must** use the **`shortlived`** profile:

  | Profile | Validity | Identifiers | CN |
  |---|---|---|---|
  | `classic` (default) | 90 days | DNS only | Yes |
  | `tlsserver` | 45 days | DNS only | No |
  | `shortlived` | **160 hours** | **DNS and IP** | **No** |

  - An order without a profile fails with `Profile "" does not permit ip type identifiers` (community report).
  - `tlsclient` is no longer available since 2026-07-08.
- **Challenges:**
  - **HTTP-01**: *"can only be done on port 80"*. Redirects are followed only to ports 80 and 443. *"It can be used to validate IP Addresses as well."*
  - **TLS-ALPN-01**: port 443, also valid for IPs.
  - **DNS-01**: *"cannot be used to validate IP Addresses"*.
  - With port forwarding, **forward TCP 80 (HTTP-01) or TCP 443 (TLS-ALPN-01)** from the router to the Pi. LE validates from several network perspectives, so geo-blocking breaks validation.
- **Rate limits** (`/docs/rate-limits/`):
  - An IPv4 address counts as its own "registered domain": **50 certificates per 7 days**. For IPv6 the unit is the /64.
  - Exact same identifier set: **5 per 7 days**.
  - Authorization failures: 5 per identifier per account per hour.
  - Accounts: 10 per IPv4 per 3 hours.
  - **ARI-coordinated renewals are exempt from all limits.**
  - With 160 h validity you renew roughly every 3 days, which is about 2–3 issuances per week. That is fine, even without ARI.
- **Operational caveats:**
  - Needs a real public IPv4: **not CGNAT**. Under CGNAT, port forwarding is impossible.
  - If the public IP changes, the certificate must be re-issued and every QR regenerated.
  - Phones on the same LAN reaching the public IP need **NAT loopback (hairpinning)** on the router; many home routers lack it. Offer the LAN QR to local phones.

### 4.2 Chain [SRC `/certificates/`, community post 243873]

- The **Generation Y** hierarchy was used for `tlsserver`/`shortlived` from **2026-01-07** and for `classic` from **2026-05-13**.
- RSA leaves are issued by **YR1/YR2**, under **ISRG Root YR**, which is **cross-signed by ISRG Root X1**. ECDSA leaves go YE1/YE2 → Root YE → cross-signed by X2 → cross-signed by X1.
- Root YE/YR are "not yet in Root Program trust stores".
- ATAK 4.6 embeds only `isrgrootx1.crt` among LE roots. **Always serve the full default chain (including the cross-signed Root YR certificate) and prefer an RSA key.** The path then ends at X1 for ATAK, and at X1 or X2 for iOS/Android system stores. That ATAK validates the Gen Y RSA chain is **[INFERRED]** from the cross-sign; test it.
- Gen Y intermediates carry no clientAuth EKU. That is irrelevant here, because LE certificates are only used as server certificates on 443/8446.

### 4.3 Go library support

| Library | IP identifiers | Profile selection | ARI | Notes |
|---|---|---|---|---|
| `golang.org/x/crypto/acme` **v0.58.0** | Yes: `acme.IPIDs(addr...)` gives `AuthzID{Type:"ip"}`; `TLSALPN01ChallengeCert` handles IP identifiers (`net.ParseIP` branch) | Yes: `acme.WithOrderIssuanceProfile(acme.ProfileName("shortlived"))` passed to `AuthorizeOrder`; `Directory.Profiles` lists them | **No** | Low level: you write the order loop, the challenge serving, the CSR (with `IPAddresses`) and the renewal timing yourself |
| `golang.org/x/crypto/acme/autocert` v0.58.0 | **No**. `GetCertificate` returns `"acme/autocert: missing server name"` without SNI and rejects names without a dot; orders use `acme.DomainIDs(domain)` only | **No** (no option is passed to `AuthorizeOrder`) | No | Fine for a **domain** (classic, 90 days), not for IPs |
| `github.com/mholt/acmez/v3` **v3.1.7** | Yes: CSR `IPAddresses` become `Type:"ip"` identifiers (RFC 8738); TLS-ALPN for IP (an IPv6 fix landed Dec 2025) | Yes: `acme.Order.Profile` / `OrderParameters.Profile`, validated against `directory.meta.profiles` | Yes (`GetRenewalInfo`, `Replaces`) | Mid level |
| `github.com/caddyserver/certmagic` **v0.25.6** | Yes for `api.letsencrypt.org` (and `api.pki.goog`) via its `PreCheck` allowlist; older versions refused public IP certificates (caddy issue #7399) | Yes: `ACMEIssuer.Profile` | Yes (on by default; `DisableARI`) | High level: storage, renewal, HTTP-01/TLS-ALPN-01 solvers |

**certmagic gotcha [SRC `handshake.go`]:** without SNI (clients connecting by IP send none), certmagic picks the certificate by `Config.DefaultServerName`, falling back to the connection's **local** IP. Behind port forwarding that is the Pi's LAN address (`192.168.x.y`), not the public IP. **Set `DefaultServerName` to the public IP.**

**Recommendation:** use **certmagic** with `Profile: "shortlived"` for the public-IP mode, and also for the domain mode (one code path; ARI; it handles `classic`). If the extra dependency is unwanted, the minimal option is `x/crypto/acme` with `IPIDs` plus `WithOrderIssuanceProfile`, HTTP-01 served on :80, and renewal at about 50 % of lifetime. Keep `autocert` only if the domain mode is the sole ACME use.

### 4.4 sslip.io / nip.io (`1-2-3-4.sslip.io` → `1.2.3.4`)

- **Public Suffix List:** neither `sslip.io` nor `nip.io` is on it (checked against VERSION 2026-10-22).
  - LE counts limits per registered domain via the PSL, so **every user on earth shares one `sslip.io` bucket** (and one `nip.io` bucket).
  - LE has granted overrides: per sslip.io issue #108 the bucket was raised 25k → 50k (Jan 15) → 100k (Feb 12) → 200k (Mar 5, 2026), and it was still being exhausted in Feb 2026 (*"too many certificates (50000) already issued for "sslip.io" in the last 168h0m0s"*).
  - ARI renewals are exempt, but the first issuance is not.
- **Challenges:** ordinary DNS names, so HTTP-01 (port 80) or TLS-ALPN-01 (port 443). DNS-01 is impossible, because you don't control the zone. Any profile works, including `classic` (90 days, with CN).
- **Advantages:** it is a DNS name, so the ATAK `enroll` hostname check passes (dNSName SAN), and phones send SNI.
- **Reliability concerns:**
  - Phones and LE's multi-perspective validators must resolve through sslip.io's nameservers (a third-party single point of failure; it reported 12k qps in Feb 2026).
  - The bucket can be exhausted at any time.
  - Some resolvers and routers drop "DNS rebinding" answers that map to private IPs. Public IPs are fine.
- **Verdict:** offer it as an **optional** "ATAK native QR without a domain" toggle, not as the default. The default for public IP is the LE IP certificate plus the ATAK `import` link (§6.2).

### 4.5 User-owned domain with `autocert` (HTTP-01)

Requirements [SRC `autocert.go`, LE docs]:

- A DNS **A record** (and AAAA, if any, must also reach the Pi) for the name, pointing at the public IP. **TCP 80 forwarded** to the Pi for HTTP-01, or TCP 443 for TLS-ALPN-01.
- `m := &autocert.Manager{Prompt: autocert.AcceptTOS, HostPolicy: autocert.HostWhitelist("tak.example.com"), Cache: autocert.DirCache("/var/lib/chameleontak/acme"), Email: …}`.
  - Serve `m.HTTPHandler(fallback)` on :80. Without `HTTPHandler` only TLS-ALPN-01 is tried.
  - Use `m.TLSConfig()` (adds `acme.ALPNProto`) or `GetCertificate: m.GetCertificate` on :443.
- The same `m.GetCertificate` can back the **8446** listener. LE only connects to 80/443 for validation, and the cache is shared.
- Clients must send SNI (`autocert` errors without it). ATAK, iTAK and TAKAware connect by hostname, so they do.
- Profile `classic`: 90 days. The default lifetime drops to 64 days in 2027 and 45 in 2028. `autocert`'s default `RenewBefore` is 30 days.
- Hairpin NAT applies to LAN phones here as well. Alternatively, run a local DNS override (dnsmasq on the Pi) so the domain resolves to the LAN or hotspot IP; the LE certificate stays valid.
- **Offline/hotspot with a domain** works only with a certificate obtained earlier via **DNS-01** (needs a DNS-provider API, which certmagic/libdns supports) and local DNS pointing the name at `10.42.0.1`. It needs internet at least every ~60 days to renew. This is an advanced option.

### 4.6 Which certificate goes where

| Port | Certificate | Why |
|---|---|---|
| 443 (panel, `/join`, package downloads) | LE (IP or domain) when available, else private CA | The ATAK import downloader and iOS Safari trust public CAs |
| 8446 (enrollment) | LE **domain** certificate (for the ATAK `enroll` QR) or LE IP certificate (iOS only); private CA otherwise | Companion doc §2.4 |
| 8089 / 8443 | **Always the private CA** | Client-cert mTLS; `caN` in the enrollment response = private CA |

---

## 5. ZeroTier on Raspberry Pi OS (Debian trixie, arm64)

### 5.1 Install [SRC `install.zerotier.com`, `download.zerotier.com`]

- **Official one-liner:** `curl -s https://install.zerotier.com | sudo bash`. It has `MAX_SUPPORTED_DEBIAN_VERSION_NAME=trixie`, writes the apt repo, installs `zerotier-one`, enables and starts the service, and waits for `/var/lib/zerotier-one/identity.secret`. The script is a PGP clear-signed bash file. `gpg --verify` reports *"Good signature from ZeroTier, Inc. …"* with the key below **[SRC, verified 2026-10-26]**, so `curl -s https://install.zerotier.com | gpg` (after importing the key) verifies it before you run it.
- **Equivalent explicit steps** (what the script does for Debian ≥ 10; use these in the image build, `image/stage-chameleontak`):

  ```sh
  curl -fsSL https://download.zerotier.com/contact%40zerotier.com.gpg \
    | gpg --dearmor | sudo tee /usr/share/keyrings/zerotier-debian-package-key.gpg >/dev/null
  echo "deb [signed-by=/usr/share/keyrings/zerotier-debian-package-key.gpg] http://download.zerotier.com/debian/trixie trixie main" \
    | sudo tee /etc/apt/sources.list.d/zerotier.list
  sudo apt-get update && sudo apt-get install -y zerotier-one
  sudo systemctl enable --now zerotier-one
  ```

  - Key: `ZeroTier, Inc. (ZeroTier Support and Release Signing Key) <contact@zerotier.com>`, rsa4096, fingerprint **`74A5 E9C4 58E1 A431 F1DA 57A7 1657 1988 23E5 2A61`**. The trixie `Release` file is signed by this key.
  - The repo `Release` lists `Architectures: amd64 arm64 armhf i386 ppc64el s390x`. `binary-arm64` has `zerotier-one` **1.16.2** (Depends: `adduser, libstdc++6 (>= 5), openssl`).
  - The script uses the `http://` base URL; `https://download.zerotier.com/debian/trixie/…` also answers 200. Apt signature checking protects either way.
- Data directory `/var/lib/zerotier-one/`. The node ID is also the first field of `identity.public`. `authtoken.secret` is locked down to root (`OSUtils::lockDownFile`).
  - The `chameleontak` service user therefore needs new `chameleontak sys` actions (run via sudo) for join/leave/status, or read access to the token.

### 5.2 CLI [SRC `one.cpp`, `OneService.cpp`]

`-j` can appear anywhere on the command line; arguments starting with `-` are parsed as options.

```sh
sudo zerotier-cli join <16-hex-nwid>      # "200 join OK"  (POST /network/<nwid> on the local API)
sudo zerotier-cli leave <nwid>
sudo zerotier-cli -j info                 # node status
sudo zerotier-cli -j listnetworks         # joined networks
sudo zerotier-cli -j peers
```

- `info -j` (`GET /status`) keys:
  - `address`: the **10-hex node ID**
  - `online` (bool), `publicIdentity`
  - `version`, `versionMajor`, `versionMinor`, `versionRev`, `versionBuild`
  - `planetWorldId`, `planetWorldTimestamp`, `tcpFallbackActive`, `clock`
  - `config` (`settings.*`, for example `surfaceAddresses`)
- `listnetworks -j` (`GET /network`), one object per network:
  - `id` / `nwid`, `name`, `mac`, `type` (`PRIVATE`/`PUBLIC`)
  - `status`: `REQUESTING_CONFIGURATION`, `OK`, `ACCESS_DENIED`, `NOT_FOUND`, `PORT_ERROR`, `CLIENT_TOO_OLD`, or `AUTHENTICATION_REQUIRED`
  - `mtu`, `dhcp`, `bridge`, `broadcastEnabled`, `portError`, `netconfRevision`
  - **`portDeviceName`** (the Linux interface, `zt…`)
  - `allowManaged`, `allowGlobal`, `allowDefault`, `allowDNS`
  - **`assignedAddresses`** (CIDR strings, for example `["10.147.17.5/24"]`)
  - `routes` (`target`, `via`, `flags`, `metric`), `multicastSubscriptions`, `dns` (`domain`, `servers`)
- Until the member is authorized: `status` = `ACCESS_DENIED` (or `REQUESTING_CONFIGURATION`) and `assignedAddresses` = `[]`. Poll until `status=="OK"` and an IPv4 is present.
- **Local service API**, same data without the CLI: `http://127.0.0.1:9993`, header `X-ZT1-Auth: <contents of authtoken.secret>`:
  - `GET /status`, `GET /network`, `POST /network/<nwid>` (join), `DELETE /network/<nwid>` (leave).

### 5.3 ZeroTier Central API (authorize, read IPs) [SRC `docs.zerotier.com/openapi/central/v1.json`, `/api-central-examples/`]

- **Legacy Central v1:**
  - Base `https://api.zerotier.com/api/v1`.
  - Security scheme `http`/`token`, header **`Authorization: token <API token>`**. The token is created on the Account page.
  - Rate limit: 20 req/s free, 100 req/s paid.
  - Authorize:
    ```sh
    curl -H "Authorization: token $ZT_TOKEN" -X POST \
      "https://api.zerotier.com/api/v1/network/$NWID/member/$NODE_ID" \
      --data '{"config": {"authorized": true}}'
    ```
    Deauthorize with `false`. Optionally set `"name":"chameleontak-pi"` or `"config":{"ipAssignments":["10.147.17.1"]}` to pin the Pi's address.
  - Read: `GET /network/{nwid}/member/{nodeId}` (or `GET /network/{nwid}/member` for all) returns `Member`:
    - top level: `id`, `networkId`, `nodeId`, `name`, `description`, `hidden`, `lastOnline`, `lastSeen`, `physicalAddress`, `clientVersion`, `protocolVersion`, `config`
    - `config`: `authorized`, **`ipAssignments`**, `activeBridge`, `noAutoAssignIps`, `creationTime`, `lastAuthorizedTime`, `lastDeauthorizedTime`, `capabilities`, `tags`, …
  - Network: `GET /network/{nwid}` → `config.private`, `config.ipAssignmentPools`, `config.routes`, `config.v4AssignMode`.
- **New Central:** *"For New Central, use Service Account tokens with the same API endpoints"*, sent as `Authorization: Bearer <token>`. Service-account tokens need the Essential plan or above (secondary). Some endpoints differ: the docs show `https://central.zerotier.com/api/v2` for MTU. Exact v2 member paths are **UNVERIFIED**. Support both header styles, or ask the user which Central they use.
- **Authorization is needed on private networks** (`config.private=true`, the default). On public networks anyone with the ID joins; don't use them.
- **Self-hosted controller (no Central account):**
  - The Pi's own `zerotier-one` can be the controller (`docs.zerotier.com/controller/`):
    - Create: `POST http://localhost:9993/controller/network/${NODEID}______` with `X-ZT1-AUTH: $TOKEN` and body `{}`.
    - Configure: `POST /controller/network/${NWID}` `{"ipAssignmentPools":[{"ipRangeStart":"…","ipRangeEnd":"…"}],"routes":[{"target":"…/24"}],"v4AssignMode":"zt","private":true}`.
    - Authorize: `POST /controller/network/${NWID}/member/${MEMID}` `{"authorized":true}`.
  - Network IDs are derived from the controller's node ID.
  - Trade-off: phones can only join while the Pi is online and reachable through ZeroTier roots, but no third-party account or token is needed. This fits a field server well.

### 5.4 Phones and the Pi

- **Phones:** ZeroTier One app (Android/iOS) → "+" → type the 16-hex network ID → join. It appears as a VPN. **iOS allows only one active VPN at a time.** We found no QR-join feature in the ZeroTier apps (UNVERIFIED), so show the ID as text plus a QR (for copy-paste from a scanner).
  - The app shows the phone's node ID. ChameleonTAK can list members (`GET …/member`) and show unauthorized ones with recent `lastSeen` in the panel, with an "Authorize" button. That beats asking the user to read the node ID aloud.
- **Interface name** [SRC `LinuxEthernetTap.cpp`]: `zt` + a base32 form of a 40-bit hash of the network ID, stable per network. It can be pinned with `/var/lib/zerotier-one/devicemap` lines `<nwid>=<ifname>`. Read the real name from `portDeviceName`.
- **Firewall** (`internal/system/sys.go` generates `policy drop`):
  - ZeroTier's own traffic: *"If you allow outgoing source:9993 and incoming related return traffic, it'll probably work OK"* (ZeroTier docs). That already works through `ct state established,related accept`. Add `udp dport 9993 accept` to improve direct (non-relayed) paths. The secondary port is random, so don't try to pin it.
  - TAK ports over ZeroTier: the existing `tcp dport {…}` rule is interface-agnostic, so it already covers `zt*`. To restrict ZeroTier users to TAK ports, add `iifname "zt*" tcp dport { 80, 443, 8089, 8443, 8446 } accept`. nft supports the trailing `*` wildcard on `iifname`.
- **Certificates:** add the ZeroTier IP to the private server certificate's SANs (and CN if it is the QR host), regenerating when the address appears or changes. LE cannot validate a ZeroTier (private) IP. A user-owned domain with a DNS-01 certificate whose A record points to the ZeroTier IP is the only public-CA option.
- **Diagnostics:** `zerotier-cli peers` shows `RELAY` when hole punching fails. `surfaceAddresses` growing in `info -j` indicates symmetric NAT.

---

## 6. Recommendation: QR onboarding per network mode

General rules for all modes:

- One **join token** per device: single use, short expiry (it already exists: `/join/<token>`).
- Serve packages at a URL that ends in `.zip`, with `Content-Type: application/zip`. For example `/join/<token>/atak.zip`, `/join/<token>/itak.zip`, `/join/<token>/takaware.zip`, next to the existing `/api/join/{token}/package/{platform}`.
- Consume the token on the **first successful download** (or after N downloads), not on page view.
- The `tak://…/import?url=` value must be fully percent-encoded (`:` → `%3A`, `/` → `%2F`, `?`/`&` too).

### 6.1 LAN and hotspot (private CA, maybe offline)

**ATAK**
- **Primary QR:** `tak://com.atakmap.app/import?url=http%3A%2F%2F10.42.0.1%2Fjoin%2FK7P2QX9M%2Fatak.zip`
  - Phone camera → open → ATAK confirms → downloads over HTTP → imports the **full connection package** (truststore + client `.p12` + `.pref`, companion doc §3.4) → connected. No typing, no 8446.
  - Basis: §1.3 [SRC for 4.6; 5.x behaviour UNVERIFIED].
  - Cleartext exposure of the client key is limited to the WPA2 hotspot or LAN, and the token is single-use. Optionally make the package `.p12` password a per-token random value. **But** ATAK 4.6 reads `clientPassword0`/`caPassword0` from the pref, so the password then travels in the same zip, and nothing is gained. Keep `atakatak` and rely on the single-use token.
- **Alternative (no private key over HTTP):** the same link to a **bootstrap package** (`enrollForCertificateWithTrust0=true`). ATAK then prompts for credentials, and the user types username + token. The page should show both.
- **Fallback for ATAK < 5.1, or if the camera won't open `tak://`:** a second QR `http://10.42.0.1/join/K7P2QX9M` opens the join page. The user downloads the zip, then ATAK › Import › Local SD › Download. Serve it as `application/zip`. "Open with ATAK" from the browser is UNVERIFIED: ATAK 4.6's VIEW filter only takes `content://` + `application/octet-stream`.
- **Do not show** the `tak://…/enroll` QR in this mode. It fails against a private CA (companion doc §2.4).

**iTAK**
- QR = `http://10.42.0.1/join/K7P2QX9M` (camera → Safari). The page has a big "Download iTAK package" button (flat zip, companion doc §3.7), then instructions: iTAK › Settings › Network › Servers › + › **Upload Server Package** › Files › Downloads.
- That is about 5 taps. It is the only path that works with a private CA as far as we know.
- **Don't** show the iTAK CSV/JSON QR in this mode, unless testing proves iTAK can trust a private CA.

**TAKAware**
- Same page and a TAKAware package (identity in the non-indexed `certificateLocation`, companion doc §3.7), imported via the app's data-package import.
- Additionally show the **JSON QR** `{"serverCredentials":{"connectionString":"10.42.0.1:8089:ssl"},"userCredentials":{"username":"jan","password":"K7P2QX9M"}}`. It auto-enrolls on TAKAware ≤ 2.0.1. On ≥ 2.0.2 it works only if the private CA is in TAKAware's custom trust store (UNVERIFIED how). Label it "if the package fails".

### 6.2 Public IP with port forwarding (no domain)

Requirements: forward **80** (HTTP-01; plus 443 if TLS-ALPN-01 is used instead), and **443, 8089, 8443, 8446**. Not behind CGNAT. Detect the public IP (user input plus a check), then obtain an LE **`shortlived` IP certificate** (certmagic, `DefaultServerName` = public IP) for **443** and **8446**.

- **ATAK:** QR = `tak://com.atakmap.app/import?url=https%3A%2F%2F203.0.113.5%2Fjoin%2FK7P2QX9M%2Fatak.zip`.
  - The LE IP certificate is trusted by the Android system store, and ATAK's downloader does no hostname check. Result: full package over **HTTPS** with no typing.
  - **Do not use** `tak://…/enroll?host=203.0.113.5`: the LE IP certificate has no CN/dNSName, and ATAK's verifier rejects it [SRC 4.6].
  - Optional toggle: an `sslip.io` name with a DNS LE certificate on 8446 (then `enroll` works), with the §4.4 caveats.
- **iTAK:** QR = `https://203.0.113.5/join/K7P2QX9M`, then package as in §6.1. Additionally the **JSON registration QR** (`connectionString` `203.0.113.5:8089:ssl`). With the LE IP certificate on 8446, iOS chain plus IP-SAN validation should pass. iTAK's own checks are **UNVERIFIED**.
- **TAKAware:** JSON QR (as above). With the LE IP certificate on 8446 even the 2.0.2+ validation should pass (UNVERIFIED), else package.
- LAN phones without NAT hairpinning: show the LAN variant (§6.1) as well.

### 6.3 User-owned domain

Requirements: A record → public IP; forward **80** (HTTP-01 with `autocert` or certmagic), **443, 8089, 8443, 8446**; an LE certificate (classic) on **443 and 8446**; the private CA on 8089/8443. Put the domain in the private server certificate too.

- **ATAK:** native QR `tak://com.atakmap.app/enroll?host=tak.example.com%3A8089&username=jan&token=K7P2QX9M`.
  - This is the Appendix E / OTS pattern. It is the only mode where the ATAK native enroll QR works.
  - 8446 must stay on 8446, unless you also hand out `tak://com.atakmap.app/preference?key1=apiCertEnrollmentPort&type1=string&value1=<port>` first.
  - Offer the `import` link to `https://tak.example.com/join/…/atak.zip` as a fallback.
- **iTAK:** JSON registration QR (2.12.3+), with the CSV QR as fallback for older iTAK (the user types credentials).
- **TAKAware:** JSON QR, or the same `tak://…/enroll` link (TAKAware ≥ 1.5 scans it).
- LAN phones: hairpin NAT, or a local DNS override on the Pi (§4.5).

### 6.4 ZeroTier

- **Pi side:**
  - The panel accepts a network ID plus a Central API token, **or** creates its own controller network (§5.3).
  - `chameleontak sys zt-join <nwid>`; poll `listnetworks -j` until `status=OK`; read `assignedAddresses` and `portDeviceName`.
  - Self-authorize via the API, add the ZeroTier IP to the server-certificate SAN/CN, and open the firewall (§5.4).
- **Phone side**, two steps on the join page:
  1. "Install ZeroTier One, join network `<16-hex>`". Show it as text and as a QR of the bare ID. The panel then lists the pending member with an "Authorize" button (or auto-authorizes the next unknown member seen within the token's validity window, if the admin enables that).
  2. The **LAN-style** onboarding with the ZeroTier IP:
     - ATAK: `tak://com.atakmap.app/import?url=http%3A%2F%2F10.147.17.1%2Fjoin%2F<token>%2Fatak.zip`. HTTP is acceptable here, because ZeroTier encrypts end to end.
     - iTAK/TAKAware: `http://10.147.17.1/join/<token>` → package.
- Private CA throughout. An LE certificate is possible only with an own domain plus DNS-01.

---

## 7. Open questions / device tests needed (all UNVERIFIED)

1. **ATAK 5.x `tak://…/import`:**
   - Does it download through the 4.6 `ImportFileDownloader` path (no hostname check; system trust)?
   - Does **`http://`** still work (`usesCleartextTraffic`)?
   - Does a full connection package imported this way connect immediately?
   - Exact confirm-dialog text?
2. **ATAK 5.x `enroll`:** is the hostname verifier still the legacy Apache one, which rejects LE IP certificates? Does the `preference` link allow `apiCertEnrollmentPort`?
3. **ATAK and the Gen Y chain:** does Quick Connect validate an RSA YR1/YR2 leaf through the Root YR → X1 cross-sign?
4. **iTAK 2.12.x:**
   - Does the JSON QR enroll against (a) an LE IP certificate and (b) a private CA, after the user installs and trusts it as an iOS profile?
   - Which enrollment port does it use, and which `Accept` header?
5. **TAKAware 2.0.2+:** how is a "custom trust store" supplied (data package `caLocation`? settings?), and does the JSON QR then enroll against the private CA?
6. **iOS:** can iTAK/TAKAware be the target of Safari's "Open in…" for a `.zip`, which would save the Files step?
7. **ZeroTier New Central v2:** exact member authorize/list endpoints and payloads.
