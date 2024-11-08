package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"runtime/debug"
	"time"

	"github.com/spf13/pflag"

	"github.com/pcekm/graphping/display"
	"github.com/pcekm/graphping/pinger"
)

const (
	icmpProtoNum = 1
)

// Flags.
var (
	listenAddr  = pflag.StringP("source", "S", "", "Source address.")
	pathPing    = pflag.Bool("path", false, "Ping complete path.")
	numeric     = pflag.BoolP("numeric", "n", false, "Only display numeric IP addresses.")
	crashOutput = pflag.String("crash_output", "", "File to write crash output (useful if it's being corrupted by ncurses).")
)

func main() {
	pflag.Parse()

	if *crashOutput != "" {
		f, err := os.OpenFile(*crashOutput, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("Error opening crash output file %q: %v", *crashOutput, err)
		}
		debug.SetCrashOutput(f, debug.CrashOptions{})
	}

	disp, err := display.New()
	if err != nil {
		log.Fatalf("Can't initialize display: %v", err)
	}
	defer disp.Close()
	initSignals(disp)

	for _, addrStr := range pflag.Args() {
		addr := resolve(addrStr)
		if *pathPing {
			trace(addr, disp)
		} else {
			idx, err := disp.AppendHost(maybeReverseResolve(addr))
			if err != nil {
				log.Fatalf("Error adding new host: %v", err)
			}
			go ping(idx, addr, disp)
		}
	}

	<-make(chan any)
}

func initSignals(disp *display.D) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	go func() {
		<-signals
		disp.Close()
		os.Exit(128 + 4) // "Standard" SIGINT exit code.
	}()
}

func resolve(addrStr string) *net.UDPAddr {
	addr, err := net.ResolveIPAddr("ip", addrStr)
	if err != nil {
		log.Fatalf("Error resolving %v: %v", addrStr, err)
	}

	return &net.UDPAddr{IP: addr.IP}
}

// Resolve IP address unless disabled via flag.
func maybeReverseResolve(addr *net.UDPAddr) string {
	s := addr.IP.String()
	if *numeric {
		return s
	}
	names, err := net.LookupAddr(s)
	if err != nil || len(names) == 0 {
		return s
	}
	return names[0]
}

func trace(addr *net.UDPAddr, disp *display.D) {
	p, err := pinger.New(nil)
	if err != nil {
		log.Fatalf("Error creating pinger: %v", err)
	}
	ch := make(chan pinger.PathComponent)
	go p.Trace(addr, ch)
	for p := range ch {
		if err := disp.AddHostAt(p.Pos-1, maybeReverseResolve(p.Host)); err != nil {
			log.Fatalf("Error ading host: %v", err)
		}
		go ping(p.Pos-1, p.Host, disp)
	}
}

func ping(dispIdx int, dest *net.UDPAddr, disp *display.D) {
	p, err := pinger.New(nil)
	if err != nil {
		log.Fatalf("Error creating pinger: %v", err)
	}
	defer p.Close()

	tick := time.Tick(time.Second)
	var avg time.Duration
	var failures, count int
	repl := make(chan pinger.PingReply, 1)
	for {
		if err := p.Send(dest, repl); err != nil {
			log.Fatalf("Error pinging %v: %v", dest, err)
		}
		r := <-repl
		if r.Type != pinger.EchoReply {
			failures++
		}
		count++
		cur := r.Latency
		if avg == 0 {
			avg = cur
		}
		avg = 90*avg/100 + 10*cur/100
		disp.UpdateHost(dispIdx, r.Type, float64(failures)/float64(count), cur, avg)
		disp.Update()

		<-tick
	}
}
