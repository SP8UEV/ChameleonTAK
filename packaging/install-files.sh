#!/usr/bin/env bash
# ChameleonTAK - instaluje pliki systemowe do wskazanego katalogu głównego.
#
# Wspólne dla obrazu Raspberry Pi (etap pi-gen: --root "${ROOTFS_DIR}") i instalatora
# (scripts/install.sh: --root /). Tylko kopiuje pliki - niczego nie uruchamia w systemie
# docelowym; użytkownika, walidację sudoers i włączanie usług robi lib/setup-system.sh.
#
# Pliki konfiguracyjne, które później zmienia użytkownik lub serwer (/etc/default/chameleontak,
# /etc/chameleontak/firewall.nft), są instalowane tylko wtedy, gdy jeszcze nie istnieją.
# /var/lib/chameleontak (dane) nigdy nie jest dotykany.
#
# Użycie:
#   install-files.sh [--root DIR] [--binary PATH] [--no-firewall] [--extra-tcp-ports 22,...]
#
#   --root DIR              katalog główny systemu docelowego (domyślnie /)
#   --binary PATH           binarka chameleontak do zainstalowania jako /usr/local/bin/chameleontak
#   --no-firewall           nie instaluj /etc/nftables.conf ani firewall.nft; zamiast tego utwórz
#                           znacznik /etc/chameleontak/firewall.disabled (serwer nie zarządza zaporą)
#   --extra-tcp-ports CSV   dodatkowe porty TCP w POCZĄTKOWYM firewall.nft (np. 22, gdy działa SSH)
set -euo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="/"
BINARY=""
FIREWALL=1
EXTRA_TCP=""

log() { printf 'install-files: %s\n' "$*"; }
die() {
	printf 'install-files: BŁĄD: %s\n' "$*" >&2
	exit 1
}
usage() { sed -n '2,/^set -euo/{/^set -euo/d;s/^# \{0,1\}//;p}' "${BASH_SOURCE[0]}"; }

while [ $# -gt 0 ]; do
	case "$1" in
		--root)
			[ $# -ge 2 ] || die "--root wymaga argumentu"
			ROOT="$2"
			shift 2
			;;
		--binary)
			[ $# -ge 2 ] || die "--binary wymaga argumentu"
			BINARY="$2"
			shift 2
			;;
		--no-firewall)
			FIREWALL=0
			shift
			;;
		--extra-tcp-ports)
			[ $# -ge 2 ] || die "--extra-tcp-ports wymaga argumentu"
			EXTRA_TCP="$2"
			shift 2
			;;
		-h | --help)
			usage
			exit 0
			;;
		*) die "nieznana opcja: $1" ;;
	esac
done

[ "$(id -u)" -eq 0 ] || die "uruchom jako root"
[ -d "$ROOT" ] || die "brak katalogu docelowego: $ROOT"
R="${ROOT%/}" # "" dla "/", dzięki czemu "$R/etc" = "/etc"

cr=$'\r'
for f in systemd/chameleontak.service systemd/chameleontak-network-fallback.service \
	sudoers/chameleontak avahi/chameleontak.service default/chameleontak issue/chameleontak.issue \
	lib/network-fallback.sh lib/setup-system.sh nftables/nftables.conf nftables/firewall.nft; do
	[ -f "$SRC/$f" ] || die "brak pliku źródłowego: $SRC/$f"
	if grep -qU -e "$cr" "$SRC/$f"; then
		die "$SRC/$f ma końce linii CRLF - wymagane LF (patrz .gitattributes)"
	fi
done

# Tworzy katalog (z podanymi uprawnieniami) tylko jeśli nie istnieje - nie zmienia istniejących.
ensure_dir() {
	local dir="$R$1" mode="$2"
	[ -d "$dir" ] || install -d -m "$mode" "$dir"
}

# put ŹRÓDŁO CEL TRYB - atomowa podmiana: plik tymczasowy w tym samym katalogu + mv.
# Nazwa tymczasowa zaczyna się od kropki, więc ani sudo (#includedir), ani dnsmasq/avahi jej nie czytają.
put() {
	local src="$1" dst="$R$2" mode="$3" dir tmp
	dir="$(dirname "$dst")"
	[ -d "$dir" ] || install -d -m 0755 "$dir"
	tmp="$dir/.$(basename "$dst").chameleontak-new"
	install -m "$mode" "$src" "$tmp"
	mv -f "$tmp" "$dst"
	log "zainstalowano $2"
}

# put_once ŹRÓDŁO CEL TRYB - tylko gdy cel nie istnieje (konfiguracja, którą zmienia użytkownik/serwer).
put_once() {
	if [ -e "$R$2" ]; then
		log "pozostawiono istniejący $2"
	else
		put "$@"
	fi
}

# --- Binarka -------------------------------------------------------------------------------
if [ -n "$BINARY" ]; then
	[ -f "$BINARY" ] || die "brak binarki: $BINARY"
	ensure_dir /usr/local/bin 0755
	put "$BINARY" /usr/local/bin/chameleontak 0755
fi

# --- Skrypty pomocnicze --------------------------------------------------------------------
ensure_dir /usr/local/lib/chameleontak 0755
put "$SRC/lib/network-fallback.sh" /usr/local/lib/chameleontak/network-fallback.sh 0755
put "$SRC/lib/setup-system.sh" /usr/local/lib/chameleontak/setup-system.sh 0755

# --- systemd -------------------------------------------------------------------------------
put "$SRC/systemd/chameleontak.service" /etc/systemd/system/chameleontak.service 0644
put "$SRC/systemd/chameleontak-network-fallback.service" \
	/etc/systemd/system/chameleontak-network-fallback.service 0644

# --- sudoers -------------------------------------------------------------------------------
ensure_dir /etc/sudoers.d 0750
if command -v visudo >/dev/null 2>&1; then
	visudo -cqf "$SRC/sudoers/chameleontak" >/dev/null || die "sudoers/chameleontak nie przechodzi visudo -c"
fi
put "$SRC/sudoers/chameleontak" /etc/sudoers.d/chameleontak 0440

# --- mDNS (avahi) --------------------------------------------------------------------------
put "$SRC/avahi/chameleontak.service" /etc/avahi/services/chameleontak.service 0644

# --- Ustawienia hotspotu kreatora i ekran logowania konsoli ---------------------------------
put_once "$SRC/default/chameleontak" /etc/default/chameleontak 0644
put "$SRC/issue/chameleontak.issue" /etc/issue.d/chameleontak.issue 0644

# --- Zapora (nftables) ---------------------------------------------------------------------
ensure_dir /etc/chameleontak 0755
if [ "$FIREWALL" -eq 1 ]; then
	rm -f "$R/etc/chameleontak/firewall.disabled"

	nftconf="$R/etc/nftables.conf"
	if [ -f "$nftconf" ] && ! grep -qF 'include "/etc/chameleontak/firewall.nft"' "$nftconf"; then
		if [ ! -e "$nftconf.pre-chameleontak" ]; then
			cp -p "$nftconf" "$nftconf.pre-chameleontak"
			log "poprzednia konfiguracja zapory zachowana w /etc/nftables.conf.pre-chameleontak"
		fi
	fi
	put "$SRC/nftables/nftables.conf" /etc/nftables.conf 0755

	if [ -e "$R/etc/chameleontak/firewall.nft" ]; then
		log "pozostawiono istniejący /etc/chameleontak/firewall.nft (zarządza nim serwer)"
	else
		# Początkowa zapora trybu kreatora: tcp 80, 443 (+ ewentualne dodatkowe porty, np. SSH).
		ports="80,443"
		if [ -n "$EXTRA_TCP" ]; then
			[[ "$EXTRA_TCP" =~ ^[0-9]+(,[0-9]+)*$ ]] || die "nieprawidłowa lista portów: $EXTRA_TCP"
			ports="$EXTRA_TCP,$ports"
		fi
		list=""
		for p in $(tr ',' '\n' <<<"$ports" | sort -n -u); do
			{ [ "$p" -ge 1 ] && [ "$p" -le 65535 ]; } || die "nieprawidłowy port: $p"
			list="${list:+$list, }$p"
		done
		tmpnft="$(mktemp)"
		sed "s/^\([[:space:]]*tcp dport \){ 80, 443 }\( accept\)\$/\1{ ${list} }\2/" \
			"$SRC/nftables/firewall.nft" >"$tmpnft"
		grep -qF "tcp dport { ${list} } accept" "$tmpnft" || {
			rm -f "$tmpnft"
			die "nie udało się przygotować firewall.nft"
		}
		put "$tmpnft" /etc/chameleontak/firewall.nft 0644
		rm -f "$tmpnft"
		log "początkowa zapora: tcp { ${list} }"
	fi
else
	: >"$R/etc/chameleontak/firewall.disabled"
	chmod 0644 "$R/etc/chameleontak/firewall.disabled"
	log "zapora NIE jest zarządzana przez ChameleonTAK (/etc/chameleontak/firewall.disabled)"
fi

log "gotowe (cel: ${ROOT})"
