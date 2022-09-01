package main

import (
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/template"
	"time"

	"dnswsforfun/ws"

	"github.com/miekg/dns"
)

const (
	dnsAddr  = ":53"
	httpAddr = ":54321"
)

func dnsServer(wsHub *ws.Hub) *dns.Server {
	udpSize := 4096
	timeout := time.Second * 10
	upstream := "1.1.1.3:53" // no malware and adult content (see https://1.1.1.1/family/)
	client := &dns.Client{
		Net:            "udp",
		Timeout:        timeout,
		DialTimeout:    timeout,
		ReadTimeout:    timeout,
		WriteTimeout:   timeout,
		UDPSize:        uint16(udpSize),
		SingleInflight: false,
	}
	sendErrorFunc := func(w dns.ResponseWriter, r *dns.Msg) {
		res := new(dns.Msg)
		res.Authoritative = true
		res.SetRcode(r, dns.RcodeServerFailure)
		if err := w.WriteMsg(res); err != nil {
			log.Printf("sendError: failed to send error response: %s", err.Error()) // stdlib log for testing
		}
	}
	questions := func(r *dns.Msg) []string {
		if r == nil {
			return nil
		}
		questions := make([]string, 0)
		question := r.Question
		for _, q := range question {
			s := strings.Join([]string{
				q.Name,
				dns.Class(q.Qclass).String(),
				dns.Type(q.Qtype).String(),
			}, "|")
			questions = append(questions, s)
		}
		return questions
	}
	answers := func(r *dns.Msg) []string {
		if r == nil {
			return nil
		}
		answer := r.Answer
		if len(answer) == 0 {
			return nil
		}
		answers := make([]string, 0)
		for _, a := range answer {
			answers = append(answers, strings.ReplaceAll(a.String(), "\t", "|"))
		}
		return answers
	}
	mux := dns.DefaultServeMux
	mux.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		rsp, rtt, err := client.ExchangeContext(ctx, r, upstream)
		if err != nil {
			sendErrorFunc(w, r)
			return
		}
		rsp.SetReply(r)
		if err = w.WriteMsg(rsp); err != nil {
			log.Printf("handler: failed to send success response: %s", err.Error())
		}
		dnslog := &ws.DNSLog{
			Questions: questions(r),
			Answers:   answers(rsp),
			Duration:  rtt.Milliseconds(),
			Upstream:  upstream,
			Timestamp: time.Now().Format(time.RFC3339),
		}
		wsHub.Publish(dnslog)
	})
	return &dns.Server{
		Addr:         dnsAddr,
		Net:          "udp",
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
		UDPSize:      udpSize,
		ReusePort:    false,
		Handler:      mux,
	}
}

//go:embed tpl/*
var tplDir embed.FS

func httpServer(wsHub *ws.Hub) *http.Server {
	tplIndex := "tpl/index.gohtml"
	tpl := template.Must(template.New("").ParseFS(tplDir, []string{tplIndex}...))
	mux := http.DefaultServeMux
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := tpl.ExecuteTemplate(w, "index.gohtml", nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})
	mux.HandleFunc("/ws", ws.Handler(wsHub))
	timeout := time.Second * 5
	return &http.Server{
		Addr:              httpAddr,
		Handler:           mux,
		ReadTimeout:       timeout,
		WriteTimeout:      timeout,
		IdleTimeout:       timeout,
		ReadHeaderTimeout: timeout,
	}
}

type httpserver struct {
	h     *http.Server
	wsHub *ws.Hub
}

func (s *httpserver) Serve(stopTimeout time.Duration, errChan chan error) chan struct{} {
	stopChan := make(chan struct{}, 1)
	go s.waitStop(stopTimeout, stopChan)
	go s.serve(errChan)
	return stopChan
}

func (s *httpserver) serve(errChan chan error) {
	log.Printf("Starting HTTP server on %s...", s.h.Addr)
	err := s.h.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		errChan <- err
	}
}

func (s *httpserver) waitStop(stopTimeout time.Duration, stopChan chan struct{}) {
	<-stopChan
	log.Printf("Stopping HTTP server with %0.fs timeout...", stopTimeout.Seconds())
	s.wsHub.Close()
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	if shutdownErr := s.h.Shutdown(ctx); shutdownErr != nil {
		log.Printf("Failed to shutdown HTTP server: %s", shutdownErr.Error())
	}
}

type dnsserver struct{ d *dns.Server }

func (s *dnsserver) Serve(stopTimeout time.Duration, errChan chan error) chan struct{} {
	stopChan := make(chan struct{}, 1)
	go s.waitStop(stopTimeout, stopChan)
	go s.serve(errChan)
	return stopChan
}

func (s *dnsserver) serve(errChan chan error) {
	log.Printf("Starting DNS server on %s...", s.d.Addr)
	if err := s.d.ListenAndServe(); err != nil {
		log.Printf("Failed to start DNS server: %s", err.Error())
		errChan <- err
	}
}

func (s *dnsserver) waitStop(stopTimeout time.Duration, stopChan chan struct{}) {
	<-stopChan
	log.Printf("Stopping DNS server with %0.fs timeout...", stopTimeout.Seconds())
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	if shutdownErr := s.d.ShutdownContext(ctx); shutdownErr != nil {
		log.Printf("Failed to shutdown DNS server: %s", shutdownErr.Error())
	}
}

func main() {
	shutdownTimeout := time.Second * 3
	hubMessage := make(ws.HubMessage, 1)
	wsHub := ws.NewHub(hubMessage)
	hs := &httpserver{h: httpServer(wsHub), wsHub: wsHub}
	ds := &dnsserver{d: dnsServer(wsHub)}
	// all server should do graceful shutdown
	osSignal := make(chan os.Signal, 1)
	signal.Notify(osSignal, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	errChan := make(chan error, 2)
	stopDNSChan := ds.Serve(shutdownTimeout, errChan)
	stopHTTPChan := hs.Serve(shutdownTimeout, errChan)
	defer func() {
		stopDNSChan <- struct{}{}
		stopHTTPChan <- struct{}{}
		timer := time.NewTicker(shutdownTimeout)
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
