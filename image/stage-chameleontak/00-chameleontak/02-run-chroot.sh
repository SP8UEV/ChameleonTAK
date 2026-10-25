#!/bin/bash -e
# Etap ChameleonTAK (wewnątrz chroota obrazu). pi-gen podaje ten plik na stdin powłoki
# w chroocie, więc linia #! nie działa - stąd jawne `set -e`.
set -e

# Użytkownik chameleontak, walidacja sudoers (visudo -c), włączenie usług:
# chameleontak, avahi-daemon, nftables oraz hotspot kreatora (chameleontak-network-fallback).
/usr/local/lib/chameleontak/setup-system.sh --hotspot enable

# Obraz startuje prosto do ChameleonTAK: tryb tekstowy, bez interaktywnych kreatorów.
# (Kreator zmiany nazwy użytkownika jest wyłączony przez DISABLE_FIRST_BOOT_USER_RENAME=1
#  w konfiguracji pi-gen; konto "tak" ma hasło ustawione w FIRST_USER_PASS.)
systemctl set-default multi-user.target

# Kontrola końcowa - przerwij budowanie, jeśli czegoś brakuje.
test -x /usr/local/bin/chameleontak
test -f /etc/chameleontak/firewall.nft
test -f /etc/avahi/services/chameleontak.service
systemctl is-enabled chameleontak.service
systemctl is-enabled chameleontak-network-fallback.service
systemctl is-enabled nftables.service
systemctl is-enabled avahi-daemon.service
visudo -c
