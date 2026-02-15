/*
Package main
File: state.go
Description: Manages the global game state, including the Universe configuration,
player ship status, and the dynamic market economy. It handles the logic for
replenishing the market based on supply/demand heat maps and planet-specific configuration.
*/

package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// GameBalance stores global tuning variables from YAML.
type GameBalance struct {
	StartingCredits    int `yaml:"starting_credits" json:"starting_credits"`
	FuelCostPerUnit    int `yaml:"fuel_cost_per_unit" json:"fuel_cost_per_unit"`
	FuelMassPerUnit    int `yaml:"fuel_mass_per_unit" json:"fuel_mass_per_unit"`
	BaseBurnRate       int `yaml:"base_burn_rate" json:"base_burn_rate"`
	DistancePayoutMult int `yaml:"distance_payout_mult" json:"distance_payout_mult"`
}

type ShipModule struct {
	Key          string `yaml:"key" json:"key"`
	Name         string `yaml:"name" json:"name"`
	Description  string `yaml:"description" json:"description"`
	Cost         int    `yaml:"cost" json:"cost"`
	StatModifier string `yaml:"stat_modifier" json:"stat_modifier"`
	StatValue    int    `yaml:"stat_value" json:"stat_value"`
}

type Contract struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	ItemName       string `json:"item_name"`
	ItemKey        string `json:"item_key"` // Critical for MarketState tracking
	Quantity       int    `json:"quantity"`
	MassPerUnit    int    `json:"mass_per_unit"`
	OriginKey      string `json:"origin_key"`
	DestinationKey string `json:"destination_key"`
	Payout         int    `json:"payout"`
}

type Planet struct {
	Key         string   `json:"key" yaml:"key"`
	Name        string   `json:"name" yaml:"name"`
	Coordinates []int    `json:"coordinates" yaml:"coordinates"`
	Production  []string `json:"production" yaml:"production"`
	Demand      []string `json:"demand" yaml:"demand"`

	// Economy Configuration: Per-planet limits
	MinCargo      int `json:"min_cargo" yaml:"min_cargo"`
	MaxCargo      int `json:"max_cargo" yaml:"max_cargo"`
	MinPassengers int `json:"min_passengers" yaml:"min_passengers"`
	MaxPassengers int `json:"max_passengers" yaml:"max_passengers"`
}

type Ship struct {
	Name             string       `json:"name" yaml:"name"`
	Fuel             int64        `json:"fuel"`
	MaxFuel          int64        `json:"max_fuel" yaml:"max_fuel"`
	LocationKey      string       `json:"location_key"`
	BurnRate         int64        `json:"burn_rate" yaml:"fuel_burn_rate"`
	CargoCapacity    int          `json:"cargo_capacity" yaml:"cargo_capacity"`
	PassengerSlots   int          `json:"passenger_slots" yaml:"passenger_slots"`
	Credits          int          `json:"credits"`
	BaseMass         int64        `json:"base_mass" yaml:"base_mass"`
	Efficiency       int64        `json:"engine_efficiency" yaml:"engine_efficiency"`
	MaxModuleSlots   int          `json:"max_module_slots" yaml:"max_module_slots"`
	InstalledModules []ShipModule `json:"installed_modules"`
	ActiveContracts  []Contract   `json:"active_contracts"`
}

type PassengerConfig struct {
	BaseTicketPrice  int `yaml:"base_ticket_price"`
	MassPerPassenger int `yaml:"mass_per_passenger"`
}

type Universe struct {
	BalanceConfig    GameBalance     `yaml:"game_balance"`
	PlayerShipConfig Ship            `yaml:"player_ship"`
	Commodities      []Commodity     `yaml:"commodities"`
	Planets          []Planet        `yaml:"planets"`
	ShipModules      []ShipModule    `yaml:"ship_modules"`
	PassengerConfig  PassengerConfig `yaml:"passenger_config"`
}

type Commodity struct {
	Key       string `yaml:"key" json:"key"`
	Name      string `yaml:"name" json:"name"`
	BaseValue int    `yaml:"base_value" json:"base_value"`
	Mass      int    `yaml:"mass" json:"mass"`
}

// MarketState tracks the "Heat" (Supply/Demand pressure) of the economy.
type MarketState struct {
	// SourceHeat maps PlanetKey -> CommodityKey -> HeatLevel (float64)
	// High SourceHeat = Resource is mined out (Lower Spawn Rate)
	SourceHeat map[string]map[string]float64

	// DestHeat maps PlanetKey -> CommodityKey -> HeatLevel (float64)
	// High DestHeat = Market is flooded (Lower Payouts)
	DestHeat map[string]map[string]float64
}

var (
	dataLock           sync.RWMutex
	CurrentUniverse    Universe
	PlayerShip         Ship
	AvailableContracts = make(map[string][]Contract)

	// Global Market Instance
	Market = MarketState{
		SourceHeat: make(map[string]map[string]float64),
		DestHeat:   make(map[string]map[string]float64),
	}
)

// InitMarket initializes the heat maps for all planets and commodities.
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

// RecordAcceptance increases Source Heat (Making it scarcer).
func (m *MarketState) RecordAcceptance(originKey, itemKey string, qty int) {
	dataLock.Lock()
	defer dataLock.Unlock()

	if m.SourceHeat[originKey] == nil {
		return
	}
	// Impact factor: 0.01 heat per unit.
	impact := float64(qty) * 0.01
	m.SourceHeat[originKey][itemKey] += impact
}

// RecordDelivery increases Destination Heat (Crashing the price).
func (m *MarketState) RecordDelivery(destKey, itemKey string, qty int) {
	dataLock.Lock()
	defer dataLock.Unlock()

	if m.DestHeat[destKey] == nil {
		return
	}
	// Impact factor: 0.02 per unit (Markets crash faster than mines deplete).
	impact := float64(qty) * 0.02
	m.DestHeat[destKey][itemKey] += impact
}

// MarketTick "Cools down" the economy (Regeneration/Consumption).
func MarketTick() {
	dataLock.Lock()
	defer dataLock.Unlock()

	recoveryRate := 0.05 // 5% recovery per tick towards 1.0

	// Cool Source Heat
	for pKey, commodities := range Market.SourceHeat {
		for cKey, heat := range commodities {
			if heat > 1.0 {
				Market.SourceHeat[pKey][cKey] = math.Max(1.0, heat-recoveryRate)
			} else if heat < 1.0 {
				Market.SourceHeat[pKey][cKey] = math.Min(1.0, heat+recoveryRate)
			}
		}
	}

	// Cool Dest Heat
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

func GetPlanet(key string) *Planet {
	for _, p := range CurrentUniverse.Planets {
		if p.Key == key {
			return &p
		}
	}
	return nil
}

func GetCommodity(key string) *Commodity {
	for _, c := range CurrentUniverse.Commodities {
		if c.Key == key {
			return &c
		}
	}
	return nil
}

func GetModule(key string) *ShipModule {
	for _, m := range CurrentUniverse.ShipModules {
		if m.Key == key {
			return &m
		}
	}
	return nil
}

func CalculateDistance(p1, p2 []int) int64 {
	if len(p1) < 2 || len(p2) < 2 {
		return 0
	}
	dist := math.Sqrt(math.Pow(float64(p2[0]-p1[0]), 2) + math.Pow(float64(p2[1]-p1[1]), 2))
	return int64(math.Round(dist))
}

func CalculateTotalMass() int64 {
	total := PlayerShip.BaseMass
	for _, c := range PlayerShip.ActiveContracts {
		if c.Type == "cargo" {
			total += int64(c.MassPerUnit * c.Quantity)
		} else {
			total += int64(CurrentUniverse.PassengerConfig.MassPerPassenger * c.Quantity)
		}
	}
	fuelMass := (PlayerShip.Fuel / 100) * int64(CurrentUniverse.BalanceConfig.FuelMassPerUnit)
	return total + fuelMass
}

func CalculateCurrentBurn() int64 {
	mass := CalculateTotalMass()
	return int64(CurrentUniverse.BalanceConfig.BaseBurnRate) + (mass / PlayerShip.Efficiency)
}

// ReplenishMarket is the "Heartbeat" logic.
// It iterates through every planet, checks if their inventory is below the Min threshold,
// and generates enough jobs to reach a target between Min and Max.
// Returns a list of planet keys that were updated.
func ReplenishMarket() []string {
	// 1. Run the economic simulation tick to recover prices
	MarketTick()

	dataLock.Lock()
	defer dataLock.Unlock()

	rand.Seed(time.Now().UnixNano())
	updatedPlanets := []string{}

	for i := range CurrentUniverse.Planets {
		// Use pointer so we access the config values correctly
		origin := &CurrentUniverse.Planets[i]

		// Fallback defaults if YAML is missing these fields
		minCargo := origin.MinCargo
		maxCargo := origin.MaxCargo
		if maxCargo == 0 {
			minCargo = 5
			maxCargo = 15
		} // Default fallback

		minPax := origin.MinPassengers
		maxPax := origin.MaxPassengers
		if maxPax == 0 {
			minPax = 2
			maxPax = 8
		} // Default fallback

		// --- CARGO REPLENISHMENT ---
		currentCargoCount := 0
		for _, c := range AvailableContracts[origin.Key] {
			if c.Type == "cargo" {
				currentCargoCount++
			}
		}

		if currentCargoCount < minCargo {
			// Determine a random target level between Min and Max
			target := rand.Intn(maxCargo-minCargo+1) + minCargo
			needed := target - currentCargoCount

			if needed > 0 {
				generateCargoJobs(origin, needed)
				updatedPlanets = append(updatedPlanets, origin.Key)
			}
		}

		// --- PASSENGER REPLENISHMENT ---
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
				// Ensure unique keys in the return list
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
		// 1. Pick Commodity
		// Preference: 80% Local Production, 20% Global Random
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

		// MARKET SCARCITY CHECK
		sourceHeat := Market.SourceHeat[origin.Key][comm.Key]
		// If heat is high, skip generating this specific contract chance
		if sourceHeat > 1.0 && rand.Float64()*sourceHeat > 1.5 {
			continue
		}

		// 2. Pick Destination
		dest := CurrentUniverse.Planets[rand.Intn(len(CurrentUniverse.Planets))]
		for dest.Key == origin.Key {
			dest = CurrentUniverse.Planets[rand.Intn(len(CurrentUniverse.Planets))]
		}

		// 3. Stats & Pricing
		qty := rand.Intn(21) + 5
		dist := CalculateDistance(origin.Coordinates, dest.Coordinates)

		// MARKET DEMAND CHECK
		destHeat := Market.DestHeat[dest.Key][comm.Key]
		priceMod := 1.0 / destHeat

		basePayout := int(dist)*CurrentUniverse.BalanceConfig.DistancePayoutMult + (comm.BaseValue * qty / 2)
		finalPayout := int(float64(basePayout) * priceMod)

		// 4. Create Contract
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

		// Append to the specific planet's board
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

func LoadConfig() error {
	dataLock.Lock()
	defer dataLock.Unlock()

	f, err := os.ReadFile("universe.yaml")
	if err != nil {
		return err
	}
	var newUni Universe
	if err := yaml.Unmarshal(f, &newUni); err != nil {
		return err
	}
	CurrentUniverse = newUni

	InitMarket()

	if PlayerShip.LocationKey == "" {
		PlayerShip = CurrentUniverse.PlayerShipConfig
		PlayerShip.Fuel = PlayerShip.MaxFuel
		PlayerShip.LocationKey = "planet_prime"
		PlayerShip.Credits = CurrentUniverse.BalanceConfig.StartingCredits
		PlayerShip.ActiveContracts = []Contract{}
		PlayerShip.InstalledModules = []ShipModule{}
	}
	return nil
}
