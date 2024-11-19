// Command graphping is a ping utility that displays pings to multiple hosts in
// a concise bargraph format. It can also ping the entire path to a remote host
// with the --path flag.
package main

import (
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"

	"github.com/pcekm/graphping/internal/lookup"
	"github.com/pcekm/graphping/internal/ping/connection"
	"github.com/pcekm/graphping/internal/tui"
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
	pflag.Parse()

	if len(pflag.Args()) == 0 {
		pflag.Usage()
		os.Exit(1)
	}

	connV4, connV6 := newPingConns()

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
	tbl, err := tui.New(connV4, connV6, pflag.Args(), opts)
	if err != nil {
		log.Fatalf("Error initializing UI: %v", err)
	}

	prog := tea.NewProgram(tbl, tea.WithAltScreen())
	prog.Run()
}

func newPingConns() (*connection.PingConn, *connection.PingConn) {
	// TODO: privileged ping support.
	v4, err := connection.New("udp4", *listenAddr)
	if err != nil {
		log.Fatalf("Error opening IPv4 connection: %v", err)
	}
	v6, err := connection.New("udp6", *listenAddr)
	if err != nil {
		log.Fatalf("Error opening IPv6 connection: %v", err)
	}
	return v4, v6
}
