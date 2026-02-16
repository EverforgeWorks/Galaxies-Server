/*
Package game
File: mechanics.go
Description:
    Contains the "Physics" and logic helper functions.
    This includes calculating distances, mass, and fuel consumption rates.
    It serves as the rules engine for the physical aspects of the game.
*/

package game

import "math"

// GetPlanet is a helper to retrieve a Planet pointer by its Key.
// Returns nil if not found.
func GetPlanet(key string) *Planet {
	for _, p := range CurrentUniverse.Planets {
		if p.Key == key {
			return &p
		}
	}
	return nil
}

// GetCommodity is a helper to retrieve a Commodity pointer by its Key.
func GetCommodity(key string) *Commodity {
	for _, c := range CurrentUniverse.Commodities {
		if c.Key == key {
			return &c
		}
	}
	return nil
}

// GetModule is a helper to retrieve a ShipModule pointer by its Key.
func GetModule(key string) *ShipModule {
	for _, m := range CurrentUniverse.ShipModules {
		if m.Key == key {
			return &m
		}
	}
	return nil
}

// CalculateDistance computes the Euclidean distance between two 2D coordinates.
// It rounds to the nearest integer for game simplicity.
func CalculateDistance(p1, p2 []int) int64 {
	if len(p1) < 2 || len(p2) < 2 {
		return 0
	}
	// Sqrt((x2-x1)^2 + (y2-y1)^2)
	dist := math.Sqrt(math.Pow(float64(p2[0]-p1[0]), 2) + math.Pow(float64(p2[1]-p1[1]), 2))
	return int64(math.Round(dist))
}

// CalculateTotalMass computes the current weight of the ship.
// Formula: BaseMass + (Cargo_Qty * Mass) + (Pax_Qty * Mass) + FuelMass
func CalculateTotalMass() int64 {
	total := PlayerShip.BaseMass

	// Sum mass of all active contracts
	for _, c := range PlayerShip.ActiveContracts {
		if c.Type == "cargo" {
			total += int64(c.MassPerUnit * c.Quantity)
		} else {
			total += int64(CurrentUniverse.PassengerConfig.MassPerPassenger * c.Quantity)
		}
	}

	// Add mass of fuel (Fuel is treated as atomic units)
	// 1 Unit of Fuel * FuelMassPerUnit = Total Fuel Mass
	fuelMass := PlayerShip.Fuel * int64(CurrentUniverse.BalanceConfig.FuelMassPerUnit)

	return total + fuelMass
}

// CalculateCurrentBurn determines the fuel cost per Light Year.
// Formula: BaseBurn + ((CurrentMass - ReferenceMass) / Damping)
// ReferenceMass = Ship Empty + 50% Fuel.
func CalculateCurrentBurn() int64 {
	currentMass := CalculateTotalMass()

	// 1. Calculate Reference Mass (The "Control" state)
	// The ship is tuned to perform at BaseBurnRate when it has exactly 50% fuel and 0 cargo.
	halfFuel := PlayerShip.MaxFuel / 2
	halfFuelMass := halfFuel * int64(CurrentUniverse.BalanceConfig.FuelMassPerUnit)
	referenceMass := PlayerShip.BaseMass + halfFuelMass

	// 2. Determine Mass Delta
	// Positive = Heavier than reference (Burn Penalty)
	// Negative = Lighter than reference (Burn Bonus)
	massDiff := currentMass - referenceMass

	// 3. Apply Damping
	// Damping represents the engine's ability to handle extra weight.
	// A damping of 100 means: For every 100kg extra mass, burn 1 extra fuel.
	burnAdjustment := massDiff / PlayerShip.BurnDamping

	finalBurn := PlayerShip.BaseBurnRate + burnAdjustment

	// 4. Safety Clamp
	// Prevent free travel or negative burn if the ship is extremely light.
	if finalBurn < 100 {
		return 100
	}

	return finalBurn
}
