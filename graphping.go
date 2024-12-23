// Command graphping is a ping utility that displays pings to multiple hosts in
// a concise bar chart format. It can also ping the entire path to a remote host
// with the --path flag.
package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"runtime/debug"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"

	"github.com/pcekm/graphping/internal/backend"
	_ "github.com/pcekm/graphping/internal/backend/icmp"
	_ "github.com/pcekm/graphping/internal/backend/udp"
	"github.com/pcekm/graphping/internal/lookup"
	"github.com/pcekm/graphping/internal/privsep"
	"github.com/pcekm/graphping/internal/tui"
)

const maxPingInterval = time.Second

var Version = "(unknown)" // Set via -ldflags

// Flags.
var (
	pingPath     = pflag.Bool("path", false, "Ping complete path.")
	logfile      = pflag.String("logfile", "/dev/null", "File to output logs.")
	pingInterval = pflag.DurationP("interval", "i", time.Second,
		fmt.Sprintf("Interval between pings to a single host. May not be less than %v.", maxPingInterval))
	queries       = pflag.IntP("queries", "q", 3, "Number of times to query each TTL during a traceroute.")
	traceInterval = pflag.Duration("trace_interval", time.Second,
		fmt.Sprintf("Interval between traceroute probes. May not be less than %v.", maxPingInterval))
	pingBackend  = backend.FlagP("protocol", "P", "icmp", "Protocol to use for pings.")
	traceBackend = backend.FlagP("trace_protocol", "T", "udp", "Protocol to use for traceroutes.")
	maxTTL       = pflag.Int("max_ttl", 64, "Maximum path length to trace.")
	printVersion = pflag.BoolP("version", "v", false, "Output the version number.")
)

// FlagVars.
func init() {
	pflag.BoolVarP(&lookup.NumericMode, "numeric", "n", false, "Only display numeric IP addresses.")
}

func main() {
	privsepCleanup := privsep.Initialize()
	defer privsepCleanup()

	pflag.Parse()

	if *printVersion {
		printVersionInfo()
		os.Exit(0)
	}

	if len(pflag.Args()) == 0 {
		pflag.Usage()
		os.Exit(1)
	}

	// This is just for user-friendliness. The important check is the rate
	// limiter in the backend, since that gets applied in the privsep server.
	if *pingInterval < maxPingInterval {
		fmt.Fprintf(os.Stderr, "Ping interval may not be less than %v.\n", maxPingInterval)
		os.Exit(1)
	}

	if *logfile != "" {
		logf, err := tea.LogToFile(*logfile, "")
		if err != nil {
			log.Fatalf("Error opening output log: %v", err)
		}
		defer logf.Close()
	}

	opts := &tui.Options{
		Trace:         *pingPath,
		PingInterval:  *pingInterval,
		PingBackend:   *pingBackend,
		TraceInterval: *traceInterval,
		TraceBackend:  *traceBackend,
		TraceMaxTTL:   *maxTTL,
		ProbesPerHop:  *queries,
	}
	tbl, err := tui.New(pflag.Args(), opts)
	if err != nil {
		log.Fatalf("Error initializing UI: %v", err)
	}

	prog := tea.NewProgram(tbl, tea.WithAltScreen())
	prog.Run()
}

func printVersionInfo() {
	inf, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Println("graphping: unknown version")
		return
	}
	fmt.Printf("%s %s\nbuilt with %s\n", path.Base(inf.Path), Version, inf.GoVersion)
}
