/*
Package api
File: handlers.go
Description:
    Contains the HTTP handlers for the REST API.
    These functions process incoming JSON requests, validate them,
    manipulate the Game State (imported from internal/game), and return JSON responses.

    Key Responsibilities:
    - Input Validation (Is the JSON valid? Does the entity exist?)
    - State Modification (Calling game logic to move ships, trade goods)
    - Thread Safety (Using game.DataLock to prevent race conditions)
*/

package api

import (
	"encoding/json"
	"net/http"

	// Import the game logic package we created
	"github.com/everforgeworks/galaxies-burn-rate/internal/game"
)

// Request DTOs (Data Transfer Objects)
// These structs define exactly what we expect the client to send us.

type TravelRequest struct {
	DestinationKey string `json:"destination_key"`
}

type ContractRequest struct {
	ContractID string `json:"contract_id"`
}

type BuyModuleRequest struct {
	ModuleKey string `json:"module_key"`
}

type TravelQuoteResponse struct {
	Distance  int64 `json:"distance"`
	FuelCost  int64 `json:"fuel_cost"`
	CanAfford bool  `json:"can_afford"`
	BurnRate  int64 `json:"burn_rate"`
}

// HandleGetPlanets returns the static list of all planets.
func HandleGetPlanets(w http.ResponseWriter, r *http.Request) {
	game.DataLock.RLock() // Read Lock (allows concurrent reads, blocks writes)
	defer game.DataLock.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(game.CurrentUniverse.Planets)
}

// HandleGetShip returns the current state of the player's ship.
func HandleGetShip(w http.ResponseWriter, r *http.Request) {
	game.DataLock.RLock()
	defer game.DataLock.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(game.PlayerShip)
}

// HandleGetContracts returns jobs available at the ship's CURRENT location.
func HandleGetContracts(w http.ResponseWriter, r *http.Request) {
	game.DataLock.RLock()
	defer game.DataLock.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	// Only show contracts for the planet the ship is currently on
	location := game.PlayerShip.LocationKey
	json.NewEncoder(w).Encode(game.AvailableContracts[location])
}

// HandleGetModules returns upgrade modules available for purchase.
// Only returns data if the player is at the central hub ("planet_prime").
func HandleGetModules(w http.ResponseWriter, r *http.Request) {
	game.DataLock.RLock()
	defer game.DataLock.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if game.PlayerShip.LocationKey != "planet_prime" {
		json.NewEncoder(w).Encode([]game.ShipModule{})
		return
	}
	json.NewEncoder(w).Encode(game.CurrentUniverse.ShipModules)
}

// HandleAcceptContract moves a contract from the Planet Board to the Ship.
// Triggers Market Scarcity (Source Heat).
func HandleAcceptContract(w http.ResponseWriter, r *http.Request) {
	var req ContractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	game.DataLock.Lock() // Write Lock (Exclusive access required)
	defer game.DataLock.Unlock()

	location := game.PlayerShip.LocationKey
	board := game.AvailableContracts[location]

	// 1. Find the contract
	var target game.Contract
	foundIdx := -1
	for i, c := range board {
		if c.ID == req.ContractID {
			target = c
			foundIdx = i
			break
		}
	}

	if foundIdx == -1 {
		http.Error(w, "Contract not found", http.StatusNotFound)
		return
	}

	// 2. Validate Ship Capacity
	// We must count currently loaded items to ensure we don't overfill.
	currentCargo, currentPass := 0, 0
	for _, ac := range game.PlayerShip.ActiveContracts {
		if ac.Type == "cargo" {
			currentCargo += ac.Quantity
		} else {
			currentPass += ac.Quantity
		}
	}

	if target.Type == "cargo" && currentCargo+target.Quantity > game.PlayerShip.CargoCapacity {
		http.Error(w, "Insufficient Cargo Space", http.StatusConflict)
		return
	}
	if target.Type == "passenger" && currentPass+target.Quantity > game.PlayerShip.PassengerSlots {
		http.Error(w, "Insufficient Passenger Slots", http.StatusConflict)
		return
	}

	// 3. Transfer Contract
	// Add to ship...
	game.PlayerShip.ActiveContracts = append(game.PlayerShip.ActiveContracts, target)
	// ...remove from planet
	game.AvailableContracts[location] = append(board[:foundIdx], board[foundIdx+1:]...)

	// 4. Update Market Economy
	// Accepting a contract makes the good scarcer at the origin.
	game.Market.RecordAcceptance(target.OriginKey, target.ItemKey, target.Quantity)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(game.PlayerShip)
}

// HandleTravel moves the ship between planets.
// Consumes fuel and triggers Market Saturation (Dest Heat) if contracts are delivered.
func HandleTravel(w http.ResponseWriter, r *http.Request) {
	var req TravelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Request", http.StatusBadRequest)
		return
	}

	game.DataLock.Lock()
	defer game.DataLock.Unlock()

	dest := game.GetPlanet(req.DestinationKey)
	current := game.GetPlanet(game.PlayerShip.LocationKey)

	if dest == nil {
		http.Error(w, "Destination invalid", http.StatusNotFound)
		return
	}

	// 1. Calculate Costs (Physics)
	dist := game.CalculateDistance(current.Coordinates, dest.Coordinates)
	currentBurn := game.CalculateCurrentBurn() // Uses the new additive mass logic
	fuelNeeded := dist * currentBurn

	if game.PlayerShip.Fuel < fuelNeeded {
		http.Error(w, "Insufficient Fuel for current mass", http.StatusPaymentRequired)
		return
	}

	// 2. Move Ship
	game.PlayerShip.Fuel -= fuelNeeded
	game.PlayerShip.LocationKey = dest.Key

	// 3. Process Deliveries
	// Check if any onboard contracts are meant for this destination.
	remainingContracts := []game.Contract{}
	payoutTotal := 0

	for _, c := range game.PlayerShip.ActiveContracts {
		if c.DestinationKey == game.PlayerShip.LocationKey {
			// Contract Completed!
			payoutTotal += c.Payout

			// Economy Update: Flooding the market at destination
			game.Market.RecordDelivery(c.DestinationKey, c.ItemKey, c.Quantity)
		} else {
			// Contract stays on board
			remainingContracts = append(remainingContracts, c)
		}
	}

	game.PlayerShip.ActiveContracts = remainingContracts
	game.PlayerShip.Credits += payoutTotal

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(game.PlayerShip)
}

// HandleRefuel fills the tank to max capacity for a credit fee.
func HandleRefuel(w http.ResponseWriter, r *http.Request) {
	game.DataLock.Lock()
	defer game.DataLock.Unlock()

	fuelNeeded := game.PlayerShip.MaxFuel - game.PlayerShip.Fuel
	if fuelNeeded <= 0 {
		http.Error(w, "Tank is already full", http.StatusBadRequest)
		return
	}

	// Cost calculation: Fuel Needed / 100 * PricePerUnit
	// (Rounded down by integer division logic, consider adjusting if precision needed)
	cost := (int(fuelNeeded) / 100) * game.CurrentUniverse.BalanceConfig.FuelCostPerUnit

	if game.PlayerShip.Credits < cost {
		http.Error(w, "Insufficient credits", http.StatusForbidden)
		return
	}

	game.PlayerShip.Credits -= cost
	game.PlayerShip.Fuel = game.PlayerShip.MaxFuel

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(game.PlayerShip)
}

// HandleBuyModule purchases and installs a ship upgrade.
func HandleBuyModule(w http.ResponseWriter, r *http.Request) {
	var req BuyModuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	game.DataLock.Lock()
	defer game.DataLock.Unlock()

	if game.PlayerShip.LocationKey != "planet_prime" {
		http.Error(w, "Upgrade service unavailable at this location", http.StatusForbidden)
		return
	}
	if len(game.PlayerShip.InstalledModules) >= game.PlayerShip.MaxModuleSlots {
		http.Error(w, "No module slots available", http.StatusConflict)
		return
	}

	mod := game.GetModule(req.ModuleKey)
	if mod == nil {
		http.Error(w, "Module not found", http.StatusNotFound)
		return
	}
	if game.PlayerShip.Credits < mod.Cost {
		http.Error(w, "Insufficient Credits", http.StatusPaymentRequired)
		return
	}

	// Apply Purchase
	game.PlayerShip.Credits -= mod.Cost
	game.PlayerShip.InstalledModules = append(game.PlayerShip.InstalledModules, *mod)

	// Apply Stat Modifier
	// Note: In a more complex system, this might be calculated dynamically
	// rather than permanently mutating the base stats.
	switch mod.StatModifier {
	case "cargo_capacity":
		game.PlayerShip.CargoCapacity += mod.StatValue
	case "passenger_slots":
		game.PlayerShip.PassengerSlots += mod.StatValue
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(game.PlayerShip)
}

// HandleTravelQuote provides a "Pre-flight check".
// It tells the UI how much a trip would cost without actually moving the ship.
func HandleTravelQuote(w http.ResponseWriter, r *http.Request) {
	var req TravelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Request", http.StatusBadRequest)
		return
	}

	game.DataLock.RLock()
	defer game.DataLock.RUnlock()

	dest := game.GetPlanet(req.DestinationKey)
	current := game.GetPlanet(game.PlayerShip.LocationKey)

	if dest == nil {
		http.Error(w, "Destination invalid", http.StatusNotFound)
		return
	}

	dist := game.CalculateDistance(current.Coordinates, dest.Coordinates)
	currentBurn := game.CalculateCurrentBurn()
	fuelNeeded := dist * currentBurn

	resp := TravelQuoteResponse{
		Distance:  dist,
		FuelCost:  fuelNeeded,
		CanAfford: game.PlayerShip.Fuel >= fuelNeeded,
		BurnRate:  currentBurn,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleDropContract discards a contract.
// NOTE: This currently does not penalize the player. Future versions should add a reputation hit.
func HandleDropContract(w http.ResponseWriter, r *http.Request) {
	var req ContractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	game.DataLock.Lock()
	defer game.DataLock.Unlock()

	foundIdx := -1
	for i, c := range game.PlayerShip.ActiveContracts {
		if c.ID == req.ContractID {
			foundIdx = i
			break
		}
	}

	if foundIdx == -1 {
		http.Error(w, "Contract not found on ship", http.StatusNotFound)
		return
	}

	// Remove from slice
	game.PlayerShip.ActiveContracts = append(
		game.PlayerShip.ActiveContracts[:foundIdx],
		game.PlayerShip.ActiveContracts[foundIdx+1:]...,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(game.PlayerShip)
}
