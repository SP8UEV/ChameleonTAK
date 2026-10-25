# ChameleonTAK research spec: LoRa = Meshtastic ⇄ TAK bridge

Compiled 2026-10-26 from the primary sources below (proto files, firmware, the ATAK plugin, client
libraries and existing bridges were read directly; numbers are quoted from code, not from blog posts).
Anything not traced to a file is marked **UNVERIFIED**. Statements marked **INFERRED** follow from the
cited code but were not observed on hardware.

---

## 0. Sources and evidence legend

| Tag | Source | Revision read |
|---|---|---|
| **[PB]** | Meshtastic protobufs: https://github.com/meshtastic/protobufs (`meshtastic/mesh.proto`, `portnums.proto`, `atak.proto`, `mqtt.proto`, `channel.proto`, `config.proto`, `module_config.proto`) | `master` @ `569a2b3` (2026-10-21). GPL-3.0 |
| **[FW27]** | Meshtastic firmware, stable line 2.7.x: https://github.com/meshtastic/firmware | `master` @ `6d41e27` (2026-08-20), `version.properties` = 2.7.27. Latest non-prerelease tag: `v2.7.26.54e0d8d` |
| **[FW28]** | Meshtastic firmware, 2.8 line | `develop` @ `c39728e` (2026-10-25), `version.properties` = 2.8.2. Tags `v2.8.0.*` and `v2.8.1.8e6a88d` (2026-10-01) are still **prereleases** |
| **[PY]** | Meshtastic Python client: https://github.com/meshtastic/python (`stream_interface.py`, `serial_interface.py`, `tcp_interface.py`, `mesh_interface.py`, `__init__.py`) | `master` @ `0a18357` (2026-10-03) |
| **[DOCS]** | meshtastic.org: Client API https://meshtastic.org/docs/development/device/client-api/, MQTT https://meshtastic.org/docs/software/integrations/mqtt/, meshtasticd https://meshtastic.org/docs/software/linux/usage/, Apple TAK protocol https://meshtastic.org/docs/software/apple/developer/tak-protocol/ | read 2026-10-26 |
| **[PLUG]** | Official ATAK plugin: https://github.com/meshtastic/ATAK-Plugin (`MeshtasticReceiver.java`, `MeshtasticMapComponent.java`, `cot/CotEventProcessor.java`, `util/Constants.java`, `util/fountain/FountainPacket.java`) | `main` @ `784530f` (2026-06-04); latest release `1.1.42` (2026-03-31). GPL-3.0 |
| **[SDK]** | TAKPacket-SDK (TAKPacketV2 + zstd dictionaries): https://github.com/meshtastic/TAKPacket-SDK (`WIRE_FORMAT.md`, `dictionaries/`) | `main` @ `95ab3c2` (2026-10-25), `VERSION` = 0.9.3. GPL-3.0. Bindings: Kotlin, Swift, C#, Python, TypeScript; **no Go** |
| **[AND]** | Meshtastic-Android built-in TAK server module: https://raw.githubusercontent.com/meshtastic/Meshtastic-Android/main/core/takserver/README.md | read 2026-10-26 (README only) |
| **[OTS]** | OpenTAKServer: https://github.com/brian7704/OpenTAKServer (`controllers/meshtastic_controller.py`, `cot_parser/cot_parser.py`, `defaultconfig.py`) + docs https://docs.opentakserver.io/meshtastic.html | `master` @ `e71c74d` (2026-10-24). GPL-3.0 |
| **[GW]** | TAK_Meshtastic_Gateway (same author as OTS): https://github.com/brian7704/TAK_Meshtastic_Gateway | `master` @ `d1d70fd` (2026-04-03). GPL-3.0 |
| **[MSAT]** | meshsat (Go, Raspberry Pi, Meshtastic+TAK gateway): https://github.com/meshsat/meshsat (`go.mod`, `internal/transport/direct_mesh.go`) | `main` @ `755512f` (2026-10-24). GPL-3.0. Used as "someone already did this in Go" evidence only |
| **[KZ]** | klauspost/compress zstd: https://github.com/klauspost/compress/tree/master/zstd (`decoder.go`, `decoder_options.go`, `encoder.go`, `encoder_options.go`, `dict.go`) | `master` @ `7cfdd8f`, latest tag v1.20.3. BSD-3/MIT-style |

Short path prefixes: `FW:` = `firmware/src/...`, `PLUG:` = `app/src/main/java/com/atakmap/android/meshtastic/...`,
`OTS:` = `opentakserver/...`.

---

## 1. TL;DR

* "LoRa support" in the TAK world = **Meshtastic**. There are three wire paths on the mesh that matter:
  1. **Plain Meshtastic apps**: `TEXT_MESSAGE_APP` (1), `POSITION_APP` (3), `NODEINFO_APP` (4), `TELEMETRY_APP` (67).
  2. **TAK v1**: `ATAK_PLUGIN` (72) carrying `TAKPacket` (PLI + GeoChat only), plus `ATAK_FORWARDER` (257)
     carrying zlib-compressed CoT XML for everything else. Used by the current official ATAK plugin (1.1.x)
     and by firmware ≤ 2.7.x.
  3. **TAK v2**: `ATAK_PLUGIN_V2` (78) carrying `[flags byte][zstd-dict-compressed TAKPacketV2]`. Used by
     firmware ≥ 2.8.0 (still prerelease on 2026-10-26), the Meshtastic iOS app and the Meshtastic Android app's
     built-in TAK server.
* The simplest reliable integration for a Pi is a **Meshtastic node on USB serial**, spoken to with the
  **Meshtastic client (stream) API**: `0x94 0xC3 <len_hi> <len_lo> <protobuf>`, 115200 baud, handshake
  `ToRadio.want_config_id` → stream of `FromRadio` → `config_complete_id`. The **same code** works over
  TCP port **4403** (Wi-Fi ESP32 nodes, or `meshtasticd` running on the Pi itself with a LoRa HAT).
* Existing servers: OpenTAKServer bridges **only via MQTT** (RabbitMQ's MQTT plugin) and only PLI + GeoChat.
  TAK_Meshtastic_Gateway, meshsat, the Android/iOS apps and the ATAK plugin use the **client API** directly.
* LoRa is extremely slow: on the default `LONG_FAST` preset one PLI costs ~0.7–0.9 s of airtime **per hop**,
  and EU_868 nodes are capped at 10 % duty cycle. Forward **only PLI (throttled, latest-wins) and GeoChat**
  TAK→mesh; forward everything useful mesh→TAK (costs no airtime).
* Recommended plan (§10): Phase 1 = serial/TCP transport + plain-Meshtastic mapping + TAKPacket v1 (port 72)
  in both directions; Phase 2 = TAKPacketV2 (port 78) receive+send with `klauspost/compress/zstd`;
  Phase 3 (optional) = embedded MQTT broker for Wi-Fi gateways / virtual per-user node IDs.

---

## 2. How others integrate Meshtastic with TAK

| Project | Transport to mesh | Directions / payloads | Node ID → CoT UID / callsign | Channels / keys |
|---|---|---|---|---|
| **Official ATAK plugin** [PLUG] (runs on the phone, talks to the Meshtastic Android app via `IMeshService`, i.e. the client API under the hood) | phone ↔ node (BLE/USB/TCP via the Meshtastic app) | TAK→mesh: own PLI as `TAKPacket` on port 72 (rate-limited, default **300 s**, `PREF_PLI_RATE_VALUE`); All-Chat + DM + receipts as `TAKPacket.chat` on 72; all other CoT as zlib XML on 257 (single packet if < 233 B, else fountain-coded). mesh→TAK: 72, 257, plus plain nodes from NodeInfo/Position, plus `TEXT_MESSAGE_APP` → GeoChat | Plain node: UID = `User.id` (e.g. `!a1b2c3d4`), type **`a-f-G-E-S`**, callsign = `long_name` (a trailing `" -N"` picks team colour N), stale 10 min. ATAK user: UID = `Contact.device_callsign` (= the ATAK UID), type `a-f-G-U-C`, how `m-g`, stale 10 min | Uses channel index from prefs (default 0), hop limit default 3, optional extra AES-256-GCM layer on 257 (marker byte `0xEE`) |
| **Meshtastic-Android `:core:takserver`** [AND] | app ↔ node | Local mTLS TAK server on **8089** inside the Android app; mesh 72/78 → all TAK clients; TAK PLI → mesh; optional "mesh nodes as CoT" (`MESH_TO_COT`, default off) | Mesh nodes: UID **`MESHTASTIC-%08X`** (upper-case hex, "ATAK compares UIDs case-sensitively"), callsign `SHORT - Long Name`, team Green / Team Member, stale **15 min**, re-sent every **5 min**, only nodes heard within **2 h** with a valid position; remarks `Battery … \| Voltage … \| Chan Util … \| Air Util Tx … \| RSSI … \| SNR …` | — |
| **Meshtastic iOS app** [DOCS Apple TAK protocol] | app ↔ node | Built-in TAK server for iTAK; send path picks **V2 if radio firmware ≥ 2.8.0**, else V1 (72 for PLI/chat, 257 for the rest); max wire payload **225 B** (`maxWirePayloadBytes`), drops `<remarks>` on overflow; offline queue 50 entries / 5 min TTL | — | — |
| **OpenTAKServer** [OTS] | **MQTT only**: Meshtastic gateway node (Wi-Fi) connects to OTS's RabbitMQ MQTT plugin; root topic `OTS_MESHTASTIC_TOPIC` = `opentakserver`; docs say TLS on, MQTT "Encryption" off | mesh→TAK: POSITION/MAP_REPORT → SA, NODEINFO, TELEMETRY (battery), TEXT → GeoChat (All Chat Rooms or DM), ATAK_PLUGIN 72 (unishox2-decompressed). TAK→mesh: every EUD's PLI at most every `OTS_MESHTASTIC_PUBLISH_INTERVAL` = **30 s** as NODEINFO (every 3rd time, `OTS_MESHTASTIC_NODEINFO_INTERVAL = 3`) + POSITION + compressed TAKPacket; GeoChat as TEXT and/or TAKPacket. Docs: "Only PLI and GeoChat CoT messages are published to Meshtastic devices" | Plain node UID = **`%08x`** (no `!`), callsign `long_name` (fallback `Meshtastic <last4>`), stale **1 day**, `takv platform="Meshtastic"`, `contact endpoint="MQTT"`. Each TAK EUD gets a **random virtual Meshtastic node number** (`EUD.meshtastic_id`) used as `MeshPacket.from` in MQTT downlinks | Channel list with name + PSK in DB; `AQ==` expanded to the full default key; AES-CTR decrypt of `/2/e/` packets; publishes **unencrypted** (decoded) `ServiceEnvelope`s with `gateway_id="OpenTAKServer"`, `hop_limit=3`, `want_ack=false` to `<root>/2/e/<channel>/outgoing` for every downlink channel |
| **TAK_Meshtastic_Gateway** [GW] | Python `meshtastic` lib: USB serial (`/dev/ttyACM0`, needs root/dialout) or TCP (`--mesh-ip`) | Multicast SA (`239.2.3.1:6969`) and GeoChat (`224.10.10.1:17012`) ↔ mesh; PLI + chat only; TX interval default **30 s** | Same mapping code as OTS; handles both compressed and uncompressed TAKPackets. Recommends node role **TAK**; sets the node's long name to the TAK callsign and short name to the last 4 chars of the UID | — |
| **meshsat** [MSAT] (Go, Raspberry Pi) | `go.bug.st/serial` + hand-rolled protowire, BSR module `buf.build/gen/go/meshtastic/protobufs/protocolbuffers/go`, `klauspost/compress` | Gateway between Meshtastic, Iridium, SMS, APRS, TAK | — | heartbeat every **300 s** (`meshHeartbeatPeriod`) |
| **FreeTAKServer** | No native integration found; `FreeTAKTeam/FreeTAKHub_meshtastic` = Node-RED + MQTT (**UNVERIFIED**, not read) | — | — | — |
| **TAK Server (official)** | Third-party plugin `niccellular/TAKServer-Meshtastic-Bridge` exists (**UNVERIFIED**, not read) | — | — | — |
| **taky** | No Meshtastic integration found (**UNVERIFIED**) | — | — | — |

Take-aways for ChameleonTAK:

* Nobody bridges anything other than **PLI + GeoChat** server-side; markers/routes/data packages are left to
  the phone plugin (257/78) because of bandwidth.
* UID conventions differ (`!a1b2c3d4` plugin+firmware, `MESHTASTIC-A1B2C3D4` Android, `a1b2c3d4` OTS).
  ChameleonTAK should use **`!%08x`** (lower-case, with `!`), because that is exactly what the ATAK plugin
  ([PLUG] `MeshtasticReceiver.java:670`, `cotEvent.setUID(ni.getUser().getId())`) and the TAK_TRACKER
  firmware role ([FW28] `PositionModule.cpp`, `snprintf(takPacket.uid, …, "!%08x", nodeNum)`) emit. A plugin
  phone that relays mesh nodes to our server will then update the **same** marker instead of creating a duplicate.

---

## 3. Meshtastic client API (USB serial / TCP 4403)

### 3.1 Stream framing [DOCS client-api, PY `stream_interface.py`]

```
+------+------+--------+--------+----------------------------+
| 0x94 | 0xC3 | len_hi | len_lo | protobuf (len bytes)        |
+------+------+--------+--------+----------------------------+
START1 START2  big-endian uint16      ToRadio (host→radio) or FromRadio (radio→host)
```

* `START1 = 0x94`, `START2 = 0xC3`, `HEADER_LEN = 4`, `MAX_TO_FROM_RADIO_SIZE = 512` [PY].
  A length > 512 means corruption: drop and resume scanning for `0x94` [DOCS].
* Any byte seen while hunting for `START1` is **debug console text** (firmware log lines on serial). Log it
  at debug level, do not treat it as an error.
* No CRC; the stream is assumed reliable and resynchronises on the start bytes [DOCS].
* **Wake-up** before the handshake: send **32 × `0xC3`** (START2), then sleep 100 ms, "to force a sleeping
  device to wake, and … resync" [PY `stream_interface.py:83-89`].
* Host→radio is only `ToRadio`; radio→host is only `FromRadio` (+ log text).
* TCP uses the identical framing on port **4403** (`DEFAULT_TCP_PORT = 4403` [PY], `SERVER_API_DEFAULT_PORT 4403`
  [FW28 `mesh/api/ServerAPI.h`]). `meshtasticd` (Linux-native firmware for SPI LoRa HATs on a Pi) also listens on
  TCP 4403 [DOCS meshtasticd].

### 3.2 Serial settings and Linux device

* **115200 baud**, 8N1 [PY `serial_interface.py:69`: `serial.Serial(dev_path, 115200, exclusive=True, timeout=0.5, write_timeout=0)`].
* Python first disables **HUPCL** (termios) "so the device will not reboot based on RTS and/or DTR"
  [PY `serial_interface.py:_set_hupcl_with_termios`]. ESP32 boards with an auto-reset circuit reboot if DTR/RTS
  are toggled. In Go (`go.bug.st/serial`) open with a fixed `Mode{BaudRate:115200}` and set
  `InitialStatusBits` so DTR and RTS are not pulsed (**UNVERIFIED** which exact combination is safest per board;
  test on T-Beam/Heltec/RAK).
* Device nodes (from [PY] `supported_device.py`): CP210x (VID `10c4`) and old CH340 (`1a86`) bridges show as
  **`/dev/ttyUSB*`**; nRF52 (RAK4631, T-Echo, VID `239a`), ESP32-S3 native USB (`303a`) and newer CH9102 boards
  show as **`/dev/ttyACM*`**. Use the stable symlinks in **`/dev/serial/by-id/`** in the panel, and show VID:PID.
  [PY] auto-detection whitelists VIDs `0x239a` (RAK4631), `0x303a` (Heltec/Espressif) and blacklists
  `0x1366, 0x0483, 0x1915, 0x0925, 0x04b4`.
* Permissions: serial devices are `root:dialout 0660` on Debian/Raspberry Pi OS. The service runs as
  `User=chameleontak` (`packaging/systemd/chameleontak.service`), so the image needs
  `usermod -aG dialout chameleontak` in `packaging/lib/setup-system.sh` (or `SupplementaryGroups=dialout` in the
  unit). TAK_Meshtastic_Gateway's README says it must run as root for USB [GW] – that is just missing dialout.
* If ModemManager is installed it may probe new `ttyACM` devices; a udev rule with
  `ENV{ID_MM_DEVICE_IGNORE}="1"` for the Meshtastic VIDs avoids that (**UNVERIFIED** whether RPi OS Lite ships
  ModemManager).
* Serial API inactivity: the firmware drops back from protobuf mode after **15 min** without traffic
  (`SERIAL_CONNECTION_TIMEOUT (15 * 60) * 1000UL`, [FW28 `SerialConsole.cpp:42`]). Send
  `ToRadio{heartbeat: Heartbeat{nonce}}` periodically; [PY] and [MSAT] use **300 s**.

### 3.3 Handshake [PB `mesh.proto`, PY `mesh_interface.py:_startConfig`, FW `mesh/PhoneAPI.h`]

1. Open port, send wake bytes (§3.1).
2. Send `ToRadio{ want_config_id = N }` with a random `uint32 N`.
   Special values [FW `PhoneAPI.h:28-29`]: `69420` = `SPECIAL_NONCE_ONLY_CONFIG` (config without node DB),
   `69421` = `SPECIAL_NONCE_ONLY_NODES`. [PY] avoids accidentally using 69420. Use a normal random nonce to get both.
3. Read `FromRadio` until `config_complete_id == N`. On the way you get (order not guaranteed [DOCS]):
   `my_info` (`MyNodeInfo.my_node_num`), `metadata` (`DeviceMetadata`, firmware version), many `node_info`
   (`NodeInfo{num, user, position, snr, last_heard, device_metrics, channel, via_mqtt, hops_away, …}`),
   `channel` (8×, `Channel{index, settings{psk, name, id, uplink_enabled, downlink_enabled, module_settings{position_precision}, use_aead}, role}`),
   `config` (`Config` incl. `lora.region`, `lora.modem_preset`, `device.role`), `moduleConfig`, possibly
   `queueStatus`, `log_record`, `clientNotification`.
4. Afterwards every received mesh packet arrives as `FromRadio.packet` (a decoded `MeshPacket`), plus
   `queueStatus` after each send, `node_info` updates, `rebooted=true` if the radio reboots (then redo the
   handshake), `clientNotification` for user-visible errors.
5. Start the heartbeat timer. To leave cleanly send `ToRadio{disconnect: true}`.

Messages (field numbers from [PB `mesh.proto`]):

```proto
message ToRadio { oneof payload_variant {
  MeshPacket packet = 1; uint32 want_config_id = 3; bool disconnect = 4;
  XModem xmodemPacket = 5; MqttClientProxyMessage mqttClientProxyMessage = 6; Heartbeat heartbeat = 7; } }
message Heartbeat { uint32 nonce = 1; }
message FromRadio { uint32 id = 1; oneof payload_variant {
  MeshPacket packet = 2; MyNodeInfo my_info = 3; NodeInfo node_info = 4; Config config = 5;
  LogRecord log_record = 6; uint32 config_complete_id = 7; bool rebooted = 8; ModuleConfig moduleConfig = 9;
  Channel channel = 10; QueueStatus queueStatus = 11; XModem xmodemPacket = 12; DeviceMetadata metadata = 13;
  MqttClientProxyMessage mqttClientProxyMessage = 14; FileInfo fileInfo = 15;
  ClientNotification clientNotification = 16; DeviceUIConfig deviceuiConfig = 17;
  LockdownStatus lockdown_status = 18; LoRaRegionPresetMap region_presets = 19; } }
message QueueStatus { int32 res = 1; uint32 free = 2; uint32 maxlen = 3; uint32 mesh_packet_id = 4; }
```

### 3.4 MeshPacket and Data [PB `mesh.proto`]

```proto
message MeshPacket {
  fixed32 from = 1;   fixed32 to = 2;   uint32 channel = 3;          // channel = local channel INDEX (0..7)
  oneof payload_variant { Data decoded = 4; bytes encrypted = 5; }
  fixed32 id = 6;  optional fixed32 rx_time = 7;  float rx_snr = 8;  uint32 hop_limit = 9;
  bool want_ack = 10;  Priority priority = 11;  optional int32 rx_rssi = 12;  bool via_mqtt = 14;
  uint32 hop_start = 15;  bytes public_key = 16;  bool pki_encrypted = 17;  uint32 next_hop = 18;
  uint32 relay_node = 19;  uint32 tx_after = 20;  TransportMechanism transport_mechanism = 21; … }
message Data {
  PortNum portnum = 1;  bytes payload = 2;  bool want_response = 3;  fixed32 dest = 4;  fixed32 source = 5;
  fixed32 request_id = 6;  fixed32 reply_id = 7;  fixed32 emoji = 8;  optional uint32 bitfield = 9;  bytes xeddsa_signature = 10; }
// Priority: UNSET 0, MIN 1, BACKGROUND 10, DEFAULT 64, RELIABLE 70, RESPONSE 80, HIGH 100, ALERT 110, ACK 120, MAX 127
// Constants.DATA_PAYLOAD_LEN = 233  (bytes inside Data.payload; the 16-byte LoRa header is outside)
```

* Broadcast address `to = 0xFFFFFFFF` (`BROADCAST_NUM`, [PY `__init__.py:114`]).
  Node IDs are displayed as `!%08x` (`User.id` comment: "`!<8 hexidecimal bytes>`" [PB]).
* **The firmware overwrites `from`** on anything the client sends: `p.from = 0; // We don't let clients assign
  nodenums to their sent messages`, and assigns `id` if 0 [FW27 `mesh/MeshService.cpp:188-193`]. Over serial/TCP
  **every packet we send appears to come from the gateway node**. Per-TAK-user identity on the mesh is only
  possible inside the payload (`TAKPacket.contact`) or via MQTT downlink (§7).
* Generate our own `id` (random uint32) so we can match `QueueStatus.mesh_packet_id` and `Routing` ACKs
  (`Data.request_id == our id`, `portnum = ROUTING_APP (5)`, `Routing.error_reason`; useful errors:
  `DUTY_CYCLE_LIMIT = 9`, `TOO_LARGE = 7`, `RATE_LIMIT_EXCEEDED = 38`, `MAX_RETRANSMIT = 5`, `NO_CHANNEL = 6`).
* Default hop limit is 3 ([PLUG] pref default 3, [OTS] `hop_limit = 3`). Leave `hop_limit = 0` to use the
  node's configured `lora.hop_limit` (**UNVERIFIED** that 0 means "use default" for client-sent packets; [PY]
  only sets it when given).
* One radio, **one client**: all API transports share a single `MeshService::toPhoneQueue` [FW27
  `MeshService.cpp:70`], so if the Meshtastic phone app is connected to the same node over BLE at the same time,
  the two clients steal each other's packets (INFERRED from the shared queue). The gateway node must be
  dedicated to the server.

### 3.5 Sending the usual payloads

All of these are `ToRadio{ packet: MeshPacket{ to, channel=<index>, id=<rand>, want_ack, hop_limit, decoded: Data{ portnum, payload } } }`.

| What | portnum | payload |
|---|---|---|
| Text (channel broadcast) | `TEXT_MESSAGE_APP = 1` | UTF-8 bytes, `to = 0xFFFFFFFF`, ≤ 233 B (keep ≤ ~200 to leave room) |
| Text DM | `TEXT_MESSAGE_APP = 1` | `to = <node num>`, `want_ack = true`. The firmware PKI-encrypts DMs when it knows the peer's `public_key` (**UNVERIFIED** exact rules) |
| Position | `POSITION_APP = 3` | `Position{latitude_i, longitude_i (sfixed32, deg×1e7), altitude (int32 m MSL), time, …}` – but over serial this **moves the gateway node** on everybody's map; do not use it for TAK users |
| Node info | `NODEINFO_APP = 4` | a `User` message (not `NodeInfo`!) – OTS comment: "The Meshtastic firmware expects a User payload when the Portnum is NodeInfo" |
| TAK v1 | `ATAK_PLUGIN = 72` | `TAKPacket` (§4.1), send `is_compressed = false`; firmware 2.7 compresses it for the air |
| TAK v2 | `ATAK_PLUGIN_V2 = 78` | `[flags][zstd body]` (§4.2) |
| CoT XML | `ATAK_FORWARDER = 257` | zlib-compressed CoT XML (§4.3) |

### 3.6 Reading nodes and positions

* Node DB: `FromRadio.node_info` → `NodeInfo.num`, `NodeInfo.user{ id ("!a1b2c3d4"), long_name, short_name, hw_model, role, public_key }`,
  `NodeInfo.position`, `NodeInfo.last_heard` (epoch s), `device_metrics{battery_level, voltage, channel_utilization, air_util_tx}`,
  `hops_away`, `via_mqtt`.
* Live updates arrive as `FromRadio.packet` with `decoded.portnum = NODEINFO_APP` (payload `User`),
  `POSITION_APP` (payload `Position`), `TELEMETRY_APP` (payload `Telemetry`, `device_metrics`).
* `Position` [PB]: `latitude_i`/`longitude_i` (`optional sfixed32`, deg×1e7), `altitude` (`optional int32`,
  "meters above MSL"), `altitude_hae` (`optional sint32`), `time`/`timestamp` (`fixed32` epoch s),
  `ground_speed` (`optional uint32`, **km/h** – firmware fills it from `reader.speed.kmph()` [FW28 `gps/GPS.cpp:2402`]),
  `ground_track` (`optional uint32`, **deg × 1e5** – [FW28 `PositionModule.cpp`: "ground_track is stored as degrees * 1e5"]),
  `precision_bits` (32 = exact; lower = deliberately truncated position), `sats_in_view`, `PDOP/HDOP`.
* A position with `latitude_i == 0 && longitude_i == 0` means "no fix" in practice ([OTS] skips POSITION with
  `latitude_i == 0`, [PLUG] has a "Hide devices without GPS" option).

---

## 4. TAK-specific payloads on the mesh

### 4.1 V1: `ATAK_PLUGIN = 72`, `TAKPacket` [PB `atak.proto`]

```proto
message TAKPacket {
  bool is_compressed = 1;  Contact contact = 2;  Group group = 3;  Status status = 4;
  oneof payload_variant { PLI pli = 5; GeoChat chat = 6; bytes detail = 7; } }
message Contact { string callsign = 1; string device_callsign = 2; }
message Group   { MemberRole role = 1; Team team = 2; }
message Status  { uint32 battery = 1; }
message PLI     { sfixed32 latitude_i = 1; sfixed32 longitude_i = 2; int32 altitude = 3; uint32 speed = 4; uint32 course = 5; }
message GeoChat { string message = 1; optional string to = 2; optional string to_callsign = 3;
                  string receipt_for_uid = 4; ReceiptType receipt_type = 5;   // None 0, Delivered 1, Read 2
                  optional string lang = 6; optional string room_id = 7; optional string voice_profile_id = 8; }
enum Team { Unspecifed_Color = 0; White = 1; Yellow = 2; Orange = 3; Magenta = 4; Red = 5; Maroon = 6; Purple = 7;
            Dark_Blue = 8; Blue = 9; Cyan = 10; Teal = 11; Green = 12; Dark_Green = 13; Brown = 14; }
enum MemberRole { Unspecifed = 0; TeamMember = 1; TeamLead = 2; HQ = 3; Sniper = 4; Medic = 5;
                  ForwardObserver = 6; RTO = 7; K9 = 8; }
```

How the string compression works (critical for a bridge):

* **Firmware 2.7.x** (`FW27 modules/AtakPluginModule.cpp`): a `TAKPacket` arriving **from the client**
  (`mp.from == 0`) is rewritten with `is_compressed = true` and Unishox2-compressed `contact.callsign`,
  `contact.device_callsign`, `chat.message`, `chat.to`, `chat.to_callsign` before going on air. A packet from the
  **air** must have `is_compressed = true` ("Received uncompressed TAKPacket over radio! Skip"); the firmware sends
  the phone a **decompressed copy** (`is_compressed = false`) via `service->sendToPhone(decompressedCopy)`, and
  because `handleReceivedProtobuf` returns `false` the original compressed packet also continues to the client
  (INFERRED: the client sees both). The ATAK plugin simply drops `is_compressed == true` packets
  ([PLUG] `MeshtasticReceiver.java`: `if (tp.is_compressed()) { … return; }`). **Bridge rule: over serial/TCP on
  2.7, accept only `is_compressed == false`, send `is_compressed == false`, no Unishox2 needed.**
* **Firmware 2.8** (`FW28 modules/AtakPluginModule.{h,cpp}`): the module now listens on port **78 only** and is a
  pure passthrough; Unishox2 was removed from the firmware tree. Port-72 packets are therefore delivered **as they
  were on air** (INFERRED): compressed if they came from a 2.7 node, uncompressed if from a 2.8 node's client.
  A bridge that must talk to mixed 2.7/2.8 meshes eventually needs a Unishox2 decoder (C original:
  siara-cc/Unishox2, Apache-2.0; no Go port found – **UNVERIFIED**). Phase 1 can ignore compressed packets.
* **MQTT** delivers the on-air form, so OTS always calls `unishox2.decompress(...)` on these fields [OTS].

ATAK plugin conventions on port 72 ([PLUG] `CotEventProcessor.java`, `MeshtasticMapComponent.java`, `MeshtasticReceiver.java`):

* **PLI**: `contact.callsign` = ATAK callsign, `contact.device_callsign` = **ATAK UID** (parsed from the event
  `uid` attribute), `group.role`/`group.team` from `__group` (default `TeamMember`/`White`), `status.battery`,
  `pli` lat/lon ×1e7, altitude int metres. Only the device's **own** SA is sent, at most every
  `PREF_PLI_RATE_VALUE` s (default **300**). Receiver builds `a-f-G-U-C`, how `m-g`, `uid = device_callsign`,
  `<contact callsign endpoint="0.0.0.0:4242:tcp"/>`, `<__group name role/>`, `<status battery/>`,
  `<track speed course/>`, `<__meshtastic/>`, stale **+10 min**.
  Note: the plugin constructs `new PLI(lat, lon, alt, data.course, data.speed, …)` while the proto order is
  `altitude, speed, course` – speed and course look **swapped** on the wire (and swapped back by the plugin on
  receive). Treat V1 speed/course as unreliable (INFERRED from Wire's declaration-order constructor).
* **All Chat Rooms**: `chat.message` = text, `chat.to = "All Chat Rooms"`, `contact.callsign` = sender callsign,
  `contact.device_callsign` = `"<senderUID>|<messageId>"` (message id "smuggled" after `|`, `MSG_ID_SEPARATOR`).
  Receiver rebuilds `b-t-f`, uid `GeoChat.<senderUID>.All Chat Rooms.<msgId>`, how `h-g-i-g-o`,
  `<__chat parent="RootContactGroup" groupOwner="false" messageId chatroom="All Chat Rooms" id="All Chat Rooms" senderCallsign>`
  `<chatgrp uid0=<senderUID> uid1="All Chat Rooms" id="All Chat Rooms"/>`, `<link uid type="a-f-G-U-C" relation="p-p"/>`,
  `<__serverdestination destination="0.0.0.0:4242:tcp"/>`, `<remarks source="BAO.F.ATAK.<senderUID>" to time>text</remarks>`, point 0,0.
* **DM**: `chat.to = <recipient ATAK UID>`; only the phone whose self-marker UID equals `to` shows it.
* **Receipts**: sent as a chat whose message is `ACK:D:<messageId>` (delivered) or `ACK:R:<messageId>` (read)
  (`handleChatReceiptPacket`). The newer proto fields `receipt_for_uid`/`receipt_type` exist but plugin 1.1.x
  uses the text form.
* **Plain-node text** (`TEXT_MESSAGE_APP` to `^all`): converted to All-Chat GeoChat with sender UID = known ATAK
  UID for that node or the node id, callsign = node `long_name`. **It is ignored if the sending node id is already
  known as an ATAK node** ("Skip TEXT_MESSAGE_APP from known ATAK users to avoid duplicates (they already send via
  ATAK_PLUGIN port 72)"). Consequence for us: once our gateway node has sent any `TAKPacket`, plugin users stop
  seeing our plain `TEXT_MESSAGE_APP` broadcasts – they must get chat as `TAKPacket.chat`, while Meshtastic-app users
  need `TEXT_MESSAGE_APP` (OTS sends both for All Chat Rooms for exactly this reason).
* Loop guard: everything the plugin injects into ATAK carries `<__meshtastic/>` in `<detail>`. The plugin can also
  relay mesh traffic to the TAK server ("Relay to Server", `PREF_PLUGIN_SERVER`) and server PLI/chat to the mesh
  ("Relay from Server", `PREF_PLUGIN_FROM_SERVER`); both default **off**. If a plugin user enables them while our
  bridge is running, traffic doubles – drop TAK→mesh events whose detail contains `__meshtastic`, and document that
  the relay options should stay off.

### 4.2 V2: `ATAK_PLUGIN_V2 = 78`, `TAKPacketV2` + zstd dictionary [PB `atak.proto`, SDK `WIRE_FORMAT.md`]

Wire layout (SDK wire format v1.2, "Stable"):

```
byte 0      : flags  = dictionary id in bits 0..5 (0x00 non-aircraft, 0x01 aircraft), bits 6..7 reserved = 0
              0xFF   = payload is a RAW TAKPacketV2 protobuf (no zstd)
bytes 1..N  : zstd frame of the serialized TAKPacketV2 WITH THE 4-BYTE MAGIC 28 B5 2F FD STRIPPED,
              frame header has dictID, contentSize and checksum DISABLED
total ≤ 237 bytes (iOS sender caps at 225)
```

* Decoding: length ≥ 2; if `flags == 0xFF` parse raw; else `dict = flags & 0x3F` (reject unknown), prepend
  `28 B5 2F FD`, zstd-decompress with that dictionary, **limit output to 4096 bytes**, parse `TAKPacketV2`.
* Encoding: build `TAKPacketV2`, choose dict by CoT type (third atom `A` → aircraft dict 0x01, else 0x00),
  compress (level 19 requested), strip magic; if the raw protobuf is not larger than the compressed body emit
  `0xFF + raw`.
* Dictionaries: `dictionaries/dict_non_aircraft.zstd` (524,288 B) and `dict_aircraft.zstd` (4,096 B) in the SDK
  repo (GPL-3.0); "All nodes on a mesh MUST use the same dictionary pair".
* Firmware 2.8 `TAK_TRACKER` role emits **`0xFF` + raw** V2 PLI: `cot_type_id = CotType_a_f_G_U_C`,
  `callsign = device_callsign = owner.long_name`, `uid = "!%08x"`, team/role from `ModuleConfig.TAKConfig{team=1, role=2}`
  (default Cyan / TeamMember), `how` `h-e` or `m-g`, `course = ground_track/1000` (deg×100), `speed =
  ground_speed` (km/h copied into a field documented as cm/s – firmware unit bug, INFERRED) [FW28 `PositionModule.cpp`].
  2.7 TAK_TRACKER sends a compressed V1 `TAKPacket` PLI on 72 with both callsigns = `owner.long_name` [FW27].

Key `TAKPacketV2` fields (full schema in `atak.proto`):
`cot_type_id = 1` (enum `CotType`, 126 values, `CotType_Other = 0` + `cot_type_str = 23`), `how = 2` (`CotHow`:
`h_e 1, m_g 2, h_g_i_g_o 3, m_r 4, m_f 5, m_p 6, m_s 7`), `callsign = 3`, `team = 4`, `role = 5`,
`latitude_i = 6`, `longitude_i = 7` (sfixed32 ×1e7), `altitude = 8` (sint32 m HAE, 9999999 = unknown),
`speed = 9` (cm/s), `course = 10` (deg×100), `battery = 11`, `geo_src = 12`, `alt_src = 13`, `uid = 14`,
`device_callsign = 15`, `stale_seconds = 16`, `tak_version/device/platform/os = 17..20`, `endpoint = 21`,
`phone = 22`, `remarks = 24`, `environment = 25`, `sensor_fov = 26`, `marti = 29` (`repeated string dest_callsign`),
oneof: **no variant = PLI**, `chat = 31` (GeoChat), `aircraft = 32`, `raw_detail = 33`, `shape = 34`,
`marker = 35`, `rab = 36`, `route = 37`, `casevac = 38`, `emergency = 39`, `task = 40`, `taktalk = 41`,
`taktalk_room = 42`.

Go/zstd specifics (from [KZ] source, interop **UNVERIFIED until tested against `testdata/golden/*.bin`**):

* Decoder: frames without a dictID have `DictionaryID == 0`; `setDict` looks up `dicts[0]`. `WithDecoderDicts`
  loads a trained dictionary under its **own** header ID and `loadDict` refuses ID 0 ("dictionaries cannot have ID 0");
  `WithDecoderDictRaw(0, content)` would drop the trained entropy tables. Robust option: after re-prepending the
  magic, **rewrite the frame header** – set `Dictionary_ID_flag` in the Frame_Header_Descriptor and insert the
  dictionary's real 4-byte ID after the window descriptor – then decode with `WithDecoderDicts(dict)` and
  `WithDecoderMaxMemory(4096)`.
* Encoder: `EncodeAll` always writes `DictID` and `ContentSize` (`encoder.go:769-773`). With
  `WithEncoderDict(dict)`, `WithEncoderCRC(false)`, `WithEncoderLevel(SpeedBestCompression)`: strip the magic,
  clear the DID flag and remove the 4 DID bytes. A present content-size field is legal zstd and costs 1–2 bytes.
* Validate against the SDK's 47 fixtures: `testdata/protobuf/*.pb` ↔ `testdata/golden/*.bin`
  (decode must round-trip; our encoder output need not be byte-identical, only decodable by the SDK).

### 4.3 `ATAK_FORWARDER = 257` [PLUG `MeshtasticMapComponent.java:750-815`, `MeshtasticReceiver.java:864-977`]

* Plain: `zlib(Deflater.DEFAULT_COMPRESSION)` of the CoT XML; sent as one packet if `< 233` bytes.
  Receiver also accepts raw-deflate (iOS) and raw XML.
* Large: fountain (LT) coding, packets start with magic `'F','T','N'`; data header 11 B
  (`MAGIC(3) + xfer(3) + seed(2) + K(1) + len(2)`), `MAX_PAYLOAD_SIZE = 214`; ACK `MFT`; requires `SHORT_TURBO`
  per the plugin README.
* `0xC2` first byte = Codec2 voice frame; `0xEE` first byte = plugin's optional AES-256-GCM layer (PSK in plugin prefs).
* Original encoding in portnums.proto comment is "libcotshrink" (atak-forwarder project); current plugin uses zlib.
* Recommendation: **receive-only, single-packet zlib** in a later phase (markers/routes from plugin phones);
  never transmit 257 from the server.

### 4.4 Version matrix (what a bridge must speak)

| Sender | Port | Payload |
|---|---|---|
| ATAK plugin 1.1.x on firmware 2.7 | 72 (PLI, chat), 257 (other CoT) | V1 `TAKPacket` (air: Unishox2 strings) / zlib XML |
| ATAK plugin 1.1.x on firmware 2.8 | 72 → passes through uncompressed (INFERRED) | V1 uncompressed |
| Meshtastic iOS / Android TAK server, radio ≥ 2.8.0 | 78 | V2 |
| Same apps, radio < 2.8.0 | 72 / 257 | V1 |
| Firmware role TAK_TRACKER 2.7 / 2.8 | 72 compressed / 78 `0xFF` raw | V1 / V2 |
| Plain Meshtastic users | 1, 3, 4, 67 | text, Position, User, Telemetry |

Bridge TX policy: **if the gateway's firmware ≥ 2.8.0 send V2 on 78, else V1 on 72** (same rule as the iOS app:
`supportsTAKv2` parses `DeviceMetadata.firmware_version` like `2.8.0.3a0c08b`). Always **receive** 72, 78 and
(later) 257.

---

## 5. Mapping tables

### 5.1 Mesh → CoT (into the ChameleonTAK router)

| Mesh input | CoT output |
|---|---|
| `NodeInfo`/`NODEINFO_APP` + last `Position` (plain node, not known as ATAK) | SA: `uid="!%08x"`, `type="a-f-G-E-S"` (plugin convention; configurable, e.g. `a-f-G-U-C`), `how="m-g"`, `point lat/lon` from ×1e-7, `hae` = `altitude_hae` if present else `altitude` (MSL, documented approximation) else `9999999.0`, `ce` from `precision_bits` (≈ ½·2^(32−bits)·1e-7·111 320 m; 32 → 9999999 unknown/GPS) **(approximation, UNVERIFIED formula)**, `le="9999999.0"`. Detail: `<contact callsign="<long_name>" endpoint="0.0.0.0:4242:tcp"/>` (endpoint for DM routing – see §5.3), `<uid Droid="<long_name>"/>`, `<__group name="<team>" role="Team Member"/>`, `<status battery="<device_metrics.battery_level>"/>`, `<track speed="<ground_speed km/h ÷ 3.6>" course="<ground_track ÷ 1e5>"/>`, `<takv platform="Meshtastic" device="<hw_model>" os="Meshtastic" version="<fw>"/>`, `<remarks>SNR/RSSI/hops/chan util</remarks>`, `<__meshtastic/>`. Stale 15 min, re-emit every 5 min while heard within 2 h (Android model) |
| `TELEMETRY_APP` `device_metrics` | update battery / chan util of that node (no CoT by itself) |
| `TEXT_MESSAGE_APP` to `0xFFFFFFFF` on the bridged channel | GeoChat `b-t-f` to All Chat Rooms, shape exactly as §4.1, sender UID `!%08x`, `senderCallsign` = `long_name` |
| `TEXT_MESSAGE_APP` DM to the gateway node | Proposal: `@CALLSIGN text` → GeoChat DM to that TAK user (`chatroom` = recipient callsign, `<marti><dest callsign/></marti>`), otherwise All Chat Rooms |
| Port 72 `pli` (uncompressed) | SA `a-f-G-U-C`, `uid = contact.device_callsign`, callsign = `contact.callsign`, team/role from `group`, battery from `status` |
| Port 72 `chat` | GeoChat: split `device_callsign` on `|` → (sender UID, messageId); `to == "All Chat Rooms"` → room, else DM to UID `to`; `ACK:D:`/`ACK:R:` → `b-t-f-d`/`b-t-f-r` receipts or drop |
| Port 78 | decompress → `TAKPacketV2` → CoT XML (port `CotXmlBuilder` logic from SDK for PLI + chat first; other variants later) |
| Port 257 (later) | zlib-inflate (cap size) → parse as CoT, add `<__meshtastic/>` |

All mesh-originated CoT goes into a configurable **TAK group** (default e.g. `Meshtastic` or `__ANON__`), using
`Router.Broadcast(raw, groups)` (`internal/stream/router.go:355`), and is remembered for SA replay to newly
connected clients.

### 5.2 CoT → mesh (from `Hooks.OnEvent`)

| TAK event | Mesh output |
|---|---|
| SA `a-f-*` of a connected user in the bridged group(s) | V1: `TAKPacket{is_compressed=false, contact{callsign, device_callsign=<UID>}, group{role, team}, status{battery}, pli{lat_i, lon_i, alt, speed, course}}` on 72. V2: `TAKPacketV2` PLI on 78. Never `POSITION_APP` over serial (it would move the gateway). Throttled per §8 |
| GeoChat `b-t-f` to All Chat Rooms | (a) for plugin users: `TAKPacket.chat{message, to="All Chat Rooms"}`, `contact.callsign` = sender callsign, `device_callsign = "<senderUID>|<messageId>"`; (b) for Meshtastic-app users: `TEXT_MESSAGE_APP` `"<callsign>: <text>"` broadcast. Panel option "chat format": plugin / app / both (both = 2× airtime) |
| GeoChat DM to a plugin user (UID known from mesh 72) | `TAKPacket.chat{to=<UID>, to_callsign}`, `to = 0xFFFFFFFF` at mesh level (the plugin filters by UID) |
| GeoChat DM to a plain mesh node (`!xxxxxxxx` contact) | `TEXT_MESSAGE_APP` with `to = <node num>`, `want_ack = true`; report ACK/NAK back as `b-t-f-d` (optional) |
| Anything with `<__meshtastic/>` in detail, ping, `t-x-*`, markers, routes, data packages | not forwarded |

### 5.3 DMs from TAK users to mesh nodes

For ATAK to offer "send message" to a mesh marker the SA needs `<contact endpoint=…/>`; the plugin uses
`0.0.0.0:4242:tcp` and the router can deliver DMs addressed (`<marti><dest callsign|uid>`) to a mesh UID by
intercepting them in the bridge (UID prefix `!`). ChameleonTAK's router already has `deliverDirect`; the bridge
must register mesh UIDs as virtual recipients (**design work**, not a protocol question).

---

## 6. Channels and encryption

* A node has up to 8 channels (`Channel.index` 0..7, `role` PRIMARY/SECONDARY/DISABLED). The API addresses them by
  **index** (`MeshPacket.channel`); on air only a 1-byte hash travels [PB `mesh.proto` comment on `channel`].
* `ChannelSettings.psk` [PB `channel.proto`]: 0 bytes = no crypto, 16 = AES-128, 32 = AES-256; 1-byte shorthand:
  `0` = none, **`1` = default key `d4 f1 bb 3a 20 29 07 59 f0 bc ff ab cf 4e 69 01`** (base64 `AQ==` →
  `1PG7OiApB1nwvP+rz05pAQ==`), `2..10` = default key with 1..9 added to the last byte. An empty channel `name` on
  the primary channel displays as the modem preset name (`LongFast`) [FW `Channels.cpp:getName`].
* Channel crypto = **AES-CTR**, no MAC. Nonce (16 B) = `packetId` as little-endian uint64 (bytes 0–7) +
  `fromNode` little-endian uint32 (bytes 8–11) + zeros [FW28 `CryptoEngine.cpp:initNonce`]. Go:
  `cipher.NewCTR(aes.NewCipher(key), nonce)`. OTS does the same with `id.to_bytes(8,"little") + from.to_bytes(8,"little")`.
  New experimental `ChannelSettings.use_aead = 8` (AES-CCM with 12-byte tag) rejects CTR packets – unsupported by
  our MQTT path until needed.
* **Over serial/TCP the bridge never sees ciphertext**: the node decrypts/encrypts with its own channel keys.
  Channel keys matter only for the MQTT path (§7) or for displaying warnings.
* Panel warning: if the bridged channel's PSK is empty, `AQ==`/1-byte default, every Meshtastic user in range
  (and the public MQTT map if any node uplinks) can read TAK positions and chat. Recommend a **private secondary
  channel with a 32-byte PSK** shared with field users (QR from the Meshtastic app). TAK traffic on index 0 is the
  plugin's default (`PREF_PLUGIN_CHANNEL` = 0) – the channel index must match on all plugin phones.
* DMs can be PKI-encrypted (Curve25519) end to end; MQTT shows them under channel id `PKI` [FW28 `mqtt/MQTT.cpp`].

---

## 7. MQTT option

### 7.1 Protocol facts

* Topics [FW28 `MQTT.cpp:reinitTopics`, DOCS]: `cryptTopic = <root> + "/2/e/"`, `mapTopic = <root> + "/2/map/"`;
  a node publishes `<root>/2/e/<channelId>/<!gatewayId>` and subscribes to `<root>/2/e/<channelId>/+` for every
  channel with `downlink_enabled`, plus `<root>/2/e/PKI/+`. Default root `msh` (`default_mqtt_root`), with region
  appended when the default is used (`msh/EU_868`, `applyRegionRootTopic`). Firmware < 2.3.0 used `/c/` instead of `/e/` [DOCS].
* Payload = `ServiceEnvelope{ MeshPacket packet = 1; string channel_id = 2; string gateway_id = 3; }` [PB `mqtt.proto`].
  `encryption_enabled = true` (default, `default_mqtt_encryption_enabled true`) → `packet.encrypted` (AES-CTR with
  the channel key, §6); false → `packet.decoded`.
* **JSON is gone in 2.8**: `MQTTConfig.json_enabled` is `deprecated = true, deprecated_since: "2.8.0"`, comment
  "JSON packet support on MQTT was removed, and this field is ignored" [PB]; [FW28] `MQTT.cpp` has no JSON code,
  while [FW27] still has `jsonTopic` (`/2/json/`, never on nRF52 [DOCS]). Use protobuf only.
* Downlink acceptance [FW28 `MQTT.cpp:onReceive`]: `channel_id` must equal the channel's global id **and** that
  channel must have `downlink_enabled`; envelopes whose `gateway_id` is the node's own id are ignored; a **decoded**
  packet is dropped when the node has `encryption_enabled` ("Ignore decoded msg on MQTT, encryption enabled") and
  decoded `ADMIN_APP` is always dropped; decoded packets must pass the XEdDSA signature policy. Accepted packets are
  marked `via_mqtt = true` and are never re-uplinked.
* Uplink: only channels with `uplink_enabled`; other nodes' packets without the "OK to MQTT" bitfield are not
  uplinked to **public** servers (private addresses are exempt).
* Unlike serial, MQTT downlink lets us set **any `from`** → each TAK user can appear as its own Meshtastic node
  (OTS assigns a random `meshtastic_id` per EUD and sends `NODEINFO_APP` with `User{id:"!xxxxxxxx", long_name:callsign,
  short_name:uid[-4:], hw_model:PRIVATE_HW}` plus `POSITION_APP`). This is the one real advantage of MQTT.
* Defaults if left blank: `mqtt.meshtastic.org`, user `meshdev`, password `large4cats`, port 1883 / 8883 TLS
  [FW28 `mesh/Default.h`].

### 7.2 Is an embedded broker on the Pi sensible?

Yes as an **optional** transport, not as the first one:

* Pros: no USB cable; the gateway node can be mounted outdoors on Wi-Fi (ChameleonTAK can run its own hotspot);
  multiple gateway nodes; per-TAK-user virtual node IDs on the mesh; a pure-Go broker (`github.com/mochi-mqtt/server/v2`,
  MIT, v2.7.9) embeds cleanly – no RabbitMQ needed.
* Cons: only Wi-Fi/Ethernet nodes (ESP32 family; nRF52 boards like RAK4631/T-Echo have no Wi-Fi); on ESP32 enabling
  Wi-Fi disables BLE (**UNVERIFIED** for all boards), so field users cannot also manage that node with the phone
  app; the bridge must handle AES-CTR and V1 **Unishox2** (on-air form); the node must be configured
  (MQTT address = Pi IP, root topic, username/password, `encryption_enabled`, per-channel uplink/downlink); duplicate
  packets from several gateways must be deduped by packet id [DOCS].
* A simpler alternative for the same Wi-Fi node is the **TCP client API on 4403** (§3), which reuses the serial code
  path exactly. Recommend TCP before MQTT.

---

## 8. Airtime, duty cycle and rate limits

Approximate LoRa time-on-air per **single transmission** (Semtech formula, preamble 16 symbols, explicit header,
CRC on, CR 4/5; total on-air bytes = 16-byte Meshtastic header + encrypted `Data`). Presets from [FW `MeshRadio.h`]
(`LONG_FAST` = SF11 / 250 kHz / CR 4/5); others from memory (**UNVERIFIED**):

| Preset | 60 B | 80 B | 120 B | 200 B | 255 B |
|---|---|---|---|---|---|
| LONG_FAST (SF11, 250 kHz) – default | 682 ms | 846 ms | 1133 ms | 1747 ms | 2157 ms |
| MEDIUM_FAST (SF9, 250 kHz) | 201 ms | 242 ms | 334 ms | 519 ms | 642 ms |
| SHORT_FAST (SF7, 250 kHz) | 61 ms | 76 ms | 104 ms | 163 ms | 204 ms |
| SHORT_TURBO (SF7, 500 kHz) | 30 ms | 38 ms | 52 ms | 81 ms | 102 ms |

Typical sizes: V1 PLI with a 24-char ATAK UID ≈ 60 B payload → ≈ 80–85 B on air ≈ **0.85 s** on LONG_FAST;
V2 PLI ≈ 42 B (SDK example) → ≈ 65 B on air ≈ **0.7 s**; a 100-char chat ≈ 1–1.2 s. Every relay hop retransmits
(hop limit 3 → up to ~4× channel time).

Regulatory/firmware limits: region table `RDEF(EU_868, 869.4f, 869.65f, 10 /*duty %*/, …, 27 dBm)`,
`RDEF(EU_433, …, 10, …)`, `RDEF(US, …, 100, …)` [FW27 `mesh/RadioInterface.cpp`] → an EU_868 node may transmit
**≤ 360 s per hour**; the firmware refuses with `Routing.Error.DUTY_CYCLE_LIMIT`. Poland uses EU_868.

Recommended bridge defaults (TAK→mesh only; mesh→TAK is free):

* **Forward only PLI and GeoChat.** No markers, routes, data packages, deletes, pings, `t-x-*`.
* **Chat first**: priority queue, chat never dropped (until the queue cap), PLI is **latest-wins per UID**
  (a newer SA replaces the queued one).
* **PLI per UID**: min interval **300 s** (= plugin default), send earlier only if moved > **50 m**; stationary
  units re-sent every **15 min**. Hard floor 60 s.
* **Global budget**: token bucket ≈ **1 packet / 20 s** (180/h ≈ 150 s airtime/h on LONG_FAST before relays),
  bursts of 3 for chat. Back off when `QueueStatus.free == 0`, on `DUTY_CYCLE_LIMIT`/`RATE_LIMIT_EXCEEDED`, or when
  the gateway's `device_metrics.channel_utilization` > 25 % (pause PLI) / > 40 % (pause everything but chat).
  The thresholds mirror firmware "polite" behaviour (**UNVERIFIED** exact firmware numbers).
* Text: max 200 UTF-8 bytes per packet; split longer chat into ≤ 2 parts, else truncate with "…".
* `want_ack`: false for broadcasts (PLI, All Chat Rooms), true for DMs to mesh nodes. Hop limit: node default (3).
* Show in the panel: queue length, packets/h, estimated airtime/h, last `channel_utilization` / `air_util_tx`.

---

## 9. Go building blocks

| Need | Choice | Module / license | Notes |
|---|---|---|---|
| Protobuf runtime | `google.golang.org/protobuf` | v1.36.12, BSD-3 | new dependency for ChameleonTAK |
| Meshtastic messages | **Generate ourselves** with `protoc-gen-go` from a pinned [PB] commit (only `mesh`, `portnums`, `atak`, `mqtt`, `channel`, `config`, `module_config`, `telemetry` + their imports) into `internal/mesh/meshpb`, override `go_package` with `M` flags | sources GPL-3.0 (see §11) | Alternative: BSR module `buf.build/gen/go/meshtastic/protobufs/protocolbuffers/go` (latest `v1.36.12-20261021110241-a5e47c4cd277.2`, used by meshsat), or hand-rolled `protowire` as meshsat does. The Go proxy also has stale `github.com/meshtastic/protobufs v2.x+incompatible` – do not use; `atak.proto`'s `go_package github.com/meshtastic/go/generated` does not exist |
| Serial port | `go.bug.st/serial` | v1.8.0, BSD-3, pure Go on Linux | `serial.GetPortsList()` + `enumerator.GetDetailedPortsList()` for VID/PID |
| zstd with dictionaries | `github.com/klauspost/compress/zstd` | v1.20.3, BSD-3 | pure Go, no CGO (fits the "no CGO" rule in ARCHITECTURE.md); header rewrite trick in §4.2 |
| zlib (257) | stdlib `compress/zlib`, `compress/flate` | – | |
| AES-CTR (MQTT) | stdlib `crypto/aes`, `crypto/cipher` | – | |
| MQTT broker (optional) | `github.com/mochi-mqtt/server/v2` | v2.7.9, MIT | embedded, auth hook backed by our users |
| Unishox2 (only for MQTT / mixed 2.7–2.8 meshes) | port C `siara-cc/Unishox2` | Apache-2.0 | no Go port found (**UNVERIFIED**) |
| Existing Go Meshtastic libs (reference only) | `meshnet-gophers/meshtastic-go` (MIT, last push 2024-06), `lmatte7/meshtastic-go` (MIT CLI), meshsat (GPL-3.0) | | none is a maintained library worth depending on |

---

## 10. Recommended implementation plan

### 10.1 Package layout

```
internal/mesh/
  meshpb/        generated protobufs (pinned, with a regen script under scripts/)
  radio/         Transport interface { ReadFrom() (*FromRadio); Send(*ToRadio) }:
                 framing (0x94 0xC3), serial.go (go.bug.st/serial, 115200), tcp.go (4403),
                 handshake (want_config_id, wake bytes, heartbeat 300 s, reconnect with backoff, rebooted → re-handshake)
  nodedb/        nodes keyed by num: User, last Position, metrics, last_heard, hops, "is ATAK" (UID/callsign from 72/78)
  takv1/         TAKPacket <-> CoT (PLI, GeoChat incl. "uid|msgId" + ACK:D/R conventions)
  takv2/         flags byte + zstd (klauspost) + TAKPacketV2 <-> CoT (PLI + chat first)
  bridge/        stream.Hooks implementation: CoT->mesh queue (priority, latest-wins, token bucket),
                 mesh->CoT emitter (Router.Broadcast + SA cache + 5-min re-emit), loop guard (__meshtastic)
  mqtt/          (phase 3) embedded broker, ServiceEnvelope, AES-CTR, virtual node ids
```

### 10.2 Phases

1. **Phase 1 – USB serial + TCP, V1** (most likely deployment: a node plugged into the Pi, or `meshtasticd`+HAT
   on `localhost:4403`):
   * transport, handshake, node DB, panel status;
   * mesh→TAK: plain nodes as SA (`!%08x`), text → All Chat Rooms, port 72 PLI/chat (`is_compressed == false` only);
   * TAK→mesh: PLI as V1 TAKPacket (throttled), All-Chat as TAKPacket chat and/or TEXT_MESSAGE_APP, DMs to mesh nodes;
   * packaging: `dialout` group for `chameleontak`, optional udev rule; `/dev/serial/by-id` picker;
   * tests: framing fuzz test (garbage + log text between frames), fake radio over `net.Pipe`, golden CoT for each mapping.
2. **Phase 2 – V2 (port 78)**: decode always; encode when `DeviceMetadata.firmware_version ≥ 2.8.0`; embed the two
   SDK dictionaries; CI test against SDK golden fixtures; plus receive-only 257 single-packet zlib.
3. **Phase 3 (optional) – MQTT**: embedded broker on 1883 (LAN/hotspot only, firewall-closed to WAN), per-user virtual
   node ids, AES-CTR, Unishox2 port, multi-gateway dedupe by packet id.

Not planned: sending markers/routes/data packages over LoRa, fountain coding, Codec2 voice, BLE transport
(BlueZ/D-Bus is heavy and the Pi's BLE adds little over a USB cable).

### 10.3 Settings (stored with the rest of `config`) – proposal

```json
"lora": {
  "enabled": false,
  "transport": "serial",              // "serial" | "tcp" | "mqtt"
  "serialPort": "/dev/serial/by-id/usb-…", "tcpHost": "127.0.0.1:4403",
  "channelIndex": 0,
  "takGroup": "Meshtastic",           // TAK group for mesh-originated CoT and the source of TAK->mesh traffic
  "meshToTak": {"nodes": true, "text": true, "takPackets": true, "nodeCotType": "a-f-G-E-S", "nodeStaleMin": 15},
  "takToMesh": {"pli": true, "allChat": true, "dm": true, "chatFormat": "both", "pliIntervalSec": 300,
                "pliMinMoveM": 50, "maxPacketsPerHour": 180},
  "protocol": "auto"                  // "auto" (V2 if fw >= 2.8.0) | "v1" | "v2"
}
```

### 10.4 Web panel ("LoRa / Meshtastic", UI texts in Polish)

* Toggle **Włącz LoRa (Meshtastic)**; transport radio buttons **USB** / **Sieć (TCP)** / **MQTT (zaawansowane)**.
* USB: dropdown of detected ports (`/dev/serial/by-id/*`, VID:PID, guessed board) + "Odśwież"; TCP: host:port with
  `127.0.0.1:4403` hint for `meshtasticd`.
* Status card: connected / reconnecting / error; gateway `long_name` / `short_name` / `!id`, hardware, firmware
  version, region + modem preset, role (hint: `CLIENT` or `TAK`; warn on `ROUTER`/`CLIENT_MUTE`), protocol in use
  (V1/V2), channel utilisation and own airtime, queue length, packets/h.
* Channel select from the 8 channels read at handshake (name, role); **red warning** when its PSK is
  empty/default (`AQ==`), with a short explanation.
* What to bridge: checkboxes for mesh→TAK (węzły na mapie, czat z aplikacji Meshtastic, pakiety wtyczki ATAK) and
  TAK→mesh (pozycje PLI, czat "All Chat Rooms", wiadomości prywatne), chat format (wtyczka ATAK / aplikacja
  Meshtastic / oba), PLI interval and hourly budget with a live airtime estimate.
* Node list table (name, id, last heard, hops, SNR, battery, position, "ATAK user" flag) and a recent-traffic log;
  button **Wyślij wiadomość testową** (one broadcast text).
* REST: `GET/PUT /api/lora/settings`, `GET /api/lora/status`, `GET /api/lora/ports`, `GET /api/lora/nodes`,
  `POST /api/lora/test-message`; live updates over the existing panel WebSocket.
* Do not change radio configuration from the server in phase 1 (admin messages, owner name); just show warnings and
  link to the Meshtastic web client / app. TAK_Meshtastic_Gateway's trick of renaming the node to the TAK callsign
  only makes sense for single-user gateways.

---

## 11. Licensing note (decision needed)

The Meshtastic protobufs, the TAKPacket-SDK (incl. the zstd **dictionaries**), the ATAK plugin, OTS and
TAK_Meshtastic_Gateway are all **GPL-3.0**. Code generated from GPL `.proto` files and an embedded GPL dictionary are
plausibly derivative works; ChameleonTAK currently has no LICENSE file in the repo root. Either license ChameleonTAK
compatibly (GPL-3.0) or get a legal opinion before shipping Phase 1 (protos) and Phase 2 (dictionaries). Do not copy
code from the GPL projects into a non-GPL tree; this spec only describes wire formats. **UNVERIFIED** (not legal advice).

---

## 12. Open questions / to verify on hardware

1. Exact DTR/RTS handling in `go.bug.st/serial` that avoids resetting ESP32 boards on open/close.
2. Whether a 2.7 gateway really hands the client both compressed and decompressed copies of each port-72 packet
   (dedupe by `MeshPacket.id` either way).
3. Whether `hop_limit = 0` from a client means "use the node default".
4. klauspost zstd ⇄ SDK interop (header rewrite) against `testdata/golden`.
5. Whether ATAK 5.x accepts `a-f-G-E-S` mesh nodes as DM targets with endpoint `0.0.0.0:4242:tcp` when routed
   through the server (needed for §5.3).
6. Behaviour with firmware 2.8 final (V2-only module, removed Unishox2) once it leaves prerelease.
7. RPi OS: ModemManager / brltty presence on the ChameleonTAK image.
