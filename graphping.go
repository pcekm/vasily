// Command graphping is a ping utility that displays pings to multiple hosts in
// a concise bar chart format. It can also ping the entire path to a remote host
// with the --path flag.
package main

import (
	"fmt"
	"log"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"

	"github.com/pcekm/graphping/internal/lookup"
	"github.com/pcekm/graphping/internal/privsep"
	"github.com/pcekm/graphping/internal/tui"
)

const maxPingInterval = time.Second

// Flags.
var (
	pingPath     = pflag.Bool("path", false, "Ping complete path.")
	logfile      = pflag.String("logfile", "/dev/null", "File to output logs.")
	pingInterval = pflag.DurationP("interval", "i", time.Second,
		fmt.Sprintf("Interval between pings to a single host. May not be less than %v.", maxPingInterval))
)

// FlagVars.
func init() {
	pflag.BoolVarP(&lookup.NumericMode, "numeric", "n", false, "Only display numeric IP addresses.")
}

func main() {
	privsepCleanup := privsep.Initialize()
	defer privsepCleanup()

	pflag.Parse()

	if len(pflag.Args()) == 0 {
		pflag.Usage()
		os.Exit(1)
	}

	// This is just for user-friendliness. The important check is the rate
	// limiter in backend/icmp, since that gets applied in the privsep server.
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
		Trace:        *pingPath,
		PingInterval: *pingInterval,
	}
	tbl, err := tui.New(newV4Conn, newV6Conn, pflag.Args(), opts)
	if err != nil {
		log.Fatalf("Error initializing UI: %v", err)
	}

	prog := tea.NewProgram(tbl, tea.WithAltScreen())
	prog.Run()
}
