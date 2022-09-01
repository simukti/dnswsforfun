package http

import (
	"context"
	"embed"
	"log"
	"net/http"
	"net/http/pprof"
	"text/template"
	"time"

	"dnswsforfun/ws"
)

type Config struct {
	Address string
	Timeout time.Duration
	Pprof   bool
}

type Server struct {
	server *http.Server
	cfg    Config
}

//go:embed template/*
var tplFS embed.FS

func NewServer(cfg Config, wsHub *ws.Hub) *Server {
	tplIndex := "template/index.gohtml"
	tpl := template.Must(template.New("").ParseFS(tplFS, []string{tplIndex}...))
	mux := http.NewServeMux()
	if cfg.Pprof {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if err := tpl.ExecuteTemplate(w, "index.gohtml", nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})
	mux.HandleFunc("/ws", ws.Handler(wsHub))
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           mux,
		ReadTimeout:       cfg.Timeout,
		WriteTimeout:      cfg.Timeout,
		IdleTimeout:       cfg.Timeout,
		ReadHeaderTimeout: cfg.Timeout,
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
	log.Printf("Starting HTTP server on %s", s.server.Addr)
	err := s.server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		errChan <- err
	}
}

func (s *Server) waitStop(stopTimeout time.Duration, stopChan chan struct{}) {
	<-stopChan
	log.Printf("Stopping HTTP server with %0.fs timeout", stopTimeout.Seconds())
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	if shutdownErr := s.server.Shutdown(ctx); shutdownErr != nil {
		log.Printf("Failed to shutdown HTTP server: %s", shutdownErr.Error())
	}
}
