// Command graphping is a ping utility that displays pings to multiple hosts in
// a concise bar chart format. It can also ping the entire path to a remote host
// with the --path flag.
package main

import (
	"log"
	"os"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/backend/icmp"
	"github.com/pcekm/graphping/internal/lookup"
	"github.com/pcekm/graphping/internal/privsep"
	"github.com/pcekm/graphping/internal/tui"
	"github.com/pcekm/graphping/internal/util"
)

// Flags.
var (
	listenAddr = pflag.StringP("source", "S", "", "Source address.")
	pingPath   = pflag.Bool("path", false, "Ping complete path.")
	logfile    = pflag.String("logfile", "/dev/null", "File to output logs.")
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

	if *logfile != "" {
		logf, err := tea.LogToFile(*logfile, "")
		if err != nil {
			log.Fatalf("Error opening output log: %v", err)
		}
		defer logf.Close()
	}

	opts := &tui.Options{
		Trace: *pingPath,
	}
	tbl, err := tui.New(newV4Conn, newV6Conn, pflag.Args(), opts)
	if err != nil {
		log.Fatalf("Error initializing UI: %v", err)
	}

	prog := tea.NewProgram(tbl, tea.WithAltScreen())
	prog.Run()
}

func newV4Conn() (backend.Conn, error) {
	switch runtime.GOOS {
	case "darwin":
		return icmp.New(util.IPv4)
	case "linux":
		// TODO: Linux shouldn't require root. It supports a similar mechanism
		// as Darwin, but a limitation with Go's x/net module makes unprivileged
		// traceroutes impossible. Connections only receive ICMP echo replies.
		// Other types of packets, like the all important (to a traceroute) time
		// exceeded message, don't get sent.
		//
		// It _is_ possible to receive those packets on Linux without root, but
		// the x/net module doesn't make it possible. (It requires recvfrom with
		// the MSG_ERRQUEUE flag, and likely setting the IP_RECVERR option as
		// well.)
		return privsep.Client.NewConn(util.IPv4)
	}
	return privsep.Client.NewConn(util.IPv4)
}

func newV6Conn() (backend.Conn, error) {
	switch runtime.GOOS {
	case "darwin":
		return icmp.New(util.IPv6)
	case "linux":
		return privsep.Client.NewConn(util.IPv6)
	}
	return privsep.Client.NewConn(util.IPv4)
}
