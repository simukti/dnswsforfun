package dns

import (
	"context"
	"log"
	"strings"
	"time"

	"dnswsforfun/ws"

	"github.com/miekg/dns"
)

type Config struct {
	Upstream string
	Address  string
	Timeout  time.Duration
	UDPSize  uint16
}

type Server struct {
	server *dns.Server
	cfg    Config
}

func NewServer(cfg Config, wsHub *ws.Hub) *Server {
	client := &dns.Client{
		Net:            "udp",
		Timeout:        cfg.Timeout,
		DialTimeout:    cfg.Timeout,
		ReadTimeout:    cfg.Timeout,
		WriteTimeout:   cfg.Timeout,
		UDPSize:        cfg.UDPSize,
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
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		defer cancel()
		rsp, rtt, err := client.ExchangeContext(ctx, r, cfg.Upstream)
		if err != nil {
			sendErrorFunc(w, r)
			return
		}
		rsp.SetReply(r)
		if err = w.WriteMsg(rsp); err != nil {
			log.Printf("handler: failed to send success response: %s", err.Error())
		}
		dnsLog := &ws.DNSLog{
			Questions: questions(r),
			Answers:   answers(rsp),
			Duration:  rtt.Milliseconds(),
			Upstream:  cfg.Upstream,
			Timestamp: time.Now().Format(time.RFC3339),
		}
		wsHub.Publish(dnsLog)
	})
	server := &dns.Server{
		Addr:         cfg.Address,
		Net:          "udp",
		ReadTimeout:  cfg.Timeout,
		WriteTimeout: cfg.Timeout,
		UDPSize:      int(cfg.UDPSize),
		ReusePort:    false,
		Handler:      mux,
	}
	return &Server{server: server, cfg: cfg}
}

func (s *Server) Serve(stopTimeout time.Duration, errChan chan error) chan struct{} {
	stopChan := make(chan struct{}, 1)
	go s.waitStop(stopTimeout, stopChan)
	go s.serve(errChan)
	return stopChan
}

func (s *Server) serve(errChan chan error) {
	log.Printf("Starting DNS server on %s and use %s as the upstream server", s.server.Addr, s.cfg.Upstream)
	if err := s.server.ListenAndServe(); err != nil {
		log.Printf("Failed to start DNS server: %s", err.Error())
		errChan <- err
	}
}

func (s *Server) waitStop(stopTimeout time.Duration, stopChan chan struct{}) {
	<-stopChan
	log.Printf("Stopping DNS server with %0.fs timeout", stopTimeout.Seconds())
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	if shutdownErr := s.server.ShutdownContext(ctx); shutdownErr != nil {
		log.Printf("Failed to shutdown DNS server: %s", shutdownErr.Error())
	}
}
