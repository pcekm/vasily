package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"time"

	"ekman.cx/betterping/display"
	"ekman.cx/betterping/pinger"
)

const (
	icmpProtoNum = 1
)

// Flags.
var (
	listenAddr = flag.String("listen_addr", "", "Address to listen on.")
	pathPing   = flag.Bool("path", false, "Ping complete path.")
)

func main() {
	flag.Parse()

	disp, err := display.New()
	if err != nil {
		log.Fatalf("Can't initialize display: %v", err)
	}
	defer disp.Close()
	initSignals(disp)

	for _, addrStr := range flag.Args() {
		addr := resolve(addrStr)
		if *pathPing {
			for _, a := range trace(addr) {
				go ping(a, disp)
			}
		} else {
			go ping(addr, disp)
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

func trace(addr *net.UDPAddr) []*net.UDPAddr {
	p, err := pinger.New(nil)
	if err != nil {
		log.Fatalf("Error creating pinger: %v", err)
	}
	var res []*net.UDPAddr
	ch := make(chan pinger.PathComponent)
	go p.Trace(addr, ch)
	for p := range ch {
		res = append(res, p.Host)
	}
	return res
}

func ping(dest *net.UDPAddr, disp *display.D) {
	p, err := pinger.New(nil)
	if err != nil {
		log.Fatalf("Error creating pinger: %v", err)
	}
	defer p.Close()

	idx, err := disp.AddHost(dest.IP.String())
	if err != nil {
		log.Fatalf("Error adding host: %v", err)
	}
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
		disp.UpdateHost(idx, r.Type, float64(failures)/float64(count), cur, avg)
		disp.Update()

		<-tick
	}
}
