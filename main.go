package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dnswsforfun/dns"
	"dnswsforfun/http"
	"dnswsforfun/ws"
)

func main() {
	var dnsPort uint
	flag.UintVar(&dnsPort, "dns-port", 53, "DNS server UDP port.")
	var dnsTimeout uint
	flag.UintVar(&dnsTimeout, "dns-timeout", 5, "DNS server timeout (seconds).")
	var dnsUpstream string
	flag.StringVar(&dnsUpstream, "dns-upstream", "1.1.1.3:53", "DNS server upstream server.")
	var httpPort uint
	flag.UintVar(&httpPort, "http-port", 8080, "HTTP server TCP port.")
	var httpTimeout uint
	flag.UintVar(&httpTimeout, "http-timeout", 30, "HTTP server timeout (seconds).")
	var httpPprof bool
	flag.BoolVar(&httpPprof, "http-pprof", false, "HTTP Pprof bool flag.")
	var shutdownTimeout uint
	flag.UintVar(&shutdownTimeout, "shutdown-timeout", 3, "Shutdown timeout for servers (seconds).")
	flag.Parse()
	if len(flag.Args()) == 1 && strings.TrimSpace(flag.Args()[0]) == "-h" {
		flag.PrintDefaults()
		return
	}
	hubMessage := make(ws.HubMessage, 1)
	wsHub := ws.NewHub(hubMessage)
	dnsCfg := dns.Config{
		Address:  fmt.Sprintf(":%d", dnsPort),
		Upstream: dnsUpstream,
		Timeout:  time.Second * time.Duration(dnsTimeout),
		UDPSize:  2048,
	}
	dnsServer := dns.NewServer(dnsCfg, wsHub)
	httpCfg := http.Config{
		Address: fmt.Sprintf(":%d", httpPort),
		Timeout: time.Second * time.Duration(httpTimeout),
		Pprof:   httpPprof,
	}
	httpServer := http.NewServer(httpCfg, wsHub)
	// all server should do graceful shutdown
	osSignal := make(chan os.Signal, 1)
	signal.Notify(osSignal, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	errChan := make(chan error, 2)
	shutdownWait := time.Second * time.Duration(shutdownTimeout)
	stopDNSChan := dnsServer.Serve(shutdownWait, errChan)
	stopHTTPChan := httpServer.Serve(shutdownWait, errChan)
	defer func() {
		wsHub.Close()
		stopDNSChan <- struct{}{}
		stopHTTPChan <- struct{}{}
		timer := time.NewTicker(shutdownWait)
		<-timer.C
		timer.Stop()
	}()
	for {
		select {
		case <-osSignal:
			return
		case err := <-errChan:
			log.Printf("Failed to start server: %s", err.Error())
			return
		}
	}
}
