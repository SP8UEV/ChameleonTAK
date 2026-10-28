import { useEffect, useRef, useSyncExternalStore } from 'react';
import { withServer } from '../api/client';
import type { WsMessage } from '../api/types';

// Jedno współdzielone połączenie WebSocket z /api/ws.
// Otwierane, gdy ktoś subskrybuje; zamykane, gdy nie ma subskrybentów.
// Ponowne łączenie z wykładniczym opóźnieniem (1 s → 30 s).

type Handler = (msg: WsMessage) => void;
export type WsStatus = 'idle' | 'connecting' | 'open' | 'closed';

const handlers = new Set<Handler>();
const statusListeners = new Set<() => void>();
let socket: WebSocket | null = null;
let status: WsStatus = 'idle';
let retry = 0;
let retryTimer: number | undefined;

function setStatus(s: WsStatus) {
  if (status === s) return;
  status = s;
  statusListeners.forEach((l) => l());
}

function wsUrl() {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${proto}//${window.location.host}${withServer('/api/ws')}`;
}

function connect() {
  if (socket || handlers.size === 0) return;
  setStatus('connecting');
  let ws: WebSocket;
  try {
    ws = new WebSocket(wsUrl());
  } catch {
    scheduleReconnect();
    return;
  }
  socket = ws;
  ws.onopen = () => {
    retry = 0;
    setStatus('open');
  };
  ws.onmessage = (ev) => {
    if (typeof ev.data !== 'string') return;
    let msg: WsMessage;
    try {
      msg = JSON.parse(ev.data) as WsMessage;
    } catch {
      return;
    }
    if (!msg || typeof msg !== 'object' || !('type' in msg)) return;
    handlers.forEach((h) => {
      try {
        h(msg);
      } catch (e) {
        console.error(e);
      }
    });
  };
  ws.onclose = () => {
    if (socket === ws) socket = null;
    setStatus('closed');
    scheduleReconnect();
  };
  ws.onerror = () => {
    try {
      ws.close();
    } catch {
      /* ignoruj */
    }
  };
}

function scheduleReconnect() {
  if (handlers.size === 0) {
    setStatus('idle');
    return;
  }
  window.clearTimeout(retryTimer);
  const delay = Math.min(30000, 1000 * 2 ** retry) + Math.random() * 500;
  retry = Math.min(retry + 1, 6);
  retryTimer = window.setTimeout(connect, delay);
}

function disconnectIfIdle() {
  if (handlers.size > 0) return;
  window.clearTimeout(retryTimer);
  if (socket) {
    const s = socket;
    socket = null;
    s.onclose = null;
    s.close();
  }
  retry = 0;
  setStatus('idle');
}

export function subscribe(h: Handler): () => void {
  handlers.add(h);
  connect();
  return () => {
    handlers.delete(h);
    // Krótka zwłoka: przy zmianie strony nowa subskrypcja przejmie połączenie.
    window.setTimeout(disconnectIfIdle, 2000);
  };
}

/** Subskrybuje komunikaty WebSocket przez czas życia komponentu. */
export function useLive(handler: Handler) {
  const ref = useRef(handler);
  ref.current = handler;
  useEffect(() => subscribe((m) => ref.current(m)), []);
}

export function useWsStatus(): WsStatus {
  return useSyncExternalStore(
    (cb) => {
      statusListeners.add(cb);
      return () => statusListeners.delete(cb);
    },
    () => status,
  );
}
