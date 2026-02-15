/*
Package main
File: hub.go
Description: Manages WebSocket connections and message broadcasting.
*/

package main

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

// Message defines the JSON structure for all real-time communication
type Message struct {
	Type    string      `json:"type"` // "chat_global", "chat_local", "ship_moved"
	Payload interface{} `json:"payload"`
	Sender  string      `json:"sender"`
}

// Client represents a single connected player
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte // Buffered channel of outbound messages
}

// Hub maintains the set of active clients and broadcasts messages
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			log.Println("WS: New Connection Registered")
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// serveWs handles websocket requests from the peer.
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WS Upgrade Error:", err)
		return
	}
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256)}
	client.hub.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("WS Read Error: %v", err)
			break
		}
		// Log this so you can see it in your VPS terminal!
		log.Printf("Received Message: %s", string(message))

		// Broadcast exactly what was received to everyone
		c.hub.broadcast <- message
	}
}

// Fixed S1000: Use for-range for channel consumption
func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()

	// Range will automatically stop when c.send is closed by the Hub
	for message := range c.send {
		err := c.conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			return
		}
	}

	// Send close message if channel was closed
	c.conn.WriteMessage(websocket.CloseMessage, []byte{})
}

