#!/usr/bin/env bash
# ChameleonTAK - konfiguracja systemu po skopiowaniu plików przez install-files.sh.
#
# Działa zarówno w chroocie pi-gen (bez działającego systemd), jak i na żywym systemie, bo
# używa wyłącznie operacji "offline": groupadd/useradd, visudo -c, systemctl enable/disable.
# Uruchamianie / restart usług na żywym systemie robi scripts/install.sh.
# Idempotentny - można uruchamiać wielokrotnie (aktualizacje).
#
# Użycie: setup-system.sh [--hotspot enable|disable|keep] [--no-firewall]
#   --hotspot enable    włącz chameleontak-network-fallback.service (hotspot kreatora, gdy brak sieci)
#   --hotspot disable   wyłącz go
#   --hotspot keep      nie zmieniaj (domyślnie)
#   --no-firewall       nie włączaj nftables.service
set -euo pipefail

HOTSPOT="keep"
FIREWALL=1
readonly SVC_USER="chameleontak"
readonly DATA_DIR="/var/lib/chameleontak"
readonly SUDOERS_FILE="/etc/sudoers.d/chameleontak"

log() { printf 'setup-system: %s\n' "$*"; }
die() {
	printf 'setup-system: BŁĄD: %s\n' "$*" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
		--hotspot)
			[ $# -ge 2 ] || die "--hotspot wymaga argumentu"
			HOTSPOT="$2"
			shift 2
			;;
		--no-firewall)
			FIREWALL=0
			shift
			;;
		*) die "nieznana opcja: $1" ;;
	esac
done
case "$HOTSPOT" in enable | disable | keep) ;; *) die "--hotspot: enable|disable|keep" ;; esac
[ "$(id -u)" -eq 0 ] || die "uruchom jako root"

unit_exists() {
	[ -e "/etc/systemd/system/$1" ] || [ -e "/lib/systemd/system/$1" ] || [ -e "/usr/lib/systemd/system/$1" ]
}

# --- 1. Użytkownik systemowy (bez powłoki logowania, bez hasła) ---------------------------
NOLOGIN="/usr/sbin/nologin"
[ -x "$NOLOGIN" ] || NOLOGIN="/sbin/nologin"
if ! getent group "$SVC_USER" >/dev/null; then
	groupadd --system "$SVC_USER"
	log "utworzono grupę $SVC_USER"
fi
if ! getent passwd "$SVC_USER" >/dev/null; then
	useradd --system --gid "$SVC_USER" --home-dir "$DATA_DIR" --no-create-home \
		--shell "$NOLOGIN" --comment "ChameleonTAK" "$SVC_USER"
	log "utworzono użytkownika systemowego $SVC_USER"
else
	shell="$(getent passwd "$SVC_USER" | cut -d: -f7)"
	if [ "$shell" != "$NOLOGIN" ]; then
		usermod --shell "$NOLOGIN" "$SVC_USER"
		log "ustawiono powłokę $NOLOGIN dla $SVC_USER"
	fi
fi
# Katalog danych tworzy systemd (StateDirectory=chameleontak); jeśli już istnieje - nie ruszamy go.

# --- 2. sudoers: tylko `chameleontak sys *` jako root -------------------------------------
[ -f "$SUDOERS_FILE" ] || die "brak $SUDOERS_FILE (uruchom najpierw install-files.sh)"
command -v visudo >/dev/null 2>&1 || die "brak visudo - zainstaluj pakiet sudo"
chown root:root "$SUDOERS_FILE"
chmod 0440 "$SUDOERS_FILE"
if ! visudo -cqf "$SUDOERS_FILE" >/dev/null; then
	rm -f "$SUDOERS_FILE"
	die "niepoprawny $SUDOERS_FILE - usunięto go, żeby nie zepsuć sudo"
fi
if ! visudo -cq >/dev/null; then
	die "visudo -c zgłasza błąd w konfiguracji sudo (/etc/sudoers lub /etc/sudoers.d/*) - popraw ją ręcznie"
fi
log "sudoers OK (visudo -c)"

# --- 3. Usługi -------------------------------------------------------------------------------
systemctl enable chameleontak.service
if unit_exists avahi-daemon.service; then
	systemctl enable avahi-daemon.service
else
	log "uwaga: brak avahi-daemon - adres <hostname>.local nie będzie działał"
fi
if [ "$FIREWALL" -eq 1 ]; then
	unit_exists nftables.service || die "brak nftables.service - zainstaluj pakiet nftables"
	systemctl enable nftables.service
fi
case "$HOTSPOT" in
	enable)
		systemctl enable chameleontak-network-fallback.service
		;;
	disable)
		systemctl disable chameleontak-network-fallback.service 2>/dev/null || true
		;;
esac

log "gotowe"
