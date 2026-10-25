// Mały klient REST: JSON, błędy jako Error(body.error), obsługa 401.

export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

type UnauthorizedHandler = () => void;
let onUnauthorized: UnauthorizedHandler | null = null;

/** Rejestruje funkcję wywoływaną przy każdym 401 (przekierowanie do logowania). */
export function setUnauthorizedHandler(fn: UnauthorizedHandler | null) {
  onUnauthorized = fn;
}

export interface RequestOptions {
  /** Nie przekierowuj do logowania przy 401 (np. sam formularz logowania). */
  noAuthRedirect?: boolean;
  signal?: AbortSignal;
}

async function parseError(res: Response): Promise<string> {
  let message = '';
  try {
    const text = await res.text();
    if (text) {
      try {
        const body = JSON.parse(text) as { error?: unknown };
        if (body && typeof body.error === 'string') message = body.error;
      } catch {
        message = text.length < 200 ? text : '';
      }
    }
  } catch {
    /* brak treści */
  }
  if (!message) {
    if (res.status === 401) message = 'Sesja wygasła. Zaloguj się ponownie.';
    else if (res.status === 403) message = 'Brak uprawnień do tej operacji.';
    else if (res.status === 404) message = 'Nie znaleziono.';
    else if (res.status >= 500) message = `Błąd serwera (${res.status}).`;
    else message = `Żądanie nie powiodło się (${res.status}).`;
  }
  return message;
}

async function request<T>(method: string, path: string, body?: unknown, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  let payload: BodyInit | undefined;
  if (body instanceof FormData) {
    payload = body;
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    payload = JSON.stringify(body);
  }

  let res: Response;
  try {
    res = await fetch(path, {
      method,
      headers,
      body: payload,
      credentials: 'same-origin',
      signal: opts.signal,
    });
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e;
    throw new ApiError('Brak połączenia z serwerem. Sprawdź sieć i spróbuj ponownie.', 0);
  }

  if (res.status === 401 && !opts.noAuthRedirect && onUnauthorized) {
    onUnauthorized();
  }
  if (!res.ok) {
    throw new ApiError(await parseError(res), res.status);
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  if (!text) return undefined as T;
  try {
    return JSON.parse(text) as T;
  } catch {
    return text as unknown as T;
  }
}

export const http = {
  get: <T>(path: string, opts?: RequestOptions) => request<T>('GET', path, undefined, opts),
  post: <T>(path: string, body?: unknown, opts?: RequestOptions) => request<T>('POST', path, body ?? {}, opts),
  put: <T>(path: string, body?: unknown, opts?: RequestOptions) => request<T>('PUT', path, body ?? {}, opts),
  del: <T>(path: string, opts?: RequestOptions) => request<T>('DELETE', path, undefined, opts),
  upload: <T>(path: string, file: File, field = 'file', opts?: RequestOptions) => {
    const fd = new FormData();
    fd.append(field, file, file.name);
    return request<T>('POST', path, fd, opts);
  },
};

export function errorMessage(e: unknown): string {
  if (e instanceof Error) return e.message;
  return String(e);
}
