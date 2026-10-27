package stream

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"time"

	"chameleontak/internal/cot"
)

const (
	idleTimeout = 5 * time.Minute
	firstLogged = 5 // events logged at the start of each connection
)

func head(b []byte, n int) string {
	if len(b) > n {
		b = b[:n]
	}
	return string(b)
}

// Authenticator maps a verified TLS client certificate to an identity.
type Authenticator func(state tls.ConnectionState) (Identity, error)

// Listener accepts CoT streaming connections on one port.
type Listener struct {
	r         *Router
	ln        net.Listener
	transport string
	auth      Authenticator
}

// ListenTLS starts the mutual-TLS CoT port (8089).
func (r *Router) ListenTLS(addr string, cfg *tls.Config, auth Authenticator) (*Listener, error) {
	ln, err := tls.Listen("tcp", addr, cfg)
	if err != nil {
		return nil, err
	}
	l := &Listener{r: r, ln: ln, transport: "tls", auth: auth}
	go l.serve()
	return l, nil
}

// ListenTCP starts the unauthenticated CoT port (8087); clients land in the anonymous group.
func (r *Router) ListenTCP(addr string) (*Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	l := &Listener{r: r, ln: ln, transport: "tcp"}
	go l.serve()
	return l, nil
}

func (l *Listener) Close() error { return l.ln.Close() }

func (l *Listener) serve() {
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			l.r.log.Warn("accept", "err", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		go l.handle(conn)
	}
}

func (l *Listener) handle(conn net.Conn) {
	var id Identity
	if tc, ok := conn.(*tls.Conn); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := tc.HandshakeContext(ctx)
		cancel()
		if err != nil {
			l.r.log.Info("nieudany handshake TLS (strumień CoT)", "ip", conn.RemoteAddr().String(), "err", err)
			conn.Close()
			return
		}
		id, err = l.auth(tc.ConnectionState())
		if err != nil {
			l.r.log.Info("odrzucono połączenie CoT", "remote", conn.RemoteAddr().String(), "err", err)
			conn.Close()
			return
		}
	}

	c := l.r.newClient(conn, l.transport, id)
	go c.writeLoop()
	l.r.register(c)
	l.r.log.Info("klient połączony", "id", c.ID, "user", id.Username, "ip", c.IP)

	defer func() {
		c.Close()
		l.r.unregister(c)
		l.r.log.Info("klient rozłączony", "id", c.ID, "user", id.Username, "callsign", c.Callsign())
	}()

	fr := cot.NewFramer(conn)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(idleTimeout))
		raw, err := fr.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				l.r.log.Debug("odczyt CoT", "id", c.ID, "err", err)
			}
			return
		}
		// The first few events of a connection go to the log, so a client that never shows up on
		// the map can be diagnosed.
		first := c.received.Add(1) <= firstLogged
		ev, err := cot.Parse(raw)
		if err != nil {
			// Non-event top-level elements (e.g. <auth>) are ignored.
			if first {
				l.r.log.Info("nieczytelny element CoT", "id", c.ID, "err", err, "head", head(raw, 120))
			}
			continue
		}
		if first {
			l.r.log.Info("zdarzenie CoT", "id", c.ID, "type", ev.Type, "uid", ev.UID, "callsign", ev.Callsign,
				"app", ev.TakvPlatform, "team", ev.GroupName, "sa", ev.IsSA(), "pos", ev.Lat != 0 || ev.Lon != 0)
		}
		l.r.Handle(c, ev)
	}
}
