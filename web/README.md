# ChameleonTAK — panel WWW

React 18 + TypeScript + Vite. Panel jest budowany do `../internal/panel/dist` i wbudowywany w binarkę Go (`embed`).

## Wymagania

Node 20.19+ (zalecany 24) i npm.

## Praca lokalna

```sh
npm install
# w osobnym terminalu: backend na :8080, np.
#   chameleontak serve --data ./data --http :8080 --dev
npm run dev            # http://localhost:5173
```

Serwer deweloperski Vite przekazuje `/api` (razem z WebSocketem `/api/ws`) do `http://localhost:8080`.

## Budowanie

```sh
npm run build          # tsc --noEmit + vite build → ../internal/panel/dist
npm run typecheck      # tylko sprawdzenie typów
```

`internal/panel/dist` jest czyszczony przy każdym buildzie. Backend musi serwować `index.html` dla każdej
ścieżki spoza `/api` i `/assets` (routing po stronie przeglądarki: `/login`, `/setup`, `/join/<token>`, `/users/7` …).

## Struktura

- `src/api/` — typy zgodne z `docs/ARCHITECTURE.md` §4 i klient `fetch` (błędy → `Error(body.error)`, 401 → logowanie).
- `src/lib/` — WebSocket na żywo (ponowne łączenie z narastającym opóźnieniem), formatowanie po polsku, motyw, kolory zespołów i role ATAK.
- `src/components/` — wspólne elementy: okna dialogowe, potwierdzenia, powiadomienia, kody QR, karta urządzenia.
- `src/pages/` — ekrany (każdy ładowany osobno).
- `src/styles/` — tokeny projektu (ciemny i jasny motyw) i style. Kroje Barlow / Barlow Condensed są dołączane lokalnie (działają bez internetu).
