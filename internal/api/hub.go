/*
Package api
File: hub.go
Description:
    The WebSocket Hub is the core of the real-time communication layer.

    It maintains a registry of all active clients (players connected via the frontend)
    and manages the broadcast channel. When the main loop or an event handler
    sends a message to 'Broadcast', this Hub ensures it is written to the
    sockets of every connected user.

    Architecture:
    - Hub: The singleton manager.
    - Client: Represents one browser connection.
    - ServeWs: The HTTP handler that upgrades a standard GET request to a WebSocket.
*/

package api

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

// Message defines the standard JSON envelope for all real-time communication.
// Every message sent over the socket will follow this structure.
type Message struct {
	Type    string      `json:"type"`    // Event Type (e.g., "market_pulse", "chat_message")
	Payload interface{} `json:"payload"` // The actual data (Struct, Map, or String)
	Sender  string      `json:"sender"`  // ID of the origin (System or User)
}

// Client represents a single connected player/browser tab.
// It acts as a middleman between the websocket connection and the Hub.
type Client struct {
	hub  *Hub            // Reference to the central Hub
	conn *websocket.Conn // The actual low-level WebSocket connection
	send chan []byte     // Buffered channel for outbound messages
}

// Hub maintains the set of active clients and broadcasts messages to them.
type Hub struct {
	// Registered clients map.
	// We use a map[Client]bool because it's faster to add/remove keys than searching a slice.
	clients map[*Client]bool

	// Inbound messages from the connections.
	// Capitalized so main.go can send Heartbeat updates to it.
	Broadcast chan []byte

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client
}

// NewHub creates a new Hub instance.
// This should be called once in main.go and run as a goroutine.
func NewHub() *Hub {
	return &Hub{
		Broadcast:  make(chan []byte), // FIX: Capitalized to match Struct
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

// Run is the main event loop for the Hub.
// It blocks, so it must be run in a goroutine: `go hub.Run()`
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			// A new player connected.
			h.clients[client] = true
			log.Println("WS: New Connection Registered")

		case client := <-h.unregister:
			// A player disconnected. Clean up resources to prevent leaks.
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}

		case message := <-h.Broadcast: // FIX: Capitalized to match Struct
			// A message came in (likely from the Market Heartbeat).
			// Send it to everyone.
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// If the client's send buffer is full, assume they hung or disconnected.
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

// upgrader configures the WebSocket handshake.
// CheckOrigin returns true to allow connections from any host (CORS permissive for development).
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// ServeWs handles the HTTP request that initiates a WebSocket connection.
// It "upgrades" the HTTP connection to a persistent TCP connection.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WS Upgrade Error:", err)
		return
	}

	// Create the client wrapper
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256)}

	// Register the client with the Hub loop
	client.hub.register <- client

	// Start the read/write pumps in their own goroutines.
	// This ensures one slow client doesn't block the entire server.
	go client.writePump()
	go client.readPump()
}

// readPump pumps messages from the websocket connection to the hub.
// (In this version, we mostly ignore incoming messages, but we log them).
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WS Error: %v", err)
			}
			break
		}
		// Echo mechanism: If a client sends a message, we broadcast it to everyone.
		// Useful for simple chat features.
		log.Printf("Received Message: %s", string(message))
		c.hub.Broadcast <- message // FIX: Capitalized to match Struct
	}
}

// writePump pumps messages from the hub to the websocket connection.
func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()

	// Range over the channel. This loop exits when c.send is closed.
	for message := range c.send {
		w, err := c.conn.NextWriter(websocket.TextMessage)
		if err != nil {
			return
		}
		w.Write(message)

		if err := w.Close(); err != nil {
			return
		}
	}
}
