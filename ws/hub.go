package ws

import (
	"encoding/json"
	"io"
	"log"
)

type HubMessage chan interface{}

type (
	DNSLog struct {
		Upstream  string   `json:"upstream"`
		Timestamp string   `json:"timestamp"`
		Questions []string `json:"questions"`
		Answers   []string `json:"answers"`
		Duration  int64    `json:"duration"`
	}
	FreeText struct {
		Sender    string `json:"sender"`
		Message   string `json:"message"`
		Timestamp string `json:"timestamp"`
	}
	OutMsg struct {
		Data interface{} `json:"data"`
		Type string      `json:"type"`
	}
)

type (
	hubAddConn struct {
		conn Connection
	}

	hubRemoveConn struct {
		conn Connection
	}

	hubStop struct{}
)

type Connection interface {
	io.WriteCloser
	ID() string
}

type Hub struct {
	connMap    map[string]Connection
	hubMessage HubMessage
}

func NewHub(hubMessage HubMessage) *Hub {
	h := &Hub{
		connMap:    make(map[string]Connection, 0),
		hubMessage: hubMessage,
	}
	go h.handler(h.hubMessage)
	return h
}

func (h *Hub) Add(conn Connection) { h.hubMessage <- &hubAddConn{conn: conn} }

func (h *Hub) Remove(conn Connection) { h.hubMessage <- &hubRemoveConn{conn: conn} }

func (h *Hub) Close() { h.hubMessage <- &hubStop{} }

func (h *Hub) Publish(data interface{}) { h.hubMessage <- data }

func (h *Hub) ConnectedConn() int { return len(h.connMap) }

// handler is a goroutine to handle incoming message to Hub.
func (h *Hub) handler(incoming HubMessage) {
	for in := range incoming {
		switch msg := in.(type) {
		case *hubAddConn: // from ws handler via Add()
			h.connMap[msg.conn.ID()] = msg.conn
		case *hubRemoveConn: // from ws handler via Remove()
			if _, ok := h.connMap[msg.conn.ID()]; !ok {
				continue
			}
			delete(h.connMap, msg.conn.ID())
		case *hubStop:
			if len(h.connMap) == 0 {
				return
			}
			for _, conn := range h.connMap {
				if err := conn.Close(); err != nil {
					// std log for testing
					log.Println("ws.Hub: closing failed")
				}
			}
			return
		case *DNSLog, *FreeText: // sample for server-to-client message
			if len(h.connMap) == 0 {
				continue
			}
			var outType string
			switch msg.(type) {
			case *DNSLog:
				outType = "dnslog"
			case *FreeText:
				outType = "freetext"
			default:
				continue
			}
			out := &OutMsg{Type: outType, Data: msg}
			for _, conn := range h.connMap {
				if err := json.NewEncoder(conn).Encode(out); err != nil {
					log.Printf("ws.Hub: failed to send to connID %s", conn.ID())
				}
			}
		default:
			log.Printf("ws.Hub: unsupported message type: %v", msg)
		}
	}
}
