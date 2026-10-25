# ChameleonTAK wire-format spec: CoT streaming, Marti API, data packages, missions (Data Sync)

Compiled 2026-10-25 by reading the source code of the projects below. Everything here is either
traced to a specific file, or marked **UNVERIFIED**.

---

## 0. Sources and evidence legend

| Tag | Source | Revision read |
|---|---|---|
| **[TAKS]** | TAK Server (official, open source) - https://github.com/TAK-Product-Center/Server | `main` @ `5187abd` (2026-04-03, 5.x line; branches up to `upstream/5.7-RELEASE-14`) |
| **[ATAK]** | ATAK-CIV + commoncommo (the C++ comms lib ATAK uses) - https://github.com/deptofdefense/AndroidTacticalAssaultKit-CIV | `main` @ `889eee2` (archived 2024-10-18, ATAK **4.6.0.5**) |
| **[OTS]** | OpenTAKServer - https://github.com/brian7704/OpenTAKServer | `master` @ `e71c74d` (2026-10-24) |
| **[TAKAware]** | TAKAware - https://github.com/flighttactics/TAKAware and SwiftTAK - https://github.com/flighttactics/SwiftTAK | `main` |
| **[FTS]** | FreeTAKServer - https://github.com/FreeTAKTeam/FreeTakServer | `master` (used only for cross-checks) |

Short path prefixes used below:

* `TAKS:core/...` = `src/takserver-core/src/main/java/...`
* `TAKS:war/...` = `src/takserver-core/takserver-war/src/main/java/...`
* `TAKS:plugins/...` = `src/takserver-plugins/src/main/java/...`
* `ATAK:app/...` = `atak/ATAK/app/src/main/java/...`
* `ATAK:commo/...` = `commoncommo/core/impl/...`
* `OTS:...` = `opentakserver/...`

**Closed-source clients.** iTAK and WinTAK are closed source, and so is ATAK's Data Sync plugin; it is
not in ATAK-CIV. Statements about them come either from OTS code comments, which record the OTS
author's observations and are a secondary source, or are marked **UNVERIFIED**. ATAK-CIV is 4.6. Current
ATAK 5.x may differ in details (**UNVERIFIED**).

---

## 1. CoT streaming (TLS 8089 / TCP)

### 1.1 Ports and transport types

TAK Server input `protocol=` values (`TAKS:core/com/bbn/marti/service/TransportCotEvent.java`):

| protocol | Meaning | Notes |
|---|---|---|
| `tls` (COTPROTOTLS) | TLS streaming. Starts as XML and **offers** TAK-proto negotiation | default `stdssl` port **8089** in `CoreConfig.example.xml` |
| `cottls` | TLS streaming, XML only, no negotiation | |
| `prototls` | TLS, protobuf from the first byte, no negotiation (`NioNettyTlsServerHandler`: `protobufSupported=true`) | |
| `stcp` | **Bidirectional** plaintext streaming | example port 8088 (commented out in the example config) |
| `tcp` | **Receive-only** plaintext input. `NioNettyTcpServerHandler` overrides `setWriter()` to a no-op and creates no subscription | example port 8087 (commented out) |
| `udp`, `mcast`, `cotmcast`, `quic` (8090) | not covered here | |

* **Gotcha:** in TAK Server, `tcp`/8087 is *input-only*. A client streaming over plaintext TCP needs
  `stcp` semantics. OTS uses **8088** for bidirectional TCP streaming
  (`OTS:defaultconfig.py OTS_TCP_STREAMING_PORT=8088`) and 8089 for SSL. For ChameleonTAK, make the
  plaintext streaming port bidirectional, whatever port number it uses.
* **HTTP API port that clients derive from the stream type** [ATAK]
  (`ATAK:app/com/atakmap/comms/SslNetCotPort.java`, `comms/http/TakHttpClient.java`):
  * SSL stream: `https://<host>:8443/Marti/...`, authenticated by the client certificate.
  * TCP stream: `http://<host>:8080/Marti/...`, with HTTP Basic auth (`useBasicAuth()` when the URL is not https).
  * Certificate enrollment: `https://<host>:8446/Marti/...` with Basic auth.
  * The commo mission-package uploader uses the same defaults (`ATAK:commo/missionpackagemanager.cpp`:
    `DEFAULT_HTTP_PORT=8080`, `DEFAULT_HTTPS_PORT=8443`).

### 1.2 XML framing

**What the server receives** [TAKS] `TAKS:core/com/bbn/marti/nio/protocol/connections/StreamingCotProtocol.java`:

* The stream is CoT `<event>` documents laid end to end, with no length prefix.
* Parser algorithm: append bytes to a buffer. While the buffer contains `</event>`, take the substring
  from the **first `<event`** through the end of `</event>`, parse it, then delete everything up to and
  including `</event>`.
* Anything before `<event` is skipped. The source comment says this covers `<auth>` messages sent to
  anonymous ports. That also covers XML declarations and whitespace.
* Limit: 8,388,608 chars per message (`INDIVIDUAL_COT_MSG_SIZE_LIMIT`). An over-long buffer is discarded.
* Charset: UTF-8.

**Optional auth message** [ATAK] `ATAK:commo/streamingsocketmanagement.cpp` (around line 1750):

* On SSL connections configured with username/password, commo queues this as the **first** message:

```xml
<?xml version="1.0"?>
<auth><cot username="USER" password="PASS" uid="ANDROID-0123456789abcdef"/></auth>
```

* The header line is libxml2's `xmlDocDumpFormatMemory` output. The trailing newline is stripped.
* OTS splits the stream on `</event>|</auth>` (`OTS:eud_handler/EudHandler.py handle()`).
* TAK handles `<auth>` in its auth codecs (`FileAuthCodec`, `LdapAuthCodec`, ...).

**What clients expect**:

* [ATAK] `ATAK:commo/protobuf/protocol.txt`, section "Traditional Protocol":
  * Each message is prefaced by an XML header (`<?xml ... ?>`), then a newline, then the `<event>`.
  * No newline may follow `</event>`. The next byte starts the next `<?xml`.
  * commo splits incoming data on the literal token `</event>` (`MESSAGE_END_TOKEN` in
    `streamingsocketmanagement.cpp`) and parses each chunk with libxml2.
* [TAKS] TAK Server writes each event with `doc.asXML()` (dom4j; `TAKS:plugins/tak/server/cot/XmlContainer.java`,
  `CotEventContainer.getOrInstantiateEncoding()`), with nothing after `</event>`.
  * In practice that is `<?xml version="1.0" encoding="UTF-8"?>` + newline + `<event ...>...</event>`.
  * The exact dom4j declaration bytes are **UNVERIFIED**, but they match protocol.txt.
* [TAKAware] `TAKAware/Parsers/StreamParser.swift` splits on `"</event>"`. SwiftTAK writes
  `<?xml version="1.0" standalone="yes"?>`.

**ChameleonTAK recommendation (server to client):**

* Send `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n<event ...>...</event>` and put
  **nothing** (no `\n`) after `</event>`. Omitting the declaration entirely is also safe.
* **Never** put whitespace between `</event>` and the next `<?xml`.
  * Reason: a chunk that starts with `\n<?xml` is not well-formed XML ("XML declaration allowed only at the start").
  * This failure mode is inferred from protocol.txt's rule; the actual libxml2 rejection is **UNVERIFIED** by test.

**Time formats**

* TAK's server-generated control messages use `yyyy-MM-dd'T'HH:mm:ss'Z'` (`DateUtil.toCotTime`,
  `TAKS:plugins/com/bbn/marti/remote/util/DateUtil.java`).
* TAK's parser accepts that form and full ISO forms with fractional seconds.

### 1.3 TAK Protocol (protobuf) negotiation: t-x-takp-v / -q / -r

Spec: `ATAK:commo/protobuf/protocol.txt`, section "Streaming Connection Protocol Negotiation".
Server implementation: `TAKS:core/com/bbn/marti/nio/protocol/connections/StreamingProtoBufOrCoTProtocol.java`
and `TAKS:core/com/bbn/marti/nio/netty/handlers/NioNettyTlsServerHandler.java` / `NioNettyHandlerBase.java`.

**Step 1. Server announces.**

* TAK Server sends the announcement only on `tls` inputs, only after authentication succeeds, and once per connection.
* It is sent from `SubmissionService.createSubscriptionFromConnection`, right **after** the latest-SA replay (section 1.5).
* Exact TAK string (single line):

```xml
<?xml version='1.0' encoding='UTF-8' standalone='yes'?><event version='2.0' uid='3f6c...-NEGOTIATION-UUID' type='t-x-takp-v' time='2026-10-25T12:00:00Z' start='2026-10-25T12:00:00Z' stale='2026-10-25T12:01:00Z' how='m-g'><point lat='0.0' lon='0.0' hae='0.0' ce='999999' le='999999'/><detail><TakControl><TakProtocolSupport version='1'/><TakServerVersionInfo serverVersion='5.2-RELEASE-16' apiVersion='3'/></TakControl></detail></event>
```

**Step 2. Client requests** (built by commo, `ATAK:commo/cotmessage.cpp`: TakControl request constructor, stale +60 s, how `m-g`).
The uid is copied from the announcement.

```xml
<event version="2.0" uid="3f6c...-NEGOTIATION-UUID" type="t-x-takp-q" time="..." start="..." stale="..." how="m-g"><point lat="0" lon="0" hae="0" ce="9999999" le="9999999"/><detail><TakControl><TakRequest version="1"/></TakControl></detail></event>
```

**Step 3. Server responds.**

* TAK checks that `TakRequest/@version` equals `"1"`, removes its negotiation listener, and replies with the same uid:

```xml
<?xml version='1.0' encoding='UTF-8' standalone='yes'?><event version='2.0' uid='3f6c...-NEGOTIATION-UUID' type='t-x-takp-r' time='...' start='...' stale='...+60s' how='m-g'><point lat='0.0' lon='0.0' hae='0.0' ce='999999' le='999999'/><detail><TakControl><TakResponse status='true'/></TakControl></detail></event>
```

* After `status='true'`, both directions switch to **TAK Protocol streaming framing**:
  `0xBF` + varint(payload length) + protobuf `TakMessage`.
  * Varint is the protobuf unsigned varint: 7 bits per byte, least-significant group first, MSB = continuation.
  * See the TAK reader at `NioNettyTlsServerHandler.convertAndSubmitProtoBufBytesAsCot/readSize`.
* Protobuf schemas: `ATAK:commo/protobuf/takmessage.proto`
  (`TakMessage { TakControl takControl = 1; CotEvent cotEvent = 2; }`), `cotevent.proto`, `detail.proto`, and the
  other files in that folder.
* The server must listen for `t-x-takp-q` for **at least 60 s** after the announcement.
  * TAK: `TIMEOUT_MILLIS = 60000`. TAK treats `t-x-takp-q` as a control message and never brokers it.

**Client behavior when the server never announces** (the key question):

* **ATAK / commo [ATAK]** `ATAK:commo/streamingsocketmanagement.cpp`:
  * Every streaming connection, TCP or SSL, starts in state `PROTO_XML_NEGOTIATE`, with `protoTimeout = now + 60 s`
    set when the connection comes up.
  * In `checkProtoTimeout()`: if no `t-x-takp-v` arrives within 60 s, the state becomes `PROTO_XML_ONLY`
    (logged as *"continuing as XML only"*) and stays that way for the connection.
  * A later announcement is ignored (`protoNegotiation()` is only called when state != `PROTO_XML_ONLY`).
  * commo only sends `t-x-takp-q` after seeing `t-x-takp-v` advertising version 1.
  * After sending the request, commo stops sending other traffic, holds its queue, and waits up to 60 s.
    No response: it disconnects and reconnects. `status="false"`: it stays on XML.
  * **So yes: ATAK stays on XML forever if the server never announces.**
* **WinTAK:** very likely identical, because WinTAK is built on commoncommo (ATAK-CIV ships `commoncommo/win32/...` build files).
  WinTAK itself is closed source: **UNVERIFIED**.
* **TAKAware [TAKAware]:** its streaming code contains no negotiation at all. There is no `t-x-takp-q` anywhere in
  `TCPMessage.swift`, `StreamParser.swift` or `TAKManager.swift`, so it always uses XML on the stream.
  (`StreamParser.parseProtobuf` exists for `0xbf` frames, but it is not used for negotiation.)
* **iTAK:** closed source, **UNVERIFIED**. Protocol.txt step 5 says clients *SHALL NOT* request without first seeing
  the announcement, so a server that never announces should stay on XML with any compliant client.

**Recommendation:** ship v1 XML-only and never send `t-x-takp-v`. If you add it later you MUST:

* implement protobuf in both directions, including control messages and pongs;
* accept `t-x-takp-q` for 60 s after the announcement;
* never send XML after replying `status='true'`.

### 1.4 Ping / pong (t-x-c-t / t-x-c-t-r)

**Client ping** [ATAK] (`ATAK:commo/cotmessage.cpp` ping constructor, `streamingsocketmanagement.cpp sendPing()`):

* `uid` = `<device uid>` + `"-ping"`.
* `type="t-x-c-t"`, `how="m-g"`, stale = now + 10 s.
* Point is `ZERO_POINT(0, 0, 0, NO_VALUE, NO_VALUE)`: lat/lon/hae are 0, and ce/le are written as `9999999`
  (`POINT_NOVALUE_STRING`).
* Empty `<detail/>`.

```xml
<?xml version="1.0"?>
<event version="2.0" uid="ANDROID-0123456789abcdef-ping" type="t-x-c-t" time="2026-10-25T12:00:00.000Z" start="2026-10-25T12:00:00.000Z" stale="2026-10-25T12:00:10.000Z" how="m-g"><point lat="0" lon="0" hae="0" ce="9999999" le="9999999"/><detail/></event>
```

(The exact number and time formatting is **UNVERIFIED**; the structure is verified.)

**When commo pings** (`streamingsocketmanagement.cpp`, constants near the top of the file):

* `RX_STALE_SECONDS = 15`: ping only after **15 s with no bytes received** from the server.
* Repeat every `RX_STALE_PING_SECONDS = 4.5`.
* `RX_TIMEOUT_SECONDS = 25`: if nothing is received for 25 s, commo **drops the connection and reconnects**.
* Implication: on a quiet server, the server's pong is what keeps ATAK and WinTAK connected.
  On a busy server, clients may never ping. Do not use pings as the server's only liveness signal.

**TAK Server pong** [TAKS] `TAKS:core/com/bbn/marti/service/SubmissionService.java sendPong()`, exact:

```xml
<event version='2.0' uid='takPong' type='t-x-c-t-r' how='h-g-i-g-o' time='2026-10-25T12:00:00Z' start='2026-10-25T12:00:00Z' stale='2026-10-25T12:00:20Z'> <point ce='9999999' le='9999999' hae='0' lat='0' lon='0' /></event>
```

* It has no `<detail>`. It is sent only to the pinging subscription, and the server records `lastPingTime`.
* Required fields: commo's `isPong()` checks **only** `type == "t-x-c-t-r"`.
  * ATAK `CotMapComponent.processCotEvent` treats both `t-x-c-t` and `t-x-c-t-r` as no-ops.
  * Any well-formed event resets commo's receive timer.
* **Do not forward pings to other clients.** TAK's `controlMsgTypes` list (`SubmissionService.java`) is never brokered:
  * `t-b`, `t-b-a`, `t-b-c`, `t-b-q`
  * `t-x-c-f` (filter), `t-x-c-t`, `t-x-c-t-r`
  * `t-x-takp-q`
  * `t-x-c-m` (metrics)
  * `t-x-c-i-e` / `t-x-c-i-d` (incognito on/off)
* OTS: `EudHandler.pong()` builds a pong but then sends `event.encode()`, which **echoes the ping back**. It is a bug,
  but it works because any received data resets the timer. Don't copy it.
* If protobuf was negotiated, send the pong as a protobuf `TakMessage`.

### 1.5 What the server does when a client connects

**TAK Server [TAKS]** (`SubmissionService.createSubscriptionFromConnection` and `MessagingUtilImpl.sendLatestReachableSA`):

1. After the TLS handshake and authentication, TAK creates the subscription and attaches the user.
2. If CoreConfig `<buffer><latestSA enable="true"/></buffer>` is set, TAK calls `sendLatestReachableSA(user)`.
   * The example config sets `enable="true"`; the XSD default is `false`.
   * It sends the latest data-feed events, then the **latest SA of every currently connected user reachable by group**
     (`CommonGroupDirectedReachability`), to the new client.
   * Whether stale SAs are filtered out is **UNVERIFIED**.
3. It then starts protocol negotiation (section 1.3, `tls` inputs only).

The same replay is triggered by `GET /Marti/api/groups/all?useCache=true&sendLatestSA=true` (section 2.4) and after active-group changes.

**How a client is identified:**

* TAK's `callsignExtractorCallback` and OTS's `parse_device_info` treat the first event carrying `<contact callsign=...>` as the
  client's own SA. Its `uid` becomes the subscription's clientUid, and its callsign is recorded.
* OTS requires `<takv>` or `<contact>` and ignores uids ending in `ping`.
* commo rewrites the contact endpoint of SAs sent over a stream to `*:-1:stcp`
  (`CoTMessage::STREAMING_ENDPOINT` in `ATAK:commo/cotmessage.cpp`).

**OTS [OTS]:** does **not** replay cached SAs. It relies on clients' periodic SA.

**Recommendation:** keep the last own-SA event per connected client and replay the visible ones to each new subscriber
(TAK behavior). Without this, a new client sees nobody until the next SA cycle.

Typical ATAK SA, for orientation only (the exact element set is **UNVERIFIED** here; the `endpoint` value is verified):

```xml
<event version="2.0" uid="ANDROID-0123456789abcdef" type="a-f-G-U-C" how="h-e" time="..." start="..." stale="...">
  <point lat="52.1" lon="21.0" hae="100.0" ce="9.9" le="9999999"/>
  <detail>
    <takv os="34" version="4.6.0.5" device="..." platform="ATAK-CIV"/>
    <contact endpoint="*:-1:stcp" callsign="ALPHA"/>
    <uid Droid="ALPHA"/>
    <__group name="Cyan" role="Team Member"/>
    <status battery="88"/>
    <track course="0" speed="0"/>
  </detail>
</event>
```

### 1.6 What the server does when a client disconnects

**TAK Server [TAKS]** (`SubmissionService.handleChannelDisconnect`, `MessagingUtilImpl.sendDisconnect`, and
`DistributedSubscriptionManager.makeDeleteMessage`):

* Only fires if the subscription had a callsign and a clientUid (it had sent an SA) and latestSA is enabled.
* Builds a delete event from the client's last SA and sends it to all **reachable** subscriptions.
* Exact template (`deleteMessageSeed` plus the added attributes):

```xml
<?xml version='1.0' encoding='UTF-8' standalone='yes'?><event how='h-g-i-g-o' type='t-x-d-d' version='2.0' uid='9c1e3b0a-RANDOM-UUID' start='2026-10-25T12:00:00Z' time='2026-10-25T12:00:00Z' stale='2026-10-25T12:00:20Z'><point ce='9999999' le='9999999' hae='0' lat='0' lon='0' /><detail><link relation='p-p' uid='ANDROID-0123456789abcdef' type='a-f-G-U-C'/></detail></event>
```

* `link/@uid` = the disconnected client's SA uid.
* `link/@type` = that SA's CoT type.

**OTS [OTS]** (`cot_parser.py send_disconnect_cot`) sends the same structure, with differences:

* stale +10 s;
* `link type` hard-coded to `a-f-G-U-C`;
* an extra `<_flow-tags_ TAK-Server-f1a8159ef7804f7a8a32d8efc4b773d0="..."/>`.

**How ATAK handles it [ATAK]** (`ATAK:app/com/atakmap/android/cotdelete/CotDeleteImporter.java`):

* `link/@uid`, `link/@relation` and `link/@type` must all be non-empty, otherwise the event is ignored.
* If a `<__forcedelete/>` child exists in `<detail>`, the map item is **removed**.
* Otherwise, if the preference `staleRemoteDisconnects` is true (default `true`), the marker is set `forceStale`:
  it shows as stale/offline.
* So send `t-x-d-d`, not a re-sent SA with an old stale time. Also update the client's `clientEndPoints`
  `lastStatus` to `Disconnected`.

TAK also adds a flow tag to forwarded events: `FlowTagFilter` adds `<_flow-tags_ TAK-Server-<serverId>="time"/>` under
`<detail>` for loop prevention (`TAKS:core/com/bbn/cot/filter/FlowTagFilter.java`). Clients don't need it. Optional.

### 1.7 Routing: broadcast, `<marti><dest .../>`, groups

**Server-side groups decide visibility. `__group` does not.**

* `<__group name="Cyan" role="Team Member"/>` is only team color and role. TAK never routes on it.
* OTS stores it as the EUD's team (`EudHandler.parse_device_info`). It does not route on it either.
* TAK model (`TAKS:plugins .../remote/groups/Group.java`, `CommonGroupDirectedReachability`):
  * Every user has group memberships with direction **IN** (write: groups their messages are tagged with) and
    **OUT** (read: groups they receive from).
  * A message reaches a receiver if the sender's IN groups intersect the receiver's OUT groups.
  * OTS mirrors this (`OTS:models/Group.py`: `IN = "IN" # Write`, `OUT = "OUT" # Read`), using RabbitMQ routing
    keys `<group>.OUT`.
  * A user with no groups, and every plain-TCP client, falls back to group **`__ANON__`** (OTS `parse_device_info`, `route_cot`).
  * TAK assigns input groups through `<filtergroup>`; anonymous users get `__ANON__`. The exact TAK default is
    **UNVERIFIED** beyond comments in `submitBroadcastMissionAnnouncementCot`.

**Explicit destinations [TAKS]** (`TAKS:core/com/bbn/cot/filter/StreamingEndpointRewriteFilter.java`):

* XPath: `/event/detail/marti/dest[@callsign or @publish or @uid or @mission or @path or @after or @mission-guid or @group]`.
* `callsign` values become the explicit callsign list. If the list contains **`All Streaming`**, the message is treated as a broadcast.
* `uid` values become the explicit uid list.
* `mission` (optional `path`/`after`) and `mission-guid`: mission content add plus delivery to the mission's subscribers (section 4.8).
* `group="NAME"` restricts the message to that group. It must be one of the sender's groups, otherwise TAK raises `ForbiddenException`.
* **TAK removes the whole `<marti>` element** before delivering. OTS forwards it unchanged.
* If any explicit destination exists (`doExplicitBrokering`), the message goes **only** to the explicit recipients.
  * Each recipient must still be group-reachable: the message's groups versus the receiver, falling back to sender user versus receiver.
  * If a `b-t-f` chat has no reachable recipient, TAK sends the sender a copy with type `b-t-f-s`
    (`MessagingUtilImpl.sendDeliveryFailure`; the meaning "stored for later delivery" is **UNVERIFIED**).

**Who sends which form:**

| Client | Directed CoT | Source |
|---|---|---|
| ATAK / WinTAK (commo) | `<marti><dest callsign="BRAVO"/><dest callsign="CHARLIE"/></marti>`. commo groups all recipients on the same stream into one message (`ContactManager` + `CoTMessage::setTAKServerRecipients`) | [ATAK] `ATAK:commo/contactmanager.cpp`, `cotmessage.cpp`. WinTAK per OTS comment: "ATAK and WinTAK use callsign" |
| ATAK, mission | `<marti><dest mission="NAME"/></marti>` (`CoTMessage::setTAKServerMissionRecipient`) | [ATAK] |
| iTAK | `<dest uid="..."/>`. When sending to a mission it puts **its own uid** in `dest uid` and calls PUT `/contents` instead (section 4.8) | [OTS] comments in `cot_parser.py route_cot`, `mission_marti_api.py` (secondary) |
| TAKAware | chat: `<marti><dest uid="..."/></marti>`. Marker send: `COTMarti(uid: contact.callsign)` produces `<dest uid="<value of contact.callsign>"/>` (possible TAKAware quirk; **UNVERIFIED** what that value holds) | [TAKAware] `TAKAware/TAK/TAKManager.swift`, `SwiftTAK COTMarti.swift` |

**ChameleonTAK rule:**

* Resolve `dest@callsign` through the live session table (callsign to clientUid), and `dest@uid` directly. Accept both on any message.
* If `dest@uid` matches no uid, optionally try it as a callsign (TAKAware tolerance).
* Strip `<marti>` before forwarding.
* Never also broadcast a message that had destinations.
* OTS additionally skips a `dest@uid` equal to the sender's own uid (iTAK mission quirk).

### 1.8 GeoChat (b-t-f)

**ATAK construction [ATAK]** (`ATAK:app/com/atakmap/android/chat/GeoChatService.java` `bundleToCot` / `addSendDetails`):

* `uid` = `GeoChat.<senderUid>.<conversationId>.<messageId>`, `type="b-t-f"`, `how="h-g-i-g-o"`, stale = +1 day.
* The point is the sender's position, or 0,0 if hidden.
* `__chat` attributes: `id`, `messageId`, `senderCallsign`, `chatroom`, `parent`, `groupOwner`, and optionally `deleteChild` and `tadilj`.
* `__chat` contains `chatgrp` with `id`, `uid0` = self, and `uid1..n` = destinations. An optional `hierarchy` child carries group-chat trees.
* `link uid=self type=selfType relation="p-p"`.
* `__serverdestination destinations="<myIP>:4242:tcp:<myUid>"`.
* `remarks source="BAO.F.ATAK.<uid>" [to=...] time=...` holds the message text.
* Default room id and name: **`All Chat Rooms`** (`GeoChatService.DEFAULT_CHATROOM_NAME`).
* Root contact group uid: **`RootContactGroup`** (`contact/Contacts.java`).

All Chat Rooms (broadcast; the "fakeGroup" contact goes through `dispatchToBroadcast`, so there is **no `<marti>`**):

```xml
<event version="2.0" uid="GeoChat.ANDROID-0123456789abcdef.All Chat Rooms.6c8a1f2e-0d1b-4c63-9b0f-0e3f1d2a4b5c" type="b-t-f" time="2026-10-25T12:00:00.000Z" start="2026-10-25T12:00:00.000Z" stale="2026-10-26T12:00:00.000Z" how="h-g-i-g-o">
  <point lat="52.1" lon="21.0" hae="100.0" ce="9999999" le="9999999"/>
  <detail>
    <__chat parent="RootContactGroup" groupOwner="false" messageId="6c8a1f2e-0d1b-4c63-9b0f-0e3f1d2a4b5c" chatroom="All Chat Rooms" id="All Chat Rooms" senderCallsign="ALPHA">
      <chatgrp uid0="ANDROID-0123456789abcdef" uid1="All Chat Rooms" id="All Chat Rooms"/>
    </__chat>
    <link uid="ANDROID-0123456789abcdef" type="a-f-G-U-C" relation="p-p"/>
    <__serverdestination destinations="192.168.1.20:4242:tcp:ANDROID-0123456789abcdef"/>
    <remarks source="BAO.F.ATAK.ANDROID-0123456789abcdef" time="2026-10-25T12:00:00.000Z">hello all</remarks>
  </detail>
</event>
```

Values are illustrative, not captured traffic. In particular, whether `chatgrp` lists `uid1="All Chat Rooms"` for the
broadcast room is **UNVERIFIED**.

Direct message to BRAVO (uid `ANDROID-bbbb`):

* `conversationId` = BRAVO's uid, `chatroom` = BRAVO's callsign, and `remarks/@to` = BRAVO's uid.
* It goes through `dispatchToContact`, so commo appends `<marti><dest callsign="BRAVO"/></marti>`:

```xml
<event version="2.0" uid="GeoChat.ANDROID-0123456789abcdef.ANDROID-bbbb.1b2c..." type="b-t-f" how="h-g-i-g-o" time="..." start="..." stale="...">
  <point .../>
  <detail>
    <__chat parent="RootContactGroup" groupOwner="false" messageId="1b2c..." chatroom="BRAVO" id="ANDROID-bbbb" senderCallsign="ALPHA">
      <chatgrp uid0="ANDROID-0123456789abcdef" uid1="ANDROID-bbbb" id="ANDROID-bbbb"/>
    </__chat>
    <link uid="ANDROID-0123456789abcdef" type="a-f-G-U-C" relation="p-p"/>
    <__serverdestination destinations="192.168.1.20:4242:tcp:ANDROID-0123456789abcdef"/>
    <remarks source="BAO.F.ATAK.ANDROID-0123456789abcdef" to="ANDROID-bbbb" time="...">hi</remarks>
    <marti><dest callsign="BRAVO"/></marti>
  </detail>
</event>
```

**Receipts [ATAK]:**

* Delivered = `b-t-f-d`, read = `b-t-f-r` (`ChatLine.Status`).
* `uid` = the original messageId, `how="m-g"`.
* The detail is the same as a chat, with `__chatreceipt` in place of `__chat`.
* Sent back directed to the original sender, so it carries `dest callsign`.

**Other clients:**

* TAKAware (`SwiftTAK COTMessage.generateChatEvent`): same structure; `remarks source="BAO.F.TAKAware.<uid>" to=<conversationId>`;
  `<marti><dest uid=.../></marti>` for directed messages.
* WinTAK sometimes sends GeoChat **without `<remarks>`** (OTS comment in `cot_parser.py parse_geochat`, secondary).

**Server duties:** only routing (section 1.7). OTS detects a DM by `dest@callsign == __chat@chatroom`.
TAK recognizes the special room names `All Chat Rooms` and `All Streaming` (`TAKS:war/com/bbn/marti/util/SpecialChatrooms.java`).

### 1.9 Group change notification on the stream (t-x-g-c)

TAK `DistributedSubscriptionManager.makeGroupChangeMessage` / `sendGroupsUpdatedMessage` [TAKS]:

```xml
<?xml version='1.0' encoding='UTF-8' standalone='yes'?><event how='h-g-i-g-o' type='t-x-g-c' version='2.0' uid='{uuid}.{clientUidThatMadeTheChange}' start='T' time='T' stale='T+20s'><point ce='9999999' le='9999999' hae='0' lat='0' lon='0' /><detail><link relation='p-p' /></detail></event>
```

* Sent to every **other** connection of the same username, or to all of them if an admin forced the change or
  `/groups/update/{username}` was called.
* ATAK (`ATAK:app/com/atakmap/android/channels/ui/overlay/ChannelsOverlay.java onCotEvent`) reacts to `t-x-g-c` as follows:
  1. clears all map items received from that server;
  2. calls `GET /Marti/api/groups/all?useCache=true&sendLatestSA=true`;
  3. expects the server to replay the latest SAs.

---

## 2. Marti HTTP API: common envelope and connect-time calls

### 2.1 Envelope

TAK `ApiResponse` (`TAKS:war/com/bbn/marti/cot/search/model/ApiResponse.java`, `@JsonInclude(NON_NULL)`):

```json
{ "version": "3", "type": "<type string>", "data": <payload>, "messages": ["optional"], "nodeId": "<server id>" }
```

* `version` is the **string** `"3"` (`tak.server.Constants.API_VERSION = "3"`).
* `nodeId` is always present (the server id). Its format in TAK is **UNVERIFIED**; OTS uses 32 random `[a-z0-9]`.
* ATAK checks the `type` string exactly in several places:
  * `com.bbn.marti.remote.groups.Group`
  * `com.bbn.marti.remote.ClientEndpoint`
  * `ServerConfig`
* ATAK's `TakHttpResponse.getStringEntity(verify)` also fails if the body does not **contain** a marker substring
  (`ATAK:app/com/atakmap/comms/http/TakHttpResponse.java`).
* Mission API requests may carry the header `API_VERSION: <n>`. TAK treats a missing header as **2**
  (`MissionServiceDefaultImpl.getApiVersionNumberFromRequest`), which changes some responses (section 4.6).

### 2.2 What ATAK calls on connect

From `ATAK:app/com/atakmap/android/cot/CotMapServerListener.java connected()`:

1. **Every connect:** `GET /Marti/api/version/config`. On **404 only** it falls back to `GET /Marti/api/version`.
2. Once per server per app run: `GET /Marti/api/clientEndPoints`.
3. If the preference `deviceProfileEnableOnConnect` is true:
   * `GET /Marti/api/device/profile/connection?syncSecago=<secondsSinceLastSync>&clientUid=<uid>`;
   * plus `GET /Marti/api/device/profile/tool/<tool>?clientUid=...` for any requested tool profiles.
4. If the Channels overlay is enabled (preference `prefs_enable_channels`, read in `ChannelsMapComponent`; OTS pushes it as a
   String `"true"`), every server connect triggers `GET /Marti/api/groups/all?useCache=true` (`ChannelsOverlay` `connected()`
   calls `ServerGroupsClient.getAllGroups(..., sendLatestSA=false)`; see section 2.4).
   The per-host preference `prefs_enable_channels_host-<host>` (String `"true"`) is checked in the overlay UI.
5. Data Sync plugin (closed source): per OTS, it calls `GET /Marti/api/missions...` and
   `GET /Marti/api/missions/invitations?clientUid=...`. The exact sequence is **UNVERIFIED**.

Not polled on connect; user actions only:

* `/Marti/vcm` (video list GET / POST of `<videoConnections>` XML; `ATAK:app/com/atakmap/android/video/http/...`);
* `/Marti/sync/search` (import packages);
* `/Marti/api/citrap` (CI-TRAP reports plugin).
* `/Marti/api/tls/config` and `/Marti/api/tls/signClient/v2?clientUid=&version=` are certificate **enrollment** calls,
  made on port 8446 with Basic auth. They are a separate topic, see section 2.7.

TAKAware calls:

* `GET /Marti/api/groups/all?useCache=true&sendLatestSA=true`, and `PUT /Marti/api/groups/activebits`;
* `GET /Marti/api/files/metadata?missionPackage=true`, and `GET /Marti/api/files/<hash>`;
* mission endpoints (section 4.10).

Source: `TAKAware/Data Models/TAKConstants.swift`.

### 2.3 `/Marti/api/version` and `/Marti/api/version/config`

[TAKS] `TAKS:war/com/bbn/marti/util/VersionApi.java`.

**`GET /Marti/api/version`**

* Body is plain text: the contents of `shortver.txt`, which `src/takserver-core/build.gradle writeVerFile` builds as
  `"$takversion-$takrelease$branch"`, for example `5.2-RELEASE-16`.
* ATAK's legacy check requires the body to **contain `TAK Server`** (`GetServerVersionRequest.SERVER_VERSION_MATCHER`).
  Return `TAK Server 5.2-RELEASE-16`. `VersionApi.getVersionConfig` strips a `"TAK Server"` prefix, which shows older builds included it.
* OTS does not implement `/Marti/api/version`.

**`GET /Marti/api/version/config`**

```json
{"version":"3","type":"ServerConfig","data":{"version":"5.2.16-RELEASE","api":"3","hostname":"tak.example.com"},"nodeId":"6ff99444fa124679a3943ee90308a44c"}
```

* `data.version` is built from shortver: split `5.2-RELEASE-16` on `-` and output `tokens[0] + "." + tokens[2] + "-" + tokens[1]`.
* `hostname` is the host of the request URL.
* ATAK (`ATAK:app/com/atakmap/android/http/rest/ServerVersion.java fromJSON`) requires:
  * top-level `version` parseable as int (`"3"` works);
  * `type == "ServerConfig"` exactly;
  * `data.version` present.
* ATAK uses the API version to decide whether to add `&tool=public` to package searches (`MPT_TOOL_PARAM_MIN_VERSION = 2`).

### 2.4 Groups (`/Marti/api/groups/...`) and channels

[TAKS] `TAKS:war/com/bbn/marti/groups/GroupsApi.java` (GET) and `TAKS:war/com/bbn/marti/sync/api/SubscriptionApi.java` (PUT).

**`GET /Marti/api/groups/all?useCache=<bool>&sendLatestSA=<bool>`**

* Admins get all groups.
* Others with `useCache=true` get the active-group cache (IN and OUT). On a cache miss, or with `useCache=false`,
  they get the request user's groups (OUT only when `useCache=false`).
* `sendLatestSA=true` triggers `subscriptionManager.sendLatestReachableSA(username)`.
* TAK normalizes every group "to meet the ATAK ServerGroup.isValid check":
  * null `bitpos` becomes 0;
  * null `created` becomes now;
  * null `type` becomes `SYSTEM`;
  * null `direction` becomes `OUT`.

```json
{"version":"3","type":"com.bbn.marti.remote.groups.Group","data":[
  {"name":"__ANON__","direction":"OUT","created":"2026-01-15","type":"SYSTEM","bitpos":2,"active":true},
  {"name":"__ANON__","direction":"IN","created":"2026-01-15","type":"SYSTEM","bitpos":2,"active":true}
],"nodeId":"6ff99444fa124679a3943ee90308a44c"}
```

Field rules (Group JSON in `TAKS:plugins/.../remote/groups/Group.java`; ATAK parser in `ATAK:app/com/atakmap/android/http/rest/ServerGroup.java`):

* `created` is formatted `yyyy-MM-dd` (`@JsonFormat`). **ATAK parses it with exactly `yyyy-MM-dd`.** A parse failure throws.
* `bitpos` must be **>= 0** and present. Missing gives -1, which makes the group invalid.
* `name`, `direction`, `type` must be non-empty.
* `description` and `distinguishedName` are optional (omitted when null).
* **If any element is invalid, ATAK rejects the whole list** (`fromResultJSON` throws "Invalid ServerGroup").
* The top-level `type` must be exactly `com.bbn.marti.remote.groups.Group`.
* `bitpos` = 2 for `__ANON__` is what OTS and FTS hard-code; TAK's actual assignment is **UNVERIFIED**.

**How ATAK channels use it** (`ChannelsOverlay.java`, `channels/net/*.java`):

* Only `direction=="OUT"` groups are shown, as toggleable channels with the `active` flag.
* When toggled, ATAK clears that server's map items, then sends
  `PUT /Marti/api/groups/active?clientUid=<deviceUid>` with `Content-Type: application/json`.
* The body is **all OUT groups plus an IN copy of each**, with the new `active` flags.
  `created` is sent as **epoch milliseconds (number)**, not a date string (`ServerGroup.toJSON`):

```json
[{"name":"OPS","direction":"OUT","created":1736899200000,"type":"SYSTEM","bitpos":3,"active":false},
 {"name":"OPS","direction":"IN","created":1736899200000,"type":"SYSTEM","bitpos":3,"active":false},
 {"name":"__ANON__","direction":"OUT","created":1736899200000,"type":"SYSTEM","bitpos":2,"active":true},
 {"name":"__ANON__","direction":"IN","created":1736899200000,"type":"SYSTEM","bitpos":2,"active":true}]
```

**TAK `PUT /groups/active` server behavior** (`SubscriptionApi.doSetActiveGroups`):

1. Optionally require at least one active group (config `x509UseGroupCacheRequiresActiveGroup`); otherwise return 400 and send `t-x-g-c`.
2. Send **disconnect** (`t-x-d-d`) for this user to everyone currently reachable.
3. Store the active-group selection; re-authenticate the user's streaming connections.
4. `sendUpdatedGroupsLatestReachableSA`: send the user every SA now reachable, and re-broadcast the user's SA to the new audience.
5. Send `t-x-g-c` to the user's **other** devices (excluding `clientUid`).
6. Return `200`, empty body.

**TAKAware alternative:** `PUT /Marti/api/groups/activebits` with a JSON array of active `bitpos` ints, for example `[2,5]`.
TAK maps the bits back to groups (`SubscriptionApi.setActiveGroups(Integer[])`).
**OTS's `activebits` is a stub that does nothing:** implement it properly.

**`GET /Marti/api/groups/groupCacheEnabled`** returns
`{"version":"3","type":"java.lang.Boolean","data":false,"nodeId":"..."}`.
Which client calls it is **UNVERIFIED**; it was not found in ATAK-CIV.

### 2.5 `/Marti/api/clientEndPoints`

[TAKS] `TAKS:war/com/bbn/marti/network/ContactManagerApi.java`; model `TAKS:common .../remote/ClientEndpoint.java`.

* Query parameters:
  * `secAgo` (default 0);
  * `showCurrentlyConnectedClients` (default false);
  * `showMostRecentOnly` (default false);
  * `group` (repeatable, must be a subset of the user's groups).
* Results are filtered by the user's **OUT** group vector.

```json
{"version":"3","type":"com.bbn.marti.remote.ClientEndpoint","data":[
  {"callsign":"ALPHA","uid":"ANDROID-0123456789abcdef","username":"alpha","team":"Cyan","role":"Team Member",
   "lastEventTime":"2026-10-25T12:00:00.123Z","lastStatus":"Connected"}
],"nodeId":"6ff99444fa124679a3943ee90308a44c"}
```

ATAK parsing (`ATAK:app/com/atakmap/android/http/rest/ServerContact.java`) requires:

* `type == "com.bbn.marti.remote.ClientEndpoint"`;
* `lastEventTime` parseable with Java `SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.S'Z'")`;
* `lastStatus` exactly `Connected` or `Disconnected`;
* `uid` and `callsign` present.

**Any invalid element rejects the whole list.**

* **Emit exactly 3 fractional digits.** `S` parses the digit string as a *millisecond count*, so
  `.871000Z` would be read as 871 s. OTS emits 4 digits (`OTS:functions.py iso8601_string_from_datetime`), which shifts times by seconds.
* A timestamp without a fraction fails to parse.
* ATAK also feeds this response into `ServerVersion.fromJSON`, which reads the top-level `version`.

### 2.6 Device profile on connect

[TAKS] `TAKS:war/com/bbn/marti/device/profile/api/ProfileAPI.java`.

**`GET /Marti/api/device/profile/connection?syncSecago=<long>&clientUid=<uid>`**

* Both parameters are required in TAK.
* If nothing applies: **`204 No Content`**, empty body. ATAK handles 204 (`ATAK:app/com/atakmap/net/DeviceProfileOperation.java`).
* Otherwise: `200` with a mission-package zip and `Content-Disposition: attachment; filename=<default>`.
* Recommendation: return 204 unless you have a profile.
* OTS always returns a zip containing `preference.pref` and the truststore (`OTS:blueprints/marti_api/device_profile_marti_api.py`).
  OTS also uses this zip to switch on ATAK channels:
  * `prefs_enable_channels` = `"true"`;
  * `prefs_enable_channels_host-<host>` = `"true"`;
  * plus `appMgmtEnableUpdateServer`, `atakUpdateServerUrl`, `repoStartupSync`, `deviceProfileEnableOnConnect`.

Other endpoints:

* `GET /Marti/api/tls/profile/enrollment?clientUid=` (port 8446): same 204-or-zip semantics.
* `GET /Marti/api/device/profile/tool/<tool>?clientUid=&syncSecago=`: tool profiles.

### 2.7 Other endpoints seen

| Endpoint | Who | Notes |
|---|---|---|
| `GET /Marti/api/tls/config` (8446, Basic auth) | ATAK, TAKAware enrollment | [TAKS] `TAKS:war/com/bbn/tak/tls/CertManagerApi.java`. JAXB XML `<ns2:certificateConfig xmlns="http://bbn.com/marti/xml/config" xmlns:ns2="com.bbn.marti.config"><nameEntries><nameEntry name="O" value="..."/><nameEntry name="OU" value="..."/></nameEntries>...`. OTS returns the same shape |
| `POST /Marti/api/tls/signClient/v2?clientUid=&version=` (8446) | enrollment | Body: base64 CSR. With `Accept: application/json` or `*/*`: `{"signedCert":"<pem>","ca0":"<pem>",...}`. With `Accept: application/xml`: `<enrollment><signedCert>..</signedCert><ca>..</ca></enrollment>`. Out of scope here |
| `GET /Marti/api/contacts/all` | **UNVERIFIED** which client | OTS returns a list of `{filterGroups,notes,callsign,team,role,takv,uid}` |
| `GET/POST /Marti/vcm` | ATAK video | XML `<videoConnections><feed>...</feed></videoConnections>` |
| `GET /Marti/api/citrap`, `/Marti/api/missions/citrap/...` | CI-TRAP plugin | TAK seeds missions `citrap` (tool `citrap`) and `exchecktemplates` (tool `ExCheck`) (`V33__data_sync_phase3.sql`) |
| `GET /Marti/ExportMissionKML?uid=&startTime=&endTime=&format=kmz` | ATAK track history | OTS implements it |

---

## 3. File sharing / data packages ("Enterprise Sync")

Server sources [TAKS]:

* `TAKS:war/com/bbn/marti/sync/MissionPackageUploadServlet.java` (`/sync/missionupload`)
* `MissionPackageQueryServlet.java` (`/sync/missionquery`)
* `ContentServlet.java` (`/sync/content`)
* `SearchServlet.java` (`/sync/search`)
* `UploadServlet.java` (`/sync/upload`)
* `MetadataApi.java` (`/api/sync/metadata/...`)

The servlets are registered under `/Marti` in `TAKS:core/tak/server/config/ApiConfiguration.java`.

Client sources [ATAK]:

* `ATAK:commo/missionpackagemanager.cpp`: upload, send and download state machine;
* `ATAK:app/.../missionpackage/http/rest/QueryMissionPackageOperation.java`, `GetFileTransferOperation.java`;
* `ATAK:app/.../missionpackage/file/task/CopyAndSendTask.java`.

### 3.1 ATAK "Send" flows (verified in commo)

Every TAK-server upload follows this state machine (`TxUploadContext` states CHECK, UPLOAD, TOOLSET):

1. **CHECK:** `GET /Marti/sync/missionquery?hash=<sha256>`.
   * `200` with the body = file URL: skip upload, go to TOOLSET.
   * `404`: go to UPLOAD.
   * Anything else: failure.
2. **UPLOAD:** `POST /Marti/sync/missionupload?hash=<sha256>&filename=<name>&creatorUid=<deviceUid>`.
   * `multipart/form-data` with one part, **name `assetfile`**, `filename=<name>`, `Content-Type: application/x-zip-compressed`.
   * `200` with the body = URL.
3. **TOOLSET:** `PUT /Marti/api/sync/metadata/<sha256>/tool`, `Content-Type: text/plain`.
   * Body `private` when sending to contacts, `public` for a server-only upload ("Send to TAK Server").
   * The result is only logged; failure is tolerated.
4. If there are contacts: commo sends one **`b-f-t-r`** CoT per contact over the stream, with
   `<marti><dest callsign=...>`. `senderUrl` is **the exact body string returned in step 1 or 2**.

```xml
<event version="2.0" uid="a3c9...-UUID" type="b-f-t-r" how="h-e" time="..." start="..." stale="...+10s">
  <point lat="52.1" lon="21.0" hae="100.0" ce="9.9" le="9999999"/>
  <detail>
    <fileshare filename="Package1.zip" senderUrl="https://tak.example.com:8443/Marti/sync/content?hash=9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08" sizeInBytes="20480" sha256="9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08" senderUid="ANDROID-0123456789abcdef" senderCallsign="ALPHA" name="Package1"/>
    <ackrequest uid="5d1e...-ACK-UUID" ackrequested="true" tag="Package1"/>
    <marti><dest callsign="BRAVO"/></marti>
  </detail>
</event>
```

(`CoTMessageImpl` file-request constructor in `ATAK:commo/cotmessage.cpp`. `peerHosted="true"` and `httpsPort` appear only for
peer-to-peer transfers.)

5. The recipient's commo downloads `senderUrl`.
   * A URL containing `/Marti/` is treated as a TAK-server URL and uses the stream's TLS client cert.
   * It then replies with **`b-f-t-a`** (`how="m-g"`, stale +10 s) carrying:

```xml
<ackresponse uid="5d1e...-ACK-UUID" senderUid="ANDROID-bbbb" success="true" tag="Package1" reason="" sha256="..." sizeInBytes="20480"/>
```

   * The ack is routed back to the sender, also with a `dest`.
   * ATAK's `CotMapComponent` "quietly consumes" the mission package ack.

**Implications:**

* The URL returned by missionquery or missionupload **must be GET-able** by other clients with their certs.
  * TAK returns `https://<host>:<port>/Marti/sync/content?hash=<uid>`. For data packages the uid **is** the SHA-256:
    `JDBCEnterpriseSyncService.insertResourceStreamUID` sets `uid = hash` when no uid is given. Port 8444 is rewritten to 8443.
  * OTS and FTS return `https://host:8443/Marti/api/sync/metadata/<hash>/tool` and serve the file on GET there. That works too, but copy TAK.
* `PUT .../tool` must store the **`tool`** field (`public`/`private`).
  OTS stores the body into *keywords* (a bug). FTS maps it to a privacy flag.
* Server-only uploads are `public`, so they appear in "Import from server"; direct sends are `private`, so they don't.

### 3.2 `POST /Marti/sync/missionupload`

[TAKS] TAK's parameters:

| param | required | default |
|---|---|---|
| `filename` | **yes** | |
| `creatorUid` | no | `""` |
| `hash` | no | ignored for the uid; the server recomputes SHA-256 |
| `mimetype` | no | `application/x-zip-compressed` |
| `keyword` | no | `missionpackage` |
| `tool` | no | `public` |
| `groups` | no | user's groups (must be a subset) |

* The request must be `multipart/form-data`. Part `assetfile`, or alternatively `resource`.
* Response: `200`, `Content-Type: text/plain`, body e.g.
  `https://tak.example.com:8443/Marti/sync/content?hash=9f86d0...0a08` (no trailing newline).
* Errors: `400` (not multipart, or a required parameter missing); `403` (duplicate of an existing entry with the same uid, filename and creatorUid).
* OTS note: ATAK can send the part with **no file extension** but a zip mimetype. OTS then assumes `.zip`.

### 3.3 `GET /Marti/sync/missionquery?hash=<sha256>`

* Found: `200 text/plain`, body `https://host:8443/Marti/sync/content?hash=<sha256>`.
* Not found: **`404`**.
* Only the `hash` parameter is allowed.
* FTS returns the file itself here, which is wrong; commo would use the bytes as the URL.

### 3.4 `GET|HEAD /Marti/sync/content?hash=<sha256>` (or `?uid=<uid>`)

[TAKS] `ContentServlet.java`. Parameters: `UID`, `Hash`, `offset`, `length` (names are case-insensitive).

* `HEAD` returns `200` or `404` without a body.
* `GET` response headers:
  * `Content-Type: <stored mimeType>`;
  * `Content-Disposition: inline; filename="<url-encoded name>"`;
  * `api-version: 3`;
  * gzip encoding if the client sends `Accept-Encoding: gzip`.
* `offset` skips N bytes; that is how ATAK resumes downloads (`GetFileTransferOperation` appends `&offset=<existingLength>`).
* `206` is returned only when `length` is given and smaller than the remainder.
* Unknown file: `404`.
* ATAK "import from server" downloads exactly `<base>/Marti/sync/content?hash=<sha256>` (`GetFileTransferOperation`). The URL is rebuilt; `senderUrl` is not used.

### 3.5 `GET /Marti/sync/search`

[TAKS] `SearchServlet.java`. ATAK sends `GET /Marti/sync/search?keywords=missionpackage[&tool=public]` (`QueryMissionPackageOperation`).

* `tool` is added only when the known server API version is 2 or higher.
* Allowed parameters (case-insensitive): `BBox`, `Circle`, `StartTime`, `StopTime`, `SubmissionDateTime`, `MinAltitude`,
  `MaxAltitude`, `PrimaryKey`, `Filename`, `Keywords`, `MIMEType`, `Name`, `Permissions`, `Remarks`, `UID`, `Tool`.
* Response: `Content-Type: text/json`, body `{"resultCount":N,"results":[...]}`.
* Each result is `Metadata.toJSONObject()` (`TAKS:plugins/com/bbn/marti/sync/Metadata.java`):
  * keys are the enum names;
  * scalar values are **strings**; array fields (`Keywords`, `Permissions`, `Contacts`, `Groups`) are JSON arrays;
  * only fields present are emitted.

```json
{"resultCount":1,"results":[{"UID":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08","Name":"Package1.zip","Hash":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08","PrimaryKey":"17","SubmissionDateTime":"2026-10-25T12:00:00.123Z","SubmissionUser":"alpha","CreatorUid":"ANDROID-0123456789abcdef","Keywords":["missionpackage"],"MIMEType":"application/x-zip-compressed","Size":"20480","Tool":"public","EXPIRATION":"-1"}]}
```

ATAK (`MissionPackageQueryResult.fromJSON`) requires:

* the body to contain the substring `resultCount`;
* each result to have `UID`, `Name`, `Hash`, `PrimaryKey` (int-coercible) and `SubmissionDateTime`.

Optional: `SubmissionUser`, `CreatorUid`, `Keywords`, `MIMEType`, `Size` (long-coercible). An invalid child rejects the whole list.

**Filter on `tool`.** `private` packages from direct sends must not appear in "Import from server".
OTS ignores both `keywords` and `tool` (it returns everything).

### 3.6 `PUT /Marti/api/sync/metadata/{hash}/{field}`

[TAKS] `MetadataApi.java`.

* `{field}` = `tool` or `mimetype`; the body is the raw text value. Response `200`, or `404` if the hash is unknown.
* Other fields: `400`.
* Related:
  * `PUT .../{hash}/keywords` with a JSON string array body;
  * `PUT .../{hash}/expiration?expiration=<long>`.

### 3.7 `POST /Marti/sync/upload` (generic file upload; Data Sync files; iTAK packages)

[TAKS] `UploadServlet.java`.

* Parameters: any non-machine-generated `Metadata` field name, case-insensitive. For example:
  * `name` (alias), `Name`, `Keywords` (comma list), `CreatorUid`/`creatorUid`;
  * `MIME`/`MIMEType`, `Tool`, `Groups`, `MissionName`, `UID`, `Latitude`, `Longitude`, ...
* **Unknown parameters cause `400`** in TAK.
* Body: raw bytes (any Content-Type), or multipart with part `assetfile`/`resource`.
* A random UUID uid is assigned if none is given.
* Response: `200`, `text/json`, `Metadata` JSON, for example:

```json
{"UID":"0b7f...-uuid","Name":"photo.jpg","Hash":"<sha256>","PrimaryKey":"42","SubmissionDateTime":"2026-10-25T12:00:00.1Z","SubmissionUser":"alpha","CreatorUid":"ANDROID-0123456789abcdef","MIMEType":"image/jpeg","Keywords":["foo"],"Size":"12345"}
```

* OTS returns the same keys, plus it rewrites `Name`.

**iTAK specifics** (OTS comments in `mission_marti_api.py upload_content`; secondary):

* iTAK uploads **data packages** here with `Content-Type: application/x-zip-compressed`, raw body, and **no extension** in `name`.
* Older iTAK uses `CreatorUid` (capital C).
* iTAK renames files to `YYYYMMDD-HHMMSS`.
* User-Agent contains `iTAK`.

### 3.8 TAKAware file listing ("files" API)

[TAKS] `TAKS:war/tak/server/filemanager/FileManagerApi.java`.

**`GET /Marti/api/files/metadata?missionPackage=true`** (also: `page`, `limit`, `mission`, `name`, `sort`, `ascending`):

```json
{"version":"3","type":"Files","data":[{"Name":"Package1.zip","User":"alpha","Creator":"ALPHA","Size":"20kB","Time":"<Date.toString()>","MimeType":"application/x-zip-compressed","Keywords":"missionpackage","Expiration":"none","Hash":"9f86...","Groups":"__ANON__"}],"nodeId":"..."}
```

* All values are strings. `Size` is human-formatted (`B`, `kB`, `MB`, `GB`). `Expiration` is `none` or ISO without the `Z`.
* `GET /Marti/api/files/{hash}` returns the bytes with `Content-Disposition: attachment`. `HEAD` and `DELETE` also exist.
* TAKAware (`TAKAware/Communications/DataPackageManager.swift`) reads `Creator, Expiration, Groups, Hash, Keywords, MimeType, Name, Size, Time, User`
  from `data[]`, with optional casts.

---

## 4. Missions / Data Sync (Marti API v3)

Server: [TAKS] `TAKS:war/com/bbn/marti/sync/api/MissionApi.java` (about 4,950 lines).

Models in `TAKS:plugins/com/bbn/marti/sync/model/`:

* `Mission.java`, `MissionRole.java`, `MissionPermission.java`, `MissionChange.java`, `Resource.java`, `UidDetails.java`, `LogEntry.java`;
* `TAKS:war/com/bbn/marti/sync/model/MissionSubscription.java` and `MissionInvitation.java`;
* change-CoT creation in `TAKS:core/com/bbn/marti/service/DistributedSubscriptionManager.java`.

OTS equivalent: `OTS:blueprints/marti_api/mission_marti_api.py` and `OTS:models/Mission*.py`.

The ATAK Data Sync plugin is closed source. OTS docstrings say which endpoints it calls ("Used by the Data Sync plugin to ...").
That call list is secondary; treat the exact sequence as **UNVERIFIED**.

### 4.1 Roles, permissions, tokens

**Roles** (`MissionRole.Role`): `MISSION_OWNER`, `MISSION_SUBSCRIBER`, **`MISSION_READONLY_SUBSCRIBER`**.

* **Gotcha:** OTS uses `MISSION_READ_ONLY`, which is wrong.
* The default role is `MISSION_SUBSCRIBER`.

**Permissions** (`MissionPermission.Permission`):

* `MISSION_READ`, `MISSION_WRITE`, `MISSION_DELETE`, `MISSION_SET_ROLE`;
* `MISSION_SET_PASSWORD`, `MISSION_UPDATE_GROUPS`, `MISSION_MANAGE_FEEDS`, `MISSION_MANAGE_LAYERS`.

**Seeded mapping** (`src/takserver-schemamanager/.../db/migration/V33__data_sync_phase3.sql`, `V37`, `V48`):

* OWNER: READ, WRITE, DELETE, SET_ROLE, SET_PASSWORD, UPDATE_GROUPS, MANAGE_FEEDS (+MANAGE_LAYERS: present in OTS and in the enum; the migration adding it was not traced, so **UNVERIFIED**).
* SUBSCRIBER: READ, WRITE.
* READONLY_SUBSCRIBER: READ.

**Role JSON:** `{"type":"MISSION_SUBSCRIBER","permissions":["MISSION_READ","MISSION_WRITE"]}`.
The `permissions` array is a Java Set, so its order is unspecified.

**Role XML** (inside mission CoT): `<role type="MISSION_SUBSCRIBER"><permissions><permission type="MISSION_READ"/><permission type="MISSION_WRITE"/></permissions></role>`.

**Token:**

* Returned in the create response (`token`, plus `ownerRole`), in the subscription response (`data.token`), and in invitations.
* The client sends it back as **`Authorization: Bearer <token>`**. TAK also accepts the header `MissionAuthorization`
  (`MissionServiceDefaultImpl.getRoleFromToken`).
* TAK token: JWT, HS256, signed with the server key bytes. Claims: `jti`, `iat`, `sub` = token type (`SUBSCRIPTION|INVITATION|ACCESS`),
  `iss` = flowtag text, a `<TYPE>` claim = id, `MISSION_NAME`, `MISSION_GUID` (`TAKS:war/.../sync/service/MissionTokenUtils.java`).
* OTS token: RS256 JWT with `sub` = EUD uid.
* Clients should treat the token as opaque (**UNVERIFIED** for closed clients). ChameleonTAK can use any opaque signed token that maps to (mission, clientUid, role).
* **iTAK sends no token** (OTS comment). TAKAware sends a Bearer token only for password-protected missions.
  * So authorize by client cert user + `uid`/`creatorUid` parameter + stored subscription role, falling back to the mission's default role.
  * TAK's `@PreAuthorize("hasPermission(#request,'MISSION_READ')")` evaluator does this kind of fallback (`MissionPermissionEvaluator`; exact logic **UNVERIFIED**).

### 4.2 Mission JSON object

From `Mission.java` (`@JsonInclude(NON_NULL)`). Dates use `yyyy-MM-dd'T'HH:mm:ss.SSS'Z'` (`COT_DATE_FORMAT_PAD_MILLIS`).

| JSON field | type | notes |
|---|---|---|
| `name` | string | |
| `description` | string | TAK default `""` on create |
| `chatRoom`, `baseLayer`, `bbox`, `boundingPolygon`, `path`, `classification` | string | default `""` |
| `tool` | string | default `public`. Others seen: `ExCheck`, `citrap`, VBM COP tool |
| `expiration` | long | `-1` = never |
| `keywords` | string[] | |
| `creatorUid` | string | |
| `createTime`, `lastEdited` | string date | `lastEdited` exists in newer TAK |
| `uids` | MissionAdd[] | see below |
| `contents` | MissionAdd&lt;Resource&gt;[] | see below |
| `groups` | string[] | group **names**. Set on GET by name/guid; may be absent in lists |
| `externalData`, `mapLayers`, `feeds` | arrays | |
| `passwordProtected` | bool | always present |
| `token` | string | only when issued (create; GET with `password=`) |
| `inviteOnly` | bool | (TAK getter is `Boolean isInviteOnly()`; whether Jackson emits it is **UNVERIFIED**. OTS emits it. Emit it) |
| `ownerRole` | Role | only in the create response |
| `defaultRole` | Role object | `{"type":...,"permissions":[...]}` |
| `missionChanges` | MissionChange[] | only with `?changes=true` or in API≥3 subscribe responses |
| `logs` | LogEntry[] | only with `?logs=true` or in API≥3 subscribe responses |
| `guid` | string (UUID) | |

**`uids[]` element** (`Mission.MissionAdd` / `MissionAddDetails`):

```json
{"data":"marker-uid-1","timestamp":"2026-10-25T12:00:00.123Z","creatorUid":"ANDROID-0123456789abcdef",
 "details":{"type":"a-h-G","callsign":"Hostile 1","iconsetPath":"COT_MAPPING_2525B/a-h/a-h-G","color":"-65536","location":{"lat":52.1,"lon":21.0}}}
```

* `UidDetails` JSON keys (`UidDetails.java`): `type`, `callsign`, `title`, `iconsetPath`, **`color` (string)**, `attachments` (string[]),
  `name`, `category`, `location {lat, lon}`.
* OTS emits `color` as an int. TAKAware reads `color` as a String and silently drops ints, so **emit strings**.

**`contents[]` element:**

```json
{"data":{"filename":"overlay.kml","keywords":[],"mimeType":"application/vnd.google-earth.kml+xml","name":"overlay.kml",
         "submissionTime":"2026-10-25T12:00:00.1Z","submitter":"alpha","uid":"0b7f...-uuid","creatorUid":"ANDROID-0123456789abcdef",
         "hash":"<sha256>","size":1234,"tool":"public","expiration":-1},
 "timestamp":"2026-10-25T12:00:00.123Z","creatorUid":"ANDROID-0123456789abcdef"}
```

* `Resource.submissionTime` uses `yyyy-MM-dd'T'HH:mm:ss.S'Z'` (`COT_DATE_FORMAT`; variable-length millis).
* Optional `latitude`, `longitude`, `altitude`, `groups`, `contentType`.

**TAKAware force-unwraps** `name`, `description`, `creatorUid`, `createTime` (String), `passwordProtected` (Bool), `uids` (array)
and `contents` (array) in `DataSyncManager.swift`. **Never omit them or set them to `null`.**

### 4.3 List missions: `GET /Marti/api/missions`

Parameters: `passwordProtected` (default false), `defaultRole` (default false), `tool` (default: **`public` only**).

* With `passwordProtected=false`, password-protected missions are excluded.
* Results are filtered by the user's groups.
* TAKAware calls `?passwordProtected=true`.

```json
{"version":"3","type":"Mission","data":[{
  "name":"op-alpha","description":"","chatRoom":"","baseLayer":"","bbox":"","path":"","classification":"","tool":"public",
  "keywords":[],"creatorUid":"ANDROID-0123456789abcdef","createTime":"2026-10-25T12:00:00.000Z","externalData":[],"feeds":[],
  "mapLayers":[],"defaultRole":{"type":"MISSION_SUBSCRIBER","permissions":["MISSION_READ","MISSION_WRITE"]},
  "inviteOnly":false,"expiration":-1,"guid":"8f0e2b5c-6a3d-4f1e-9b7a-2c4d6e8f0a1b","uids":[],"contents":[],"passwordProtected":false}],
 "nodeId":"6ff99444fa124679a3943ee90308a44c"}
```

OTS gotcha: OTS filters `tool == <param>`, so without `?tool=public` it returns only missions with a null tool. Follow TAK: default `public`.

### 4.4 Create / update: `PUT /Marti/api/missions/{name}`

Query parameters (TAK `createMission`):

* `creatorUid`;
* `group` (repeatable `group=A&group=B`; default `__ANON__`; OTS also splits on commas);
* `description`, `chatRoom`, `baseLayer`, `bbox`, `boundingPolygon`, `path`, `classification`;
* `tool` (default `public`);
* `password` (stored with BCrypt);
* `defaultRole` (`MISSION_OWNER|MISSION_SUBSCRIBER|MISSION_READONLY_SUBSCRIBER`);
* `expiration`, `inviteOnly`, `allowGroupChange`.

Optional JSON body (`Content-Type: application/json`): a Mission object, used for `mapLayers`/`feeds`/`groups`.

**New mission:** **`201`**.

* TAK creates the mission, **auto-subscribes the creator as `MISSION_OWNER`**, and returns `token` and `ownerRole`.
* It broadcasts `t-x-m-n` (section 4.9) to every connected subscription whose groups intersect the mission's, except the creator.

```json
{"version":"3","type":"Mission","data":[{ ...mission fields...,
  "token":"eyJhbGciOiJIUzI1NiJ9....",
  "ownerRole":{"type":"MISSION_OWNER","permissions":["MISSION_READ","MISSION_WRITE","MISSION_DELETE","MISSION_SET_ROLE","MISSION_SET_PASSWORD","MISSION_UPDATE_GROUPS","MISSION_MANAGE_FEEDS","MISSION_MANAGE_LAYERS"]}}],
 "nodeId":"..."}
```

**Existing mission:** **`200`**, updated metadata.

* Changing groups requires owner or admin, plus `allowGroupChange=true` or config `missionAllowGroupChange`; otherwise `403`.
* `POST /Marti/api/missions/{name}?allowDupe=true` is an alternative create.

### 4.5 Get one / delete

**`GET /Marti/api/missions/{name}`**

* Parameters: `password`, `changes`, `logs`, `secago`, `start`, `end`.
* Response: `{"version":"3","type":"Mission","data":[<mission>],"nodeId":"..."}`.
* With a valid `password`, `data[0].token` is an ACCESS token.
* Without `MISSION_READ`, API≥3 returns the mission with contents cleared rather than `403`.
* By guid: `GET /Marti/api/missions/guid/{guid}`.

**`DELETE /Marti/api/missions/{name}?creatorUid=&deepDelete=false`**

* Returns the deleted mission in the same envelope.
* Requires `MISSION_WRITE`; with `missionDeleteRequiresOwner`, it requires OWNER.
* Broadcasts `t-x-m-d`.
* OTS returns `{"success":true}`. Use TAK's envelope.

### 4.6 Subscribe / unsubscribe / roles

**`PUT /Marti/api/missions/{name}/subscription?uid=<clientUid>[&password=][&secago=|start=&end=][&topic=]`** returns **`201`**.

* Authorization:
  * password-protected missions need `password` or a Bearer token (invitation, subscription or access);
  * invite-only missions need a token or a `userName` invitation.
* Effects:
  1. stores the subscription with the token role or the mission's default role;
  2. starts streaming change notifications to that uid;
  3. sends the latest data-feed events;
  4. deletes clientUid and callsign invitations.

```json
{"version":"3","type":"com.bbn.marti.sync.model.MissionSubscription","data":{
  "token":"eyJhbGciOiJIUzI1NiJ9....","clientUid":"ANDROID-0123456789abcdef","username":"alpha",
  "createTime":"2026-10-25T12:00:00.1Z",
  "role":{"type":"MISSION_SUBSCRIBER","permissions":["MISSION_READ","MISSION_WRITE"]},
  "mission":{ ...mission incl. "missionChanges":[...], "logs":[...] ... }},
 "nodeId":"..."}
```

* **`mission` is only included when the request header `API_VERSION` is 3 or higher.** No header means 2 (TAK).
* Whether ATAK Data Sync sends `API_VERSION` is **UNVERIFIED**. OTS always includes `mission`. **Recommendation: always include it.**
* TAKAware checks only for 2xx with Content-Type `application/json` or `text/plain`.

**`GET /Marti/api/missions/{name}/subscription?uid=`** returns the caller's subscription (`404` if none).

**`DELETE /Marti/api/missions/{name}/subscription?uid=<clientUid>[&disconnectOnly=true|false][&topic=]`** returns `200`, empty.

* **TAK's default is `disconnectOnly=true`:** it stops notifications but keeps the subscription and role record.
  `false` deletes the record.
* TAKAware sends `?disconnectOnly=true&uid=...` **without a token**. OTS would answer 401 for non-iTAK user agents, so don't require tokens here.

**`GET /Marti/api/missions/{name}/subscriptions`** returns `{"version":"3","type":"MissionSubscription","data":["ANDROID-1","ANDROID-2"],...}`
(clientUids of all subscriptions, connected or not).

**`GET /Marti/api/missions/{name}/subscriptions/roles`:**

```json
{"version":"3","type":"MissionSubscription","data":[
  {"clientUid":"ANDROID-0123456789abcdef","username":"alpha","createTime":"2026-10-25T12:00:00.1Z",
   "role":{"type":"MISSION_OWNER","permissions":[...]}}],"nodeId":"..."}
```

Tokens are removed.

**`PUT /Marti/api/missions/{name}/role?clientUid=&username=&role=<ROLE>`**

* Sets a role.
* OTS treats a missing `role` as "kick from mission", sending `t-x-m-r` without a role (**UNVERIFIED** in TAK).
* TAK sends the target `t-x-m-r` with role XML.

**`GET /Marti/api/missions/{name}/role`** returns the caller's role from the token. GUID variants exist for all of these.

### 4.7 Contents: add and remove

**`PUT /Marti/api/missions/{name}/contents?creatorUid=<uid>`** (also `/missions/guid/{guid}/contents`). Requires `MISSION_WRITE`.

```json
{"hashes":["9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"],"uids":["marker-uid-1"]}
```

* An optional `"paths":{...}` / `"after":"..."` exists for mission layers.
* At least one hash, uid or path is required, otherwise `400`.
* Response: `{"version":"3","type":"Mission","data":[<mission>],...}`.
* Hashes must already exist, uploaded with `/Marti/sync/upload` or `/Marti/sync/missionupload`. TAK's model is
  `TAKS:plugins/com/bbn/marti/remote/sync/MissionContent.java`.
* One `t-x-m-c` notification is sent per added item (section 4.9).
* With config `missionBrokerUidAddsFromApi`, TAK also brokers the latest CoT for the added uids to subscribers.

**`DELETE /Marti/api/missions/{name}/contents?hash=<sha256>|uid=<cotUid>&creatorUid=<uid>`**

* Response: the mission envelope.
* Notification: `t-x-m-c` with `type=REMOVE_CONTENT`.

**`PUT /Marti/api/missions/{name}/contents/missionpackage?creatorUid=`**

* Body: a raw mission package zip. TAK extracts it into the mission.
* Response: `{"type":"MissionChange","data":[conflicts]}`; `409` on conflicts.

**`PUT /Marti/api/missions/{name}/keywords`**

* Body: a JSON string array. **TAK replaces** all keywords (OTS appends).
* Notification: `t-x-m-c-k`.
* Per-item keywords: `PUT|DELETE /missions/{name}/uid/{uid}/keywords` and `/missions/{name}/content/{hash}/keywords`.

### 4.8 Adding CoT to a mission over the stream

ATAK and WinTAK (commo `setTAKServerMissionRecipient`) send the event with `<marti><dest mission="op-alpha"/></marti>`.
TAK (`StreamingEndpointRewriteFilter.processTracksByMissionName`) then:

1. looks up the sender's mission subscription; ignores the add without one or without `MISSION_WRITE` (logs an error);
2. adds every **connected** subscriber's clientUid except the sender to the explicit uid list.
   **The CoT itself goes only to mission subscribers**, not to a group broadcast (explicit brokering, section 1.7);
3. `addMissionContentAtTime(uid)`, which stores the change and sends a `t-x-m-c` ADD_CONTENT notification with `details`;
4. for `b-t-f` sent to a mission, makes per-subscriber copies with mission chat fix-ups.

Variants:

* `<dest mission="NAME" path="..." after="..."/>`: layer placement.
* `<dest mission-guid="UUID"/>`.

**iTAK** (OTS comments, secondary):

* sends a normal CoT, with its own uid in `<dest uid>`;
* then calls `PUT /missions/{name}/contents` with `{"uids":[<event uid>]}`;
* the PUT can arrive before the CoT is parsed, so OTS fills in the uid details later.

**ChameleonTAK:** support both paths. On a PUT with uids, (re)deliver the latest stored CoT for those uids to the subscribers.

`GET /Marti/api/missions/{name}/cot[?path=]` returns `Content-Type: application/xml` (TAK `MissionServiceDefaultImpl.getCachedCotImpl`):

```xml
<?xml version='1.0' encoding='UTF-8' standalone='yes'?><events><event version="2.0" uid="marker-uid-1" type="a-h-G" ...>...</event>
<event ...>...</event>
</events>
```

The latest event per mission uid, separated by `\n`. OTS returns `<events>...</events>` without the declaration.

### 4.9 Mission CoT notifications (server to subscribers)

TAK templates live in `DistributedSubscriptionManager.createMissionMessage`. The seed is:

```xml
<event how='h-g-i-g-o' type='t-x-m-c' version='2.0'><point ce='9999999' le='9999999' hae='0' lat='0' lon='0' /><detail><mission type="CHANGE" tool=""/></detail></event>
```

Added to it:

* event `uid` (random UUID), `type`, `start`/`time` = now, `stale` = now + 20 s;
* `mission@name`, `@guid`, `@type`, and `@authorUid` / `@tool` / `@token` if non-empty;
* child XML (`MissionChanges` or `role`).

| CoT type | `mission@type` | Sent to | Trigger |
|---|---|---|---|
| `t-x-m-n` | `CREATE` | all connected subs with group access, except the creator | mission created |
| `t-x-m-d` | `DELETE` | same broadcast | mission deleted |
| `t-x-m-c` | `CHANGE` | mission subscribers (connected) | content add/remove (`ADD_CONTENT`/`REMOVE_CONTENT`), data feed create/delete |
| `t-x-m-c-l` | `CHANGE` | subscribers | log entry (no child XML) |
| `t-x-m-c-k` | `CHANGE` | subscribers / broadcast | mission keywords |
| `t-x-m-c-k-u` / `t-x-m-c-k-c` | `CHANGE` | subscribers | uid / resource keywords |
| `t-x-m-c-m` | `CHANGE` | broadcast | metadata change |
| `t-x-m-c-e` | `CHANGE` | subscribers | external data |
| `t-x-m-c-h` | `CHANGE` | subscribers | mission layer |
| `t-x-m-i` | `INVITE` (+`token`, +`<role>`) | invitee uids | invitation |
| `t-x-m-r` | `INVITE` (+`<role>`, no token) | one uid | role change |

`MissionChangeType` values: `CREATE_MISSION`, `DELETE_MISSION`, `ADD_CONTENT`, `REMOVE_CONTENT`, `CREATE_DATA_FEED`, `DELETE_DATA_FEED`
(`TAKS:common/com/bbn/marti/remote/sync/MissionChangeType.java`). OTS also defines `CHANGE`.

**ADD_CONTENT of a map item (uid)** example:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<event how="h-g-i-g-o" type="t-x-m-c" version="2.0" uid="4b0d...-UUID" start="2026-10-25T12:00:00Z" time="2026-10-25T12:00:00Z" stale="2026-10-25T12:00:20Z">
  <point ce="9999999" le="9999999" hae="0" lat="0" lon="0"/>
  <detail>
    <mission type="CHANGE" tool="public" name="op-alpha" guid="8f0e2b5c-6a3d-4f1e-9b7a-2c4d6e8f0a1b" authorUid="ANDROID-0123456789abcdef">
      <MissionChanges>
        <MissionChange>
          <contentUid>marker-uid-1</contentUid>
          <creatorUid>ANDROID-0123456789abcdef</creatorUid>
          <details type="a-h-G" callsign="Hostile 1" iconsetPath="COT_MAPPING_2525B/a-h/a-h-G" color="-65536">
            <location lat="52.1" lon="21.0"/>
          </details>
          <isFederatedChange>false</isFederatedChange>
          <missionGuid>8f0e2b5c-6a3d-4f1e-9b7a-2c4d6e8f0a1b</missionGuid>
          <missionName>op-alpha</missionName>
          <timestamp>2026-10-25T12:00:00.1Z</timestamp>
          <type>ADD_CONTENT</type>
        </MissionChange>
      </MissionChanges>
    </mission>
  </detail>
</event>
```

How the `MissionChange` XML is built:

* JAXB of `MissionChange` from TAK's `MissionChange.java`. `serverTime` and `contentHash` are `@XmlTransient`.
* `timestamp` uses `DateAdapter`, which writes `yyyy-MM-dd'T'HH:mm:ss.S'Z'`.
* Optional children: `contentResource`, `logEntry`, `externalData`, `missionFeed`, `mapLayer`.
* The **element order above is OTS's** (`OTS:models/MissionChange.py generate_mission_change_cot`). TAK's JAXB order and
  whether clients care about order are **UNVERIFIED**; assume parsing is by name.

**ADD_CONTENT of a file:**

* Replace `contentUid` and `details` with `<contentResource>` (Resource JAXB elements):
  `<creatorUid/>`, `<expiration>-1</expiration>`, `<hash/>`, `<mimeType/>`, `<name/>`, `<size/>`, `<submissionTime/>`, `<submitter/>`, `<uid/>`
  (plus `filename`, `keywords`, `tool` in TAK).
* TAK does not emit `contentUid` for file changes (`contentHash` is transient). OTS emits `<contentUid>` = resource uid.

**Create / delete / invite** examples:

```xml
<event how="h-g-i-g-o" type="t-x-m-n" version="2.0" uid="..." start="..." time="..." stale="...+20s"><point ce="9999999" le="9999999" hae="0" lat="0" lon="0"/><detail><mission type="CREATE" tool="public" name="op-alpha" guid="8f0e..." authorUid="ANDROID-0123456789abcdef"/></detail></event>

<event how="h-g-i-g-o" type="t-x-m-i" version="2.0" uid="..." start="..." time="..." stale="..."><point ce="9999999" le="9999999" hae="0" lat="0" lon="0"/><detail><mission type="INVITE" tool="public" name="op-alpha" guid="8f0e..." authorUid="ANDROID-0123456789abcdef" token="eyJ..."><role type="MISSION_SUBSCRIBER"><permissions><permission type="MISSION_READ"/><permission type="MISSION_WRITE"/></permissions></role></mission></detail></event>
```

(Attribute order in TAK: the seed's `type`/`tool`, then `name`, `guid`, `authorUid`. dom4j `addAttribute` overwrites the seed values.)

The OTS variants are the same shape, with stale +1 h for create/invite/delete and +2 min for changes.

### 4.10 Changes, logs, invitations

**`GET /Marti/api/missions/{name}/changes?secago=|start=&end=&squashed=true`**

```json
{"version":"3","type":"MissionChange","data":[
 {"isFederatedChange":false,"type":"CREATE_MISSION","missionName":"op-alpha","missionGuid":"8f0e...","timestamp":"2026-10-25T12:00:00.000Z","serverTime":"2026-10-25T12:00:00.000Z","creatorUid":"ANDROID-0123456789abcdef"},
 {"isFederatedChange":false,"type":"ADD_CONTENT","contentUid":"marker-uid-1","missionName":"op-alpha","missionGuid":"8f0e...","timestamp":"...","serverTime":"...","creatorUid":"...","details":{"type":"a-h-G","callsign":"Hostile 1","color":"-65536","location":{"lat":52.1,"lon":21.0}}},
 {"isFederatedChange":false,"type":"ADD_CONTENT","missionName":"op-alpha","missionGuid":"8f0e...","timestamp":"...","serverTime":"...","creatorUid":"...","contentResource":{"name":"overlay.kml","hash":"<sha256>","uid":"0b7f...","mimeType":"...","size":1234,"submitter":"alpha","submissionTime":"...","creatorUid":"...","keywords":[],"expiration":-1}}],
 "nodeId":"..."}
```

* JSON keys from `MissionChange.java`: `type`, `timestamp`, `serverTime`, `missionName`, `missionGuid`, `isFederatedChange`, `contentUid`,
  `details`, `contentResource`, `logEntry`, `creatorUid`, plus `externalData`/`mapLayer`/`missionFeed`.
* `squashed=true` (the default) collapses an add followed by a remove (exact semantics **UNVERIFIED**).

**Logs:**

* `POST /Marti/api/missions/logs/entries` (`201`). Body (`LogEntry`, `id` must be absent):

```json
{"content":"Contact at grid 34U...","creatorUid":"ANDROID-0123456789abcdef","missionNames":["op-alpha"],"dtg":"2026-10-25T12:00:00.000Z","keywords":["contact"],"contentHashes":[]}
```

  The response is a single object, not an array:
  `{"version":"3","type":"com.bbn.marti.sync.model.LogEntry","data":{"id":"...","content":"...","creatorUid":"...","entryUid":"...","missionNames":["op-alpha"],"servertime":"...","dtg":"...","created":"...","contentHashes":[],"keywords":["contact"]},"nodeId":"..."}`.
  OTS wraps it in a list. A `t-x-m-c-l` notification follows.
* `GET /Marti/api/missions/{name}/log` returns `type` `com.bbn.marti.sync.model.LogEntry` with a `data` array.
* `PUT /missions/logs/entries` updates; `DELETE /missions/logs/entries/{id}` deletes; `GET /missions/all/logs` is admin-only.

**Invitations:**

* `GET /Marti/api/missions/invitations?clientUid=<uid>` (`clientUid` required):

```json
{"version":"3","type":"MissionInvitation","data":[{"missionName":"op-alpha","invitee":"ANDROID-0123456789abcdef","type":"clientUid","creatorUid":"ANDROID-9999","createTime":"2026-10-25T12:00:00.1Z","token":"eyJ...","missionGuid":"8f0e...","role":{"type":"MISSION_SUBSCRIBER","permissions":["MISSION_READ","MISSION_WRITE"]}}],"nodeId":"..."}
```

  The data is a Set: invitations for the clientUid, its callsign, its username and its groups (**UNVERIFIED** exact matching).
* `GET /Marti/api/missions/all/invitations?clientUid=<uid>` returns `{"version":"3","type":"MissionInvitation","data":["op-alpha","op-bravo"],...}`
  (mission **names** only).
* `GET /Marti/api/missions/{name}/invitations` returns that mission's invitation list.
* Invite:
  * `PUT /Marti/api/missions/{name}/invite/{type}/{invitee}?creatorUid=&role=`, where `type` is one of `clientUid|callsign|userName|group|team`;
  * or `POST /Marti/api/missions/{name}/invite` with a JSON array of `{"type":"clientUid","invitee":"ANDROID-x","role":{"type":"MISSION_SUBSCRIBER"}}`.
  * Both send `t-x-m-i` to connected invitees.
* `DELETE /missions/{name}/invite/{type}/{invitee}` uninvites.

Other mission endpoints present in TAK but likely optional:

* `PUT|DELETE /missions/{name}/password?password=`;
* `PUT /missions/{name}/expiration`;
* `GET /missions/{name}/token?password=` (ACCESS token; `201`, `{"type":"java.lang.String","data":"<jwt>"}`);
* `/missions/{name}/externaldata`, `/feed`, `/maplayers`, `/layers`, `/parent`, `/children`, `/kml`, `/contacts`, `/archive`, `/send`, `/copy`;
* `/pagedmissions`, `/missioncount`;
* all `/missions/guid/{guid}/...` twins.

### 4.11 Minimal subset

**ATAK Data Sync.** Derived from OTS docstrings plus TAK; the exact client call set is **UNVERIFIED**:

1. Mission list and metadata:
   * `GET /Marti/api/missions` (with `tool=public` default; honor `passwordProtected`, `defaultRole`);
   * `GET /Marti/api/missions/{name}`.
2. Lifecycle:
   * `PUT /Marti/api/missions/{name}` create;
   * `DELETE /Marti/api/missions/{name}`;
   * `PUT|DELETE /Marti/api/missions/{name}/password`.
3. Subscription and roles:
   * `PUT|DELETE /Marti/api/missions/{name}/subscription`;
   * `GET /.../subscriptions/roles`;
   * `PUT /.../role`.
4. Contents:
   * `GET /Marti/api/missions/{name}/cot`;
   * `PUT|DELETE /.../contents`;
   * `POST /Marti/sync/upload` + `GET /Marti/sync/content`;
   * `PUT /.../keywords`.
5. Changes, logs, invitations:
   * `GET /.../changes`, `GET /.../log`, `POST /missions/logs/entries`;
   * `GET /Marti/api/missions/invitations?clientUid=`, `POST /.../invite`, `PUT /.../invite/{type}/{invitee}`.
6. Streaming:
   * accept `<dest mission="...">`;
   * emit `t-x-m-c` (+ `-l`, `-k`), `t-x-m-n`, `t-x-m-d`, `t-x-m-i`, `t-x-m-r`.

**iTAK.** Secondary, via OTS:

* the same endpoints, with no Bearer token (authorize by cert and `creatorUid`);
* `PUT /contents` with `uids` instead of `<dest mission>`;
* `/Marti/sync/upload` for files and packages.
* Use of the `guid` endpoint variants is **UNVERIFIED** (OTS implements them for some client).

**TAKAware** (verified, `TAKAware/Data Models/TAKConstants.swift`, `Communications/DataSyncManager.swift`):

* `GET /Marti/api/missions?passwordProtected=true`;
* `GET /Marti/api/missions/{name}`, `GET /.../token`, `GET /.../cot`;
* `PUT /.../subscription?uid=[&password=]`, `DELETE /.../subscription?disconnectOnly=true&uid=`.

---

## 5. Gotchas checklist

1. **TAK `tcp` (8087) is receive-only; `stcp` is the bidirectional plaintext stream.** ATAK on a TCP stream uses
   **`http://host:8080/Marti`** with Basic auth for every REST call.
2. **Ping:** clients ping only after 15 s of receive silence, repeat every 4.5 s, and reconnect after 25 s.
   * Always pong. Only `type="t-x-c-t-r"` matters.
   * Never broadcast `t-x-c-t`, `t-x-takp-q` or the other control types.
3. **Without a `t-x-takp-v` announcement, every client stays on XML.**
   * commo gives up after 60 s and ignores later announcements; TAKAware never negotiates.
   * If you announce, you must speak protobuf after `status='true'`.
4. **Framing:** split on `</event>` and start parsing at `<event`.
   * Tolerate a leading `<?xml ...?>`, whitespace, and `<auth>...</auth>`.
   * On output, never put whitespace between `</event>` and the next `<?xml`.
5. **Disconnect:** send `t-x-d-d` with `<link uid=… type=… relation="p-p"/>`. ATAK ignores it if any of the three is empty.
   `<__forcedelete/>` would remove the marker instead of staling it.
6. **Latest-SA replay** on connect, on `groups/all?sendLatestSA=true`, and after active-group changes.
   OTS doesn't do it; TAK does when `latestSA` is enabled.
7. **groups/all:**
   * `type` must be exactly `com.bbn.marti.remote.groups.Group`;
   * `created` must be `yyyy-MM-dd`;
   * `bitpos` must be present and ≥ 0;
   * one bad element makes ATAK reject the whole list.
   * ATAK PUTs back `created` as epoch millis.
8. **clientEndPoints:**
   * `type` must be exactly `com.bbn.marti.remote.ClientEndpoint`;
   * `lastEventTime` must parse as `yyyy-MM-dd'T'HH:mm:ss.S'Z'`; use **exactly 3** fractional digits;
   * `lastStatus` must be `Connected` or `Disconnected`;
   * one bad element rejects the whole list.
9. **version/config:** the `type` must be `ServerConfig`. The legacy `/version` body must contain `TAK Server`
   (it is used only when `/version/config` returns 404).
10. **missionupload:**
    * multipart part `assetfile` (TAK also accepts `resource`);
    * the response body is a **text/plain URL** that commo copies verbatim into `b-f-t-r@senderUrl`;
    * it must be GET-able, e.g. `/Marti/sync/content?hash=<sha256>`;
    * data-package uid = SHA-256 computed by the server.
11. **missionquery:** `200` + URL, or **`404`**. commo uploads only on 404.
    The tool PUT (`private` for contact sends, `public` for server-only) controls visibility in `sync/search`.
12. **sync/search:**
    * keys are Metadata enum names with string values;
    * the body must contain `resultCount`;
    * ATAK needs `UID`, `Name`, `Hash`, `PrimaryKey`, `SubmissionDateTime`;
    * filter by `keywords` and `tool`.
13. **Mission roles** are `MISSION_OWNER`, `MISSION_SUBSCRIBER`, `MISSION_READONLY_SUBSCRIBER` (OTS's `MISSION_READ_ONLY` is wrong).
    Role JSON is `{type, permissions[]}`.
14. **The `API_VERSION` header** (missing means 2) controls whether the subscribe response embeds `mission`. Always embed it.
15. **Subscription DELETE** defaults to `disconnectOnly=true` (keep the role record). Don't require Bearer tokens:
    iTAK never sends them, and TAKAware sends them only for password-protected missions.
16. **`<dest mission>` CoT goes only to mission subscribers**, plus a `t-x-m-c` notification; there is no group broadcast.
    iTAK uses `PUT /contents {"uids":[...]}` instead.
17. **Mission JSON:**
    * TAKAware crashes if `name`, `description`, `creatorUid`, `createTime`, `passwordProtected`, `uids` or `contents` are missing or null;
    * `details.color` is a **string**;
    * dates use `.SSS` millis.
18. **Keywords PUT replaces** them in TAK (OTS appends). A log POST returns a single LogEntry object.
19. **OTS bugs not to copy:**
    * the ping echo instead of a pong;
    * `metadata/{hash}/tool` writing keywords;
    * the `activebits` stub;
    * `MISSION_READ_ONLY`;
    * the 4-digit fractions;
    * `missions` with no tool parameter returning nothing;
    * `sync/search` ignoring filters.
20. **TAK strips `<marti>`** before forwarding, honors `dest callsign="All Streaming"` as a broadcast, and returns `b-t-f-s` to the sender
    when a directed chat has no reachable recipient.
