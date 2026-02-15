/*
Package main
File: handlers.go
Description: HTTP Handlers for the API. Includes logic for accepting contracts,
traveling, refueling, and buying modules. Now hooks into MarketState.
*/

package main

import (
	"encoding/json"
	"net/http"
)

type TravelRequest struct {
	DestinationKey string `json:"destination_key"`
}

type ContractRequest struct {
	ContractID string `json:"contract_id"`
}

type BuyModuleRequest struct {
	ModuleKey string `json:"module_key"`
}

func handleGetPlanets(w http.ResponseWriter, r *http.Request) {
	dataLock.RLock()
	defer dataLock.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CurrentUniverse.Planets)
}

func handleGetShip(w http.ResponseWriter, r *http.Request) {
	dataLock.RLock()
	defer dataLock.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PlayerShip)
}

func handleGetContracts(w http.ResponseWriter, r *http.Request) {
	dataLock.RLock()
	defer dataLock.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AvailableContracts[PlayerShip.LocationKey])
}

// handleAcceptContract moves a contract to the ship and triggers Market Scarcity.
func handleAcceptContract(w http.ResponseWriter, r *http.Request) {
	var req ContractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	dataLock.Lock()
	defer dataLock.Unlock()

	board := AvailableContracts[PlayerShip.LocationKey]
	var target Contract
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

	// Capacity Logic
	currentCargo, currentPass := 0, 0
	for _, ac := range PlayerShip.ActiveContracts {
		if ac.Type == "cargo" {
			currentCargo += ac.Quantity
		} else {
			currentPass += ac.Quantity
		}
	}

	if target.Type == "cargo" && currentCargo+target.Quantity > PlayerShip.CargoCapacity {
		http.Error(w, "Insufficient Cargo Space", http.StatusConflict)
		return
	}
	if target.Type == "passenger" && currentPass+target.Quantity > PlayerShip.PassengerSlots {
		http.Error(w, "Insufficient Passenger Slots", http.StatusConflict)
		return
	}

	// 1. Add to Ship
	PlayerShip.ActiveContracts = append(PlayerShip.ActiveContracts, target)

	// 2. Remove from Board
	AvailableContracts[PlayerShip.LocationKey] = append(board[:foundIdx], board[foundIdx+1:]...)

	// 3. MARKET EVENT: Record Acceptance (Increase Scarcity at Origin)
	// We release the lock briefly or handle this inside the locked context.
	// Since Market struct is global and has its own maps, we can call the method directly.
	// Note: We are already holding dataLock, so we must be careful if MarketState methods use dataLock.
	// In the state.go implementation, RecordAcceptance USES dataLock.
	// To avoid Deadlock, we should perform the market update AFTER unlocking,
	// OR remove the lock from RecordAcceptance if it's only called from here.
	// FIX: For safety in this simple implementation, we will manually update the map here
	// to avoid recursive locking issues, or we assume MarketState uses a separate lock.
	// For MVP simplicity: We will modify the map directly here since we hold dataLock.

	if Market.SourceHeat[target.OriginKey] != nil {
		impact := float64(target.Quantity) * 0.01
		Market.SourceHeat[target.OriginKey][target.ItemKey] += impact
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PlayerShip)
}

// handleTravel moves the ship and triggers Market Saturation on delivery.
func handleTravel(w http.ResponseWriter, r *http.Request) {
	var req TravelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Request", http.StatusBadRequest)
		return
	}

	dataLock.Lock()
	defer dataLock.Unlock()

	dest := GetPlanet(req.DestinationKey)
	current := GetPlanet(PlayerShip.LocationKey)
	if dest == nil {
		http.Error(w, "Destination invalid", http.StatusNotFound)
		return
	}

	dist := CalculateDistance(current.Coordinates, dest.Coordinates)

	currentBurn := CalculateCurrentBurn()
	fuelNeeded := dist * currentBurn

	if PlayerShip.Fuel < fuelNeeded {
		http.Error(w, "Insufficient Fuel for current mass", http.StatusPaymentRequired)
		return
	}

	PlayerShip.Fuel -= fuelNeeded
	PlayerShip.LocationKey = dest.Key

	// Handle automatic delivery upon arrival
	remainingContracts := []Contract{}
	payoutTotal := 0

	for _, c := range PlayerShip.ActiveContracts {
		if c.DestinationKey == PlayerShip.LocationKey {
			payoutTotal += c.Payout

			// MARKET EVENT: Record Delivery (Increase Saturation at Destination)
			// Direct map access to avoid recursive locking
			if Market.DestHeat[c.DestinationKey] != nil {
				impact := float64(c.Quantity) * 0.02
				Market.DestHeat[c.DestinationKey][c.ItemKey] += impact
			}

		} else {
			remainingContracts = append(remainingContracts, c)
		}
	}
	PlayerShip.ActiveContracts = remainingContracts
	PlayerShip.Credits += payoutTotal

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PlayerShip)
}

func handleRefuel(w http.ResponseWriter, r *http.Request) {
	dataLock.Lock()
	defer dataLock.Unlock()

	fuelNeeded := PlayerShip.MaxFuel - PlayerShip.Fuel
	if fuelNeeded <= 0 {
		http.Error(w, "Tank is already full", http.StatusBadRequest)
		return
	}

	cost := (int(fuelNeeded) / 100) * CurrentUniverse.BalanceConfig.FuelCostPerUnit
	if PlayerShip.Credits < cost {
		http.Error(w, "Insufficient credits", http.StatusForbidden)
		return
	}

	PlayerShip.Credits -= cost
	PlayerShip.Fuel = PlayerShip.MaxFuel

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PlayerShip)
}

func handleGetModules(w http.ResponseWriter, r *http.Request) {
	dataLock.RLock()
	defer dataLock.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	if PlayerShip.LocationKey != "planet_prime" {
		json.NewEncoder(w).Encode([]ShipModule{})
		return
	}
	json.NewEncoder(w).Encode(CurrentUniverse.ShipModules)
}

func handleBuyModule(w http.ResponseWriter, r *http.Request) {
	var req BuyModuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	dataLock.Lock()
	defer dataLock.Unlock()

	if PlayerShip.LocationKey != "planet_prime" {
		http.Error(w, "Upgrade service unavailable at this location", http.StatusForbidden)
		return
	}
	if len(PlayerShip.InstalledModules) >= PlayerShip.MaxModuleSlots {
		http.Error(w, "No module slots available", http.StatusConflict)
		return
	}
	mod := GetModule(req.ModuleKey)
	if mod == nil {
		http.Error(w, "Module not found", http.StatusNotFound)
		return
	}
	if PlayerShip.Credits < mod.Cost {
		http.Error(w, "Insufficient Credits", http.StatusPaymentRequired)
		return
	}

	PlayerShip.Credits -= mod.Cost
	PlayerShip.InstalledModules = append(PlayerShip.InstalledModules, *mod)

	switch mod.StatModifier {
	case "cargo_capacity":
		PlayerShip.CargoCapacity += mod.StatValue
	case "passenger_slots":
		PlayerShip.PassengerSlots += mod.StatValue
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PlayerShip)
}

// New Struct for the Quote Response
type TravelQuoteResponse struct {
	Distance  int64 `json:"distance"`
	FuelCost  int64 `json:"fuel_cost"`
	CanAfford bool  `json:"can_afford"`
	BurnRate  int64 `json:"burn_rate"`
}

func handleTravelQuote(w http.ResponseWriter, r *http.Request) {
	var req TravelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Request", http.StatusBadRequest)
		return
	}

	dataLock.RLock()
	defer dataLock.RUnlock()

	dest := GetPlanet(req.DestinationKey)
	current := GetPlanet(PlayerShip.LocationKey)
	if dest == nil {
		http.Error(w, "Destination invalid", http.StatusNotFound)
		return
	}

	dist := CalculateDistance(current.Coordinates, dest.Coordinates)
	currentBurn := CalculateCurrentBurn()
	fuelNeeded := dist * currentBurn

	resp := TravelQuoteResponse{
		Distance:  dist,
		FuelCost:  fuelNeeded,
		CanAfford: PlayerShip.Fuel >= fuelNeeded,
		BurnRate:  currentBurn,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleDropContract(w http.ResponseWriter, r *http.Request) {
	var req ContractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	dataLock.Lock()
	defer dataLock.Unlock()

	foundIdx := -1
	for i, c := range PlayerShip.ActiveContracts {
		if c.ID == req.ContractID {
			foundIdx = i
			break
		}
	}

	if foundIdx == -1 {
		http.Error(w, "Contract not found on ship", http.StatusNotFound)
		return
	}

	PlayerShip.ActiveContracts = append(PlayerShip.ActiveContracts[:foundIdx], PlayerShip.ActiveContracts[foundIdx+1:]...)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PlayerShip)
}
