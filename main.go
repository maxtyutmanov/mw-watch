// Command mw-watch tracks active and screen-on time on this computer and
// serves the stats on a localhost web page.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "time/tzdata" // IANA time zones for the UI's tz parameter, also on Windows

	"github.com/maxtyutmanov/mw-watch/internal/autostart"
	"github.com/maxtyutmanov/mw-watch/internal/console"
	"github.com/maxtyutmanov/mw-watch/internal/paths"
	"github.com/maxtyutmanov/mw-watch/internal/probe"
	"github.com/maxtyutmanov/mw-watch/internal/store"
	"github.com/maxtyutmanov/mw-watch/internal/tracker"
	"github.com/maxtyutmanov/mw-watch/internal/web"
)

const (
	defaultAddr = "127.0.0.1:47823"
	maxLogSize  = 5 << 20
)

const usage = `mw-watch tracks active and screen-on time and shows it at http://localhost:47823

Usage:
  mw-watch [run] [flags]      track and serve the UI (default)
  mw-watch install [flags]    start at login (and now); flags are passed to run
  mw-watch uninstall          stop starting at login and stop running copies
  mw-watch probe              print idle time, display and lock state every second

Flags for run and install:
`

func main() {
	console.Attach()
	cmd, args := "run", os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "run":
		err = run(args)
	case "install":
		err = install(args)
	case "uninstall":
		if err = autostart.Uninstall(); err == nil {
			fmt.Println("mw-watch will no longer start at login. Your data was kept.")
		}
	case "probe":
		err = runProbe()
	case "help":
		printUsage()
	default:
		printUsage()
		os.Exit(2)
	}
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "mw-watch:", err)
		os.Exit(1)
	}
}

type runOptions struct {
	addr    string
	dataDir string
}

func newFlagSet(name string, o *runOptions) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = printUsage
	fs.StringVar(&o.addr, "addr", defaultAddr, "loopback `address` for the web UI")
	fs.StringVar(&o.dataDir, "data-dir", "", "`directory` for the database and log (default: per-user app data)")
	return fs
}

func printUsage() {
	fmt.Fprint(os.Stderr, usage)
	newFlagSet("", &runOptions{}).PrintDefaults()
}

func parseRunFlags(name string, args []string) (runOptions, error) {
	var o runOptions
	if err := newFlagSet(name, &o).Parse(args); err != nil {
		return o, err
	}
	return o, web.CheckLoopback(o.addr)
}

func run(args []string) error {
	o, err := parseRunFlags("run", args)
	if err != nil {
		return err
	}
	dir := o.dataDir
	if dir == "" {
		if dir, err = paths.DataDir(); err != nil {
			return err
		}
	} else if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	logFile, err := openLog(filepath.Join(dir, "mw-watch.log"))
	if err != nil {
		return err
	}
	defer logFile.Close()
	// The file comes first: io.MultiWriter stops at the first failing writer,
	// and stderr is unusable when started at login.
	logger := log.New(io.MultiWriter(logFile, os.Stderr), "", log.LstdFlags)

	ln, err := net.Listen("tcp", o.addr)
	if err != nil {
		return fmt.Errorf("listen on %s (is mw-watch already running?): %w", o.addr, err)
	}
	st, err := store.Open(filepath.Join(dir, "mw-watch.db"))
	if err != nil {
		ln.Close()
		return err
	}
	defer st.Close()
	p, err := probe.New()
	if err != nil {
		ln.Close()
		return err
	}
	defer p.Close()

	srv := &http.Server{Handler: web.Handler(st), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Printf("web server: %v", err)
		}
	}()
	logger.Printf("tracking; data in %s; UI at http://%s", dir, o.addr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tracker.New(p, st, logger).Run(ctx)

	logger.Printf("stopping")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// openLog opens the log for appending, starting over once it grows past maxLogSize.
func openLog(path string) (*os.File, error) {
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogSize {
		flags |= os.O_TRUNC
	}
	return os.OpenFile(path, flags, 0o600)
}

func install(args []string) error {
	o, err := parseRunFlags("install", args)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	running := false
	if c, err := net.DialTimeout("tcp", o.addr, 500*time.Millisecond); err == nil {
		c.Close()
		running = true
	}
	if err := autostart.Install(exe, append([]string{"run"}, args...), !running); err != nil {
		return err
	}
	fmt.Printf("mw-watch will start at login from %s\n", exe)
	fmt.Printf("Open http://%s to see your stats.\n", o.addr)
	fmt.Println("If you move the executable, run install again.")
	return nil
}

func runProbe() error {
	p, err := probe.New()
	if err != nil {
		return err
	}
	defer p.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Println("Ctrl+C to stop.")
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		s, err := p.Sample()
		if err != nil {
			fmt.Println("sample error:", err)
		} else {
			fmt.Printf("%s  idle %6.1fs  display %-3s  locked %-5t  -> screen-on %t\n",
				time.Now().Format("15:04:05"), s.IdleSeconds, onOff(s.DisplayOn), s.Locked, s.ScreenOn())
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
