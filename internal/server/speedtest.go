package server

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"time"

	"chameleontak/internal/system"
)

// Network self-test: the speed of the Pi's internet link (measured from the Pi against
// Cloudflare's speed test) and of the local link between the browser and the Pi (the browser
// downloads and uploads test data to the panel). The panel turns the numbers into an estimate
// of how many TAK users and video viewers the links carry.

const (
	speedDownURL = "https://speed.cloudflare.com/__down?bytes=%d"
	speedUpURL   = "https://speed.cloudflare.com/__up"
	maxLocalTest = 200 << 20
)

var speedBlock = func() []byte {
	b := make([]byte, 256<<10)
	_, _ = rand.Read(b) // incompressible, so no proxy shrinks it
	return b
}()

type internetSpeed struct {
	PingMs   float64   `json:"pingMs"`
	JitterMs float64   `json:"jitterMs"`
	DownMbps float64   `json:"downMbps"`
	UpMbps   float64   `json:"upMbps"`
	Error    string    `json:"error"`
	Checked  time.Time `json:"checked"`
}

type serverLoad struct {
	Clients  int     `json:"clients"` // TAK clients on every server
	Planes   int     `json:"planes"`  // aircraft the ADS-B bridge sends
	CPUs     int     `json:"cpus"`
	Load1    float64 `json:"load1"`
	MemTotal uint64  `json:"memTotal"`
	MemAvail uint64  `json:"memAvail"`
}

var speedMu sync.Mutex // one internet test at a time

func (a *App) hSpeedInternet(w http.ResponseWriter, r *http.Request) {
	if !speedMu.TryLock() {
		fail(w, invalid("Test łącza już trwa."))
		return
	}
	defer speedMu.Unlock()
	res := measureInternet(r.Context())
	if res.Error == "" {
		a.log.Info("test łącza internetowego", "ping_ms", res.PingMs, "pobieranie_mbps", res.DownMbps, "wysyłanie_mbps", res.UpMbps)
	}
	writeJSON(w, 200, map[string]any{"internet": res, "load": a.serverLoad()})
}

func (a *App) hSpeedLoad(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.serverLoad())
}

func (a *App) serverLoad() serverLoad {
	l := serverLoad{CPUs: runtime.NumCPU()}
	for _, s := range a.servers() {
		l.Clients += s.router.Count()
	}
	if cfg := a.cfg.Get(); cfg.ADSB.Bridge {
		a.adsb.mu.Lock()
		l.Planes = len(a.adsb.planes)
		a.adsb.mu.Unlock()
	}
	info := system.Collect(a.DataPath())
	l.Load1, l.MemTotal, l.MemAvail = info.Load1, info.MemTotal, info.MemTotal-info.MemUsed
	return l
}

func measureInternet(ctx context.Context) internetSpeed {
	res := internetSpeed{Checked: time.Now().UTC()}
	cl := &http.Client{Timeout: 20 * time.Second}

	// Latency: the median and spread of a few tiny requests on a warm connection.
	var rtts []float64
	for i := 0; i < 6; i++ {
		start := time.Now()
		if err := fetchDiscard(ctx, cl, fmt.Sprintf(speedDownURL, 0)); err != nil {
			res.Error = "Brak połączenia z serwerem testowym (speed.cloudflare.com). Czy Raspberry Pi ma internet?"
			return res
		}
		if i > 0 { // the first request also opens the connection
			rtts = append(rtts, float64(time.Since(start).Microseconds())/1000)
		}
	}
	res.PingMs, res.JitterMs = medianSpread(rtts)

	// Download and upload: as much as fits in about 6 seconds each.
	res.DownMbps = timedTransfer(ctx, cl, true)
	res.UpMbps = timedTransfer(ctx, cl, false)
	if res.DownMbps == 0 && res.UpMbps == 0 {
		res.Error = "Test prędkości nie powiódł się."
	}
	return res
}

func fetchDiscard(ctx context.Context, cl *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("odpowiedź %d", resp.StatusCode)
	}
	return err
}

// timedTransfer moves data in 10 MB requests until about 6 s pass and returns megabits per second.
func timedTransfer(ctx context.Context, cl *http.Client, down bool) float64 {
	const chunk = 10 << 20
	deadline := time.Now().Add(6 * time.Second)
	var moved int64
	start := time.Now()
	for time.Now().Before(deadline) {
		var req *http.Request
		var err error
		if down {
			req, err = http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(speedDownURL, chunk), nil)
		} else {
			req, err = http.NewRequestWithContext(ctx, http.MethodPost, speedUpURL, &repeatReader{left: chunk})
			if req != nil {
				req.ContentLength = chunk
				req.Header.Set("Content-Type", "application/octet-stream")
			}
		}
		if err != nil {
			break
		}
		resp, err := cl.Do(req)
		if err != nil {
			break
		}
		n, _ := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if down {
			moved += n
		} else if resp.StatusCode == 200 {
			moved += chunk
		}
		if resp.StatusCode != 200 {
			break
		}
	}
	secs := time.Since(start).Seconds()
	if moved == 0 || secs <= 0 {
		return 0
	}
	return float64(moved) * 8 / secs / 1e6
}

// repeatReader yields left bytes of random test data.
type repeatReader struct{ left int64 }

func (r *repeatReader) Read(p []byte) (int, error) {
	if r.left <= 0 {
		return 0, io.EOF
	}
	n := copy(p, speedBlock)
	if int64(n) > r.left {
		n = int(r.left)
	}
	r.left -= int64(n)
	return n, nil
}

func medianSpread(v []float64) (median, spread float64) {
	if len(v) == 0 {
		return 0, 0
	}
	s := append([]float64(nil), v...)
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
	return s[len(s)/2], s[len(s)-1] - s[0]
}

// ---------- local link (browser <-> Pi) ----------

func (a *App) hSpeedDown(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.ParseInt(r.URL.Query().Get("bytes"), 10, 64)
	if err != nil || n <= 0 || n > maxLocalTest {
		n = 25 << 20
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(n, 10))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, &repeatReader{left: n})
}

func (a *App) hSpeedUp(w http.ResponseWriter, r *http.Request) {
	n, err := io.Copy(io.Discard, io.LimitReader(r.Body, maxLocalTest))
	if err != nil && !errors.Is(err, io.EOF) {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"bytes": n})
}

func (a *App) hSpeedPing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok"))
}
