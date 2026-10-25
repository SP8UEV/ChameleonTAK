# ChameleonTAK — do zrobienia

Lista aktualizowana na bieżąco. Zrobione pozycje przenosimy na dół do „Zrobione”.

## Do zrobienia

- [ ] **Paczka ATAK się nie importuje** (zgłoszone na v0.1.1, Raspberry 192.168.1.102, kabel LAN). Serwer i porty
      działają, certyfikat OK — potrzebny log z Raspberry albo komunikat z ATAK.
- [ ] **QR ma działać bez problemów** na Androidzie i iOS (najwyższy priorytet).
- [ ] **Tryby sieci w kreatorze:** lokalnie / własny hotspot / **publiczny adres IP** / **własna domena** / **ZeroTier**.
- [ ] **Obsługa LoRa** (Meshtastic — sprzęt jeszcze nie wybrany; najpewniej moduł na USB).

## Do sprawdzenia na prawdziwym sprzęcie

- [ ] Pierwsze uruchomienie obrazu v0.1.1 na Raspberry Pi (kabel LAN i hotspot `ChameleonTAK-Setup`).
- [ ] Połączenie i paczki konfiguracyjne: ATAK, iTAK, WinTAK (paczka zagnieżdżona — niezweryfikowana),
      TAKAware (ryzyko: import truststore bez klucza prywatnego).
- [ ] Misje / Data Sync i udostępnianie plików z prawdziwymi klientami.

## Znane ograniczenia (do przemyślenia)

- [ ] Kreator pierwszego uruchomienia jest dostępny bez logowania dla każdego w sieci, dopóki ktoś go nie dokończy.
- [ ] Link dołączania (`/join/…`) przez HTTP przesyła paczkę z certyfikatem otwartym tekstem w sieci lokalnej.
- [ ] Natywny QR w ATAK/iTAK działa tylko z Let's Encrypt (publiczna domena); w LAN zostaje QR aparatem + paczka.
- [ ] Po przywróceniu ustawień fabrycznych SSH nie da się włączyć z kreatora (blokada pomocnika root zostaje).

## Porządki

- [ ] Usunąć z GitHuba tag `v0.1.0` bez wydania: `git push origin --delete v0.1.0`.

## Zrobione

- [x] SSH włączone automatycznie w obrazie (konto `tak`); kreator zmienia hasło na hasło administratora.
- [x] Zapora otwiera port 22, gdy SSH działa (niezależnie od tego, jak je włączono); zapora stosowana przy każdym starcie.
- [x] Dziennik zdarzeń w panelu (Więcej → Dziennik): nieudane połączenia TLS, odmowy logowania, enrollment, pobrane paczki.
