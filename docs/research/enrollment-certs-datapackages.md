# Enrollment, certificates and data packages: wire-format spec

Research notes for ChameleonTAK. The goal is byte-level interoperability with ATAK (Android), iTAK (iOS), TAKAware (iOS) and WinTAK.

This was researched on 2026-10-25, mainly by reading source code. Each statement is tagged with how it was established:

| Tag | Meaning |
|---|---|
| **[SRC]** | Read in primary source code or official docs (file path given). |
| **[SRC-2]** | Secondary source: third-party code or docs that claim to be derived from client source we could not read, such as the ATAK 5.x SDK. |
| **[INFERRED]** | Deduced from several primary sources. Not seen directly. |
| **UNVERIFIED** | No primary source found. Treat as an assumption and test on real devices. |

## Sources and versions read

| Source | Version / commit | What it covers |
|---|---|---|
| ATAK-CIV (deptofdefense/AndroidTacticalAssaultKit-CIV) | `main` = **ATAK 4.6.0.5**, commit 889eee292c (2024-10-18). This is the newest public ATAK source; 4.7+/5.x are not public. | Enrollment client, trust managers, data package import, pref parsing, commo TLS |
| TAK Server (TAK-Product-Center/Server) | `main` 5187abd46d (2026-04-03) | Reference server: `CertManagerApi`, `CertManagerService`, `ProfileAPI`, cert scripts, bundled official docs (Appendix B/C/E) |
| OpenTAKServer (brian7704/OpenTAKServer) | `master` e71c74dd83 (2026-10-24) | A second working server implementation; the QR string generators; iTAK/WinTAK package layouts |
| TAKAware (flighttactics/TAKAware) | `main` @ 2025-06-08 (≈ v1.4.2). TAKAware 2.x (2026) is **not** public. | iOS enrollment, QR parsing, package parsing |
| SwiftTAK (flighttactics/SwiftTAK) | `main` @ 2026-10-14 | Constants, pref/manifest/package parsers used by TAKAware |
| goatak (kdudkov/goatak) | `master` 7d6aabaa93 (2026-06-30) | A **Go** TAK server that already uses go-pkcs12 for v1 and v2 enrollment; live QR examples |
| FreeTAKServer | `master` 19fcc6126f (2024-10-29) | iTAK and WinTAK package layouts (no enrollment API) |
| go-pkcs12 (software.sslmate.com/src/go-pkcs12) | **v0.7.4** (same version as ChameleonTAK's `go.mod`) | Encoders |
| BouncyCastle `PKCS12KeyStoreSpi` (bcgit/bc-java `main`) | upstream; Android ships a fork of it | How PKCS#12 aliases appear on Android |
| swift-nio-ssl 2.27.0 / swift-asn1 0.8.0 | versions pinned by TAKAware | PKCS#12 and PEM parsing on TAKAware |
| Apple support docs 103769 and 102028, Apple DTS forum threads | — | iOS TLS certificate rules; PKCS#12 algorithm support |

iTAK and WinTAK are closed source. Everything said about them comes from server implementations that are known to work with them, so it is **[INFERRED]** or **UNVERIFIED**.

---

## 0. Summary of decisions

1. **Implement both enrollment endpoints:**
   - `POST /Marti/api/tls/signClient` (v1) returns a **PKCS#12** with password `atakatak`, aliases `signedCert`, `ca0`, `ca1`, …
   - `POST /Marti/api/tls/signClient/v2` returns **JSON**, or **XML** when `Accept` asks for XML.

   ATAK 4.6 (public source) uses v1. TAKAware uses v2 with JSON. Modern ATAK uses v2 with XML **[INFERRED]**.
2. **Certificate values in the v2 responses:** each value is the base64 of the DER certificate, with **no PEM header or footer**, wrapped with `\n` at **exactly 64 characters**. TAKAware rejects any other line length.
3. **Port 8446** carries `/Marti/api/tls/config`, `/Marti/api/tls/signClient[/v2]` and `/Marti/api/tls/profile/enrollment`. It uses HTTP Basic auth and must **not** require a client certificate. **Port 8443** is mTLS. **Port 8089** is mTLS streaming.
4. **Self-signed CA and QR / Quick Connect:**
   - **ATAK will not enroll** against a private-CA certificate on 8446 unless that CA was imported first. It shows no trust prompt; enrollment simply fails.
   - **iTAK QR enrollment also needs a publicly trusted certificate** on 8446 (per OTS docs; UNVERIFIED against iTAK itself).
   - **TAKAware (public source) accepts any server certificate on 8446.**
   - For a LAN or offline server, give ATAK a **bootstrap ("enrollment-only") data package**: CA truststore plus a pref with `enrollForCertificateWithTrust0=true`. Give iOS clients a **full connection package** with a server-generated client `.p12`. If you own a domain, an alternative is a Let's Encrypt certificate on 8446 only.
5. **PKCS#12 for anything shipped to devices:**
   - Use legacy algorithms: `pkcs12.LegacyDES.WithIterations(2048)`, or `LegacyRC2.WithIterations(2048)`, which matches TAK Server's own output exactly.
   - The password is `atakatak`.
   - Every certificate bag in a truststore **must have a friendlyName**. Android BouncyCastle hides unnamed certificate bags.
   - Never use `Passwordless`. Do not use `Modern`/`Modern2026` for iOS.
6. **Certificates:** use RSA-2048 with SHA-256 signatures.
   - Server certificate: EKU serverAuth (plus clientAuth), SAN with the DNS name **and** the IP address, validity **≤ 397 days**.
   - Client certificate: EKU clientAuth, CN = username.
   - A single root CA is fine. **The username is the CN.**
7. **Go TLS:** keep TLS 1.3 enabled on 8089. ATAK's commo restricts TLS 1.2 ciphers with `DEFAULT:!ECDH`, and Go 1.22+ no longer offers RSA-kex suites by default.

---

## 1. Certificate enrollment API

### 1.1 Ports and end-to-end flow

ATAK builds every base URL as `https://<host>` + `":%d/Marti"`. **[SRC]** `atak/.../comms/SslNetCotPort.java`

| Type | Default port | Auth | ATAK pref key that overrides it |
|---|---|---|---|
| `CERT_ENROLLMENT` | **8446** | HTTP Basic, no client certificate | `apiCertEnrollmentPort` (String) |
| `SECURE` | **8443** | client certificate (mTLS) | `apiSecureServerPort` |
| `UNSECURE` | 8080 | Basic | `apiUnsecureServerPort` |

The pref keys and defaults are in `atak/ATAK/app/src/main/res/xml/network_connections_preferences.xml`. TAKAware defaults are `DEFAULT_CSR_PORT = "8446"`, `DEFAULT_SECURE_API_PORT = "8443"` and `DEFAULT_STREAMING_PORT = "8089"`. **[SRC]** `SwiftTAK/Sources/SwiftTAK/Utilities/TAKConstants.swift`

The TAK Server guide confirms the port and the auth: *"The Certificate Signing endpoint resides on port 8446 and requires HTTP Basic Authentication backed by either File or LDAP authentication."* **[SRC]** Appendix C, `src/takserver-core/src/main/webapp/Marti/documentation/takserver/appendixc/index.html`. The example CoreConfig defines it as `<connector port="8446" clientAuth="false" _name="cert_https"/>`.

ATAK 4.6 flow **[SRC]** (`atak/.../net/CertificateEnrollmentClient.java`, `CertificateConfigOperation.java`, `CertificateSigningOperation.java`, `DeviceProfileOperation.java`):

```
1. GET  https://H:8446/Marti/api/tls/config                         (Basic auth, preemptive)
2. POST https://H:8446/Marti/api/tls/signClient?clientUid=U&version=V   (Basic; body = CSR)   <- 4.6 uses v1
3. GET  https://H:8446/Marti/api/tls/profile/enrollment?clientUid=U  (Basic)  -> 200 zip | 204
4. reconnect streaming  H:8089 (TLS + new client cert)
5. (only if pref deviceProfileEnableOnConnect=true)
   GET  https://H:8443/Marti/api/device/profile/connection?syncSecago=N&clientUid=U   (mTLS) -> 200 zip | 204
```

TAKAware flow **[SRC]** (`TAKAware/Communications/CSRRequestor.swift`):

```
1. GET  https://H:<csrPort>/Marti/api/tls/config        (Basic)
2. POST https://H:<csrPort>/Marti/api/tls/signClient/v2?clientUid=U&version=V  (Basic)
3. connect H:<port> (8089) TLS with the new identity
```

TAKAware does not request the enrollment profile.

**Corrections to the brief:**
- The enrollment-profile path is **`/Marti/api/tls/profile/enrollment`** on 8446, not `/Marti/api/device/profile/enrollment`. **[SRC]** TAK Server `ProfileAPI.java` `@RequestMapping("/tls/profile/enrollment")`; ATAK `DeviceProfileOperation.java` `path = "/api/tls/profile/enrollment?clientUid=" + clientUid; portType = CERT_ENROLLMENT`.
- The connection profile is **`/Marti/api/device/profile/connection?syncSecago=&clientUid=`** on 8443.

### 1.2 `GET /Marti/api/tls/config`

**Request**
- ATAK sends `Authorization: Basic base64(user:pass)` preemptively, through `HttpUtil.AddBasicAuthentication`. It sends no `Accept` header. **[SRC]** `CertificateConfigOperation.java`, `TakHttpClient.java`
- TAKAware sends `Authorization: Basic …` and `Content-Type: text/plain; charset=utf-8`. **[SRC]** `CSRRequestor.generateConfigRequest()`
- ATAK treats HTTP 401 on any enrollment step as `BAD_CREDENTIALS` and re-opens the credentials dialog. Validating credentials here therefore gives good UX. Return `401` with `WWW-Authenticate: Basic realm="ChameleonTAK"`. **[SRC]** `CertificateEnrollmentClient.onRequestConnectionError`. OTS does not authenticate this endpoint. TAK Server's auth on it is UNVERIFIED.

**Response body.** This is a byte-exact template that works with both ATAK and TAKAware (it is what OTS emits, and mirrors TAK Server's JAXB output):

```xml
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<ns2:certificateConfig xmlns="http://bbn.com/marti/xml/config" xmlns:ns2="com.bbn.marti.config"><nameEntries><nameEntry name="O" value="ChameleonTAK"/><nameEntry name="OU" value="TAK"/></nameEntries></ns2:certificateConfig>
```

- TAK Server marshals `CertificateConfig` under `QName("com.bbn.marti.config","certificateConfig")`. **[SRC]** `CertManagerApi.getConfig()`
- OTS builds the same structure (`ns2:certificateConfig`, `xmlns="http://bbn.com/marti/xml/config"`, `xmlns:ns2="com.bbn.marti.config"`). **[SRC]** `opentakserver/blueprints/marti_api/certificate_enrollment_api.py`
- goatak sends a namespace-less `<certificateConfig validityDays="N"><nameEntries>…` and also works. **[SRC]** `cmd/goatak_server/cert_api.go`

**Content-Type:** send `text/plain; charset=UTF-8` (as OTS does) or `application/xml`. TAKAware **rejects any other MIME type**: it requires `mimeType == "text/plain" || mimeType == "application/xml"`. **[SRC]** `CSRRequestor.beginEnrollment()`

**ATAK's parser** is SimpleXML `@Root(name="certificateConfig")` with an `@ElementList nameEntries` (each `nameEntry` has attributes `name` and `value`) and an optional `@Attribute validityDays`. **[SRC]** `atak/.../net/certconfig/CertificateConfig.java`, `NameEntry.java`. `Persister.read(Class,String)` is strict by default, so do **not** add other elements or attributes. (The strictness claim comes from SimpleXML library semantics and is UNVERIFIED on a device.)

**Allowed `name` values**
- ATAK passes each name to OpenSSL `OBJ_txt2nid()`. An unknown name makes CSR generation fail and enrollment aborts. Use OpenSSL short names: `C`, `ST`, `L`, `O`, `OU`, `DC`. **[SRC]** `commoncommo/core/impl/cryptoutil.cpp`
- TAKAware honours only `O`, `OU` and `DC`, case-insensitively. Anything else is logged and **dropped from the CSR**. **[SRC]** `TAKAware/Parsers/TAKCAConfigResponseParser.swift`
- **Recommendation:** send only `O` and `OU`, with **unique names**. ATAK stores entries in a `HashMap` keyed by name, so duplicate names collapse.

### 1.3 How clients build the CSR

| | ATAK 4.6 **[SRC]** | TAKAware **[SRC]** | iTAK |
|---|---|---|---|
| Subject | `CN=<username typed in UI / QR>` plus every `nameEntry`. RDN order follows Java `HashMap` iteration, so it is **unpredictable**. Values are `MBSTRING_ASC`. | `CN=<username>`, then each `O`, then each `OU`, then each `DC`, all as PrintableString | CN = username **UNVERIFIED** |
| Key | RSA, length from pref `apiCertEnrollmentKeyLength`, **default 4096** (`CommsMapComponent.generateKey`) | RSA 2048, generated in the Keychain (`CertificateManager.generateTaggedPrivateKey`) | UNVERIFIED |
| CSR signature | **SHA-1** with RSA (`X509_REQ_sign(req, pkey, EVP_sha1())` in `cryptoutil.cpp`) | sha256WithRSAEncryption, CSR version v1, no attributes | UNVERIFIED |
| Body sent | PEM with only the header and footer *strings* removed. That leaves `"\n" + 64-char base64 lines each ending in "\n" + "\n"` (a leading newline and a trailing blank line). Sent via Apache `StringEntity`, so `Content-Type: text/plain; charset=ISO-8859-1` (UNVERIFIED on the wire; this is the Apache library default). | `base64(DER)` as **one line** with no newlines. `Content-Type: text/plain; charset=utf-8`. | JSON client **[INFERRED]** |
| Query | `?clientUid=<MapView.getDeviceUid()>&version=<URL-encoded ATAK version>` | `?clientUid=<AppConstants.getClientID()>&version=<release+build>`, built with `URLComponents` | ? |

**Server parsing rules for ChameleonTAK**
1. Strip `-----BEGIN CERTIFICATE REQUEST-----` and `-----END CERTIFICATE REQUEST-----` if present (also accept `NEW CERTIFICATE REQUEST`), then remove all whitespace, then base64-decode. TAK Server does the same through commons-codec `Base64.decodeBase64`, which ignores whitespace. **[SRC]** `CertManagerService.signClient`
2. `x509.ParseCertificateRequest` followed by `csr.CheckSignature()`. Go **allows SHA-1 on CSRs**: `checkSignature(..., allowSHA1=true)` with the comment "SHA-1 signatures are only allowed for CRLs and CSRs". **[SRC]** `src/crypto/x509/x509.go`
3. Read the query parameter `clientUid` **case-insensitively**. OTS also accepts `clientUID`, and goatak uses `queryIgnoreCase`.

### 1.4 `POST /Marti/api/tls/signClient` (v1), used by ATAK 4.6

The response is a PKCS#12 holding certificates only, password **`atakatak`**:
- TAK Server: `keyStore.setCertificateEntry("signedCert", cert)`, then `"ca"+ndx` for each certificate in the signing chain, then `keyStore.store(bos, "atakatak")`. Body is `application/octet-stream`. **[SRC]** `CertManagerApi.signClientCert`
- ATAK loads it with `KeyStore.getInstance("PKCS12").load(bais, "atakatak")`. It **enumerates aliases**. The alias that is `compareTo("signedCert") == 0` (case-sensitive) becomes the client certificate; every other alias becomes a CA certificate in the new per-server truststore. **[SRC]** `CertificateSigningOperation.processResults`. The password comes from `takkernel/engine/src/main/res/values/strings.xml`: `<string name="defaultTrustStorePassword">atakatak</string>`.
- On Android, BouncyCastle only exposes a certificate bag as an alias if it has a **friendlyName**. See §4.2. The aliases must therefore be friendlyName attributes.

Go implementation (identical to goatak `pkg/tlsutil/util.go` `MakeP12TrustStoreNamed`):

```go
entries := []pkcs12.TrustStoreEntry{{Cert: signed, FriendlyName: "signedCert"}}
for i, ca := range chain { // issuing CA first, root last
    entries = append(entries, pkcs12.TrustStoreEntry{Cert: ca, FriendlyName: fmt.Sprintf("ca%d", i)})
}
body, err := pkcs12.LegacyRC2.WithIterations(2048).EncodeTrustStoreEntries(entries, "atakatak")
// Content-Type: application/octet-stream
```

### 1.5 `POST /Marti/api/tls/signClient/v2`, JSON or XML

**TAK Server reference behaviour [SRC]** (`CertManagerApi.signClientCertV2`):

| `Accept` (lower-cased) | Response |
|---|---|
| absent, empty, or **contains** `*/*` or `application/json` | JSON, `Content-Type: application/json` |
| contains `application/xml` | XML, `Content-Type: application/xml` |
| anything else | **400** |

**OTS behaviour [SRC]**:
- `Accept` exactly `text/plain` → JSON body but **`Content-Type: text/plain`**. OTS comments: *"iTAK expects a JSON response but with the Content-Type header set to text/plain for some reason"*.
- Exactly `application/json`, exactly `*/*`, or absent → JSON.
- Anything else → XML.

**Recommended ChameleonTAK negotiation.** Never return 400.

```
a := strings.ToLower(r.Header.Get("Accept"))
if strings.Contains(a, "application/xml") && !strings.Contains(a, "application/json") { -> XML, "application/xml; charset=utf-8" }
else -> JSON; Content-Type = "text/plain; charset=utf-8" if a == "text/plain" else "application/json"
```

TAKAware requires the response MIME to be `application/json` or `text/plain`. **[SRC]** `CSRRequestor.makeCSRRequest`

**Certificate encoding inside v2 responses [SRC]**. TAK Server uses `Util.certToPEM(cert, false)`, which is `printBase64Binary(DER).replaceAll("(.{64})", "$1\n")`. That gives **no header or footer** and a **`\n` after every 64 characters**. A trailing `\n` is present only when the length is a multiple of 64. OTS strips the PEM armour, leaving 64-char lines with no trailing newline.

**TAKAware hard requirement [SRC].** TAKAware re-wraps each value as `"-----BEGIN CERTIFICATE-----\n" + value + "\n-----END CERTIFICATE-----"` and parses it with swift-asn1 `PEMDocument`. That parser requires **every line except the last to be exactly 64 characters, and the last ≤ 64** (`lines.dropLast().allSatisfy({ $0.utf8.count == 64 })`). Empty lines are skipped. **Single-line base64 therefore fails on TAKAware.** Emit 64-char lines joined by `\n` with no trailing newline. That is safe for every client.

**TAKAware hard requirement 2 [SRC].** The JSON is parsed as `json as? [String: String]`. **Every value must be a string.** Adding something like `"success": true` makes the cast fail silently and enrollment never completes. All keys starting with `ca` are treated as trust anchors.

**JSON example.** This is TAK Server's format; Jackson pretty-printer, `\n` inside the strings:

```json
{
  "signedCert" : "MIIDszCCApugAwIBAgIEbHq3PTANBgkqhkiG9w0BAQsFADBRMQswCQYDVQQGEwJV\nUzELMAkGA1UECAwCVkExEzARBgNVBAoMCkNoYW1lbGVvbjENMAsGA1UECwwEVEFL\n...\nq0Yb1w==",
  "ca0" : "MIIDZTCCAk2gAwIBAgIUQ2k3...\n...\n4rKz",
  "ca1" : "MIIDXTCCAkWgAwIBAgIJAKx...\n...\nZ9Q="
}
```

**XML example.** TAK Server, no whitespace between elements, raw newlines inside the text:

```xml
<?xml version="1.0" encoding="UTF-8"?><enrollment><signedCert>MIIDszCCApugAwIBAgIEbHq3PTANBgkqhkiG9w0BAQsFADBRMQswCQYDVQQGEwJV
...
q0Yb1w==</signedCert><ca>MIIDZTCCAk2gAwIBAgIUQ2k3...
...
4rKz</ca></enrollment>
```

There is one `<ca>` element per chain certificate. OTS emits a single `<ca>`. goatak emits several `<ca>` elements.

**What goes into `ca0…caN`**
- TAK Server: `caN` = `subMgr.getSigningCertChain()`, with the issuing (intermediate signing) CA first and the root last. **[SRC]** `CertManagerService.signClient`
- OTS: single root CA, emitted as **both** `ca0` and `ca1`. **[SRC]**
- goatak: `ca0` is the server's own certificate, with the comment `// iTAK needs server cert in answer`. **[SRC]**
- **Critical [SRC]:** ATAK 4.6 Quick Connect (no truststore for that host) **saves the returned `caN` as the per-server truststore for streaming on 8089** (`CertificateSigningOperation`: `if (!initialRequest.hasTruststore()) saveCertificateForServerAndPort(TYPE_TRUST_STORE_CA, …)`). TAKAware stores `caN` as `serverCertificateTruststore` and uses them as anchors for 8089/8443 (`TCPMessage.swift`, `APIRequestObject.swift`).
- **Therefore `caN` must contain the CA chain that validates the server's 8089/8443 certificate.** It must **not** contain the public CA of a Let's Encrypt certificate used only on 8446.

**Which client uses which format**

| Client | Endpoint | Format |
|---|---|---|
| ATAK 4.6 | v1 | PKCS#12 **[SRC]** |
| ATAK 4.7+/5.x | v2 + XML | **[INFERRED]**: OTS's v1 handler returns an empty `200`, which would crash the 4.6 parser, yet OTS supports current ATAK. OTS and goatak implement an XML branch, and TAK Server only emits XML for `Accept: application/xml`. The exact `Accept` value modern ATAK sends is UNVERIFIED. |
| TAKAware | v2 | JSON (no explicit `Accept`; URLSession default `*/*`) **[SRC]** |
| iTAK | v2 | JSON **[INFERRED]** (OTS comment) |
| WinTAK | ? | UNVERIFIED |

### 1.6 Server-side validation and signing (what TAK Server does)

**[SRC]** `CertManagerService.validateCSR` / `signClient`, `CertManager.signCertificate`:

- The CSR must contain **exactly `len(nameEntries)+1` RDNs**.
  - **Do not copy this rule.** TAKAware silently drops names other than O/OU/DC, so a strict count would break it.
- The CSR **CN must equal the HTTP Basic username, case-insensitively.** OTS compares exactly and returns `400 {"success":false,"error":"Invalid Certificate Common Name"}`. goatak also compares exactly.
- Each configured nameEntry must be present. Optional for ChameleonTAK; be lenient.
- The signed certificate uses the **CSR subject verbatim**. In Go, set `template.RawSubject = csr.RawSubject` to preserve RDN order and encoding.
- Validity: `notBefore = now - validityNotBeforeOffsetMinutes` (default **720 min = 12 h back-dating**), `notAfter = now + validityDays` (default **365**). Source: `CoreConfig.xsd` `TAKServerCAConfig`.
- Client extensions: KeyUsage `digitalSignature, keyAgreement, nonRepudiation`; EKU `clientAuth`. If the request has a `version` query parameter, the EKU also gets **`1.2.840.113549.1.9.7`** (challengePassword). The code comment says *"TAK 4.4 clients that support Channels will pass in the version parameter"*. What clients do with that OID is UNVERIFIED. Replicating it is harmless.
- The username is later taken from the certificate with `DNUsernameExtractorRegex`, default `CN=(.*?)(?:,|$)`. **[SRC]** `CoreConfig.xsd`

### 1.7 After enrollment

`GET /Marti/api/tls/profile/enrollment?clientUid=U` on 8446, Basic auth:
- **204** if there is nothing to send. ATAK treats this as success.
- Otherwise **200**, body is a mission-package zip (with `MANIFEST/manifest.xml`), `Content-Type: application/zip`, `Content-Disposition: attachment; filename=profile.zip`.
- **[SRC]** TAK Server `ProfileAPI.getEnrollmentTimeProfiles`. ATAK `DeviceProfileOperation` handles `SC_NO_CONTENT` and `SC_NOT_MODIFIED` as non-errors, and auto-imports the 200 body through `MissionPackageExtractorFactory.Extract`. The enrollment profile is always requested, regardless of the `deviceProfileEnableOnConnect` pref.
- TAK Server's package parameters are `uid`, `name="Enrollment"`, `onReceiveImport=true`, `onReceiveDelete=true`. Files go in `file0/<name>`, `file1/<name>`, … plus `MANIFEST/manifest.xml`. **[SRC]** `ProfileService.createProfileMissionPackage`, `util/missionpackage/MissionPackage.java`

`GET /Marti/api/device/profile/connection?syncSecago=N&clientUid=U` on 8443 (mTLS) returns 200 zip or 204. ATAK only calls it when the pref `deviceProfileEnableOnConnect=true`. **[SRC]** `DeviceProfileClient.getProfile`

### 1.8 Streaming auth message (8089)

When a connection has `useAuth=true` and a username, commo sends this **as the first message after the TLS handshake**:

```xml
<?xml version="1.0"?>
<auth><cot username="alice" password="secret" uid="ANDROID-xxxxxxxx"/></auth>
```

The trailing newline is removed. **[SRC]** `commoncommo/core/impl/streamingsocketmanagement.cpp`, `SSLConnectionContext` constructor. ChameleonTAK's 8089 parser must accept or ignore this. OTS's ATAK instructions tell users to enable "Use Authentication" for enrollment, so expect it.

---

## 2. QR codes

### 2.1 ATAK enrollment URI

```
tak://com.atakmap.app/enroll?host=<host>[:<port>[:quic]]&username=<user>&token=<secret>
```

**Evidence**
- OTS generates `tak://com.atakmap.app/enroll?host={hostname}:{OTS_SSL_STREAMING_PORT}&username={username}&token={jwt}` in `opentakserver/blueprints/ots_api/token_api.py`. PR #348 added the port because *"When ATAK receives a host= value with no port, it falls back to its compiled-in default SSL CoT port: 8089"*.
- goatak's live public-server QR, decoded from `img/qr-enroll.png`, is exactly `tak://com.atakmap.app/enroll?host=takserver.ru&username=test&token=111111`. The token there is just the password.
- **[SRC-2]** qrtak issue #240 quotes ATAK CIV SDK 5.5.0.0 `CertificateEnrollmentClient.java`: `split(":")`, `port = split.length > 1 ? parseInt(split[1], 8089) : 8089`, and protocol is overridden only when `split[2]` equals `quic`. ATAK 4.6's manual Quick-Connect handler (`onEnrollmentOk`) applies the same defaults **[SRC]**.

**Parameters**

| Parameter | Meaning |
|---|---|
| `host` | **Streaming** host plus optional streaming port, default 8089, protocol `ssl`. There is **no parameter for the enrollment port**: it always comes from the ATAK pref `apiCertEnrollmentPort` (default 8446). |
| `username` | Basic-auth username; also becomes the CN. |
| `token` | Sent as the **Basic-auth password**. OTS's `basic_auth()` falls back to `Token.verify_token(username, password)`; microtak describes it as "tokens as Basic password". So ChameleonTAK may accept one-time or limited-use tokens in the password field on 8446. |

- Percent-encode all values.
- Which ATAK release introduced the deep link is UNVERIFIED. It is not in 4.6 (no `tak` scheme in `AndroidManifest.xml`). The OTS docs say *"ATAK QR code (since [OTS] version 1.5.0)"*.

**Other ATAK deep links [SRC-2, UNVERIFIED]** (qrtak `docs/qr-formats.md`, which says it was checked against ATAK CIV source):
- `tak://com.atakmap.app/import?url=<percent-encoded-url>`: asks the user to confirm, then imports.
- `tak://com.atakmap.app/preference?key1=<k>&type1=<string|boolean|long|int>&value1=<v>&key2=…`

### 2.2 iTAK QR

```
<description>,<host>,<port>,<protocol>
```

Four comma-separated fields. Protocol is `ssl` (or `tcp`).

- OTS: `"OpenTAKServer_{host},{host},{8089},SSL"` (uppercase). **[SRC]** `blueprints/ots_api/api.py` `/api/itak_qr_string`
- goatak live QR: `takserver.ru,takserver.ru,8089,ssl` (lowercase), decoded from `img/qr.png`.
- TAKAware comment: *"iTAK QR Codes are of the form serverName,serverURL,serverPort,serverProtocol"*.
- There are **no credentials** in the iTAK QR.
- That iTAK then asks for username and password and enrolls on 8446 is widely reported but **UNVERIFIED** (closed source).

### 2.3 TAKAware QR (public source v1.4)

**[SRC]** `TAKAware/Parsers/QRCodeParser.swift`:

1. **iTAK CSV**: exactly 4 comma-separated parts. Fills host and port. The user must type the username and password; there is no auto-submit.
2. **JSON** (string starts with `{`):
   ```json
   {"serverCredentials":{"connectionString":"tak.example.com:8089"},
    "userCredentials":{"username":"alice","password":"secret"}}
   ```
   - `connectionString` is split on `:`, so `host:port[:proto]` works.
   - If both username and password are non-empty, enrollment **auto-submits**.
   - Where this JSON format originates is UNVERIFIED.
3. Public v1.4 source does **not** parse `tak://…/enroll`, and the app registers no URL scheme. **[SRC-2]** TAK-Portal issues #55/#62 report that **TAKAware 2.x (2026)** registers `tak://` and handles `tak://com.atakmap.app/enroll?host&username&token` with an "Autoenroll to <host>?" dialog. TAK Tracker registers `tak://` too, and iOS picks one of them arbitrarily.

### 2.4 Does username/password or QR enrollment work with a private self-signed CA on 8446?

| Client | Result | Evidence |
|---|---|---|
| **ATAK** | **No.** It fails with "TAK Server registration failed" or "The TAK Server's identity could not be verified". **There is no trust prompt** (4.6). The Android system/user CA store is *ignored* for enrollment. | **[SRC]** `takkernel/.../net/CertificateManager.java`: every enrollment socket factory is `new CentralTrustManager(this)` with `setIgnoreSystemCerts(true)`. On failure of the local trust manager it calls `cp.promptToAccept(certs)`, and the default `PERMISSIVE_CERT_PROMPT` returns `false` (*"self signed server cert detected, without anchor, blocked by default"*). The local trust set is only the embedded DoD roots, DigiCert High Assurance EV / Global Root CA, **ISRG Root X1**, plus every CA truststore already imported into ATAK's certificate DB (`AtakCertificateDatabase.getCACerts()`). |
| | Official confirmation | TAK Server **Appendix E**: *"When enrolling using a server certificate from a self-signed TAK CA, clients must be bootstrapped with server's CA certificate prior to enrollment … Quick Connect … performs Certificate Enrollment using trust provided by LetsEncrypt or DigiCert CAs … TAK clients have embedded the LetsEncrypt and DigiCert CAs."* Configure it with `<connector port="8446" clientAuth="false" _name="cert_https" keystore="JKS" keystoreFile="certs/files/letsencrypt-server-cert.jks" …/>`, and never use the public CA in `<tls>` or in client truststores. |
| | OTS confirmation | `token_api.py` docstring: *"ATAK certificate enrollment via QR code only works if your server has a Let's Encrypt certificate."* OTS docs: QR codes need *"a domain name with a trusted certificate from Let's Encrypt or a similar authority."* |
| | Hostname check | With **no** truststore for the host, ATAK verifies the hostname strictly (`allowAllHostnames = truststore != null`). With a truststore, the hostname is not checked. **[SRC]** `CertificateEnrollmentClient.verifyTrust` |
| **iTAK** | **No.** Needs a publicly trusted certificate. | OTS docs: *"iTAK QR enrollment requires a certificate from Let's Encrypt or a similar trusted authority. Self-signed certificates won't work for this."* (secondary, not iTAK source). Whether iTAK can enroll after importing a truststore-only package is UNVERIFIED. |
| **TAKAware** (v1.4) | **Yes.** It accepts **any** server certificate on 8446 (TOFU-less: no validation at all), then pins `caN` from the response for 8089/8443 with hostname checks disabled. | **[SRC]** `CSRRequestor.urlSession(_:didReceive:)` answers every `NSURLAuthenticationMethodServerTrust` challenge with `.useCredential, URLCredential(trust: serverTrust)` without evaluating it. `Info.plist` has `NSAllowsArbitraryLoads=true`. |
| **WinTAK** | UNVERIFIED | — |

### 2.5 Workarounds for a LAN or offline server with no public domain

1. **ATAK bootstrap ("enrollment-only") data package**. Recommended; verified in 4.6 source.
   - Ship a zip containing the CA truststore `.p12` and a `.pref` with `connectString0`, `caLocation0`, `caPassword0` and **`enrollForCertificateWithTrust0=true`**. Template in §3.6.
   - On import, `ImportCertSort.finalizeImport()` imports the truststore *for that connection*. Because `enrollForCertificateWithTrust` is set, it then calls **`CertificateEnrollmentClient.enroll(...)` automatically**.
   - ATAK then shows the credentials dialog: `CredentialsDialog.createCredentialDialog` runs when no `TYPE_COT_SERVICE` credentials are stored for the host.
   - Enrollment then trusts the private CA, because `getCACerts()` includes it.
   - Credentials cannot be pre-filled from the pref. 4.6's `PreferenceControl` reads no `username0`/`password0`, and **[SRC-2]** qrtak reports the same for ATAK 5.8.0.5.
   - goatak's `cert/make_connect_dp.sh` builds exactly this package.
2. **Full connection package** (server-side key generation) for iTAK, WinTAK, or any client: truststore + client `.p12` + pref. No enrollment and no trust problem. This is what OTS emits for iTAK and WinTAK.
3. **TAKAware**: plain username/password or QR enrollment works against a private CA. See the security note in §2.4.
4. **Publicly trusted certificate on 8446 only.** This needs a domain you control.
   - Get a Let's Encrypt certificate with the **DNS-01** challenge (works without inbound internet), and resolve the name to the LAN IP with local DNS.
   - Keep the **private CA on 8089/8443** and return the private CA in `caN` (Appendix E pattern).
   - Prefer an **RSA** LE certificate chaining to **ISRG Root X1**, because ATAK 4.6 embeds only `isrgrootx1.crt` (`CertificateManager.initialize`). That is a recommendation, not a tested requirement.
5. *Possible but UNVERIFIED:* serve the bootstrap package over plain HTTP on the LAN and hand it out with `tak://com.atakmap.app/import?url=http://…`. Android cleartext policy may block it.

---

## 3. Data packages

### 3.1 How ATAK imports a zip

**[SRC]** `atak/.../missionpackage/file/*`, `importfiles/sort/*`

- **Manifest detection.** ATAK reads the zip entry **`MANIFEST/manifest.xml`** (exact path; Java `ZipFile.getEntry` is case-sensitive). It accepts it if the text contains `<MissionPackageManifest` **and** `version="2"` (or `version='2'`).
- **With a valid manifest**, only the `<Content zipEntry="…">` entries are extracted. The lookup is an exact match with `/` separators. Entries not in the manifest are ignored, and `ignore="true"` entries are skipped.
- **With no manifest or an invalid one** (`PlainZipExtractor`), ATAK rebuilds a package containing **every** non-directory entry, except anything under `MANIFEST/` (prefix compared case-insensitively). It then imports it normally. So a flat zip with no manifest also works on ATAK.
  - Example: goatak writes `MANIFEST/MANIFEST.xml`. ATAK does not match it, so the package takes this path and still works.
- **Sorters**
  - `*.p12` is copied to **`<atak root>/cert/<basename>`** (`ImportCertSort`: `super(".p12", "cert", …)`).
  - `*.pref` is accepted only if the first 8192 characters contain `<preferences` **and** either `<entry key` or `<preference key`. It is then loaded with `PreferenceControl.loadSettings`.
  - **Gotcha:** write `<entry key="…"` with `key` as the **first** attribute and a single space.
- After all contents are extracted, each sorter's `finalizeImport()` runs. `ImportCertSort.finalizeImport()` matches up truststores and certificates with the connections the `.pref` just created.
- **Path rule.** The certificate paths in the pref are resolved as `new File(location)`, falling back to `FileSystemUtils.getItem(location)`, which is relative to the ATAK root. So **`caLocation0 = "cert/<basename of the .p12>"`**. The `.p12` can sit anywhere in the zip; only the basename matters.
  - After import, the file is **deleted** (`importCertificate(..., delete=true)`) and stored in ATAK's encrypted certificate DB.
  - Use **unique basenames** per server, such as `truststore-<host>.p12`. Packages that share a basename overwrite each other's file in `atak/cert/`.
- `onReceiveDelete` and `onReceiveImport` are applied **only to packages received over the network**, not to local imports. **[SRC]** `MissionPackageConfiguration.java` comments

### 3.2 `MANIFEST/manifest.xml`

**[SRC]** `MissionPackageManifest.java`, `MissionPackageConfiguration.java`, `MissionPackageContent.java`:

- `version` is required.
- `<Configuration>` and `<Contents>` are required.
- `isValid()` needs Configuration parameters **`uid` and `name`**.

```xml
<MissionPackageManifest version="2">
  <Configuration>
    <Parameter name="uid" value="4e0c6a8e-2a7b-4c27-9a0c-3f4f6c9d1b77"/>
    <Parameter name="name" value="ChameleonTAK_tak.example.lan"/>
    <Parameter name="onReceiveDelete" value="true"/>
  </Configuration>
  <Contents>
    <Content ignore="false" zipEntry="certs/chameleontak.pref"/>
    <Content ignore="false" zipEntry="certs/truststore-tak.example.lan.p12"/>
    <Content ignore="false" zipEntry="certs/alice.p12"/>
  </Contents>
</MissionPackageManifest>
```

Other parameters:
- Configuration: `remarks`, `onReceiveImport`, `onReceiveAction`.
- Content-level `<Parameter name=… value=…/>`: `uid`, `name`, `contentType`, `isCoT`, `visible`, `localpath`, `refContent`, `deleteWithPackage`.

TAK Server additionally writes directory entries such as `MANIFEST/` and `file0/`. They are not required.

### 3.3 `.pref` format and every `cot_streams` key ATAK 4.6 reads

**[SRC]** `PreferenceControl.loadSettings(InputStream)`

- `<preference name="cot_streams">` entries are read as raw text. The `class` attribute is **ignored** for this section.
- Every other `<preference name=X>` is written to SharedPreferences X with typed values. `class` must be one of `class java.lang.String|Boolean|Integer|Float|Long` (or a set class).
- `com.atakmap.app_preferences` (and `civ_`/`fvey_`) map to the default preferences.

| Key (`N` = index from 0) | Read as | Notes |
|---|---|---|
| `count` | `Integer.parseInt` | **Required**; the section is skipped if it is empty. |
| `descriptionN` | String | Display name |
| `connectStringN` | String | `host:port:proto`, with `proto` one of `ssl`, `tcp` (`quic` on 5.x **[SRC-2]**) |
| `enabledN` | `Boolean.parseBoolean` | **A missing value means `false`**. Always write `true`. |
| `useAuthN` | Boolean | Sends the `<auth>` message on 8089 (§1.8) and shows the credential fields. |
| `compressN` | Boolean | |
| `cacheCredsN` | String | Must equal the **localized** strings `Cache credentials` / `Cache username` / `Do not cache` (`R.string.cache_creds_*`). They are compared with `getString()`, so this is locale-dependent. |
| `caLocationN` / `caPasswordN` | String | Per-connection truststore and its password. Password defaults to `atakatak`. |
| `certificateLocationN` / `clientPasswordN` | String | Per-connection client `.p12` and its password. Password defaults to `atakatak`. |
| `enrollForCertificateWithTrustN` | Boolean | Triggers automatic enrollment after import (§2.5). |
| `expirationN` | `Long.parseLong` | `-1` means never. |

- **Not read in 4.6:** `usernameN` and `passwordN`. **[SRC-2]** Also not used for the enrollment prompt in 5.8.0.5.
- Default (non-indexed) keys in `com.atakmap.app_preferences`, imported by `ImportCertSort.importCertificateFromPreferences`: `caLocation`, `caPassword`, `certificateLocation`, `clientPassword`, `updateServerCaLocation`, `updateServerCaPassword`.
- After import, ATAK scrubs `clientPassword`, `caPassword`, `certificateLocation`, `caLocation` and `networkMeshKey` from the stored copy of the `.pref` (`ImportPrefSort.entrysToDelete`).
- Useful app prefs, all in `network_connections_preferences.xml`:
  - `displayServerConnectionWidget` (Boolean)
  - `deviceProfileEnableOnConnect` (Boolean, default false)
  - `apiCertEnrollmentPort` (String, default `8446`)
  - `apiSecureServerPort` (String, default `8443`)
  - `apiCertEnrollmentKeyLength` (String, default `4096`)
  - `certEnrollmentExport` (Boolean)

### 3.4 ATAK full connection package (pre-issued client certificate)

Zip layout:

```
MANIFEST/manifest.xml                          (as in §3.2)
certs/chameleontak.pref
certs/truststore-tak.example.lan.p12
certs/alice.p12
```

`certs/chameleontak.pref`:

```xml
<?xml version='1.0' encoding='ASCII' standalone='yes'?>
<preferences>
  <preference version="1" name="cot_streams">
    <entry key="count" class="class java.lang.Integer">1</entry>
    <entry key="description0" class="class java.lang.String">ChameleonTAK</entry>
    <entry key="enabled0" class="class java.lang.Boolean">true</entry>
    <entry key="connectString0" class="class java.lang.String">tak.example.lan:8089:ssl</entry>
    <entry key="caLocation0" class="class java.lang.String">cert/truststore-tak.example.lan.p12</entry>
    <entry key="caPassword0" class="class java.lang.String">atakatak</entry>
    <entry key="certificateLocation0" class="class java.lang.String">cert/alice.p12</entry>
    <entry key="clientPassword0" class="class java.lang.String">atakatak</entry>
  </preference>
  <preference version="1" name="com.atakmap.app_preferences">
    <entry key="displayServerConnectionWidget" class="class java.lang.Boolean">true</entry>
  </preference>
</preferences>
```

OTS uses the same keys with `cert/<file>` paths and a random-hex folder in the zip. **[SRC]** `certificate_authority.py` `generate_zip`

### 3.5 Nested package (OTS/FTS "because WinTAK")

OTS and FTS wrap the ATAK package above in an **outer** mission package:

```
<outer>.zip
  MANIFEST/manifest.xml   -> <Content ignore="false" zipEntry="80b828699e074a239066d454a76284eb/alice.zip"/>
  80b828699e074a239066d454a76284eb/alice.zip      (the inner package of §3.4)
```

- The outer manifest uses `name=…_CONFIG` and no `onReceiveDelete`. The same file is served to both ATAK and WinTAK.
- The code comment is *"Create outer DP...because WinTAK"*. **[SRC]** OTS `generate_zip`, FTS `core/util/certificate_generation.py` `generate_wintak_zip`
- That WinTAK *requires* nesting is **UNVERIFIED**. Shipping the nested form to WinTAK is the low-risk choice.

### 3.6 ATAK enrollment-only (bootstrap) package

```
MANIFEST/manifest.xml       (Contents: certs/chameleontak.pref, certs/truststore-tak.example.lan.p12)
certs/chameleontak.pref
certs/truststore-tak.example.lan.p12
```

```xml
<?xml version='1.0' encoding='ASCII' standalone='yes'?>
<preferences>
  <preference version="1" name="cot_streams">
    <entry key="count" class="class java.lang.Integer">1</entry>
    <entry key="description0" class="class java.lang.String">ChameleonTAK</entry>
    <entry key="enabled0" class="class java.lang.Boolean">true</entry>
    <entry key="connectString0" class="class java.lang.String">tak.example.lan:8089:ssl</entry>
    <entry key="caLocation0" class="class java.lang.String">cert/truststore-tak.example.lan.p12</entry>
    <entry key="caPassword0" class="class java.lang.String">atakatak</entry>
    <entry key="enrollForCertificateWithTrust0" class="class java.lang.Boolean">true</entry>
    <entry key="useAuth0" class="class java.lang.Boolean">true</entry>
    <entry key="cacheCreds0" class="class java.lang.String">Cache credentials</entry>
  </preference>
  <preference version="1" name="com.atakmap.app_preferences">
    <entry key="displayServerConnectionWidget" class="class java.lang.Boolean">true</entry>
  </preference>
</preferences>
```

- goatak's version uses `useAuth0=false`. Either works for enrollment.
- `useAuth0=true` adds the `<auth>` message on 8089.
- The `.p12` **must** be in the package. `ImportCertSort.finalizeImport()` returns early when no `.p12` was sorted, and enrollment is triggered from there.

### 3.7 iTAK package

The format is **[INFERRED]** from TAK-Server-generated packages (the TAKAware test fixture `itak-do-foyc.zip`), OTS `_CONFIG_iTAK.zip`, and FTS `generate_standard_zip`:

- A **flat** zip with everything at the root. A manifest is optional:
  - OTS: none.
  - TAK fixture and FTS: `manifest.xml` at the root, not in `MANIFEST/`, with backslash `zipEntry` values that don't match the actual entries.
- Any `*.pref` filename. Cert paths in the pref (`cert/…`) do **not** have to match zip paths; files are found by basename.
- Uses **non-indexed** keys in `com.atakmap.app_preferences`.

```
config.pref
alice.p12
truststore-tak.example.lan.p12
```

```xml
<?xml version='1.0' standalone='yes'?>
<preferences>
  <preference version="1" name="cot_streams">
    <entry key="count" class="class java.lang.Integer">1</entry>
    <entry key="description0" class="class java.lang.String">ChameleonTAK</entry>
    <entry key="enabled0" class="class java.lang.Boolean">true</entry>
    <entry key="connectString0" class="class java.lang.String">tak.example.lan:8089:ssl</entry>
  </preference>
  <preference version="1" name="com.atakmap.app_preferences">
    <entry key="displayServerConnectionWidget" class="class java.lang.Boolean">true</entry>
    <entry key="caLocation" class="class java.lang.String">cert/truststore-tak.example.lan.p12</entry>
    <entry key="caPassword" class="class java.lang.String">atakatak</entry>
    <entry key="clientPassword" class="class java.lang.String">atakatak</entry>
    <entry key="certificateLocation" class="class java.lang.String">cert/alice.p12</entry>
  </preference>
</preferences>
```

- Use the **same password for the truststore and the client `.p12`**. OTS, FTS and TAK fixtures all do. That iTAK requires it is UNVERIFIED.
- An iTAK enrollment-only package (truststore + `enrollForCertificateWithTrust`): **UNVERIFIED / not supported as far as we know**.

**TAKAware package parsing [SRC]** (`SwiftTAK/Parsers/DataPackagerParser.swift`, `PreferencesParser.swift`):

- Reads every `*.pref` and merges them.
- The **user certificate is read only from non-indexed `certificateLocation`/`clientPassword`**. `certificateLocation0` is ignored.
- `caLocation*`/`caPassword*` are read indexed or not.
- The connection comes from `connectStringN` (or bare `connectString`).
- Certificate files are resolved by the pref path, then by the zip-relative path, then by **case-insensitive basename anywhere in the zip**. If no CA file matches, **every `.p12` that isn't the identity** is loaded as a server truststore, with the CA password.
- Backslash paths are normalized.
- TAKAware does **not** act on `enrollForCertificateWithTrust` (public v1.4).

**Universal-package note.** A single zip *can* carry both indexed `cot_streams` keys (for ATAK) and non-indexed `app_preferences` keys (for iOS). But ATAK deletes each `.p12` after its first import. If the default keys and the indexed keys point at the same file, only the first import succeeds; ATAK then falls back to the global default truststore, which still works. **Generating per-platform packages, as OTS does, is cleaner.**

---

## 4. PKCS#12 compatibility

### 4.1 What each client uses to read `.p12` files

| Client / path | Parser | Source |
|---|---|---|
| ATAK Java side: truststore and cert DB enumeration, v1 enrollment response | `KeyStore.getInstance("PKCS12")`, which is Android's BouncyCastle `PKCS12KeyStoreSpi` | `AtakCertificateDatabase.loadCertificate`, `CertificateSigningOperation` **[SRC]** |
| ATAK streaming TLS (commo) | OpenSSL **1.1.1g** `PKCS12_parse` | The `takthirdparty/distfiles/openssl.tar.gz` LFS sha256 `ddb04774…6d46` equals the official openssl-1.1.1g tarball hash **[SRC]** |
| TAKAware identity | Apple `SecPKCS12Import` | `SwiftTAK/Certificates/PKCS12.swift` **[SRC]** |
| TAKAware truststore | swift-nio-ssl `NIOSSLPKCS12Bundle` (BoringSSL) | `TAKDataPackageParser.storeServerCertificate` **[SRC]** |
| iTAK, WinTAK | UNVERIFIED (presumably `SecPKCS12Import`, .NET/CryptoAPI) | — |

**Algorithm findings**

- **TAK Server ships legacy p12s.** `makeRootCa.sh` and `makeCert.sh` detect OpenSSL 3 (`openssl list -providers`) and then add **`-legacy`**. The `-fips` mode additionally produces `truststore-root-legacy.p12`. **[SRC]**
  - A TAK-Server-generated package (TAKAware fixture `itak-do-foyc.zip`) inspected with `openssl pkcs12 -info`: `MAC: sha1, Iteration 2048`, certificates `pbeWithSHA1And40BitRC2-CBC, Iteration 2048`, key `pbeWithSHA1And3-KeyTripleDES-CBC, Iteration 2048`. Truststore certificate bags carry a `friendlyName` and **no** Java trust attribute.
- **OTS** also uses `openssl pkcs12 -legacy -export …` whenever OpenSSL 3 is present. **[SRC]** `certificate_authority.py`
- **iOS and macOS with OpenSSL 3 defaults** (PBES2/AES-256-CBC + HMAC-SHA256 MAC):
  - `SecPKCS12Import` fails on iOS 16 with -25293 (Apple dev forums thread 721566).
  - Apple DTS: *"In macOS 15 we updated the system to work with modern PKCS#12 crypto algorithms. macOS 14 doesn't have that fix, so it can only import PKCS#12 items with legacy crypto … pass in the `-legacy`"* (thread 810723).
  - The matching iOS version (presumably iOS 18) is UNVERIFIED. **Conclusion: legacy is required for iOS ≤ 17.**
- **Android BouncyCastle with PBES2/AES:** UNVERIFIED for ATAK. Legacy is proven by the TAK ecosystem.
- **Default password `atakatak`:**
  - ATAK `defaultTrustStorePassword`, used when a pref omits `caPassword`/`clientPassword`, and mandatory for the v1 response.
  - TAK Server `CertManagerApi.DEFAULT_PASSWORD`; `cert-metadata.sh` `CAPASS=${CAPASS:-atakatak}`.
  - OTS `OTS_CA_PASSWORD` default.
  - goatak `p12Password`.
- **BoringSSL (TAKAware truststore)** **requires a MAC** (`PKCS8_R_MISSING_MAC`), so a password-less p12 is rejected. It ignores unknown bag attributes.
  - **Risk:** `NIOSSLPKCS12Bundle` 2.27.0 calls `fatalError("Failed to obtain cert and pkey…")` when the p12 has no private key. That would make a certificate-only truststore crash TAKAware's *package import*. TAKAware's own unit test uses a certificate-only truststore through this path, so real behaviour is **UNVERIFIED**. Test it.
  - TAKAware's *enrollment* path never parses a truststore p12, so it is unaffected.

### 4.2 Truststore: plain certificate bag or Java "trusted key usage" attribute?

- **ATAK (Android BouncyCastle) needs a `friendlyName` on each certificate bag.** In `PKCS12KeyStoreSpi.engineLoad`, a certificate bag goes into `certs` (and therefore `engineAliases()`) only `if (alias != null)`. The alias comes from the `pkcs_9_at_friendlyName` attribute. Unnamed bags go only into `chainCerts`.
  - ATAK's `loadCertificate()` enumerates aliases. A truststore without friendlyNames therefore loads as **zero certificates**, and trust fails.
  - **[SRC]** bcgit/bc-java `PKCS12KeyStoreSpi.java`. Android ships a BC fork; we assume it has the same logic.
- The **Java trust attribute** `2.16.840.1.113894.746875.1.1` is only needed by the JDK's own PKCS12 keystore. BC and BoringSSL ignore it. It is **harmless and not required**:
  - TAK Server and OTS truststores do not have it.
  - goatak's v1 response has it and works with ATAK.
- go-pkcs12 `EncodeTrustStore` / `EncodeTrustStoreEntries` add **both** the friendlyName (BMPString; `EncodeTrustStore` uses `cert.Subject.String()`) and the Java attribute. That is fine everywhere.
- Do **not** create truststores with go-pkcs12 `Encode` or `Modern` without names, and never with `Passwordless`.

### 4.3 go-pkcs12 v0.7.4 encoder choice

**[SRC]** `pkcs12.go`, v0.7.4

| Encoder | Cert bags | Key bag | MAC | Use |
|---|---|---|---|---|
| `LegacyRC2` | PBE-SHA1-RC2-40 | PBE-SHA1-3DES | HMAC-SHA1, **1 iteration** | Matches TAK Server/OTS output (use `.WithIterations(2048)` for an exact match). OpenSSL 3 without the legacy provider cannot read it. |
| `LegacyDES` (= `Legacy`) | PBE-SHA1-3DES | PBE-SHA1-3DES | HMAC-SHA1, 1 iteration | go-pkcs12's "most compatible" choice; equivalent to `openssl -descert`. |
| `Modern2023` (= `Modern`) | PBES2 AES-256-CBC, PBKDF2-SHA256 | same | HMAC-SHA256 | **Not for iOS ≤ 17 or macOS ≤ 14** |
| `Modern2026` | PBES2 | PBES2 | PBMAC1 | OpenSSL ≥ 3.4 only. **No.** |
| `Passwordless` | none | none | none | **Never** (BoringSSL rejects it). |

`WithIterations(n)` sets both the MAC and the encryption iteration counts.

**Recommendation.** Proven: `LegacyRC2.WithIterations(2048)`. Most universally parseable: `LegacyDES.WithIterations(2048)`. Do not ship 1-iteration MACs: whether iOS accepts them is UNVERIFIED, and matching OpenSSL's 2048 costs nothing.

```go
enc := pkcs12.LegacyDES.WithIterations(2048) // or LegacyRC2.WithIterations(2048)

// Client identity .p12 (packages for ATAK / iTAK / WinTAK)
clientP12, _ := enc.Encode(clientKey, clientCert, []*x509.Certificate{caCert}, "atakatak")

// CA truststore .p12 (friendlyName REQUIRED for Android BC)
trustP12, _ := enc.EncodeTrustStoreEntries([]pkcs12.TrustStoreEntry{
    {Cert: caCert, FriendlyName: caCert.Subject.CommonName},
}, "atakatak")

// v1 /tls/signClient response
v1, _ := pkcs12.LegacyRC2.WithIterations(2048).EncodeTrustStoreEntries([]pkcs12.TrustStoreEntry{
    {Cert: signed, FriendlyName: "signedCert"},
    {Cert: caCert, FriendlyName: "ca0"},
}, "atakatak")
```

`Encode` writes the client key and leaf certificate with `localKeyId` only and no friendlyName. That is fine:
- BC aliases the key entry by hex(localKeyId).
- `SecPKCS12Import` and OpenSSL match key and certificate by localKeyId or public key.
- ATAK 4.6's own enrollment builds client keystores with friendly name `"TAK Client Cert"`. That is not required.

---

## 5. Certificate requirements

### 5.1 CA

- A **single self-signed root is fine.**
  - OTS uses only a root: `-x509 -extensions v3_ca`, `basicConstraints=critical,CA:TRUE`, `keyUsage=critical,cRLSign,keyCertSign`, RSA-2048, SHA-256. It supports ATAK, iTAK and WinTAK.
  - TAK Server's root + intermediate design exists because its signer keystore must be separate; it is not a client requirement. **[SRC]** OTS `ca_config.py`, TAK `scripts/certs/config.cfg`
- If you use an intermediate: send `ca0 = intermediate`, `ca1 = root`. The 8089/8443 TLS chain must include the intermediate. Put the root (or both) in truststores.
- CA validity can be long. The TAK Server root is valid 3652 days and the OTS root 3650 days. Apple's limits apply to *leaf TLS server* certificates.

### 5.2 Server certificate (8089, 8443, and 8446 when private)

| Item | Requirement | Evidence |
|---|---|---|
| Key / signature | RSA-2048, SHA-256 | Apple: RSA ≥ 2048 and SHA-2 for *all* TLS server certificates **[SRC]** (support.apple.com/103769). TAK `Constants.KEY_TYPE="RSA"`, `CERTBITS=2048`. |
| EKU | `serverAuth` (+ `clientAuth`, as TAK and OTS do for federation) | Apple: EKU must contain id-kp-serverAuth (for certificates issued after 2019-07-01). TAK/OTS `[server] extendedKeyUsage = critical, clientAuth, serverAuth`. |
| KeyUsage | `critical, digitalSignature, keyEncipherment` | TAK/OTS `config.cfg` (TAK Server's Java signer also adds keyAgreement and nonRepudiation) |
| SAN | **DNS name(s) AND IP address(es)** that clients will use. CN = primary hostname. | Apple: *"DNS names in the CommonName of a certificate are no longer trusted"*; the name must be in the SAN. ATAK checks hostnames on 8446 only when no truststore is present. ATAK commo on 8089 **intentionally does not check the hostname** (*"we intentionally do not check authenticity of peer certificate by CN v. hostname comparison"*, `streamingsocketmanagement.cpp`). TAKAware disables hostname checks with custom anchors. iTAK: UNVERIFIED, so include both SAN types. |
| Validity | **≤ 397 days** (≤ 825 is the hard Apple rule for all TLS server certificates) | Apple 103769: *"TLS server certificates must have a validity period of 825 days or fewer"*, applies to all certificates issued after 2019-07-01. Apple 102028: the 398-day rule applies only to certificates from Apple's preinstalled roots, *"will not affect certificates issued from user-added or administrator-added Root CAs"*. TAKAware works around Apple's "not standards compliant" error with `SecTrustSetExceptions`. OTS ships 3650-day server certificates and reportedly works with iTAK, but that is UNVERIFIED. **Rotate automatically.** |

### 5.3 Client certificate (enrollment or pre-issued)

- Subject: the CSR subject, with **CN = username**. Use `RawSubject = csr.RawSubject`.
- KeyUsage: `digitalSignature` (+ `keyEncipherment` for RSA, or TAK's `keyAgreement,nonRepudiation`).
- EKU: `clientAuth`. Optionally add `1.2.840.113549.1.9.7` when the `version` query parameter is present (TAK Server behaviour).
- No SAN is needed. goatak puts `clientUid` in the SAN as an email address; that is optional.
- Validity: 365 days (TAK default `validityDays`). Back-date `notBefore` by 12 h.
- Serial: random, at least 64 bits.

### 5.4 RSA vs ECDSA

- Every reference generates RSA: ATAK RSA-4096 (default), TAKAware RSA-2048, TAK/OTS/goatak RSA-2048.
- **ATAK commo sets `SSL_CTX_set_cipher_list(sslCtx, "DEFAULT:!ECDH")`.** In TLS 1.2 that removes every (EC)DHE-ECDSA and ECDHE-RSA suite.
  - An **ECDSA server certificate cannot work over TLS 1.2** with ATAK. Over TLS 1.2, ATAK can only use RSA-kex (or DHE) suites.
  - TLS 1.3, which commo's OpenSSL 1.1.1g supports, is not affected by that list.
- **Server and CA keys: RSA-2048. Accept any RSA client key size (2048 or 4096).** ECDSA client keys: UNVERIFIED; don't offer them.

### 5.5 Go TLS listener settings

**8089** (streaming) and **8443** (Marti mTLS):
- `ClientAuth: tls.RequireAndVerifyClientCert` with `ClientCAs` = CA pool.
- `MinVersion: tls.VersionTLS12`; keep TLS 1.3 enabled (the default).
- Go 1.22+ dropped non-ECDHE suites from the default pre-1.3 list (*"cipher suites without ECDHE support are no longer offered … reverted with the tlsrsakex=1 GODEBUG setting"*). Combined with ATAK's `!ECDH`, that makes **TLS 1.2 impossible** with older or alternative commo builds.
  - Either keep TLS 1.3 available, which works for ATAK 4.6+, or explicitly add `TLS_RSA_WITH_AES_128_GCM_SHA256` / `TLS_RSA_WITH_AES_256_GCM_SHA384` to `CipherSuites` (or set `GODEBUG=tlsrsakex=1`) for legacy clients. Which WinTAK/iTAK builds need TLS 1.2 RSA-kex is UNVERIFIED.

**8446** (enrollment):
- `ClientAuth: tls.NoClientCert`.
- TAKAware *rejects* client-certificate challenges here (`.rejectProtectionSpace`). ATAK's enrollment socket factory may hold key managers, but enrollment does not depend on them.
- Use the Let's Encrypt certificate here if you use the Quick Connect model.

**Identity:**
- Username = client certificate Subject CN. TAK Server: `DNUsernameExtractorRegex` default `CN=(.*?)(?:,|$)`. OTS also uses the CN.
- Store `clientUid` → (username, certificate serial) at signing time, as TAK Server, OTS and goatak all do.
- **Split the Basic-auth header on the first `:` only**, because passwords and tokens may contain `:`.

---

## 6. Gotcha checklist

1. v2 certificate strings: no PEM armour, **exact 64-char lines** (TAKAware / swift-asn1), and **string-only JSON values** (TAKAware `[String:String]` cast).
2. `/tls/config` must be `text/plain` or `application/xml` (TAKAware), contain no extra XML elements (SimpleXML strict), use unique OpenSSL-known RDN names, and send only O/OU.
3. Accept CSR bodies as PEM-ish text with leading and trailing blank lines (ATAK) **and** as one-line base64 (TAKAware). Accept SHA-1-signed CSRs (ATAK 4.6).
4. Don't enforce TAK's exact RDN count. Do enforce CN == username, preferably case-insensitively like TAK Server.
5. v1 response: friendlyNames `signedCert`/`ca0`… (case-sensitive), password `atakatak`.
6. `caN` = the CA that validates **8089/8443**, never the public CA used on 8446.
7. `/Marti/api/tls/profile/enrollment` (8446, Basic) → 204 or a mission-package zip. Don't 404 it.
8. `.pref`: `<entry key="…"` with key as the first attribute; `enabledN=true` is required; `cacheCredsN` is a localized display string; there are no `usernameN`/`passwordN`.
9. Certificate paths in prefs are `cert/<basename>`. Use unique `.p12` basenames per server. ATAK deletes the `.p12` after import.
10. Truststore certificate bags need a friendlyName (Android BC). Legacy PBE plus SHA-1 MAC with 2048 iterations; never password-less; same password for truststore and client p12.
11. ATAK QR `host` sets the **streaming** port only. The enrollment port is always the device pref (default 8446). The token travels as the Basic password.
12. Private CA + QR/Quick Connect on ATAK or iTAK fails with no prompt. Use the bootstrap package, full packages, or LE-on-8446.
13. Keep TLS 1.3 on 8089. Don't use ECDSA server certificates (`DEFAULT:!ECDH` in commo).
14. Handle the `<auth><cot username= password= uid=/></auth>` first message on 8089.

## 7. Still UNVERIFIED (test on devices)

- The exact `Accept` header and endpoint (v2 XML assumed) used by ATAK 4.7–5.x, and whether newer ATAK still calls the v1 endpoint.
- iTAK: its enrollment `Accept`/`Content-Type` expectations; whether it prompts for credentials after the CSV QR; whether it can enroll after importing a truststore; its manifest and file-name requirements; its PKCS#12 algorithm support; whether it enforces Apple's 825-day rule or hostname validation.
- WinTAK: whether the nested-zip requirement is real; whether it supports enrollment; its TLS cipher requirements.
- Whether `NIOSSLPKCS12Bundle` crashes on certificate-only truststores in TAKAware's package import.
- Android BouncyCastle support for PBES2/AES p12 files in ATAK (avoided by using legacy).
- iOS release that added "modern" PKCS#12 support (macOS 15 per Apple DTS; iOS 18 presumed).
- ATAK `tak://com.atakmap.app/import?url=` and `/preference?...` deep links (secondary source only), and the ATAK version that introduced `tak://…/enroll`.
- Origin of TAKAware's JSON QR format.

## Sources

- ATAK-CIV 4.6.0.5 (github.com/deptofdefense/AndroidTacticalAssaultKit-CIV, branch `main`):
  - `atak/ATAK/app/src/main/java/com/atakmap/net/{CertificateEnrollmentClient,CertificateConfigOperation,CertificateConfigRequest,CertificateSigningOperation,DeviceProfileClient,DeviceProfileOperation}.java`
  - `.../net/certconfig/{CertificateConfig,NameEntry}.java`
  - `.../comms/{SslNetCotPort,CotService,TAKServer,CommsMapComponent}.java`, `.../comms/http/TakHttpClient.java`
  - `.../android/importfiles/sort/{ImportCertSort,ImportPrefSort}.java`, `.../app/preferences/PreferenceControl.java`
  - `.../android/missionpackage/file/{MissionPackageManifest,MissionPackageConfiguration,MissionPackageContent,MissionPackageExtractor,MissionPackageExtractorFactory,PlainZipExtractor,MissionPackageBuilder}.java`
  - `atak/ATAK/app/src/main/res/xml/network_connections_preferences.xml`, `res/values/strings.xml`
  - `takkernel/engine/src/main/java/com/atakmap/net/CertificateManager.java`, `takkernel/engine/src/android/java/com/atakmap/net/AtakCertificateDatabase.java`, `takkernel/engine/src/main/res/values/strings.xml`
  - `commoncommo/core/impl/{cryptoutil.cpp,streamingsocketmanagement.cpp}`, `takthirdparty/distfiles/openssl.tar.gz` (LFS pointer)
- TAK Server (github.com/TAK-Product-Center/Server, `main`):
  - `src/takserver-core/takserver-war/src/main/java/com/bbn/tak/tls/{CertManagerApi,CertManager,Util,Constants}.java`, `.../tls/Service/CertManagerService.java`
  - `.../com/bbn/marti/device/profile/{api/ProfileAPI,service/ProfileService}.java`, `.../util/missionpackage/MissionPackage.java`
  - `src/takserver-core/example/CoreConfig.example.xml`, `src/takserver-common/src/main/xsd/CoreConfig.xsd`
  - `src/takserver-core/scripts/certs/{makeRootCa.sh,makeCert.sh,cert-metadata.sh,config.cfg,ReadMe.txt}`
  - Official docs bundled at `src/takserver-core/src/main/webapp/Marti/documentation/takserver/{appendixb,appendixc,appendixe,deviceprofiles}/index.html`
- OpenTAKServer (github.com/brian7704/OpenTAKServer, `master`):
  - `opentakserver/blueprints/marti_api/{certificate_enrollment_api,device_profile_marti_api}.py`
  - `opentakserver/{certificate_authority,ca_config,defaultconfig}.py`
  - `opentakserver/blueprints/ots_api/{token_api,api}.py`
  - PR #348
  - docs.opentakserver.io/certificate_enrollment.html
- TAKAware (github.com/flighttactics/TAKAware, `main` 2025-06-08):
  - `TAKAware/Communications/{CSRRequestor,TCPMessage,APIRequestObject}.swift`
  - `TAKAware/Parsers/{QRCodeParser,TAKCAConfigResponseParser,TAKDataPackageParser}.swift`
  - `TAKAware/Screens/SettingsScreens/ConnectionOptions.swift`, `TAKAware-Info.plist`
  - `TAKAwareTests/Test Files/{itak-do-foyc.zip,tak-server.pref,manifest.xml}`, `TAKAwareTests/{DataPackageParserTests,TestConstants}.swift`
- SwiftTAK (github.com/flighttactics/SwiftTAK, `main`): `Sources/SwiftTAK/{Utilities/TAKConstants,Certificates/PKCS12,Parsers/DataPackagerParser,Parsers/PreferencesParser,Parsers/ManifestParser,Models/DataPackageModels}.swift`
- goatak (github.com/kdudkov/goatak, `master`): `cmd/goatak_server/cert_api.go`, `pkg/tlsutil/util.go`, `cert/{make_connect_dp.sh,make_user_cert_dp.sh}`, `img/{qr-enroll.png,qr.png}`, `README.md`
- FreeTAKServer (github.com/FreeTAKTeam/FreeTakServer, `master`): `FreeTAKServer/core/util/certificate_generation.py`
- go-pkcs12 v0.7.4: github.com/SSLMate/go-pkcs12 `pkcs12.go`
- BouncyCastle: github.com/bcgit/bc-java `prov/src/main/java/org/bouncycastle/jcajce/provider/keystore/pkcs12/PKCS12KeyStoreSpi.java`
- swift-nio-ssl 2.27.0 `Sources/NIOSSL/SSLPKCS12Bundle.swift`, `Sources/CNIOBoringSSL/crypto/pkcs8/pkcs8_x509.c`; swift-asn1 0.8.0 `Sources/SwiftASN1/Basic ASN1 Types/PEMDocument.swift`
- Go: `src/crypto/x509/x509.go` (CSR SHA-1), go.dev/doc/go1.22 (crypto/tls RSA-kex default change)
- Apple: support.apple.com/103769 (iOS 13 TLS certificate requirements), support.apple.com/102028 (398-day rule scope), developer.apple.com/forums/thread/810723 and /thread/721566 (PKCS#12 modern-algorithm failures)
- Secondary: github.com/joshuafuller/qrtak `docs/qr-formats.md`, `docs/data-packages.md`, issue #240; github.com/ValleyTacticalSolutions/TAK-Portal issues #55, #62; github.com/snstac/pytak `src/pytak/client_functions.py`
