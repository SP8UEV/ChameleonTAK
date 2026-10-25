#!/bin/bash -e
# shellcheck disable=SC2154 # ROOTFS_DIR i copy_previous dostarcza pi-gen (build.sh / scripts/common)

# Standardowy prerun pi-gen: zaczynamy od kopii systemu z poprzedniego etapu (stage2 = Lite).
if [ ! -d "${ROOTFS_DIR}" ]; then
	copy_previous
fi
