import type { AccessMode } from '../api/types';

const IPV4 = /^\d{1,3}(\.\d{1,3}){3}$/;

/** Czy nazwa wygląda na publiczną domenę (dla Let's Encrypt)? */
export function looksPublicDomain(name: string): boolean {
  const n = name.trim().toLowerCase();
  if (!n || IPV4.test(n) || n.includes(':')) return false;
  if (!/^[a-z0-9-]+(\.[a-z0-9-]+)+$/.test(n)) return false;
  const tld = n.split('.').pop() ?? '';
  if (!/^[a-z]{2,}$/.test(tld)) return false;
  return !['local', 'lan', 'home', 'internal', 'localdomain', 'arpa', 'test', 'invalid', 'localhost', 'corp'].includes(tld);
}

/** Czy to adres IP osiągalny z internetu (nie prywatny, nie CGNAT)? Odpowiada IsPublicIP na serwerze. */
export function isPublicIp(name: string): boolean {
  const n = name.trim().toLowerCase();
  if (IPV4.test(n)) {
    const [a, b] = n.split('.').map(Number);
    if (n.split('.').some((p) => Number(p) > 255)) return false;
    if (a === 0 || a === 10 || a === 127 || a >= 224) return false;
    if (a === 169 && b === 254) return false;
    if (a === 172 && b >= 16 && b <= 31) return false;
    if (a === 192 && b === 168) return false;
    if (a === 100 && b >= 64 && b <= 127) return false; // CGNAT
    return true;
  }
  if (n.includes(':') && /^[0-9a-f:]+$/.test(n)) {
    return !(n === '::1' || n.startsWith('fe80') || n.startsWith('fc') || n.startsWith('fd') || n.startsWith('ff'));
  }
  return false;
}

export function accessModeOf(name: string): AccessMode {
  if (looksPublicDomain(name)) return 'domain';
  if (isPublicIp(name)) return 'ip';
  return 'local';
}

export function validEmail(s: string): boolean {
  return /^\S+@\S+\.\S+$/.test(s.trim());
}
