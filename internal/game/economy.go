/*
Package game
File: economy.go
Description:
    Handles the economic simulation of the universe.
    This includes:
    1. Managing "Market Heat" (Supply/Demand fluctuations).
    2. Replenishing job boards based on planet configuration.
    3. Generating procedural contracts (Cargo and Passengers).
*/

package game

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

// InitMarket prepares the Market heat maps.
// Sets all Source and Destination heat values to 1.0 (Neutral).
func InitMarket() {
	for _, p := range CurrentUniverse.Planets {
		Market.SourceHeat[p.Key] = make(map[string]float64)
		Market.DestHeat[p.Key] = make(map[string]float64)

		for _, c := range CurrentUniverse.Commodities {
			Market.SourceHeat[p.Key][c.Key] = 1.0
			Market.DestHeat[p.Key][c.Key] = 1.0
		}
	}
}

// RecordAcceptance is called when a player takes a job.
// It increases Source Heat, representing that the item is becoming scarcer at this location.
func (m *MarketState) RecordAcceptance(originKey, itemKey string, qty int) {
	// Note: Caller must hold DataLock
	if m.SourceHeat[originKey] == nil {
		return
	}
	// Impact: 0.01 Heat per unit accepted.
	impact := float64(qty) * 0.01
	m.SourceHeat[originKey][itemKey] += impact
}

// RecordDelivery is called when a player finishes a job.
// It increases Destination Heat, representing market saturation (lowering future payouts).
func (m *MarketState) RecordDelivery(destKey, itemKey string, qty int) {
	// Note: Caller must hold DataLock
	if m.DestHeat[destKey] == nil {
		return
	}
	// Impact: 0.02 Heat per unit delivered (Markets crash faster than they recover).
	impact := float64(qty) * 0.02
	m.DestHeat[destKey][itemKey] += impact
}

// MarketTick "Cools down" the economy, simulating consumption and production over time.
// It pushes all heat values slowly back towards 1.0.
func MarketTick() {
	DataLock.Lock()
	defer DataLock.Unlock()

	recoveryRate := 0.05 // 5% recovery per tick

	// 1. Recover Source Heat (Mines produce more ore)
	for pKey, commodities := range Market.SourceHeat {
		for cKey, heat := range commodities {
			if heat > 1.0 {
				Market.SourceHeat[pKey][cKey] = math.Max(1.0, heat-recoveryRate)
			} else if heat < 1.0 {
				Market.SourceHeat[pKey][cKey] = math.Min(1.0, heat+recoveryRate)
			}
		}
	}

	// 2. Recover Dest Heat (Populations consume goods)
	for pKey, commodities := range Market.DestHeat {
		for cKey, heat := range commodities {
			if heat > 1.0 {
				Market.DestHeat[pKey][cKey] = math.Max(1.0, heat-recoveryRate)
			} else if heat < 1.0 {
				Market.DestHeat[pKey][cKey] = math.Min(1.0, heat+recoveryRate)
			}
		}
	}
}

// ReplenishMarket is the main heartbeat function called by the server loop.
// It iterates through all planets and generates new contracts if inventory is low.
// Returns a list of planet keys that were updated.
func ReplenishMarket() []string {
	// 1. Run the simulation tick first
	MarketTick()

	DataLock.Lock()
	defer DataLock.Unlock()

	updatedPlanets := []string{}

	for i := range CurrentUniverse.Planets {
		origin := &CurrentUniverse.Planets[i]

		// Defaults if YAML is missing configuration
		minCargo := origin.MinCargo
		maxCargo := origin.MaxCargo
		if maxCargo == 0 {
			minCargo = 5
			maxCargo = 15
		}

		minPax := origin.MinPassengers
		maxPax := origin.MaxPassengers
		if maxPax == 0 {
			minPax = 2
			maxPax = 8
		}

		// --- CARGO CHECK ---
		currentCargoCount := 0
		for _, c := range AvailableContracts[origin.Key] {
			if c.Type == "cargo" {
				currentCargoCount++
			}
		}

		if currentCargoCount < minCargo {
			target := rand.Intn(maxCargo-minCargo+1) + minCargo
			needed := target - currentCargoCount
			if needed > 0 {
				generateCargoJobs(origin, needed)
				updatedPlanets = append(updatedPlanets, origin.Key)
			}
		}

		// --- PASSENGER CHECK ---
		currentPaxCount := 0
		for _, c := range AvailableContracts[origin.Key] {
			if c.Type == "passenger" {
				currentPaxCount++
			}
		}

		if currentPaxCount < minPax {
			target := rand.Intn(maxPax-minPax+1) + minPax
			needed := target - currentPaxCount
			if needed > 0 {
				generatePassengerJobs(origin, needed)
				// Deduplicate planet keys in return list
				found := false
				for _, k := range updatedPlanets {
					if k == origin.Key {
						found = true
						break
					}
				}
				if !found {
					updatedPlanets = append(updatedPlanets, origin.Key)
				}
			}
		}
	}
	return updatedPlanets
}

// generateCargoJobs creates 'count' new cargo contracts for the given origin.
func generateCargoJobs(origin *Planet, count int) {
	for i := 0; i < count; i++ {
		// 1. Pick Commodity: 80% chance for Local Production, 20% Global Random
		var comm Commodity
		if len(origin.Production) > 0 && rand.Float32() < 0.8 {
			prodKey := origin.Production[rand.Intn(len(origin.Production))]
			commPtr := GetCommodity(prodKey)
			if commPtr != nil {
				comm = *commPtr
			} else {
				comm = CurrentUniverse.Commodities[rand.Intn(len(CurrentUniverse.Commodities))]
			}
		} else {
			comm = CurrentUniverse.Commodities[rand.Intn(len(CurrentUniverse.Commodities))]
		}

		// 2. Scarcity Check: If Source Heat is too high, maybe fail to generate
		sourceHeat := Market.SourceHeat[origin.Key][comm.Key]
		if sourceHeat > 1.0 && rand.Float64()*sourceHeat > 1.5 {
			continue
		}

		// 3. Pick Destination: Must be different from Origin
		dest := CurrentUniverse.Planets[rand.Intn(len(CurrentUniverse.Planets))]
		for dest.Key == origin.Key {
			dest = CurrentUniverse.Planets[rand.Intn(len(CurrentUniverse.Planets))]
		}

		// 4. Calculate Economics
		qty := rand.Intn(21) + 5
		dist := CalculateDistance(origin.Coordinates, dest.Coordinates)
		destHeat := Market.DestHeat[dest.Key][comm.Key]
		priceMod := 1.0 / destHeat // High saturation = Low Price

		basePayout := int(dist)*CurrentUniverse.BalanceConfig.DistancePayoutMult + (comm.BaseValue * qty / 2)
		finalPayout := int(float64(basePayout) * priceMod)

		// 5. Create Contract
		job := Contract{
			ID:             fmt.Sprintf("CRG-%d-%d", rand.Intn(99999), time.Now().UnixNano()%1000),
			Type:           "cargo",
			ItemName:       comm.Name,
			ItemKey:        comm.Key,
			Quantity:       qty,
			MassPerUnit:    comm.Mass,
			OriginKey:      origin.Key,
			DestinationKey: dest.Key,
			Payout:         finalPayout,
		}
		AvailableContracts[origin.Key] = append(AvailableContracts[origin.Key], job)
	}
}

// generatePassengerJobs creates 'count' new passenger contracts.
func generatePassengerJobs(origin *Planet, count int) {
	for i := 0; i < count; i++ {
		dest := CurrentUniverse.Planets[rand.Intn(len(CurrentUniverse.Planets))]
		for dest.Key == origin.Key {
			dest = CurrentUniverse.Planets[rand.Intn(len(CurrentUniverse.Planets))]
		}

		dist := CalculateDistance(origin.Coordinates, dest.Coordinates)
		payout := int(dist)*15 + CurrentUniverse.PassengerConfig.BaseTicketPrice

		job := Contract{
			ID:             fmt.Sprintf("PAX-%d-%d", rand.Intn(99999), time.Now().UnixNano()%1000),
			Type:           "passenger",
			ItemName:       "Passenger",
			ItemKey:        "passenger",
			Quantity:       1,
			MassPerUnit:    CurrentUniverse.PassengerConfig.MassPerPassenger,
			OriginKey:      origin.Key,
			DestinationKey: dest.Key,
			Payout:         payout,
		}
		AvailableContracts[origin.Key] = append(AvailableContracts[origin.Key], job)
	}
}
