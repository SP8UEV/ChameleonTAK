package system

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
)

// Live video: MediaMTX (github.com/bluenviron/mediamtx) receives streams from phones (RTMP from
// apps such as Larix Broadcaster) and serves them to TAK clients (RTSP) and to the panel (HLS on
// loopback). Every publish and read is authorized by the ChameleonTAK service through MediaMTX's
// HTTP authentication hook.

const (
	mediamtxVersion = "v1.21.2"
	mediamtxURL     = "https://github.com/bluenviron/mediamtx/releases/download/%[1]s/mediamtx_%[1]s_linux_%[2]s.tar.gz"
	MediamtxBinary  = "/usr/local/bin/mediamtx"
	mediamtxConfig  = "/etc/chameleontak/mediamtx.yml"
	VideoUnit       = "chameleontak-video"

	VideoRTMPPort = 1935
	VideoRTSPPort = 8554
	VideoHLSAddr  = "127.0.0.1:8888"
	VideoAPIAddr  = "127.0.0.1:9997"
)

// VideoInstalled reports whether the media server is installed.
func VideoInstalled() bool { return FileExists(MediamtxBinary) }

func runVideo(action string, rest []string) error {
	switch action {
	case "video-install":
		return installMediamtx()
	case "video-on":
		if len(rest) != 1 {
			return errors.New("video-on: oczekiwano portu panelu")
		}
		port, err := strconv.Atoi(rest[0])
		if err != nil || port < 1 || port > 65535 {
			return errors.New("niepoprawny port panelu")
		}
		if err := os.MkdirAll("/etc/chameleontak", 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(mediamtxConfig, []byte(MediamtxConfig(port)), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile("/etc/systemd/system/"+VideoUnit+".service", []byte(videoUnit), 0o644); err != nil {
			return err
		}
		if err := run("systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := run("systemctl", "enable", VideoUnit); err != nil {
			return err
		}
		return run("systemctl", "restart", VideoUnit)
	case "video-off":
		_ = run("systemctl", "disable", "--now", VideoUnit)
		return nil
	}
	return fmt.Errorf("nieznana akcja %q", action)
}

func installMediamtx() error {
	arch := map[string]string{"arm64": "arm64", "arm": "armv7", "amd64": "amd64"}[runtime.GOARCH]
	if arch == "" {
		return fmt.Errorf("brak serwera wideo dla architektury %s", runtime.GOARCH)
	}
	archive, err := download(fmt.Sprintf(mediamtxURL, mediamtxVersion, arch), "mediamtx-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(archive)
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return errors.New("w archiwum nie ma programu mediamtx")
		}
		if err != nil {
			return err
		}
		if h.Name != "mediamtx" || h.Typeflag != tar.TypeReg {
			continue
		}
		tmp := MediamtxBinary + ".new"
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, 200<<20)); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return os.Rename(tmp, MediamtxBinary)
	}
}

// MediamtxConfig makes MediaMTX ask the panel (on loopback) about every publish and read. RTMP
// takes streams from phones, RTSP serves TAK clients over TCP; HLS and the API stay on loopback
// for the panel.
func MediamtxConfig(panelPort int) string {
	return `# Wygenerowane przez ChameleonTAK — nie edytuj ręcznie
logLevel: warn
logDestinations: [stdout]

authMethod: http
authHTTPAddress: http://127.0.0.1:` + strconv.Itoa(panelPort) + `/api/video/auth
authHTTPExclude:
  - action: api
  - action: metrics
  - action: pprof

api: true
apiAddress: ` + VideoAPIAddr + `
metrics: false
pprof: false
playback: false

rtsp: true
rtspTransports: [tcp]
rtspAddress: :` + strconv.Itoa(VideoRTSPPort) + `

rtmp: true
rtmpAddress: :` + strconv.Itoa(VideoRTMPPort) + `

hls: true
hlsAddress: ` + VideoHLSAddr + `
hlsVariant: mpegts

webrtc: false
srt: false

paths:
  all_others:
`
}

const videoUnit = `# Wygenerowane przez ChameleonTAK — nie edytuj ręcznie
[Unit]
Description=ChameleonTAK - serwer wideo na żywo (MediaMTX)
Wants=network-online.target
After=network-online.target chameleontak.service

[Service]
ExecStart=` + MediamtxBinary + ` ` + mediamtxConfig + `
DynamicUser=yes
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`
