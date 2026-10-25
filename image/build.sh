#!/usr/bin/env bash
# Lokalne budowanie obrazu ChameleonTAK (Linux + Docker) - to samo, co robi CI, bez GitHub Actions.
#
# Użycie:
#   image/build.sh ŚCIEŻKA_DO_BINARKI_LINUX_ARM64 [WERSJA]
#
# Zmienne środowiskowe:
#   PI_GEN_REPO          repozytorium pi-gen (domyślnie https://github.com/RPi-Distro/pi-gen.git)
#   PI_GEN_REF           gałąź/tag pi-gen (domyślnie "arm64" = 64-bitowy Raspberry Pi OS, Debian Trixie)
#   CONTINUE=1           wznowienie przerwanego budowania (build-docker.sh)
#   PRESERVE_CONTAINER=1 nie usuwaj kontenera pigen_work po budowaniu
#
# Wynik: image/out/chameleontak-<WERSJA>-arm64.img.xz oraz plik .sha256 obok.
# Wymagania: Docker (uprawnienia do uruchamiania kontenerów --privileged), git, ~20 GB wolnego miejsca.
# Na hoście x86_64 pi-gen sam rejestruje qemu-aarch64 (binfmt); gdyby się nie udało:
#   docker run --privileged --rm tonistiigi/binfmt --install arm64
set -euo pipefail

die() {
	printf 'build: BŁĄD: %s\n' "$*" >&2
	exit 1
}

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${1:-}"
VERSION="${2:-dev}"
PI_GEN_REPO="${PI_GEN_REPO:-https://github.com/RPi-Distro/pi-gen.git}"
PI_GEN_REF="${PI_GEN_REF:-arm64}"
WORK="${REPO_ROOT}/image/.build"
PIGEN="${WORK}/pi-gen"
OUT="${REPO_ROOT}/image/out"

[ -n "$BIN" ] || die "użycie: $0 ŚCIEŻKA_DO_BINARKI_LINUX_ARM64 [WERSJA]"
for cmd in docker git; do
	command -v "$cmd" >/dev/null 2>&1 || die "brak polecenia: $cmd"
done
BIN="$(cd "$(dirname "$BIN")" && pwd)/$(basename "$BIN")"

# 1. Etap: binarka + packaging/ + bity wykonywalności.
"${REPO_ROOT}/image/prepare-stage.sh" "$BIN"

# 2. pi-gen (gałąź arm64).
mkdir -p "$WORK"
if [ -d "${PIGEN}/.git" ]; then
	git -C "$PIGEN" fetch --depth 1 origin "$PI_GEN_REF"
	git -C "$PIGEN" checkout -f FETCH_HEAD
else
	git clone --depth 1 --branch "$PI_GEN_REF" "$PI_GEN_REPO" "$PIGEN"
fi

# 3. Konfiguracja + nasz etap wewnątrz katalogu pi-gen (build-docker.sh buduje kontener z tego katalogu).
cp "${REPO_ROOT}/image/config" "${PIGEN}/config"
rm -rf "${PIGEN}/stage-chameleontak"
cp -R "${REPO_ROOT}/image/stage-chameleontak" "${PIGEN}/stage-chameleontak"
# Eksportujemy tylko obraz ChameleonTAK, nie zwykły Raspberry Pi OS Lite ze stage2.
touch "${PIGEN}/stage2/SKIP_IMAGES"

# 4. Budowanie.
if [ "${CONTINUE:-0}" != "1" ]; then
	rm -rf "${PIGEN}/deploy"
fi
(cd "$PIGEN" && ./build-docker.sh)

# 5. Wynik.
img="$(find "${PIGEN}/deploy" -maxdepth 1 -name '*chameleontak*.img.xz' -printf '%T@ %p\n' 2>/dev/null |
	sort -nr | head -n 1 | cut -d' ' -f2- || true)"
[ -n "$img" ] || die "nie znaleziono *.img.xz w ${PIGEN}/deploy (sprawdź deploy/build.log)"
mkdir -p "$OUT"
name="chameleontak-${VERSION}-arm64.img.xz"
cp -f "$img" "${OUT}/${name}"
(cd "$OUT" && sha256sum "$name" >"${name}.sha256")
echo "build: gotowe: ${OUT}/${name}"
