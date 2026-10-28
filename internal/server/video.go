package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"chameleontak/internal/config"
	"chameleontak/internal/cot"
	"chameleontak/internal/system"
)

// Live video from phones (system/video.go): a phone publishes RTMP with the stream's key (e.g.
// from Larix Broadcaster), TAK clients in the stream's audience read it over RTSP with their
// username and a password derived from it, and the panel previews it over HLS through this
// server. MediaMTX asks /api/video/auth about every publish and read. While a stream is live,
// the audience gets it as a video feed (CoT b-i-v and /Marti/vcm) with their own credentials.

const (
	videoPanelUser = "chameleontak-panel"
	videoPublisher = "pub"
	videoFeedEvery = 30 * time.Second
)

type videoPath struct {
	Ready   bool  `json:"ready"`
	Readers int   `json:"readers"`
	BytesIn int64 `json:"bytesIn"`
}

// videoState is kept by the main server: what MediaMTX reports about each path.
type videoState struct {
	mu     sync.Mutex
	paths  map[string]videoPath
	err    string
	pushed map[string]time.Time // path -> last feed sent to the audience
}

func (a *App) videoPath(s config.VideoStream) string { return a.ID() + "-" + s.ID }

// videoToken is the password a user (or the panel) reads streams with.
func (a *App) videoToken(user string) string {
	h := a.host()
	secret := h.cfg.Get().Video.Secret
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(a.ID() + "\x00" + user))
	return hex.EncodeToString(mac.Sum(nil))[:24]
}

// canWatch reports whether a TAK user of this server is in the stream's audience.
func (a *App) canWatch(s config.VideoStream, username string) bool {
	u, err := a.store.GetUserByName(username)
	if err != nil || u.Disabled {
		return false
	}
	switch s.Audience {
	case "users":
		return slices.Contains(s.Users, username)
	case "groups":
		return slices.ContainsFunc(u.Groups, func(g string) bool { return slices.Contains(s.Groups, g) })
	}
	return true
}

// ---------- MediaMTX: authorization and status ----------

// hVideoAuth answers MediaMTX's authentication hook; only MediaMTX on loopback may ask.
func (a *App) hVideoAuth(w http.ResponseWriter, r *http.Request) {
	if ip := net.ParseIP(clientIP(r)); ip == nil || !ip.IsLoopback() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req struct {
		User     string `json:"user"`
		Password string `json:"password"`
		IP       string `json:"ip"`
		Action   string `json:"action"`
		Path     string `json:"path"`
		Protocol string `json:"protocol"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	srv, st, ok := a.findStream(req.Path)
	allowed := false
	if ok {
		switch req.Action {
		case "publish":
			allowed = req.User == videoPublisher && hmac.Equal([]byte(req.Password), []byte(st.PublishKey))
		case "read":
			switch {
			case req.User == videoPanelUser:
				allowed = hmac.Equal([]byte(req.Password), []byte(srv.videoToken(videoPanelUser)))
			case req.User != "":
				allowed = hmac.Equal([]byte(req.Password), []byte(srv.videoToken(req.User))) && srv.canWatch(st, req.User)
			}
		}
	}
	if !allowed {
		if req.User != "" || req.Action == "publish" {
			a.log.Info("wideo: odmowa", "akcja", req.Action, "ścieżka", req.Path, "użytkownik", req.User, "ip", req.IP)
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if req.Action == "publish" {
		a.log.Info("wideo: nadawanie", "strumień", st.Name, "serwer", srv.ID(), "ip", req.IP)
	}
	w.WriteHeader(http.StatusOK)
}

// findStream maps a MediaMTX path ("<server>-<stream>") to its server and stream.
func (a *App) findStream(path string) (*App, config.VideoStream, bool) {
	for _, s := range a.servers() {
		for _, st := range s.cfg.Get().Streams {
			if s.videoPath(st) == path {
				return s, st, true
			}
		}
	}
	return nil, config.VideoStream{}, false
}

// videoLoop (main server) polls MediaMTX for live paths and sends live streams to their
// audiences now and then, so a phone that connects later still gets them.
func (a *App) videoLoop(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		a.pollVideo(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (a *App) pollVideo(ctx context.Context) {
	cfg := a.cfg.Get()
	if !cfg.Video.Enabled || !system.VideoInstalled() {
		return
	}
	paths, err := mediamtxPaths(ctx)
	v := a.video
	v.mu.Lock()
	if err != nil {
		v.err = err.Error()
	} else {
		v.err, v.paths = "", paths
	}
	v.mu.Unlock()
	for _, s := range a.servers() {
		for _, st := range s.cfg.Get().Streams {
			path := s.videoPath(st)
			v.mu.Lock()
			live := v.paths[path].Ready
			due := live && time.Since(v.pushed[path]) >= videoFeedEvery
			if !live {
				delete(v.pushed, path)
			}
			if due {
				v.pushed[path] = time.Now()
			}
			v.mu.Unlock()
			if due {
				s.pushVideoFeed(st)
			}
		}
	}
}

func mediamtxPaths(ctx context.Context) (map[string]videoPath, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+system.VideoAPIAddr+"/v3/paths/list?itemsPerPage=1000", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serwer wideo nie odpowiada")
	}
	defer resp.Body.Close()
	var body struct {
		Items []struct {
			Name          string `json:"name"`
			Ready         bool   `json:"ready"`
			BytesReceived int64  `json:"bytesReceived"`
			Readers       []any  `json:"readers"`
		} `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("serwer wideo: nieczytelna odpowiedź")
	}
	out := map[string]videoPath{}
	for _, it := range body.Items {
		out[it.Name] = videoPath{Ready: it.Ready, Readers: len(it.Readers), BytesIn: it.BytesReceived}
	}
	return out, nil
}

func (a *App) videoLive(s config.VideoStream) videoPath {
	v := a.host().video
	if v == nil {
		return videoPath{}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.paths[a.videoPath(s)]
}

// ---------- delivery to TAK clients ----------

// viewerURL is the RTSP address a user plays the stream from, with their credentials.
// ATAK builds the URL from address, port and path, so the credentials go into the address.
func (a *App) viewerURL(st config.VideoStream, username string) (rtsp, address string, port int) {
	host := a.cfg.Get().ServerName
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	address = url.UserPassword(username, a.videoToken(username)).String() + "@" + host
	port = system.VideoRTSPPort
	return fmt.Sprintf("rtsp://%s:%d/%s", address, port, a.videoPath(st)), address, port
}

// pushVideoFeed sends the live stream to every connected member of its audience (CoT b-i-v,
// which ATAK adds to its video list), each with their own credentials.
func (a *App) pushVideoFeed(st config.VideoStream) {
	sent := map[string]bool{}
	for _, c := range a.router.Clients() {
		if c.UID == "" || c.Username == "" || sent[c.UID] || !a.canWatch(st, c.Username) {
			continue
		}
		sent[c.UID] = true
		a.router.SendToUIDs(a.videoFeedCoT(st, c.Username), []string{c.UID})
	}
}

func (a *App) videoFeedCoT(st config.VideoStream, username string) []byte {
	u, address, port := a.viewerURL(st, username)
	now := time.Now()
	uid := "video-" + a.videoPath(st)
	return []byte(fmt.Sprintf(`<event version="2.0" uid="%[1]s" type="b-i-v" how="m-g" time="%[2]s" start="%[2]s" stale="%[3]s"><point lat="0.0" lon="0.0" hae="0.0" ce="9999999.0" le="9999999.0"/><detail><__video uid="%[1]s" url="%[4]s"><ConnectionEntry networkTimeout="12000" uid="%[1]s" path="/%[5]s" protocol="rtsp" bufferTime="-1" address="%[6]s" port="%[7]d" roverPort="-1" rtspReliable="1" ignoreEmbeddedKLV="false" alias="%[8]s"/></__video><contact callsign="%[8]s"/></detail></event>`,
		cot.Escape(uid), cot.FormatTime(now), cot.FormatTime(now.Add(2*time.Minute)), cot.Escape(u),
		cot.Escape(a.videoPath(st)), cot.Escape(address), port, cot.Escape(st.Name)))
}

// mVideoConnections is TAK Server's video list (/Marti/vcm): the streams the caller may watch.
func (a *App) mVideoConnections(w http.ResponseWriter, r *http.Request) {
	caller := callerOf(r)
	type feed struct {
		UID          string `xml:"uid"`
		Active       bool   `xml:"active"`
		Alias        string `xml:"alias"`
		TypeName     string `xml:"typeName"`
		URL          string `xml:"url"`
		Protocol     string `xml:"protocol"`
		Address      string `xml:"address"`
		Port         int    `xml:"port"`
		Path         string `xml:"path"`
		RoverPort    int    `xml:"roverPort"`
		IgnoreKLV    bool   `xml:"ignoreEmbeddedKLV"`
		Timeout      int    `xml:"timeout"`
		Buffer       int    `xml:"buffer"`
		RTSPReliable int    `xml:"rtspReliable"`
	}
	out := struct {
		XMLName xml.Name `xml:"videoConnections"`
		Feeds   []feed   `xml:"feed"`
	}{}
	if a.host().cfg.Get().Video.Enabled {
		for _, st := range a.cfg.Get().Streams {
			if !a.canWatch(st, caller.User.Username) {
				continue
			}
			u, address, port := a.viewerURL(st, caller.User.Username)
			out.Feeds = append(out.Feeds, feed{
				UID: "video-" + a.videoPath(st), Active: a.videoLive(st).Ready, Alias: st.Name, TypeName: "Vid", URL: u,
				Protocol: "rtsp", Address: address, Port: port, Path: "/" + a.videoPath(st), RoverPort: -1,
				Timeout: 12000, Buffer: -1, RTSPReliable: 1,
			})
		}
	}
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(out)
}

// ---------- panel API: the media server (main server) ----------

type videoServiceView struct {
	Supported bool   `json:"supported"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Active    bool   `json:"active"`
	Error     string `json:"error"`
	RTMPPort  int    `json:"rtmpPort"`
	RTSPPort  int    `json:"rtspPort"`
}

func (a *App) videoServiceStatus() videoServiceView {
	h := a.host()
	v := videoServiceView{
		Supported: h.helper.Available(), Installed: system.VideoInstalled(), Enabled: h.cfg.Get().Video.Enabled,
		Active: system.ServiceActive(system.VideoUnit), RTMPPort: system.VideoRTMPPort, RTSPPort: system.VideoRTSPPort,
	}
	if h.video != nil {
		h.video.mu.Lock()
		v.Error = h.video.err
		h.video.mu.Unlock()
	}
	return v
}

func (a *App) hVideoService(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.videoServiceStatus())
}

func (a *App) hVideoInstall(w http.ResponseWriter, r *http.Request) {
	if !a.helper.Available() {
		fail(w, invalid("Serwer wideo instaluje się na Raspberry Pi z ChameleonTAK."))
		return
	}
	a.log.Info("wideo: instalacja serwera MediaMTX")
	if err := a.helper.Run("", "video-install"); err != nil {
		a.log.Error("wideo: instalacja nie powiodła się", "err", err)
		fail(w, err)
		return
	}
	if err := a.setVideoEnabled(true); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, a.videoServiceStatus())
}

func (a *App) hVideoEnable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	if err := a.setVideoEnabled(req.Enabled); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, a.videoServiceStatus())
}

// setVideoEnabled starts or stops MediaMTX and opens or closes its ports in the firewall.
func (a *App) setVideoEnabled(on bool) error {
	if _, err := a.cfg.Update(func(c *config.Config) error {
		c.Video.Enabled = on
		if c.Video.Secret == "" {
			c.Video.Secret = randomToken()
		}
		return nil
	}); err != nil {
		return err
	}
	if a.helper.Available() {
		var err error
		if on {
			err = a.helper.Run("", "video-on", strconv.Itoa(a.httpPort()))
		} else {
			err = a.helper.Run("", "video-off")
		}
		if err != nil {
			return err
		}
		if err := a.applyFirewall(); err != nil && err != system.ErrUnsupported {
			a.log.Warn("zapora", "err", err)
		}
	}
	return nil
}

// ---------- panel API: streams (per server) ----------

type streamView struct {
	config.VideoStream
	Path       string `json:"path"`
	Live       bool   `json:"live"`
	Viewers    int    `json:"viewers"`
	PublishURL string `json:"publishUrl"`
	LarixURL   string `json:"larixUrl"`
}

func (a *App) streamOut(st config.VideoStream) streamView {
	host := a.cfg.Get().ServerName
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	publish := fmt.Sprintf("rtmp://%s:%d/%s?user=%s&pass=%s", host, system.VideoRTMPPort, a.videoPath(st), videoPublisher, st.PublishKey)
	larix := "larix://set/v1?conn[][url]=" + url.QueryEscape(publish) + "&conn[][name]=" + url.QueryEscape("TAK: "+st.Name) + "&conn[][overwrite]=on"
	live := a.videoLive(st)
	if st.Groups == nil {
		st.Groups = []string{}
	}
	if st.Users == nil {
		st.Users = []string{}
	}
	return streamView{VideoStream: st, Path: a.videoPath(st), Live: live.Ready, Viewers: live.Readers, PublishURL: publish, LarixURL: larix}
}

func (a *App) hListStreams(w http.ResponseWriter, r *http.Request) {
	out := []streamView{}
	for _, st := range a.cfg.Get().Streams {
		out = append(out, a.streamOut(st))
	}
	writeJSON(w, 200, map[string]any{"streams": out, "service": a.videoServiceStatus()})
}

type streamInput struct {
	Name     string   `json:"name"`
	Audience string   `json:"audience"`
	Groups   []string `json:"groups"`
	Users    []string `json:"users"`
}

func (in *streamInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 64 {
		return invalid("Podaj nazwę strumienia (do 64 znaków), np. „Kamera Jan” albo „Dron 1”.")
	}
	switch in.Audience {
	case "", "all":
		in.Audience, in.Groups, in.Users = "all", nil, nil
	case "groups":
		if len(in.Groups) == 0 {
			return invalid("Wybierz co najmniej jedną grupę.")
		}
		in.Users = nil
	case "users":
		if len(in.Users) == 0 {
			return invalid("Wybierz co najmniej jednego użytkownika.")
		}
		in.Groups = nil
	default:
		return invalid("Odbiorcy: wszyscy, grupy albo wybrani użytkownicy.")
	}
	return nil
}

func (a *App) hCreateStream(w http.ResponseWriter, r *http.Request) {
	var in streamInput
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	if err := in.validate(); err != nil {
		fail(w, err)
		return
	}
	st := config.VideoStream{
		ID: strings.ToLower(randomCode(8)), Name: in.Name, Audience: in.Audience, Groups: in.Groups, Users: in.Users,
		PublishKey: randomToken()[:24], CreatedAt: time.Now().UTC(),
	}
	if _, err := a.cfg.Update(func(c *config.Config) error { c.Streams = append(c.Streams, st); return nil }); err != nil {
		fail(w, err)
		return
	}
	a.log.Info("wideo: nowy strumień", "nazwa", st.Name, "odbiorcy", st.Audience)
	writeJSON(w, 201, a.streamOut(st))
}

func (a *App) hUpdateStream(w http.ResponseWriter, r *http.Request) {
	var in streamInput
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	if err := in.validate(); err != nil {
		fail(w, err)
		return
	}
	id := r.PathValue("id")
	var out config.VideoStream
	found := false
	if _, err := a.cfg.Update(func(c *config.Config) error {
		for i := range c.Streams {
			if c.Streams[i].ID == id {
				c.Streams[i].Name, c.Streams[i].Audience, c.Streams[i].Groups, c.Streams[i].Users = in.Name, in.Audience, in.Groups, in.Users
				out, found = c.Streams[i], true
			}
		}
		return nil
	}); err != nil {
		fail(w, err)
		return
	}
	if !found {
		httpError(w, http.StatusNotFound, "Nie ma takiego strumienia.")
		return
	}
	writeJSON(w, 200, a.streamOut(out))
}

func (a *App) hDeleteStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.cfg.Update(func(c *config.Config) error {
		c.Streams = slices.DeleteFunc(c.Streams, func(s config.VideoStream) bool { return s.ID == id })
		return nil
	}); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// hStreamHLS passes the panel's preview to MediaMTX's HLS server on loopback. The server is in
// the path (/api/video/hls/{server}/{id}/...), because the player requests the segments by
// relative URLs without the panel's server header.
func (a *App) hStreamHLS(w http.ResponseWriter, r *http.Request) {
	srv := a.serverByID(r.PathValue("server"))
	if srv == nil {
		httpError(w, http.StatusNotFound, "Nie ma takiego serwera.")
		return
	}
	var st config.VideoStream
	found := false
	for _, s := range srv.cfg.Get().Streams {
		if s.ID == r.PathValue("id") {
			st, found = s, true
		}
	}
	if !found {
		httpError(w, http.StatusNotFound, "Nie ma takiego strumienia.")
		return
	}
	target := &url.URL{Scheme: "http", Host: system.VideoHLSAddr}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = "/" + srv.videoPath(st) + "/" + r.PathValue("rest")
			pr.Out.URL.RawQuery = ""
			pr.Out.Header.Del("Cookie")
			pr.Out.SetBasicAuth(videoPanelUser, srv.videoToken(videoPanelUser))
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			httpError(w, http.StatusBadGateway, "Serwer wideo nie odpowiada.")
		},
	}
	proxy.ServeHTTP(w, r)
}

// hStreamAudience lists who a stream can be shown to (for the panel's audience picker).
func (a *App) hStreamAudience(w http.ResponseWriter, r *http.Request) {
	users, _ := a.store.ListUsers()
	groups, _ := a.store.ListGroups()
	names := []string{}
	for _, u := range users {
		if !u.Disabled {
			names = append(names, u.Username)
		}
	}
	gnames := []string{}
	for _, g := range groups {
		gnames = append(gnames, g.Name)
	}
	writeJSON(w, 200, map[string]any{"users": names, "groups": gnames})
}
