import { useEffect, useMemo, useRef, useState } from 'react';
import { MessagesSquare, Send } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import type { ChatMessage, Client } from '../api/types';
import { useToast } from '../components/Toast';
import { AsyncView, Empty, PageHeader } from '../components/ui';
import { fmtDate, fmtTime } from '../lib/format';
import { useApi } from '../lib/useApi';
import { useLive } from '../lib/ws';

const ALL_ROOMS = ['', 'All Chat Rooms', 'all', '*'];

function sortByTime(list: ChatMessage[]): ChatMessage[] {
  return [...list].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime());
}

function upsert(list: ChatMessage[] | undefined, m: ChatMessage): ChatMessage[] {
  const base = list ?? [];
  if (base.some((x) => String(x.id) === String(m.id))) return base;
  return sortByTime([...base, m]);
}

export default function Chat() {
  const messages = useApi(() => api.chat(200).then(sortByTime), []);
  const clients = useApi(api.clients, [], 30000);
  const toast = useToast();
  const [text, setText] = useState('');
  const [to, setTo] = useState('');
  const [sending, setSending] = useState(false);
  const logRef = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  useLive((msg) => {
    if (msg.type === 'chat') messages.setData((list) => upsert(list, msg.data));
    if (msg.type === 'client') {
      const { event, client } = msg.data;
      clients.setData((list) => {
        if (!list) return list;
        const rest = list.filter((c) => c.id !== client.id);
        return event === 'disconnect' ? rest : [...rest, client];
      });
    }
  });

  // Przewijaj na dół, jeśli użytkownik był na dole.
  useEffect(() => {
    const el = logRef.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [messages.data]);

  const recipients = useMemo(() => {
    const seen = new Set<string>();
    return (clients.data ?? [])
      .filter((c) => {
        if (seen.has(c.uid)) return false;
        seen.add(c.uid);
        return true;
      })
      .sort((a, b) => (a.callsign || a.uid).localeCompare(b.callsign || b.uid, 'pl'));
  }, [clients.data]);

  const nameFor = (uidOrName: string, list: Client[]) => {
    if (ALL_ROOMS.includes(uidOrName)) return 'wszyscy';
    return list.find((c) => c.uid === uidOrName)?.callsign || uidOrName;
  };

  const send = async () => {
    const t = text.trim();
    if (!t) return;
    setSending(true);
    try {
      const res = await api.sendChat(t, to || undefined);
      if (res && typeof res === 'object' && 'id' in res && 'text' in res) messages.setData((list) => upsert(list, res));
      setText('');
      stick.current = true;
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setSending(false);
    }
  };


  return (
    <>
      <PageHeader title="Czat" description="Wiadomości GeoChat z aplikacji. Wiadomości wysłane stąd przychodzą od serwera." />
      <div className="chat">
        <div
          className="chat-log"
          ref={logRef}
          onScroll={(e) => {
            const el = e.currentTarget;
            stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 60;
          }}
          aria-live="polite"
        >
          <AsyncView state={messages}>
            {(list) =>
              list.length === 0 ? (
                <Empty icon={MessagesSquare} title="Cisza w eterze" compact>
                  Wiadomości z czatu ATAK i iTAK pojawią się tutaj na żywo.
                </Empty>
              ) : (
                <ol className="chat-list">
                  {list.map((m, idx) => {
                    const day = fmtDate(m.time);
                    const showDay = idx === 0 || fmtDate(list[idx - 1].time) !== day;
                    const toName = nameFor(m.to, recipients);
                    return (
                      <li key={String(m.id)}>
                        {showDay && <div className="chat-day">{day}</div>}
                        <div className="chat-msg">
                          <time className="chat-time" dateTime={m.time}>
                            {fmtTime(m.time)}
                          </time>
                          <div className="grow" style={{ minWidth: 0 }}>
                            <div className="chat-meta">
                              <span className="chat-from">{m.from || m.fromUid}</span>
                              {toName !== 'wszyscy' && <span className="chat-to">do {toName}</span>}
                            </div>
                            <div className="chat-text">{m.text}</div>
                          </div>
                        </div>
                      </li>
                    );
                  })}
                </ol>
              )
            }
          </AsyncView>
        </div>
        <form
          className="chat-composer"
          onSubmit={(e) => {
            e.preventDefault();
            void send();
          }}
        >
          <select className="select chat-to-select" value={to} onChange={(e) => setTo(e.target.value)} aria-label="Odbiorca">
            <option value="">Do wszystkich</option>
            {recipients.map((c) => (
              <option key={c.uid} value={c.uid}>
                {c.callsign || c.uid}
              </option>
            ))}
          </select>
          <div className="chat-input-row">
            <textarea
              className="textarea chat-input"
              rows={1}
              placeholder="Napisz wiadomość…"
              value={text}
              maxLength={2000}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  void send();
                }
              }}
            />
            <button type="submit" className="btn btn-primary chat-send" disabled={sending || !text.trim()} aria-label="Wyślij">
              <Send size={18} /> <span className="hide-sm">Wyślij</span>
            </button>
          </div>
        </form>
      </div>
    </>
  );
}
