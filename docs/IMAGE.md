# ChameleonTAK — obraz Raspberry Pi, pierwsze uruchomienie i instalacja

Ten dokument opisuje gotowy obraz karty SD dla Raspberry Pi, pierwsze uruchomienie (kreator),
instalację na istniejącym Debianie / Raspberry Pi OS skryptem `install.sh`, samodzielne budowanie
obrazu oraz rozwiązywanie problemów. Architektura serwera: [ARCHITECTURE.md](ARCHITECTURE.md).

## 1. Co zawiera obraz

- **Raspberry Pi OS Lite 64-bit (arm64), Debian 13 „Trixie”** — zbudowany narzędziem
  [pi-gen](https://github.com/RPi-Distro/pi-gen) (gałąź `arm64`) z własnym etapem `image/stage-chameleontak`.
- **ChameleonTAK** — `/usr/local/bin/chameleontak`, usługa `chameleontak.service`
  (użytkownik systemowy `chameleontak`, dane w `/var/lib/chameleontak`). Startuje automatycznie
  w trybie kreatora (panel HTTP na porcie 80).
- **mDNS (avahi)** — Raspberry jest widoczne jako `chameleontak.local`, panel jest ogłaszany jako usługa `_http._tcp`.
- **Zapora nftables** — w trybie kreatora otwarte są tylko porty 80 i 443 (oraz DNS/DHCP/mDNS/ICMP).
- **Hotspot kreatora** — gdy przy starcie nie ma sieci, Raspberry samo tworzy Wi-Fi `ChameleonTAK-Setup`.
- Język polski (`pl_PL.UTF-8`), polska klawiatura, strefa czasowa `Europe/Warsaw`, kraj Wi-Fi `PL`.
- Konto systemowe `tak` / hasło `chameleontak`, **SSH wyłączone**.

Pliki dodane do systemu:

| Plik | Rola |
|---|---|
| `/usr/local/bin/chameleontak` | serwer (jedna binarka, panel WWW wbudowany) |
| `/etc/systemd/system/chameleontak.service` | usługa serwera (`serve --data /var/lib/chameleontak`) |
| `/etc/systemd/system/chameleontak-network-fallback.service` | jednorazowe sprawdzenie sieci przy starcie → hotspot kreatora |
| `/usr/local/lib/chameleontak/network-fallback.sh` | skrypt hotspotu kreatora |
| `/usr/local/lib/chameleontak/setup-system.sh` | konfiguracja systemu (użytkownik, sudoers, usługi) — używana przez obraz i `install.sh` |
| `/etc/sudoers.d/chameleontak` | użytkownik `chameleontak` może uruchamiać jako root **tylko** `chameleontak sys …` |
| `/etc/nftables.conf` | `flush ruleset` + `include "/etc/chameleontak/firewall.nft"` |
| `/etc/chameleontak/firewall.nft` | reguły zapory (po kreatorze generuje je serwer) |
| `/etc/avahi/services/chameleontak.service` | ogłoszenie panelu przez mDNS |
| `/etc/default/chameleontak` | ustawienia hotspotu kreatora (kraj Wi-Fi, SSID, hasło, czas oczekiwania) |
| `/etc/issue.d/chameleontak.issue` | adres panelu na ekranie logowania (monitor HDMI) |

## 2. Obsługiwany sprzęt

Obraz jest **64-bitowy** — działa na:

- Raspberry Pi **3B+**, **4B**, **5**,
- Raspberry Pi **Zero 2 W**,
- Raspberry Pi **400** i **500**.

Nie działa na Raspberry Pi 1, 2, Zero i Zero W (procesory 32-bitowe).

Zalecenia: karta microSD co najmniej 8 GB (lepiej 16 GB+, klasa A1/A2), oryginalny zasilacz.
Pi 3B+ i Zero 2 W mają tylko Wi-Fi 2,4 GHz — hotspot kreatora i tak działa na 2,4 GHz (kanał 6).
Zero 2 W nie ma gniazda Ethernet — pierwsze uruchomienie zwykle przez hotspot.

## 3. Pobranie i nagranie karty

1. Z zakładki **Releases** repozytorium pobierz `chameleontak-X.Y.Z-arm64.img.xz` (i opcjonalnie `SHA256SUMS`).
   Sprawdzenie sumy kontrolnej:
   - Linux/macOS: `sha256sum -c SHA256SUMS --ignore-missing`
   - Windows (PowerShell): `Get-FileHash .\chameleontak-X.Y.Z-arm64.img.xz -Algorithm SHA256`
2. Uruchom **Raspberry Pi Imager** (<https://www.raspberrypi.com/software/>):
   1. **Urządzenie** — wybierz swój model Raspberry Pi.
   2. **System operacyjny** — przewiń listę na sam dół i wybierz **„Use custom”** („Użyj własnego”),
      wskaż pobrany plik `.img.xz` (nie trzeba go rozpakowywać).
   3. **Pamięć** — wybierz kartę microSD.
   4. Jeśli Imager zaproponuje **dostosowanie systemu** (OS customisation: użytkownik, Wi-Fi, SSH) —
      **pomiń / wybierz „Nie”**. Konto, sieć i SSH ustawia się w kreatorze ChameleonTAK.
   5. Zapisz i poczekaj na weryfikację.
3. Alternatywy: balenaEtcher (wskaż `.img.xz`) albo na Linuksie:
   ```sh
   xzcat chameleontak-X.Y.Z-arm64.img.xz | sudo dd of=/dev/sdX bs=4M conv=fsync status=progress
   ```
   (`/dev/sdX` = karta SD — sprawdź dokładnie poleceniem `lsblk`, `dd` nadpisuje bez pytania).

## 4. Pierwsze uruchomienie

Włóż kartę i podłącz zasilanie. **Pierwszy start trwa dłużej (1–3 min)**: system powiększa partycję
na całą kartę (Raspberry może się przy tym raz samo zrestartować) i generuje klucze.

### 4.1 Z kablem sieciowym (zalecane, jeśli masz router)

1. Podłącz Raspberry kablem do routera / switcha z DHCP i włącz zasilanie.
2. Po 1–2 min otwórz w przeglądarce **<http://chameleontak.local>**.
   - `.local` działa w Windows 10/11, macOS, iOS, Linuksie (avahi); na Androidzie zależy od wersji.
   - Jeśli nie działa — otwórz **`http://<adres-IP-Raspberry>`**. Adres znajdziesz na liście klientów
     DHCP w routerze albo na ekranie monitora podłączonego do Raspberry (ekran logowania pokazuje adres panelu).

### 4.2 Bez kabla — hotspot kreatora `ChameleonTAK-Setup`

Jeśli w ciągu **~60 s** od startu Raspberry nie dostanie adresu IP (brak kabla i brak skonfigurowanego
Wi-Fi), samo uruchomi sieć Wi-Fi:

| | |
|---|---|
| Nazwa sieci (SSID) | **`ChameleonTAK-Setup`** |
| Hasło (WPA2) | **`chameleontak`** |
| Adres kreatora | **<http://10.42.0.1>** (także <http://chameleontak.local>) |

1. Odczekaj ok. 2 minuty od włączenia zasilania (pierwszy start + 60 s czekania na sieć).
2. Połącz telefon lub laptop z siecią `ChameleonTAK-Setup`.
3. Telefon zwykle **sam otworzy stronę logowania do sieci** z kreatorem (portal przechwytujący —
   każda nazwa DNS w tej sieci wskazuje na Raspberry). Jeśli nie — otwórz ręcznie <http://10.42.0.1>.
4. Telefon może zgłosić „brak dostępu do internetu” — wybierz **pozostań połączony**. Jeśli przeglądarka
   próbuje iść przez sieć komórkową, wyłącz na chwilę dane komórkowe.

Hotspot kreatora istnieje **tylko do ukończenia kreatora**. Potem serwer go usuwa (w trybie terenowym
kreator tworzy zamiast niego stały hotspot o nazwie i haśle podanych przez Ciebie). Jeśli w trakcie
podłączysz kabel, panel będzie dostępny jednocześnie przez kabel i przez hotspot.

### 4.3 Kreator

Kreator prowadzi przez: nazwę serwera (podpowiedź = wykryty adres IP), porty (wstępnie wypełnione),
login i hasło administratora panelu, platformy (ATAK / iTAK / WinTAK / TAKAware), tryb sieci
(LAN albo własny hotspot w terenie) oraz opcjonalnie Let's Encrypt i SSH. Przeglądarka przekazuje
swój czas, więc certyfikaty powstają z poprawną datą nawet bez internetu (Raspberry nie ma zegara RTC).
Po zakończeniu serwer otwiera w zaporze ustawione porty i uruchamia usługi TAK — bez restartu.

> **Ważne:** dopóki kreator nie zostanie ukończony, może go wypełnić każdy, kto ma dostęp do tej
> samej sieci (albo do hotspotu z domyślnym hasłem). Dokończ konfigurację od razu po uruchomieniu.

## 5. Konto systemowe i SSH

- Konto: **`tak`**, hasło **`chameleontak`** (ma `sudo`). Logowanie lokalne: monitor + klawiatura (układ polski).
- **SSH jest wyłączone.** Włącz je w kreatorze (opcja SSH) — serwer włącza wtedy `ssh.service`
  i otwiera port 22 w zaporze.
- **Zmień hasło konta `tak`**, zwłaszcza przed włączeniem SSH — lokalnie poleceniem `passwd`
  (albo z panelu, jeśli udostępnia zmianę hasła konta systemowego).
- Użytkownik `chameleontak` (usługa) nie ma powłoki ani hasła; nie da się na niego zalogować.

## 6. Jak działa sieć, zapora i mDNS

- **Zapora** — `nftables.service` przy starcie wczytuje `/etc/nftables.conf`, który robi `flush ruleset`
  i dołącza `/etc/chameleontak/firewall.nft` (tabela `inet chameleontak`, polityka `drop` na wejściu).
  W trybie kreatora otwarte są: TCP 80, 443, DNS (53), DHCP (67/68), mDNS (5353), ICMP.
  Po kreatorze plik generuje serwer (`chameleontak sys firewall …`) i podmienia **tylko swoją tabelę**.
  - Podgląd: `sudo nft list table inet chameleontak`
  - Ponowne zastosowanie: `sudo nft -f /etc/chameleontak/firewall.nft`
  - **Nie** rób `systemctl restart nftables` na działającym systemie — `flush ruleset` usunąłby też
    tabele NetworkManagera i hotspot przestałby rozdawać adresy / przekazywać ruch.
- **mDNS** — `avahi-daemon` ogłasza `<nazwa-hosta>.local` i panel (`_http._tcp`, port 80).
  Gdy w kreatorze zmienisz nazwę hosta, adres `.local` zmieni się razem z nią.
- **Hotspot kreatora** — `chameleontak-network-fallback.service` uruchamia przy starcie
  `/usr/local/lib/chameleontak/network-fallback.sh`, który:
  1. kończy od razu, jeśli istnieje `/var/lib/chameleontak/.setup-done` (kreator ukończony),
  2. czeka do 60 s na adres IPv4 (kabel lub skonfigurowane Wi-Fi),
  3. jeśli go nie ma i jest `wlan0`: ustawia kraj Wi-Fi (gdy nie jest ustawiony), odblokowuje Wi-Fi (`rfkill`),
     zapisuje `/etc/NetworkManager/dnsmasq-shared.d/chameleontak-captive.conf` (`address=/#/10.42.0.1`)
     i uruchamia połączenie NetworkManagera `chameleontak-setup` (AP, WPA2, `ipv4.method shared` → 10.42.0.1/24).

  Ustawienia (kraj Wi-Fi, SSID, hasło, interfejs, kanał, czas oczekiwania) są w `/etc/default/chameleontak`.

## 7. Instalacja na istniejącym systemie (`install.sh`)

Dla serwera z Debianem 12/13 albo Raspberry Pi OS **64-bit** (arm64 lub amd64) z systemd:

```sh
curl -fsSL https://github.com/SP8UEV/ChameleonTAK/releases/latest/download/install.sh | sudo bash
```

Na Raspberry Pi, które ma też korzystać z hotspotu kreatora, gdy nie ma sieci:

```sh
curl -fsSL https://github.com/SP8UEV/ChameleonTAK/releases/latest/download/install.sh | sudo bash -s -- --with-hotspot
```

Z checkoutu repozytorium, z własnoręcznie zbudowaną binarką (bez pobierania czegokolwiek z wydań):

```sh
sudo ./scripts/install.sh --binary ./dist/chameleontak-linux-arm64
```

(`install.sh` dołączony do wydania ma już wpisane repozytorium `SP8UEV/ChameleonTAK`; inne podasz przez `--repo`.)

| Opcja | Znaczenie |
|---|---|
| `--binary PATH` | użyj lokalnej binarki zamiast pobierać z wydania |
| `--packaging DIR` | użyj lokalnego katalogu `packaging/` (domyślnie `../packaging` obok skryptu, inaczej `chameleontak-packaging.tar.gz` z wydania) |
| `--version VER` | konkretna wersja, np. `v0.1.0` (domyślnie najnowsza) |
| `--repo OWNER/NAME` | repozytorium z wydaniami |
| `--with-hotspot` / `--no-hotspot` | włącz / wyłącz hotspot kreatora (domyślnie: wyłączony, przy aktualizacji bez zmian) |
| `--no-firewall` / `--with-firewall` | nie zarządzaj / zarządzaj zaporą nftables |
| `--no-start` | tylko zainstaluj, nie (re)startuj usług |

Zmienne środowiskowe: `CHAMELEONTAK_REPO`, `CHAMELEONTAK_VERSION`
(np. `curl … | sudo CHAMELEONTAK_VERSION=v0.1.0 bash`).

Co robi skrypt:

1. sprawdza system (root, systemd, apt, architektura arm64/amd64),
2. doinstalowuje brakujące pakiety: `ca-certificates curl sudo avahi-daemon nftables`
   (+ `network-manager dnsmasq-base rfkill iw` przy `--with-hotspot`),
3. pobiera `chameleontak-linux-<arch>` i `chameleontak-packaging.tar.gz` z wydania i sprawdza je z `SHA256SUMS`,
4. instaluje pliki z tabeli w rozdz. 1, tworzy użytkownika `chameleontak`, sprawdza sudoers (`visudo -c`),
   włącza usługi,
5. stosuje zaporę (`nft -f /etc/chameleontak/firewall.nft` — tylko tabela ChameleonTAK, reguły Dockera
   czy NetworkManagera zostają), restartuje `chameleontak.service` i wypisuje adresy panelu.

**Aktualizacja** = ponowne uruchomienie tego samego polecenia. Dane w `/var/lib/chameleontak`,
wygenerowana przez serwer zapora (`/etc/chameleontak/firewall.nft`) i `/etc/default/chameleontak`
nie są ruszane; ustawienia hotspotu i zapory zostają takie jak poprzednio.

Uwagi o zaporze:

- Jeśli działa **SSH**, jego port jest dopisywany do początkowej zapory, żeby instalacja nie odcięła sesji.
- Jeśli działa **ufw** albo **firewalld**, skrypt sam przechodzi w tryb `--no-firewall` — porty
  (80, 443 oraz porty TAK: domyślnie 8089, 8443, 8446) otwierasz wtedy samodzielnie.
- W trybie `--no-firewall` powstaje znacznik `/etc/chameleontak/firewall.disabled` i serwer nie zmienia zapory.
- Poprzedni `/etc/nftables.conf` jest zachowywany jako `/etc/nftables.conf.pre-chameleontak`.
- Port 80 musi być wolny (nginx/apache na tym samym serwerze uniemożliwią start panelu).

Odinstalowanie (dane usuwasz tylko, jeśli na pewno nie są potrzebne):

```sh
sudo systemctl disable --now chameleontak.service chameleontak-network-fallback.service
sudo rm -f /usr/local/bin/chameleontak /etc/systemd/system/chameleontak*.service \
  /etc/sudoers.d/chameleontak /etc/avahi/services/chameleontak.service /etc/issue.d/chameleontak.issue \
  /etc/default/chameleontak /etc/NetworkManager/dnsmasq-shared.d/chameleontak-captive.conf
sudo rm -rf /usr/local/lib/chameleontak /etc/chameleontak
sudo nft delete table inet chameleontak
# zapora: przywróć /etc/nftables.conf.pre-chameleontak albo wyłącz nftables.service
sudo systemctl daemon-reload
sudo userdel chameleontak
# sudo rm -rf /var/lib/chameleontak     # DANE: certyfikaty, użytkownicy, pliki
```

## 8. Budowanie obrazu samodzielnie

### 8.1 GitHub Actions (zalecane)

- **`.github/workflows/build.yml`** — przy każdym pushu i pull requeście: sprawdzenie skryptów
  (`bash -n`, shellcheck, `visudo -c`, `nft -c`), build panelu (`web/` → `internal/panel/dist`),
  `go vet`, `go test`, kompilacja (`CGO_ENABLED=0`) dla linux/arm64, linux/amd64 i windows/amd64.
  Artefakt: `chameleontak-binaries` (binarki + `chameleontak-packaging.tar.gz`).
- **`.github/workflows/image.yml`** — po wypchnięciu taga `v*` (albo ręcznie: *Actions → image → Run workflow*):
  binarki jak wyżej, obraz pi-gen, a dla taga — **wydanie GitHub** z plikami:
  `chameleontak-X.Y.Z-arm64.img.xz`, `chameleontak-linux-arm64`, `chameleontak-linux-amd64`,
  `chameleontak-windows-amd64.exe`, `chameleontak-packaging.tar.gz`, `install.sh`, `SHA256SUMS`.

```sh
git tag v0.1.0
git push origin v0.1.0
```

Szczegóły:

- pi-gen: `RPi-Distro/pi-gen`, gałąź **`arm64`** — to ona buduje 64-bitowy Raspberry Pi OS
  (domyślnie Debian Trixie); gałąź `master` buduje obrazy 32-bitowe (armhf).
- Akcja: [`usimd/pi-gen-action@v1`](https://github.com/usimd/pi-gen-action).
  Na zwykłym runnerze x86_64 budowanie idzie przez emulację QEMU i trwa długo (zwykle ponad godzinę).
  Przy ręcznym uruchomieniu można wybrać runner **`ubuntu-24.04-arm`** (natywne arm64 — dużo szybciej;
  dostępność runnerów ARM zależy od planu GitHub / widoczności repozytorium).
- Ręczne uruchomienie z gałęzi nie tworzy wydania — obraz jest w artefakcie `chameleontak-image`.

### 8.2 Lokalnie (Linux + Docker)

Wymagania: Linux z Dockerem (możliwość uruchamiania kontenerów `--privileged`), `git`, ok. 20 GB
wolnego miejsca, internet. Na Windows użyj WSL2 (np. Ubuntu) z Dockerem.

```sh
# 1. Binarka linux/arm64
(cd web && npm ci && npm run build)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
  -ldflags "-s -w -X main.version=0.1.0" -o dist/chameleontak-linux-arm64 .

# 2. Obraz
image/build.sh dist/chameleontak-linux-arm64 0.1.0
# wynik: image/out/chameleontak-0.1.0-arm64.img.xz (+ .sha256)
```

(Jeśli pakiet `main` jest w `cmd/chameleontak`, podaj `./cmd/chameleontak` zamiast `.` — CI wykrywa to samo.)

`image/build.sh` klonuje pi-gen (gałąź `arm64`) do `image/.build/pi-gen`, kopiuje tam `image/config`
i etap `stage-chameleontak`, wyłącza eksport zwykłego obrazu Lite i uruchamia `build-docker.sh`.
Przerwane budowanie wznowisz przez `CONTINUE=1 image/build.sh …`. Na hoście x86_64 pi-gen sam
rejestruje emulację arm64; gdyby się to nie udało: `docker run --privileged --rm tonistiigi/binfmt --install arm64`.

Struktura etapu:

```
image/
  config                         konfiguracja pi-gen (budowanie lokalne)
  build.sh                       budowanie lokalne (pi-gen w Dockerze)
  prepare-stage.sh               wkłada binarkę i packaging/ do etapu, nadaje bity +x
  stage-chameleontak/
    prerun.sh                    copy_previous (start od systemu ze stage2)
    EXPORT_IMAGE                 ten etap jest eksportowany jako obraz
    00-chameleontak/
      00-packages                avahi-daemon nftables sudo network-manager dnsmasq-base ca-certificates rfkill iw curl
      01-run.sh                  (host) packaging/install-files.sh --root ${ROOTFS_DIR}
      02-run-chroot.sh           (chroot) setup-system.sh --hotspot enable, multi-user.target, kontrola
      files/                     generowane: chameleontak + packaging/ (w .gitignore)
```

Ustawienia obrazu (nazwa hosta, konto, język, kraj Wi-Fi…) są w `image/config` **oraz** w krokach
`pi-gen` w `image.yml` — przy zmianie aktualizuj oba miejsca.

## 9. Rozwiązywanie problemów

Polecenia diagnostyczne (lokalnie: monitor + klawiatura, login `tak`):

```sh
systemctl status chameleontak chameleontak-network-fallback
journalctl -u chameleontak -b              # logi serwera
journalctl -u chameleontak-network-fallback -b
nmcli device status; nmcli connection show
ip -4 addr
sudo nft list ruleset
rfkill list
```

**Nie widzę sieci `ChameleonTAK-Setup`**
- Odczekaj 2–3 min od włączenia (pierwszy start jest dłuższy).
- Hotspot nie powstaje, gdy Raspberry ma sieć (podłączony kabel / znane Wi-Fi) — wtedy wejdź przez `chameleontak.local` lub IP.
- Hotspot nie powstaje po ukończeniu kreatora (`/var/lib/chameleontak/.setup-done`).
- Sprawdź `journalctl -u chameleontak-network-fallback -b` oraz `rfkill list` (Wi-Fi nie może być „Soft blocked”).
- Ponowna próba: `sudo systemctl restart chameleontak-network-fallback`.
- Kraj Wi-Fi: `sudo raspi-config nonint do_wifi_country PL` (albo zmień `CHAMELEONTAK_WIFI_COUNTRY` w `/etc/default/chameleontak`).

**Telefon połączony z hotspotem, ale kreator się nie otwiera**
- Otwórz ręcznie <http://10.42.0.1> (koniecznie `http://`, nie `https://`).
- Wyłącz dane komórkowe i „Prywatny DNS” (Android) / iCloud Private Relay (iOS) na czas konfiguracji.
- Jeśli telefon pyta o „brak internetu” — wybierz pozostanie w sieci.

**`chameleontak.local` nie działa**
- Niektóre sieci (firmowe, goście) blokują multicast; starsze Androidy nie obsługują `.local` — użyj adresu IP.
- Sprawdź `systemctl status avahi-daemon`.

**Panel nie odpowiada**
- `systemctl status chameleontak` i `journalctl -u chameleontak -e`.
- Port 80 zajęty przez inny program? `sudo ss -ltnp 'sport = :80'`.
- Zapora: `sudo nft list table inet chameleontak` — w trybie kreatora musi być `tcp dport { 80, 443 } accept`.

**Odciąłem się zaporą**
- Lokalnie (monitor + klawiatura): `sudo nft delete table inet chameleontak` zdejmuje zaporę ChameleonTAK do
  następnego zastosowania reguł / restartu. Potem popraw porty w panelu.

**Zapomniane hasło administratora panelu**
```sh
sudo -u chameleontak /usr/local/bin/chameleontak reset-admin --password 'NoweHaslo123'
```
(uruchamiaj jako użytkownik `chameleontak`, nie jako root — inaczej pliki bazy mogą dostać złego właściciela).

**Chcę zacząć od nowa (kreator od początku)**
```sh
sudo systemctl stop chameleontak
sudo -u chameleontak /usr/local/bin/chameleontak factory-reset --yes
sudo systemctl start chameleontak
```
albo w panelu (przywrócenie ustawień fabrycznych). Hotspot kreatora wróci przy następnym starcie bez sieci.

**Zły czas / certyfikaty „z przyszłości”**
- Raspberry nie ma zegara RTC (poza Pi 5 z baterią). Bez internetu (NTP) czas ustawia kreator z przeglądarki.
  Sprawdź `timedatectl`. Po podłączeniu do internetu czas synchronizuje się sam.

**Obraz nie startuje**
- Sprawdź, czy model jest na liście z rozdz. 2 (obraz jest tylko 64-bitowy) i czy karta została zapisana
  bez błędów weryfikacji. Wypróbuj inną kartę i oryginalny zasilacz.
