// Command graphping is a ping utility that displays pings to multiple hosts in
// a concise bargraph format. It can also ping the entire path to a remote host
// with the --path flag.
package main

import (
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"

	"github.com/pcekm/graphping/internal/ping/connection"
	"github.com/pcekm/graphping/internal/tui"
)

const (
	icmpProtoNum = 1
)

// Flags.
var (
	listenAddr = pflag.StringP("source", "S", "", "Source address.")
	pingPath   = pflag.Bool("path", false, "Ping complete path.")
	// numeric     = pflag.BoolP("numeric", "n", false, "Only display numeric IP addresses.")
	logfile = pflag.String("logfile", "/dev/null", "File to output logs.")
)

func main() {
	pflag.Parse()

	if len(pflag.Args()) == 0 {
		pflag.Usage()
		os.Exit(1)
	}

	// TODO: Support IPv6.
	v4conn, _ := newPingConns()

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
	tbl, err := tui.New(v4conn, pflag.Args(), opts)
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
	return v4, nil
}
