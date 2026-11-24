// Command linkctl controls an Insta360 Link camera from a localhost web UI
// and over MQTT, with a tray icon.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/tlanfer/linkctl/internal/autostart"
	"github.com/tlanfer/linkctl/internal/camera"
	"github.com/tlanfer/linkctl/internal/config"
	"github.com/tlanfer/linkctl/internal/icon"
	"github.com/tlanfer/linkctl/internal/mqtt"
	"github.com/tlanfer/linkctl/internal/platform"
	"github.com/tlanfer/linkctl/internal/service"
	"github.com/tlanfer/linkctl/internal/tray"
	"github.com/tlanfer/linkctl/internal/web"
)

// version is set at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

func init() {
	// The macOS menu bar (and Win32 tray) must run on the main thread.
	runtime.LockOSThread()
}

func main() {
	attachConsole()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "diag":
			fmt.Printf("linkctl %s diagnostics (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
			if err := platform.Diagnose(os.Stdout); err != nil {
				fmt.Println("ERROR:", err)
				os.Exit(1)
			}
			return
		case "version", "--version", "-v":
			fmt.Println("linkctl", version)
			return
		}
	}
	fs := flag.NewFlagSet("linkctl", flag.ExitOnError)
	background := fs.Bool("background", false, "do not open the browser on start (used by start on login)")
	noTray := fs.Bool("no-tray", false, "run without tray icon (stop with Ctrl+C)")
	fake := fs.Bool("fake", false, "use a simulated camera (for UI development)")
	cfgPath := fs.String("config", "", "config file path (default: user config dir)")
	verbose := fs.Bool("verbose", false, "debug logging")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: linkctl [flags]\n       linkctl diag      print camera diagnostics\n       linkctl version\n\nflags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])
	if err := run(options{background: *background, noTray: *noTray, fake: *fake, cfgPath: *cfgPath, verbose: *verbose}); err != nil {
		fmt.Fprintln(os.Stderr, "linkctl:", err)
		os.Exit(1)
	}
}

type options struct {
	background, noTray, fake, verbose bool
	cfgPath                           string
}

func run(o options) error {
	path := o.cfgPath
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return err
		}
		path = p
	}
	store, err := config.Open(path)
	if err != nil {
		return err
	}
	log, closeLog := newLogger(filepath.Join(filepath.Dir(path), "linkctl.log"), o.verbose)
	defer closeLog()
	log.Info("starting", "version", version, "config", path)

	cfg := store.Get()
	url := fmt.Sprintf("http://127.0.0.1:%d/", cfg.HTTPPort)
	ln, err := web.Listen(cfg.HTTPPort)
	if err != nil {
		if alreadyRunning(cfg.HTTPPort) {
			log.Info("another instance is running; opening its control panel")
			if !o.background {
				_ = platform.OpenBrowser(url)
			}
			return nil
		}
		return fmt.Errorf("listen on port %d: %w", cfg.HTTPPort, err)
	}

	var backend camera.Backend
	if o.fake {
		backend = camera.NewFakeBackend()
	} else {
		backend = platform.Backend(func(f string, a ...any) { log.Debug(fmt.Sprintf(f, a...)) })
	}
	mqtt.SoftwareVersion = version
	secrets := config.Keyring{}
	opts := service.Options{}
	if !o.fake {
		opts.InUse = platform.InUse
	}
	svc := service.New(backend, store, log, opts)
	bridge := mqtt.NewBridge(svc, store, secrets, log)
	exe, _ := os.Executable()
	auto := autostartAdapter{exe: exe, log: log}
	srv := &web.Server{Svc: svc, Store: store, Secrets: secrets, MQTT: bridge, Autostart: auto, Log: log}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var wg sync.WaitGroup
	start := func(fn func()) {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}
	start(func() { svc.Run(ctx) })
	start(func() { bridge.Run(ctx) })
	start(func() {
		if err := srv.Serve(ctx, ln); err != nil {
			log.Error("http server failed", "err", err)
			cancel()
		}
	})
	log.Info("control panel ready", "url", url)
	if !o.background {
		_ = platform.OpenBrowser(url)
	}

	if o.noTray {
		<-ctx.Done()
	} else {
		go func() {
			<-ctx.Done()
			tray.Quit()
		}()
		tray.Run(tray.Menu{
			Tooltip:         "linkctl — Insta360 Link control",
			IconPNG:         icon.PNG(64),
			TemplatePNG:     icon.TemplatePNG(36),
			OnOpen:          func() { _ = platform.OpenBrowser(url) },
			StartOnLogin:    auto.Enabled,
			SetStartOnLogin: auto.SetEnabled,
			OnQuit:          cancel,
		}, nil)
		cancel()
	}

	// Give MQTT time to publish "offline" and the server to drain.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		log.Warn("shutdown timed out")
	}
	log.Info("stopped")
	return nil
}

func alreadyRunning(port int) bool {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/state", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

type autostartAdapter struct {
	exe string
	log *slog.Logger
}

func (a autostartAdapter) Enabled() bool {
	on, err := autostart.Enabled()
	if err != nil {
		a.log.Warn("autostart query failed", "err", err)
	}
	return on
}

func (a autostartAdapter) SetEnabled(on bool) error {
	var err error
	if on {
		err = autostart.Enable(a.exe)
	} else {
		err = autostart.Disable()
	}
	if err != nil {
		a.log.Warn("autostart change failed", "err", err)
	}
	return err
}

// newLogger logs to stderr and to a file next to the config (truncated when
// it grows beyond 5 MB).
func newLogger(path string, verbose bool) (*slog.Logger, func()) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	var w io.Writer = os.Stderr
	closeFn := func() {}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
		if st, err := os.Stat(path); err == nil && st.Size() > 5<<20 {
			flags |= os.O_TRUNC
		}
		if f, err := os.OpenFile(path, flags, 0o600); err == nil {
			w = io.MultiWriter(os.Stderr, f)
			closeFn = func() { _ = f.Close() }
		}
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})), closeFn
}
