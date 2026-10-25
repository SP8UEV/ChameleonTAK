// Formatowanie po polsku: daty, rozmiary, liczebniki.

const dtf = new Intl.DateTimeFormat('pl-PL', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
});
const df = new Intl.DateTimeFormat('pl-PL', { day: 'numeric', month: 'long', year: 'numeric' });
const tf = new Intl.DateTimeFormat('pl-PL', { hour: '2-digit', minute: '2-digit' });
const tfs = new Intl.DateTimeFormat('pl-PL', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
const nf1 = new Intl.NumberFormat('pl-PL', { maximumFractionDigits: 1 });

function parse(s: string | null | undefined): Date | null {
  if (!s) return null;
  const d = new Date(s);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1971) return null;
  return d;
}

export function fmtDateTime(s: string | null | undefined): string {
  const d = parse(s);
  return d ? dtf.format(d) : '—';
}

export function fmtDate(s: string | null | undefined): string {
  const d = parse(s);
  return d ? df.format(d) : '—';
}

export function fmtTime(s: string | null | undefined, seconds = false): string {
  const d = parse(s);
  return d ? (seconds ? tfs : tf).format(d) : '—';
}

/** Liczebnik: plural(5, 'plik', 'pliki', 'plików') → „plików”. */
export function plural(n: number, one: string, few: string, many: string): string {
  const abs = Math.abs(n);
  if (abs === 1) return one;
  const d = abs % 10;
  const dd = abs % 100;
  if (d >= 2 && d <= 4 && (dd < 12 || dd > 14)) return few;
  return many;
}

export function countLabel(n: number, one: string, few: string, many: string): string {
  return `${n} ${plural(n, one, few, many)}`;
}

/** „5 min temu”, „za 2 dni”. */
export function fmtRelative(s: string | null | undefined, now = Date.now()): string {
  const d = parse(s);
  if (!d) return '—';
  const diff = Math.round((d.getTime() - now) / 1000);
  const abs = Math.abs(diff);
  const future = diff > 0;
  let text: string;
  if (abs < 45) return future ? 'za chwilę' : 'przed chwilą';
  if (abs < 3600) {
    const m = Math.round(abs / 60);
    text = `${m} min`;
  } else if (abs < 86400) {
    const h = Math.round(abs / 3600);
    text = countLabel(h, 'godzinę', 'godziny', 'godzin');
  } else if (abs < 86400 * 60) {
    const days = Math.round(abs / 86400);
    text = days === 1 ? '1 dzień' : `${days} dni`;
  } else {
    return fmtDate(s);
  }
  return future ? `za ${text}` : `${text} temu`;
}

export function daysUntil(s: string | null | undefined, now = Date.now()): number | null {
  const d = parse(s);
  if (!d) return null;
  return Math.floor((d.getTime() - now) / 86400000);
}

export function isPast(s: string | null | undefined, now = Date.now()): boolean {
  const d = parse(s);
  return d ? d.getTime() < now : false;
}

export function fmtBytes(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return '—';
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${nf1.format(v)} ${units[i]}`;
}

export function fmtNumber(n: number | null | undefined, digits = 1): string {
  if (n == null || !Number.isFinite(n)) return '—';
  return new Intl.NumberFormat('pl-PL', { maximumFractionDigits: digits }).format(n);
}

export function fmtUptime(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec)) return '—';
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (d > 0) return `${d} d ${h} h`;
  if (h > 0) return `${h} h ${m} min`;
  return `${m} min`;
}

/** Prędkość m/s → km/h. */
export function fmtSpeed(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms) || ms < 0) return '—';
  return `${nf1.format(ms * 3.6)} km/h`;
}

export function percent(used: number, total: number): number {
  if (!total || total <= 0) return 0;
  return Math.max(0, Math.min(100, (used / total) * 100));
}
