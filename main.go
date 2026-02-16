/*
Package main
File: main.go
Description:
    The Entry Point of the application.

    Responsibility:
    1. Orchestration: Initializes the Game State and the API Layer.
    2. Scheduling: Runs the background "Heartbeat" (Economic Simulation).
    3. Routing: Maps HTTP/WebSocket endpoints to their specific handlers.
    4. Lifecycle: Handles OS signals (like SIGHUP) for hot-reloading.

    Architecture:
    Main -> Imports internal/game (The Logic)
    Main -> Imports internal/api  (The Interface)
*/

package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Import our strictly separated internal packages
	"github.com/everforgeworks/galaxies-burn-rate/internal/api"
	"github.com/everforgeworks/galaxies-burn-rate/internal/game"
)

// gameHub is the global reference to the WebSocket manager.
// It is declared here so it can be passed to the heartbeat loop and the connection handler.
var gameHub *api.Hub

func main() {
	// =========================================================================
	// 1. INITIALIZATION
	// =========================================================================

	// Load the static universe configuration (YAML) into memory.
	// This establishes the "World" (Planets, Items, Ship Specs).
	if err := game.LoadConfig(); err != nil {
		log.Fatalf("CRITICAL: Failed to load universe config: %v", err)
	}

	// Seed the market with initial jobs.
	// We call this immediately so the world isn't empty when the server starts.
	log.Println("INIT: Seeding initial market data...")
	game.ReplenishMarket()

	// Initialize the WebSocket Hub (Real-time communication layer).
	// This structure manages all active client connections.
	gameHub = api.NewHub()

	// Start the Hub's event loop in a separate goroutine.
	// It will run in the background, listening for register/unregister/broadcast events.
	go gameHub.Run()

	// =========================================================================
	// 2. BACKGROUND PROCESSES (The Heartbeat)
	// =========================================================================

	// The "Heartbeat" is the pulse of the economy.
	// Every 60 seconds, it triggers the simulation to:
	// a) Recover market prices (Cool down heat maps).
	// b) Generate new contracts if planets are running low.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {
			// Run the simulation logic (Thread-safe inside the game package).
			// Returns a list of planet IDs that received new jobs.
			updatedPlanets := game.ReplenishMarket()

			if len(updatedPlanets) > 0 {
				// Construct the broadcast message.
				// We use the standardized Message struct from the API package.
				msg := api.Message{
					Type: "market_pulse",
					Payload: map[string]interface{}{
						"updated_planets": updatedPlanets,
					},
					Sender: "system",
				}

				// Marshal to JSON for transport.
				jsonBytes, err := json.Marshal(msg)
				if err != nil {
					log.Printf("ERROR: Failed to marshal heartbeat: %v", err)
					continue
				}

				// Broadcast to all connected clients.
				// NOTE: Ensure 'Broadcast' is capitalized in internal/api/hub.go!
				gameHub.Broadcast <- jsonBytes

				log.Printf("HEARTBEAT: Updated %d planets", len(updatedPlanets))
			}
		}
	}()

	// Hot-Reload Listener.
	// Allows updating 'universe.yaml' without killing the server process.
	// Usage: `kill -SIGHUP <pid>`
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGHUP)
		for {
			<-sigChan
			log.Println("SIGNAL: Received SIGHUP. Reloading Universe Configuration...")

			// Reloads YAML and resets non-persistent state
			if err := game.LoadConfig(); err != nil {
				log.Printf("ERROR: Hot-reload failed: %v", err)
				continue
			}

			// Re-run population logic with new settings
			game.ReplenishMarket()
			log.Println("SIGNAL: Reload Complete.")
		}
	}()

	// =========================================================================
	// 3. ROUTING & TRANSPORT
	// =========================================================================

	mux := http.NewServeMux()

	// -- Information Endpoints (Read-Only) --
	mux.HandleFunc("/api/ship", api.HandleGetShip)           // Get player status
	mux.HandleFunc("/api/planets", api.HandleGetPlanets)     // Get static map data
	mux.HandleFunc("/api/contracts", api.HandleGetContracts) // Get jobs at current location
	mux.HandleFunc("/api/modules", api.HandleGetModules)     // Get upgrades (only at Prime)

	// -- Action Endpoints (State-Changing) --
	mux.HandleFunc("/api/contracts/accept", api.HandleAcceptContract) // Take a job
	mux.HandleFunc("/api/contracts/drop", api.HandleDropContract)     // Abandon a job
	mux.HandleFunc("/api/travel", api.HandleTravel)                   // Move ship (burn fuel)
	mux.HandleFunc("/api/travel/quote", api.HandleTravelQuote)        // Calculate fuel cost (pre-flight)
	mux.HandleFunc("/api/refuel", api.HandleRefuel)                   // Buy fuel
	mux.HandleFunc("/api/modules/buy", api.HandleBuyModule)           // Buy upgrade

	// -- WebSocket Endpoint --
	// This upgrades the HTTP connection to a persistent socket.
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		api.ServeWs(gameHub, w, r)
	})

	// =========================================================================
	// 4. SERVER START
	// =========================================================================

	port := ":8081"
	log.Printf("GALAXIES: BURN RATE Server live on port %s", port)
	log.Printf("Architecture: [Internal Game Logic] <-> [Internal API Layer]")

	// Start listening with CORS middleware enabled.
	if err := http.ListenAndServe(port, corsMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}

// corsMiddleware allows the frontend (Wails/React) to communicate with this
// server even if they are running on different ports/domains during dev.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow any origin for development simplicity
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Handle pre-flight OPTIONS requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
