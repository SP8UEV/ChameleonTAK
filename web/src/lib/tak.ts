import type { PlatformKey } from '../api/types';

export const PLATFORMS: { key: PlatformKey; label: string; os: string; short: string }[] = [
  { key: 'atak', label: 'ATAK', os: 'Android', short: 'ATAK' },
  { key: 'itak', label: 'iTAK', os: 'iPhone / iPad', short: 'iTAK' },
  { key: 'wintak', label: 'WinTAK', os: 'Windows', short: 'WinTAK' },
  { key: 'takaware', label: 'TAKAware', os: 'iPhone / iPad', short: 'TAKAware' },
];

export function platformLabel(key: string): string {
  return PLATFORMS.find((p) => p.key === key)?.label ?? key;
}

export function platformOs(key: string): string {
  return PLATFORMS.find((p) => p.key === key)?.os ?? '';
}

/** Rozpoznaje platformę z navigator.userAgent. */
export function detectPlatform(ua: string = navigator.userAgent): PlatformKey | null {
  const nav = navigator as Navigator & { maxTouchPoints?: number };
  if (/android/i.test(ua)) return 'atak';
  if (/iphone|ipad|ipod/i.test(ua)) return 'itak';
  // iPadOS udaje Maca; rozpoznajemy po ekranie dotykowym.
  if (/macintosh/i.test(ua) && (nav.maxTouchPoints ?? 0) > 1) return 'itak';
  if (/windows/i.test(ua)) return 'wintak';
  return null;
}

// Kolory zespołów ATAK (wartości jak w ATAK, etykiety po polsku).
export const TEAMS: { value: string; label: string; color: string }[] = [
  { value: 'White', label: 'Biały', color: '#ffffff' },
  { value: 'Yellow', label: 'Żółty', color: '#ffeb3b' },
  { value: 'Orange', label: 'Pomarańczowy', color: '#ff8c00' },
  { value: 'Magenta', label: 'Magenta', color: '#ff00ff' },
  { value: 'Red', label: 'Czerwony', color: '#f02020' },
  { value: 'Maroon', label: 'Bordowy', color: '#8b1a1a' },
  { value: 'Purple', label: 'Fioletowy', color: '#8e3fc0' },
  { value: 'Dark Blue', label: 'Granatowy', color: '#1a2f9e' },
  { value: 'Blue', label: 'Niebieski', color: '#2f6bff' },
  { value: 'Cyan', label: 'Cyjan', color: '#00d0e0' },
  { value: 'Teal', label: 'Morski', color: '#0f8f8a' },
  { value: 'Green', label: 'Zielony', color: '#33c24d' },
  { value: 'Dark Green', label: 'Ciemnozielony', color: '#19702b' },
  { value: 'Brown', label: 'Brązowy', color: '#8a5a2b' },
];

export const OFFLINE_COLOR = '#8a8f86';

export function teamColor(team: string | null | undefined): string {
  if (!team) return OFFLINE_COLOR;
  const t = TEAMS.find((x) => x.value.toLowerCase() === team.toLowerCase());
  return t?.color ?? OFFLINE_COLOR;
}

export function teamLabel(team: string | null | undefined): string {
  if (!team) return '—';
  return TEAMS.find((x) => x.value.toLowerCase() === team.toLowerCase())?.label ?? team;
}

// Role ATAK (wartości po angielsku — tak oczekuje ATAK).
export const ROLES: { value: string; label: string }[] = [
  { value: 'Team Member', label: 'Członek zespołu' },
  { value: 'Team Lead', label: 'Dowódca zespołu' },
  { value: 'HQ', label: 'Sztab (HQ)' },
  { value: 'Sniper', label: 'Strzelec wyborowy' },
  { value: 'Medic', label: 'Medyk' },
  { value: 'Forward Observer', label: 'Obserwator' },
  { value: 'RTO', label: 'Łącznościowiec (RTO)' },
  { value: 'K9', label: 'Przewodnik psa (K9)' },
];

export function roleLabel(role: string | null | undefined): string {
  if (!role) return '—';
  return ROLES.find((r) => r.value === role)?.label ?? role;
}
