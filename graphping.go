// Command graphping is a ping utility that displays pings to multiple hosts in
// a concise bar chart format. It can also ping the entire path to a remote host
// with the --path flag.
package main

import (
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/backend/icmp"
	"github.com/pcekm/graphping/internal/backend/privsep"
	"github.com/pcekm/graphping/internal/lookup"
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
	privClient := privsep.Initialize()
	defer privClient.Close()

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

// TODO: privileged ping support.
func newV4Conn() (backend.Conn, error) {
	return icmp.New("udp4", *listenAddr)
}

func newV6Conn() (backend.Conn, error) {
	return icmp.New("udp6", *listenAddr)
}
