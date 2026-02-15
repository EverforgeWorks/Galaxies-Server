/*
Package main
File: main.go
Description: Server entry point. Initializes the universe, the real-time WebSocket hub,
and runs the background heartbeat that keeps the economy alive.
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
)

// Declare gameHub at the package level so it's accessible to handlers.go
var gameHub *Hub

func main() {
	// 1. Load the static universe configuration from YAML
	if err := LoadConfig(); err != nil {
		log.Fatalf("Config Fail: %v", err)
	}

	// 2. Initial Population (Seeding the market)
	// We call ReplenishMarket instead of GenerateJobBoard to respect the new limits.
	log.Println("Seeding initial market...")
	ReplenishMarket()

	// 3. Initialize and start the Real-Time WebSocket Hub
	gameHub = NewHub()
	go gameHub.Run()

	// 4. THE MARKET HEARTBEAT
	// Runs every 60 seconds to top up planets that have dropped below minimums.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {

			// Update the market state and get a list of changed planets
			updatedPlanets := ReplenishMarket()

			if len(updatedPlanets) > 0 {
				// Create the broadcast message
				msg := map[string]interface{}{
					"type":            "market_pulse",
					"updated_planets": updatedPlanets,
				}

				// FIX: Marshal the map to JSON bytes before sending
				jsonBytes, err := json.Marshal(msg)
				if err != nil {
					log.Printf("Error marshaling heartbeat: %v", err)
					continue
				}

				// Send to all connected clients via the Hub
				gameHub.broadcast <- jsonBytes

				log.Printf("Market Pulse: Updated %d planets", len(updatedPlanets))
			}
		}
	}()

	// 5. Hot-reload logic: Listen for SIGHUP to refresh universe without restart
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGHUP)
		for {
			<-sigChan
			log.Println("SIGNAL: Reloading Universe & Jobs...")
			LoadConfig()
			ReplenishMarket()
		}
	}()

	// 6. Setup Router and Handlers
	mux := http.NewServeMux()

	// Persistence & Information Endpoints
	mux.HandleFunc("/api/ship", handleGetShip)
	mux.HandleFunc("/api/planets", handleGetPlanets)
	mux.HandleFunc("/api/contracts", handleGetContracts)
	mux.HandleFunc("/api/contracts/drop", handleDropContract)
	mux.HandleFunc("/api/modules", handleGetModules)

	// Action Endpoints
	mux.HandleFunc("/api/contracts/accept", handleAcceptContract)
	mux.HandleFunc("/api/travel", handleTravel)
	mux.HandleFunc("/api/travel/quote", handleTravelQuote)
	mux.HandleFunc("/api/refuel", handleRefuel)
	mux.HandleFunc("/api/modules/buy", handleBuyModule)

	// Real-Time WebSocket Endpoint
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(gameHub, w, r)
	})

	// 7. Start the Server
	port := ":8081"
	log.Printf("GALAXIES: BURN RATE Server live on %s", port)
	log.Printf("Real-time Hub: Online")

	if err := http.ListenAndServe(port, corsMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}

// corsMiddleware ensures our Wails client can talk to the VPS across domains.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
