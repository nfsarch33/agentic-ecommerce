package ws

import (
	"sync"
)

type Client struct {
	ID      string
	msgCh   chan []byte
	closeCh chan struct{}
	once    sync.Once
}

func NewClient(id string, buf int) *Client {
	return &Client{
		ID:      id,
		msgCh:   make(chan []byte, buf),
		closeCh: make(chan struct{}),
	}
}

func (c *Client) Send(msg []byte) bool {
	select {
	case c.msgCh <- msg:
		return true
	case <-c.closeCh:
		return false
	default:
		return false
	}
}

func (c *Client) Receive() <-chan []byte { return c.msgCh }

func (c *Client) Close() {
	c.once.Do(func() { close(c.closeCh) })
}

type Hub struct {
	mu          sync.RWMutex
	clients     map[string]*Client
	subs        map[string]map[string]bool // topic -> clientID set
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]*Client),
		subs:    make(map[string]map[string]bool),
	}
}

func (h *Hub) Connect(c *Client) {
	h.mu.Lock()
	h.clients[c.ID] = c
	h.mu.Unlock()
}

func (h *Hub) Disconnect(clientID string) {
	h.mu.Lock()
	delete(h.clients, clientID)
	for _, set := range h.subs {
		delete(set, clientID)
	}
	h.mu.Unlock()
}

func (h *Hub) Subscribe(clientID, topic string) {
	h.mu.Lock()
	if h.subs[topic] == nil {
		h.subs[topic] = make(map[string]bool)
	}
	h.subs[topic][clientID] = true
	h.mu.Unlock()
}

func (h *Hub) Unsubscribe(clientID, topic string) {
	h.mu.Lock()
	if set, ok := h.subs[topic]; ok {
		delete(set, clientID)
	}
	h.mu.Unlock()
}

func (h *Hub) Broadcast(topic string, data []byte) {
	h.mu.RLock()
	subs := h.subs[topic]
	var targets []*Client
	for id := range subs {
		if c, ok := h.clients[id]; ok {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		c.Send(data)
	}
}

func (h *Hub) Shutdown() {
	h.mu.Lock()
	for _, c := range h.clients {
		c.Close()
	}
	h.mu.Unlock()
}
