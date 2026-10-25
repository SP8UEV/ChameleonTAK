#!/usr/bin/env bash
# ChameleonTAK - instalator dla istniejącego Debiana / Raspberry Pi OS (64-bit: arm64 lub amd64).
#
#   curl -fsSL https://github.com/OWNER/chameleontak/releases/latest/download/install.sh | sudo bash
#   curl -fsSL https://github.com/OWNER/chameleontak/releases/latest/download/install.sh | sudo bash -s -- --with-hotspot
#   sudo ./scripts/install.sh --binary ./chameleontak-linux-arm64     # z checkoutu repozytorium
#
# Ponowne uruchomienie = aktualizacja w miejscu: podmienia binarkę i pliki systemowe,
# NIE dotyka danych w /var/lib/chameleontak ani konfiguracji wygenerowanej przez serwer
# (/etc/chameleontak/firewall.nft) czy ustawień w /etc/default/chameleontak.
#
# Opcje:
#   --binary PATH       użyj lokalnej binarki zamiast pobierać ją z wydania na GitHubie
#   --packaging DIR     użyj lokalnego katalogu packaging/ (domyślnie: ../packaging obok skryptu,
#                       a jeśli go nie ma - chameleontak-packaging.tar.gz z wydania)
#   --version VER       wersja do pobrania, np. v0.1.0 (domyślnie: najnowsza; env CHAMELEONTAK_VERSION)
#   --repo OWNER/NAME   repozytorium GitHub z wydaniami (env CHAMELEONTAK_REPO)
#   --with-hotspot      włącz hotspot kreatora "ChameleonTAK-Setup", gdy przy starcie brak sieci
#                       (tylko dla Raspberry Pi / urządzeń z wlan0; instaluje NetworkManager)
#   --no-hotspot        wyłącz hotspot kreatora
#   --no-firewall       nie zarządzaj zaporą (nftables) - porty otwierasz sam
#   --with-firewall     przywróć zarządzanie zaporą po wcześniejszym --no-firewall
#   --no-start          tylko zainstaluj, nie (re)startuj usług
#   -h, --help          pomoc
#
# Domyślnie hotspot NIE jest włączany (serwer, który nie jest Raspberry Pi, nie powinien przejmować
# wlan0), a przy aktualizacji zachowywane są poprzednie ustawienia hotspotu i zapory.
set -euo pipefail

# Podmieniane przez CI przy publikacji wydania (.github/workflows/image.yml).
DEFAULT_REPO="OWNER/chameleontak"
DEFAULT_VERSION="latest"

readonly BIN_PATH="/usr/local/bin/chameleontak"
readonly DATA_DIR="/var/lib/chameleontak"
readonly FALLBACK_UNIT="chameleontak-network-fallback.service"

REPO="${CHAMELEONTAK_REPO:-$DEFAULT_REPO}"
VERSION="${CHAMELEONTAK_VERSION:-$DEFAULT_VERSION}"
BINARY_ARG=""
PACKAGING_ARG=""
HOTSPOT="keep"   # enable | disable | keep
FIREWALL="auto"  # yes | no | auto
START=1

ARCH=""
BASE_URL=""
WORKDIR=""
PKG_DIR=""
HAVE_SUMS=0
SUMS_TRIED=0
UPGRADE=0

say() { printf '==> %s\n' "$*"; }
warn() { printf 'UWAGA: %s\n' "$*" >&2; }
die() {
	printf 'BŁĄD: %s\n' "$*" >&2
	exit 1
}

usage() {
	sed -n '2,/^set -euo/{/^set -euo/d;s/^# \{0,1\}//;p}' "${BASH_SOURCE[0]:-}" 2>/dev/null ||
		echo "Użycie: install.sh [--binary PATH] [--version VER] [--repo OWNER/NAME] [--with-hotspot] [--no-firewall]"
}

cleanup() {
	if [ -n "$WORKDIR" ] && [ -d "$WORKDIR" ]; then
		rm -rf "$WORKDIR"
	fi
}

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
			--binary)
				[ $# -ge 2 ] || die "--binary wymaga ścieżki"
				BINARY_ARG="$2"
				shift 2
				;;
			--packaging)
				[ $# -ge 2 ] || die "--packaging wymaga katalogu"
				PACKAGING_ARG="$2"
				shift 2
				;;
			--version)
				[ $# -ge 2 ] || die "--version wymaga argumentu"
				VERSION="$2"
				shift 2
				;;
			--repo)
				[ $# -ge 2 ] || die "--repo wymaga argumentu OWNER/NAME"
				REPO="$2"
				shift 2
				;;
			--with-hotspot)
				HOTSPOT="enable"
				shift
				;;
			--no-hotspot)
				HOTSPOT="disable"
				shift
				;;
			--no-firewall)
				FIREWALL="no"
				shift
				;;
			--with-firewall)
				FIREWALL="yes"
				shift
				;;
			--no-start)
				START=0
				shift
				;;
			-h | --help)
				usage
				exit 0
				;;
			*) die "nieznana opcja: $1 (zobacz --help)" ;;
		esac
	done
}

preflight() {
	[ "$(id -u)" -eq 0 ] || die "uruchom jako root, np.: curl -fsSL …/install.sh | sudo bash"
	[ "$(uname -s)" = "Linux" ] || die "obsługiwany jest tylko Linux"
	[ -d /run/systemd/system ] || die "wymagany systemd (system nie został uruchomiony przez systemd)"
	{ [ -f /etc/debian_version ] && command -v apt-get >/dev/null 2>&1 && command -v dpkg >/dev/null 2>&1; } ||
		die "obsługiwany jest Debian / Raspberry Pi OS (apt)"

	local dpkg_arch
	dpkg_arch="$(dpkg --print-architecture)"
	case "$dpkg_arch" in
		arm64 | amd64) ARCH="$dpkg_arch" ;;
		armhf | armel | i386)
			die "system 32-bitowy ($dpkg_arch) - zainstaluj 64-bitowy Raspberry Pi OS / Debian (arm64 lub amd64)"
			;;
		*) die "nieobsługiwana architektura: $dpkg_arch (wspierane: arm64, amd64)" ;;
	esac

	if [ -x "$BIN_PATH" ]; then
		UPGRADE=1
	fi

	# Ustawienia, które przy aktualizacji zostają takie jak były.
	if [ "$FIREWALL" = "auto" ]; then
		if [ -e /etc/chameleontak/firewall.disabled ]; then
			FIREWALL="no"
		elif systemctl is-active --quiet ufw.service 2>/dev/null || systemctl is-active --quiet firewalld.service 2>/dev/null; then
			FIREWALL="no"
			warn "wykryto aktywną zaporę ufw/firewalld - ChameleonTAK NIE będzie zarządzał nftables."
			warn "Otwórz ręcznie porty: 80, 443 (panel), 8089, 8443, 8446 (TAK) oraz te, które ustawisz w kreatorze."
		else
			FIREWALL="yes"
		fi
	fi
	if [ "$HOTSPOT" = "enable" ] && [ ! -e /sys/class/net/wlan0 ]; then
		warn "--with-hotspot: brak interfejsu wlan0 - hotspot zadziała dopiero, gdy pojawi się karta Wi-Fi"
		warn "(inną kartę ustawisz w /etc/default/chameleontak: CHAMELEONTAK_SETUP_IFACE)."
	fi
}

hotspot_will_be_enabled() {
	case "$HOTSPOT" in
		enable) return 0 ;;
		disable) return 1 ;;
		*) systemctl is-enabled --quiet "$FALLBACK_UNIT" 2>/dev/null ;;
	esac
}

install_packages() {
	local pkgs=(ca-certificates curl sudo avahi-daemon) missing=() p
	if [ "$FIREWALL" = "yes" ]; then
		pkgs+=(nftables)
	fi
	if hotspot_will_be_enabled; then
		pkgs+=(network-manager dnsmasq-base rfkill iw)
	fi
	for p in "${pkgs[@]}"; do
		if ! dpkg-query -W -f='${Status}' "$p" 2>/dev/null | grep -q "install ok installed"; then
			missing+=("$p")
		fi
	done
	if [ "${#missing[@]}" -eq 0 ]; then
		say "Wymagane pakiety są już zainstalowane."
		return 0
	fi
	say "Instaluję pakiety: ${missing[*]}"
	export DEBIAN_FRONTEND=noninteractive
	apt-get update -q
	apt-get install -y -q --no-install-recommends "${missing[@]}"
}

download() {
	# download URL PLIK
	curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 20 \
		--proto '=https' --proto-redir '=https' --tlsv1.2 -o "$2" "$1"
}

release_base() {
	[ -n "$BASE_URL" ] && return 0
	[[ "$REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || die "nieprawidłowe repozytorium: $REPO (oczekiwano OWNER/NAME)"
	if [ "$REPO" = "OWNER/chameleontak" ]; then
		die "nie ustawiono repozytorium z wydaniami - podaj --repo OWNER/NAME albo CHAMELEONTAK_REPO=OWNER/NAME
       (albo zainstaluj lokalnie: --binary PATH, uruchamiając skrypt z checkoutu repozytorium)"
	fi
	if [ "$VERSION" = "latest" ]; then
		BASE_URL="https://github.com/${REPO}/releases/latest/download"
	else
		case "$VERSION" in v*) ;; *) VERSION="v${VERSION}" ;; esac
		BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
	fi
	say "Źródło: ${BASE_URL}"
}

fetch_sums() {
	[ "$SUMS_TRIED" -eq 1 ] && return 0
	SUMS_TRIED=1
	if download "${BASE_URL}/SHA256SUMS" "${WORKDIR}/SHA256SUMS" 2>/dev/null; then
		HAVE_SUMS=1
	else
		warn "brak SHA256SUMS w wydaniu - pobrane pliki nie zostaną zweryfikowane"
	fi
}

verify() {
	# verify PLIK NAZWA_W_WYDANIU
	local file="$1" name="$2" expected actual
	fetch_sums
	[ "$HAVE_SUMS" -eq 1 ] || return 0
	expected="$(awk -v n="$name" '$2 == n || $2 == "*" n { print $1; exit }' "${WORKDIR}/SHA256SUMS")"
	[ -n "$expected" ] || die "brak ${name} w SHA256SUMS"
	actual="$(sha256sum "$file" | cut -d' ' -f1)"
	[ "$expected" = "$actual" ] || die "suma kontrolna ${name} się nie zgadza (oczekiwano ${expected}, jest ${actual})"
	say "Suma kontrolna ${name}: OK"
}

obtain_packaging() {
	local script_dir=""
	if [ -n "$PACKAGING_ARG" ]; then
		[ -f "${PACKAGING_ARG}/install-files.sh" ] || die "${PACKAGING_ARG} nie wygląda na katalog packaging/ (brak install-files.sh)"
		PKG_DIR="$(cd "$PACKAGING_ARG" && pwd)"
	else
		# Uruchomiony z pliku (np. z checkoutu repozytorium)? Wtedy użyj ../packaging.
		if [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ]; then
			script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
		fi
		if [ -n "$script_dir" ] && [ -f "${script_dir}/../packaging/install-files.sh" ]; then
			PKG_DIR="$(cd "${script_dir}/../packaging" && pwd)"
		else
			release_base
			say "Pobieram pliki systemowe (chameleontak-packaging.tar.gz)…"
			download "${BASE_URL}/chameleontak-packaging.tar.gz" "${WORKDIR}/packaging.tar.gz" ||
				die "nie udało się pobrać ${BASE_URL}/chameleontak-packaging.tar.gz"
			verify "${WORKDIR}/packaging.tar.gz" "chameleontak-packaging.tar.gz"
			mkdir -p "${WORKDIR}/pkg"
			tar -xzf "${WORKDIR}/packaging.tar.gz" -C "${WORKDIR}/pkg" --no-same-owner
			[ -f "${WORKDIR}/pkg/packaging/install-files.sh" ] || die "archiwum nie zawiera packaging/install-files.sh"
			PKG_DIR="${WORKDIR}/pkg/packaging"
		fi
	fi
	say "Pliki systemowe: ${PKG_DIR}"
}

obtain_binary() {
	local name="chameleontak-linux-${ARCH}"
	if [ -n "$BINARY_ARG" ]; then
		[ -f "$BINARY_ARG" ] || die "brak pliku: $BINARY_ARG"
		install -m 0755 "$BINARY_ARG" "${WORKDIR}/chameleontak"
		say "Binarka: ${BINARY_ARG}"
	else
		release_base
		say "Pobieram ${name}…"
		download "${BASE_URL}/${name}" "${WORKDIR}/chameleontak" || die "nie udało się pobrać ${BASE_URL}/${name}"
		verify "${WORKDIR}/chameleontak" "$name"
		chmod 0755 "${WORKDIR}/chameleontak"
	fi
	# Sprawdzenie architektury po nagłówku ELF (działa także, gdy katalog tymczasowy jest noexec).
	local magic machine want v
	magic="$(od -An -tx1 -N4 "${WORKDIR}/chameleontak" | tr -d ' \n')"
	machine="$(od -An -tx1 -j18 -N2 "${WORKDIR}/chameleontak" | tr -d ' \n')"
	case "$ARCH" in
		arm64) want="b700" ;;
		amd64) want="3e00" ;;
		*) want="" ;;
	esac
	if [ "$magic" != "7f454c46" ] || [ "$machine" != "$want" ]; then
		die "binarka nie jest programem linux/${ARCH} (ELF magic=${magic}, e_machine=${machine})"
	fi
	if v="$("${WORKDIR}/chameleontak" version 2>/dev/null)"; then
		say "Wersja: ${v}"
	else
		warn "nie udało się uruchomić 'chameleontak version' z katalogu tymczasowego (noexec?) - kontynuuję"
	fi
}

ssh_ports() {
	# Porty sshd, jeśli SSH działa / jest włączone - żeby początkowa zapora nie odcięła sesji.
	local u ports=""
	for u in ssh.service sshd.service ssh.socket; do
		if systemctl is-active --quiet "$u" 2>/dev/null || systemctl is-enabled --quiet "$u" 2>/dev/null; then
			if command -v sshd >/dev/null 2>&1; then
				ports="$(sshd -T 2>/dev/null | awk '$1 == "port" { print $2 }' | paste -sd, - || true)"
			fi
			echo "${ports:-22}"
			return 0
		fi
	done
	return 0
}

install_everything() {
	local files_args=() setup_args=() sshp
	if [ "$FIREWALL" = "no" ]; then
		files_args+=(--no-firewall)
		setup_args+=(--no-firewall)
	elif [ ! -e /etc/chameleontak/firewall.nft ]; then
		sshp="$(ssh_ports)"
		if [ -n "$sshp" ]; then
			files_args+=(--extra-tcp-ports "$sshp")
			say "SSH jest włączone - port(y) ${sshp} zostaną otwarte w początkowej zaporze."
		fi
	fi

	say "Instaluję pliki…"
	bash "${PKG_DIR}/install-files.sh" --root / --binary "${WORKDIR}/chameleontak" "${files_args[@]}"
	bash /usr/local/lib/chameleontak/setup-system.sh --hotspot "$HOTSPOT" "${setup_args[@]}"
	systemctl daemon-reload

	# Przejście z zapory zarządzanej przez ChameleonTAK na --no-firewall: oddaj nftables użytkownikowi.
	if [ "$FIREWALL" = "no" ] && grep -qF 'include "/etc/chameleontak/firewall.nft"' /etc/nftables.conf 2>/dev/null; then
		if [ -f /etc/nftables.conf.pre-chameleontak ]; then
			mv -f /etc/nftables.conf.pre-chameleontak /etc/nftables.conf
			say "Przywrócono poprzednią /etc/nftables.conf."
		else
			systemctl disable nftables.service 2>/dev/null || true
			say "Wyłączono nftables.service (konfiguracja zapory należała do ChameleonTAK)."
		fi
		if command -v nft >/dev/null 2>&1; then
			nft delete table inet chameleontak 2>/dev/null || true
		fi
	fi
}

apply_live() {
	if [ "$START" -eq 0 ]; then
		say "--no-start: usługi nie zostały (re)startowane."
		return 0
	fi

	if [ "$FIREWALL" = "yes" ]; then
		# Tylko nasza tabela (atomowa podmiana) - bez `flush ruleset`, więc reguły Dockera /
		# NetworkManagera zostają. nftables.service wczyta całość przy następnym starcie systemu.
		if nft -f /etc/chameleontak/firewall.nft; then
			say "Zapora: zastosowano /etc/chameleontak/firewall.nft"
		else
			warn "nie udało się zastosować /etc/chameleontak/firewall.nft (sprawdź: nft -c -f /etc/chameleontak/firewall.nft)"
		fi
	fi

	systemctl restart avahi-daemon.service 2>/dev/null || warn "nie udało się uruchomić avahi-daemon (adres .local może nie działać)"

	local holder
	holder="$(ss -Hltnp 'sport = :80' 2>/dev/null || true)"
	if [ -n "$holder" ] && ! grep -q '"chameleontak"' <<<"$holder"; then
		warn "port 80 jest zajęty przez inny program - panel ChameleonTAK może nie wystartować:"
		printf '%s\n' "$holder" >&2
	fi

	say "Uruchamiam chameleontak.service…"
	systemctl restart chameleontak.service
	for _ in $(seq 1 15); do
		if systemctl is-active --quiet chameleontak.service; then
			break
		fi
		sleep 1
	done
	sleep 2
	if ! systemctl is-active --quiet chameleontak.service; then
		warn "chameleontak.service nie działa. Logi: journalctl -u chameleontak -e"
		return 0
	fi

	if systemctl is-enabled --quiet "$FALLBACK_UNIT" 2>/dev/null; then
		say "Hotspot kreatora włączony - zadziała przy następnym starcie, jeśli nie będzie sieci."
	fi
}

summary() {
	local host ips ip
	host="$(cat /proc/sys/kernel/hostname 2>/dev/null || echo chameleontak)"
	ips="$(ip -4 -o addr show scope global 2>/dev/null |
		awk '$2 !~ /^(docker|br-|veth|virbr|vnet|cni|flannel|podman|lxc|lxd|incus)/ { split($4, a, "/"); print a[1] }' || true)"

	echo
	if [ "$UPGRADE" -eq 1 ]; then
		say "ChameleonTAK zaktualizowany. Dane w ${DATA_DIR} pozostały bez zmian."
	else
		say "ChameleonTAK zainstalowany."
	fi
	if [ -e "${DATA_DIR}/.setup-done" ]; then
		echo "Panel WWW:"
	else
		echo "Kreator pierwszego uruchomienia (otwórz w przeglądarce):"
	fi
	for ip in $ips; do
		echo "  http://${ip}/"
	done
	if systemctl is-active --quiet avahi-daemon.service 2>/dev/null; then
		echo "  http://${host}.local/"
	fi
	if [ ! -e "${DATA_DIR}/.setup-done" ]; then
		echo
		warn "dopóki kreator nie zostanie ukończony, może go wypełnić każdy w tej sieci - zrób to od razu."
	fi
	echo
	echo "Status: systemctl status chameleontak    Logi: journalctl -u chameleontak -f"
}

main() {
	parse_args "$@"
	preflight
	if [ "$UPGRADE" -eq 1 ]; then
		say "Wykryto istniejącą instalację - aktualizacja w miejscu (${ARCH})."
	else
		say "Nowa instalacja ChameleonTAK (${ARCH})."
	fi
	install_packages
	WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/chameleontak-install.XXXXXX")"
	trap cleanup EXIT
	obtain_packaging
	obtain_binary
	install_everything
	apply_live
	summary
}

# Wywołanie na samym końcu: przy `curl | bash` niepełne pobranie nie uruchomi połowy skryptu.
main "$@"
