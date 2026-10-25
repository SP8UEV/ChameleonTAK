#!/usr/bin/env bash
# ChameleonTAK - awaryjny hotspot kreatora pierwszego uruchomienia.
#
# Uruchamiany raz przy starcie przez chameleontak-network-fallback.service:
#   1. Jeśli istnieje /var/lib/chameleontak/.setup-done -> natychmiast kończy (nic do zrobienia).
#   2. Czeka do ~60 s na adres IPv4 na dowolnym interfejsie (poza lo / wirtualnymi):
#      kabel (eth0) albo wcześniej skonfigurowane Wi-Fi. Jest adres -> kończy.
#   3. Brak sieci i jest wlan0 -> ustawia kraj Wi-Fi (domyślnie PL), odblokowuje radio (rfkill),
#      zapisuje /etc/NetworkManager/dnsmasq-shared.d/chameleontak-captive.conf
#      (address=/#/10.42.0.1 - portal przechwytujący) i tworzy + aktywuje połączenie
#      NetworkManagera "chameleontak-setup": AP "ChameleonTAK-Setup", WPA2-PSK "chameleontak",
#      ipv4.method shared (Raspberry = 10.42.0.1), autoconnect no.
#
# Skrypt jest idempotentny - można go uruchamiać wielokrotnie. Po kreatorze serwer
# (`chameleontak sys hotspot-off`) usuwa połączenie "chameleontak-setup" i plik portalu.
#
# Ustawienia (opcjonalnie) w /etc/default/chameleontak.
set -euo pipefail

readonly SETUP_MARKER="/var/lib/chameleontak/.setup-done"
readonly CON_NAME="chameleontak-setup"
readonly AP_ADDR="10.42.0.1"
readonly CAPTIVE_DIR="/etc/NetworkManager/dnsmasq-shared.d"
readonly CAPTIVE_CONF="${CAPTIVE_DIR}/chameleontak-captive.conf"

SSID="${CHAMELEONTAK_SETUP_SSID:-ChameleonTAK-Setup}"
PSK="${CHAMELEONTAK_SETUP_PSK:-chameleontak}"
COUNTRY="${CHAMELEONTAK_WIFI_COUNTRY:-PL}"
WIFI_IFACE="${CHAMELEONTAK_SETUP_IFACE:-wlan0}"
CHANNEL="${CHAMELEONTAK_SETUP_CHANNEL:-6}"
WAIT_SECONDS="${CHAMELEONTAK_NET_WAIT:-60}"

log() {
	# stdout trafia do dziennika systemd (journalctl -u chameleontak-network-fallback)
	printf 'network-fallback: %s\n' "$*"
}

setup_done() {
	[ -e "$SETUP_MARKER" ]
}

validate_settings() {
	if [ "${#PSK}" -lt 8 ] || [ "${#PSK}" -gt 63 ]; then
		log "Hasło hotspotu musi mieć 8-63 znaki - używam domyślnego."
		PSK="chameleontak"
	fi
	if [ -z "$SSID" ] || [ "${#SSID}" -gt 32 ]; then
		log "Nieprawidłowa nazwa sieci - używam domyślnej."
		SSID="ChameleonTAK-Setup"
	fi
	COUNTRY="$(printf '%s' "$COUNTRY" | tr '[:lower:]' '[:upper:]')"
	if ! [[ "$COUNTRY" =~ ^[A-Z]{2}$ ]]; then
		log "Nieprawidłowy kod kraju Wi-Fi '${COUNTRY}' - używam PL."
		COUNTRY="PL"
	fi
	if ! [[ "$CHANNEL" =~ ^([1-9]|1[0-3])$ ]]; then
		CHANNEL=6
	fi
	if ! [[ "$WAIT_SECONDS" =~ ^[0-9]+$ ]]; then
		WAIT_SECONDS=60
	fi
}

# Czy NetworkManager działa i odpowiada?
nm_running() {
	local state
	state="$(nmcli -t -f RUNNING general 2>/dev/null || true)"
	[ "$state" = "running" ]
}

connection_exists() {
	nmcli -t -f NAME connection show 2>/dev/null | grep -Fxq -- "$CON_NAME"
}

connection_active() {
	nmcli -t -f NAME connection show --active 2>/dev/null | grep -Fxq -- "$CON_NAME"
}

# Wypisuje pierwszy "prawdziwy" interfejs z globalnym adresem IPv4 (np. "eth0 192.168.1.50/24").
# Pomija lo i typowe interfejsy wirtualne (docker, mosty, VPN), żeby nie udawały sieci.
first_ipv4() {
	local out iface addr
	out="$(ip -4 -o addr show scope global 2>/dev/null || true)"
	# Format: "2: eth0    inet 192.168.1.50/24 brd ... scope global ..."
	while read -r _ iface _ addr _; do
		[ -n "${iface:-}" ] || continue
		iface="${iface%%@*}"
		case "$iface" in
			lo | docker* | br-* | veth* | virbr* | vnet* | tun* | tap* | wg* | tailscale* | zt* | \
				podman* | cni* | flannel* | lxc* | lxd* | incus*)
				continue
				;;
		esac
		printf '%s %s\n' "$iface" "$addr"
		return 0
	done <<<"$out"
	return 1
}

current_wifi_country() {
	local out
	command -v iw >/dev/null 2>&1 || return 0
	out="$(iw reg get 2>/dev/null || true)"
	# Pierwsza linia "country XX: ..." to domena globalna.
	awk '/^country [A-Z0-9][A-Z0-9]:/ { c = $2; sub(":", "", c); print c; exit }' <<<"$out"
}

ensure_wifi_country() {
	local cur
	cur="$(current_wifi_country)"
	if [ -n "$cur" ] && [ "$cur" != "00" ]; then
		log "Kraj Wi-Fi już ustawiony: ${cur}."
		return 0
	fi
	log "Ustawiam kraj Wi-Fi: ${COUNTRY}."
	if command -v raspi-config >/dev/null 2>&1; then
		# Raspberry Pi OS: zapisuje cfg80211.ieee80211_regdom w cmdline.txt, ustawia domenę
		# regulacyjną i odblokowuje Wi-Fi (rfkill).
		raspi-config nonint do_wifi_country "$COUNTRY" ||
			log "raspi-config nie ustawił kraju Wi-Fi (kod $?) - kontynuuję."
	elif command -v iw >/dev/null 2>&1; then
		iw reg set "$COUNTRY" || log "iw reg set ${COUNTRY} nie powiodło się - kontynuuję."
	else
		log "Brak raspi-config i iw - nie mogę ustawić kraju Wi-Fi."
	fi
}

ensure_wifi_enabled() {
	local state="" i
	ensure_wifi_country
	if command -v rfkill >/dev/null 2>&1; then
		rfkill unblock wifi || log "rfkill unblock wifi nie powiodło się."
	fi
	nmcli radio wifi on || log "nmcli radio wifi on nie powiodło się."
	nmcli device set "$WIFI_IFACE" managed yes >/dev/null 2>&1 || true

	# Po odblokowaniu radia NetworkManager potrzebuje chwili, żeby urządzenie było "disconnected".
	for i in $(seq 1 30); do
		state="$(nmcli -t -f DEVICE,STATE device status 2>/dev/null |
			awk -F: -v d="$WIFI_IFACE" '$1 == d { print $2; exit }' || true)"
		case "$state" in
			"" | unavailable | unmanaged) sleep 1 ;;
			*) return 0 ;;
		esac
	done
	log "Uwaga: ${WIFI_IFACE} nadal w stanie '${state:-brak}' (po ${i} s) - próbuję mimo to."
}

write_captive_conf() {
	local tmp="${CAPTIVE_DIR}/.chameleontak-captive.conf.tmp"
	mkdir -p "$CAPTIVE_DIR"
	# dnsmasq (--conf-dir) pomija pliki zaczynające się od kropki, więc plik tymczasowy jest bezpieczny.
	cat >"$tmp" <<EOF
# Wygenerowane przez ChameleonTAK (network-fallback.sh) — portal przechwytujący kreatora.
# Każda nazwa DNS wskazuje na Raspberry, więc telefon po podłączeniu sam otwiera kreator.
# Plik usuwa serwer po ukończeniu kreatora.
address=/#/${AP_ADDR}
EOF
	chmod 0644 "$tmp"
	mv -f "$tmp" "$CAPTIVE_CONF"
	log "Zapisano ${CAPTIVE_CONF}."
}

create_connection() {
	if connection_exists; then
		# Odtwarzamy od zera, żeby ustawienia zawsze były aktualne (idempotentnie).
		nmcli connection delete "$CON_NAME" >/dev/null || true
	fi
	nmcli connection add type wifi ifname "$WIFI_IFACE" con-name "$CON_NAME" \
		autoconnect no ssid "$SSID" \
		802-11-wireless.mode ap \
		802-11-wireless.band bg \
		802-11-wireless.channel "$CHANNEL" \
		802-11-wireless-security.key-mgmt wpa-psk \
		802-11-wireless-security.proto rsn \
		802-11-wireless-security.pairwise ccmp \
		802-11-wireless-security.group ccmp \
		802-11-wireless-security.pmf disable \
		802-11-wireless-security.psk "$PSK" \
		ipv4.method shared \
		ipv4.addresses "${AP_ADDR}/24" \
		ipv6.method disabled >/dev/null
	log "Utworzono połączenie ${CON_NAME} (SSID \"${SSID}\", kanał ${CHANNEL})."
}

activate_connection() {
	local attempt
	for attempt in 1 2 3; do
		if nmcli --wait 45 connection up "$CON_NAME" >/dev/null; then
			log "Hotspot \"${SSID}\" działa - kreator: http://${AP_ADDR}/"
			return 0
		fi
		log "Próba ${attempt}/3 uruchomienia hotspotu nieudana."
		sleep 5
	done
	return 1
}

main() {
	if setup_done; then
		log "Serwer już skonfigurowany (${SETUP_MARKER}) - nic do zrobienia."
		exit 0
	fi
	if [ "$(id -u)" -ne 0 ]; then
		log "Wymagane uprawnienia roota."
		exit 1
	fi
	if ! command -v nmcli >/dev/null 2>&1; then
		log "Brak NetworkManagera (nmcli) - hotspot kreatora niedostępny."
		exit 0
	fi
	validate_settings

	local i ipv4=""
	for i in $(seq 1 30); do
		nm_running && break
		sleep 1
	done
	if ! nm_running; then
		log "NetworkManager nie działa - hotspot kreatora niedostępny."
		exit 1
	fi

	if connection_active; then
		log "Hotspot kreatora już działa - nic do zrobienia."
		exit 0
	fi

	log "Czekam do ${WAIT_SECONDS} s na adres IPv4 (kabel lub skonfigurowane Wi-Fi)..."
	for ((i = 0; i <= WAIT_SECONDS; i++)); do
		if ipv4="$(first_ipv4)"; then
			log "Sieć dostępna (${ipv4}) - hotspot niepotrzebny."
			exit 0
		fi
		if setup_done; then
			exit 0
		fi
		if [ "$i" -lt "$WAIT_SECONDS" ]; then
			sleep 1
		fi
	done

	if [ ! -e "/sys/class/net/${WIFI_IFACE}" ]; then
		log "Brak sieci i brak interfejsu ${WIFI_IFACE} - nie mogę uruchomić hotspotu."
		exit 0
	fi
	if [ ! -x /usr/sbin/dnsmasq ] && ! command -v dnsmasq >/dev/null 2>&1; then
		log "Brak dnsmasq (pakiet dnsmasq-base) - wymagany przez tryb 'shared' NetworkManagera."
		exit 1
	fi

	log "Brak sieci - uruchamiam hotspot kreatora na ${WIFI_IFACE}."
	ensure_wifi_enabled
	write_captive_conf
	create_connection
	if ! activate_connection; then
		log "Nie udało się uruchomić hotspotu. Sprawdź: journalctl -u NetworkManager -b"
		exit 1
	fi
}

main "$@"
