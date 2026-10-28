// Command chameleontak is a self-configuring TAK server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"chameleontak/internal/config"
	"chameleontak/internal/server"
	"chameleontak/internal/system"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe(os.Args[2:])
	case "setup":
		err = cmdSetup(os.Args[2:])
	case "reset-admin":
		err = cmdResetAdmin(os.Args[2:])
	case "panel-access":
		err = cmdPanelAccess(os.Args[2:])
	case "factory-reset":
		err = cmdFactoryReset(os.Args[2:])
	case "sys":
		err = system.RunSys(os.Args[2:], os.Stdin)
	case "version", "--version", "-v":
		fmt.Println("ChameleonTAK", version)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "błąd:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `ChameleonTAK `+version+` — samokonfigurujący się serwer TAK

Użycie:
  chameleontak serve         [--data DIR] [--http ADDR] [--https ADDR] [--debug]
  chameleontak setup         --server-name X --admin-user U --admin-pass P [--platforms atak,itak] [--data DIR]
  chameleontak reset-admin   --password P [--user admin] [--data DIR]
  chameleontak panel-access  off [--data DIR]   (wyłącza ograniczenie panelu do wybranych adresów)
  chameleontak factory-reset --yes [--data DIR]
  chameleontak version
`)
}

func defaultDataDir() string {
	if d := os.Getenv("CHAMELEONTAK_DATA"); d != "" {
		return d
	}
	return "/var/lib/chameleontak"
}

func newLogger(debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func openApp(dataDir string, opts server.Options, debug bool) (*server.App, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, err
	}
	if err := server.PrepareDataDir(dataDir); err != nil {
		return nil, fmt.Errorf("przygotowanie katalogu danych: %w", err)
	}
	opts.DataDir, opts.Version = dataDir, version
	return server.New(opts, newLogger(debug))
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	data := fs.String("data", defaultDataDir(), "katalog danych")
	httpAddr := fs.String("http", "", "adres panelu HTTP (np. :8080), nadpisuje ustawienia")
	httpsAddr := fs.String("https", "", "adres panelu HTTPS (np. :8443), nadpisuje ustawienia")
	debug := fs.Bool("debug", false, "szczegółowe logi")
	_ = fs.Parse(args)

	app, err := openApp(*data, server.Options{HTTPAddr: *httpAddr, HTTPSAddr: *httpsAddr, Dev: *debug}, *debug)
	if err != nil {
		return err
	}
	defer app.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return app.Run(ctx)
}

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	data := fs.String("data", defaultDataDir(), "katalog danych")
	name := fs.String("server-name", "", "adres serwera (IP lub domena), pod którym łączą się urządzenia")
	display := fs.String("display-name", "ChameleonTAK", "nazwa wyświetlana w aplikacjach")
	user := fs.String("admin-user", "admin", "login administratora panelu")
	pass := fs.String("admin-pass", "", "hasło administratora (min. 8 znaków)")
	platforms := fs.String("platforms", "atak,itak", "obsługiwane platformy: atak,itak,wintak,takaware")
	ports := config.DefaultPorts()
	fs.IntVar(&ports.CoTTLS, "cot-port", ports.CoTTLS, "port CoT TLS")
	fs.IntVar(&ports.Marti, "marti-port", ports.Marti, "port Marti API")
	fs.IntVar(&ports.Enroll, "enroll-port", ports.Enroll, "port enrollmentu certyfikatów")
	fs.IntVar(&ports.HTTP, "http-port", ports.HTTP, "port panelu HTTP")
	fs.IntVar(&ports.HTTPS, "https-port", ports.HTTPS, "port panelu HTTPS")
	tcp := fs.Bool("tcp", false, "włącz nieszyfrowany port CoT TCP 8087")
	_ = fs.Parse(args)
	ports.CoTTCPEnabled = *tcp

	app, err := openApp(*data, server.Options{}, false)
	if err != nil {
		return err
	}
	defer app.Close()
	var req server.SetupRequest
	req.ServerName, req.DisplayName, req.Ports = *name, *display, ports
	req.Admin.Username, req.Admin.Password = *user, *pass
	req.Platforms = strings.Split(*platforms, ",")
	req.ClientTime = time.Now().UTC().Format(time.RFC3339)
	req.Network.Mode = "lan"
	if err := app.SetupCLI(req); err != nil {
		return err
	}
	fmt.Println("Gotowe. Uruchom usługę: systemctl restart chameleontak")
	return nil
}

func cmdResetAdmin(args []string) error {
	fs := flag.NewFlagSet("reset-admin", flag.ExitOnError)
	data := fs.String("data", defaultDataDir(), "katalog danych")
	user := fs.String("user", "admin", "login administratora")
	pass := fs.String("password", "", "nowe hasło (min. 8 znaków)")
	_ = fs.Parse(args)
	app, err := openApp(*data, server.Options{}, false)
	if err != nil {
		return err
	}
	defer app.Close()
	if err := app.ResetAdmin(*user, *pass); err != nil {
		return err
	}
	fmt.Printf("Hasło administratora %q zostało zmienione.\n", *user)
	return nil
}

func cmdPanelAccess(args []string) error {
	if len(args) == 0 || args[0] != "off" {
		return fmt.Errorf("użycie: chameleontak panel-access off [--data DIR]")
	}
	fs := flag.NewFlagSet("panel-access", flag.ExitOnError)
	data := fs.String("data", defaultDataDir(), "katalog danych")
	_ = fs.Parse(args[1:])
	app, err := openApp(*data, server.Options{}, false)
	if err != nil {
		return err
	}
	defer app.Close()
	if err := app.DisablePanelAccess(); err != nil {
		return err
	}
	fmt.Println("Panel jest znowu dostępny z każdego adresu. Uruchom usługę ponownie: sudo systemctl restart chameleontak")
	return nil
}

func cmdFactoryReset(args []string) error {
	fs := flag.NewFlagSet("factory-reset", flag.ExitOnError)
	data := fs.String("data", defaultDataDir(), "katalog danych")
	yes := fs.Bool("yes", false, "potwierdzenie")
	_ = fs.Parse(args)
	if !*yes {
		return fmt.Errorf("to usunie wszystkie dane z %s — dodaj --yes, aby potwierdzić", *data)
	}
	if err := server.FactoryReset(*data); err != nil {
		return err
	}
	fmt.Println("Dane usunięte. Po restarcie usługi uruchomi się kreator konfiguracji.")
	return nil
}
