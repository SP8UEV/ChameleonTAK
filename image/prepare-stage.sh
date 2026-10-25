#!/usr/bin/env bash
# Przygotowuje etap pi-gen image/stage-chameleontak do budowania obrazu:
#   - kopiuje binarkę linux/arm64 do 00-chameleontak/files/chameleontak,
#   - kopiuje katalog packaging/ do 00-chameleontak/files/packaging/ (etap musi być samowystarczalny,
#     bo pi-gen-action montuje do kontenera tylko katalog etapu, a build-docker.sh kopiuje tylko pi-gen/),
#   - nadaje bity wykonywalności skryptom etapu (pi-gen uruchamia tylko pliki wykonywalne, a checkout
#     na Windows / z core.fileMode=false może je zgubić),
#   - sprawdza, że żaden plik nie ma końców linii CRLF.
#
# Użycie: image/prepare-stage.sh ŚCIEŻKA_DO_BINARKI_LINUX_ARM64
set -euo pipefail

die() {
	printf 'prepare-stage: BŁĄD: %s\n' "$*" >&2
	exit 1
}

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STAGE="${REPO_ROOT}/image/stage-chameleontak"
FILES="${STAGE}/00-chameleontak/files"
BIN="${1:-}"

[ -n "$BIN" ] || die "użycie: $0 ŚCIEŻKA_DO_BINARKI_LINUX_ARM64"
[ -f "$BIN" ] || die "brak pliku: $BIN"
[ -d "${REPO_ROOT}/packaging" ] || die "brak katalogu ${REPO_ROOT}/packaging"

# ELF (7f 45 4c 46), e_machine = AArch64 (0xb7)
magic="$(od -An -tx1 -N4 "$BIN" | tr -d ' \n')"
machine="$(od -An -tx1 -j18 -N2 "$BIN" | tr -d ' \n')"
if [ "$magic" != "7f454c46" ] || [ "$machine" != "b700" ]; then
	die "$BIN nie jest binarką linux/arm64 (magic=${magic}, e_machine=${machine})"
fi

rm -rf "$FILES"
mkdir -p "$FILES"
install -m 0755 "$BIN" "${FILES}/chameleontak"
cp -R "${REPO_ROOT}/packaging" "${FILES}/packaging"

cr=$'\r'
crlf="$(grep -rIlU -e "$cr" "$STAGE" || true)"
[ -z "$crlf" ] || die "pliki z końcami linii CRLF (wymagane LF):"$'\n'"$crlf"

chmod 0755 "${STAGE}/prerun.sh"
for f in "${STAGE}"/*/[0-9][0-9]-run.sh "${STAGE}"/*/[0-9][0-9]-run-chroot.sh \
	"${FILES}/packaging/install-files.sh" "${FILES}/packaging/lib/"*.sh; do
	if [ -e "$f" ]; then
		chmod 0755 "$f"
	fi
done

echo "prepare-stage: etap gotowy: ${STAGE}"
