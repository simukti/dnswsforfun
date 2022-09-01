package ws

import (
	"bytes"
	"html"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// TODO: you should make this configurable.
const (
	wsWriteTimeout     = time.Second * 5
	wsPingPeriod       = time.Second * 5
	wsMessageSizeLimit = 1024

	wsHandshakeTimeout = time.Second * 5
	wsReadBufferSize   = 1024
	wsWriteBufferSize  = 1024
)

func Handler(hub *Hub) http.HandlerFunc {
	uidGen := newUIDGenerator()
	upgrader := &websocket.Upgrader{
		HandshakeTimeout:  wsHandshakeTimeout,
		ReadBufferSize:    wsReadBufferSize,
		WriteBufferSize:   wsWriteBufferSize,
		CheckOrigin:       func(r *http.Request) bool { return true },
		EnableCompression: false,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		ws.SetPingHandler(nil) // will use default as of gorilla ws v1.5.0
		ws.SetPongHandler(nil) // will use default as of gorilla ws v1.5.0
		c := &Conn{
			ws:            ws,
			hub:           hub,
			id:            uidGen.UID(),
			writerStopper: make(chan struct{}, 1),
			outMsgChan:    make(chan []byte, 1),
			pingPeriod:    wsPingPeriod,
			writeTimeout:  wsWriteTimeout,
			readLimit:     wsMessageSizeLimit,
		}
		go c.writer(c.writerStopper, c.outMsgChan)
		go c.reader()
		hub.Add(c)
	}
}

var _ Connection = (*Conn)(nil)

type Conn struct {
	outMsgChan    chan []byte
	writerStopper chan struct{}
	hub           *Hub
	ws            *websocket.Conn
	id            string
	pingPeriod    time.Duration
	writeTimeout  time.Duration
	readLimit     int64 // KB
}

func (c *Conn) ID() string { return c.id }

func (c *Conn) Write(p []byte) (int, error) { c.outMsgChan <- p; return len(p), nil }

func (c *Conn) Close() error {
	c.hub.Remove(c)
	c.writerStopper <- struct{}{}
	return c.ws.Close()
}

func (c *Conn) reader() {
	c.ws.SetReadLimit(c.readLimit)
	defer func() { _ = c.Close() }()
	for {
		t, p, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		if t != websocket.TextMessage {
			continue
		}
		// sample for client-to-server message
		// the message will be broadcasted to all connected user
		c.hub.Publish(&FreeText{
			Sender:    c.ID(),
			Message:   html.EscapeString(string(bytes.TrimSpace(p))),
			Timestamp: time.Now().Format(time.RFC3339),
		})
	}
}

func (c *Conn) writer(stopChan chan struct{}, outMsgChan chan []byte) {
	ticker := time.NewTicker(c.pingPeriod)
	defer func() { ticker.Stop(); _ = c.Close() }()
	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			if err := c.ws.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
				return
			}
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case msg := <-outMsgChan:
			if err := c.ws.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
				return
			}
			if err := c.ws.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}
}
