#!/bin/bash -e
# shellcheck disable=SC2154 # ROOTFS_DIR eksportuje pi-gen (build.sh)
# Etap ChameleonTAK (host, katalog roboczy = ten podkatalog etapu):
# kopiuje binarkę i pliki systemowe do ${ROOTFS_DIR}.
#
# files/chameleontak  - binarka linux/arm64 (wkłada ją CI albo image/prepare-stage.sh)
# files/packaging/    - kopia katalogu packaging/ z repozytorium (j.w.)
set -euo pipefail

BIN="files/chameleontak"
PKG="files/packaging"

if [ ! -f "${BIN}" ]; then
	echo "BŁĄD: brak ${PWD}/${BIN} - uruchom najpierw image/prepare-stage.sh <binarka-linux-arm64>" >&2
	exit 1
fi
if [ ! -f "${PKG}/install-files.sh" ]; then
	echo "BŁĄD: brak ${PWD}/${PKG} - uruchom najpierw image/prepare-stage.sh <binarka-linux-arm64>" >&2
	exit 1
fi

# Sanity check: ELF (7f 45 4c 46), e_machine = AArch64 (0xb7).
magic="$(od -An -tx1 -N4 "${BIN}" | tr -d ' \n')"
machine="$(od -An -tx1 -j18 -N2 "${BIN}" | tr -d ' \n')"
if [ "${magic}" != "7f454c46" ] || [ "${machine}" != "b700" ]; then
	echo "BŁĄD: ${BIN} nie jest binarką linux/arm64 (magic=${magic}, e_machine=${machine})" >&2
	exit 1
fi

bash "${PKG}/install-files.sh" --root "${ROOTFS_DIR}" --binary "${BIN}"
