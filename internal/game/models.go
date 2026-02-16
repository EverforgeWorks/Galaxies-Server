/*
Package game
File: models.go
Description:
    Defines all data structures (Structs) used throughout the Galaxies universe.
    This file serves as the "schema" for the application, mapping directly to
    YAML configuration files and JSON API responses.

    No logic is performed here; this file is strictly for type definitions.
*/

package game

// GameBalance stores global tuning variables loaded from 'universe.yaml'.
// These values control the macro-economy and physics constants.
type GameBalance struct {
	StartingCredits    int `yaml:"starting_credits" json:"starting_credits"`         // Credits given to a new player/reset
	FuelCostPerUnit    int `yaml:"fuel_cost_per_unit" json:"fuel_cost_per_unit"`     // Cost to buy 1.0 fuel at a depot
	FuelMassPerUnit    int `yaml:"fuel_mass_per_unit" json:"fuel_mass_per_unit"`     // Weight of 1.0 fuel (Impacts burn rate)
	DistancePayoutMult int `yaml:"distance_payout_mult" json:"distance_payout_mult"` // Credits earned per Light Year traveled
}

// ShipModule represents an installable upgrade for the player ship.
type ShipModule struct {
	Key          string `yaml:"key" json:"key"`                     // Unique ID (e.g., "mod_cargo_bay")
	Name         string `yaml:"name" json:"name"`                   // Display name
	Description  string `yaml:"description" json:"description"`     // Flavor text
	Cost         int    `yaml:"cost" json:"cost"`                   // Purchase price in Credits
	StatModifier string `yaml:"stat_modifier" json:"stat_modifier"` // The struct field this mod affects (e.g., "cargo_capacity")
	StatValue    int    `yaml:"stat_value" json:"stat_value"`       // The numeric amount added to the modifier
}

// Contract represents a generated job (Cargo or Passenger) available on a planet.
type Contract struct {
	ID             string `json:"id"`              // Unique runtime ID (e.g., "CRG-1024-55")
	Type           string `json:"type"`            // "cargo" or "passenger"
	ItemName       string `json:"item_name"`       // Display name of the goods/person
	ItemKey        string `json:"item_key"`        // ID used for MarketState heat-map tracking
	Quantity       int    `json:"quantity"`        // Number of units/passengers
	MassPerUnit    int    `json:"mass_per_unit"`   // Weight per unit (Total Mass = Qty * MassPerUnit)
	OriginKey      string `json:"origin_key"`      // Planet Key where the contract starts
	DestinationKey string `json:"destination_key"` // Planet Key where the contract must be delivered
	Payout         int    `json:"payout"`          // Reward in Credits upon completion
}

// Planet represents a static location (Node) in the universe.
type Planet struct {
	Key         string   `json:"key" yaml:"key"`                 // Unique ID (e.g., "planet_prime")
	Name        string   `json:"name" yaml:"name"`               // Display Name
	Coordinates []int    `json:"coordinates" yaml:"coordinates"` // [X, Y] position on the starmap
	Production  []string `json:"production" yaml:"production"`   // List of Commodity Keys this planet SELLS
	Demand      []string `json:"demand" yaml:"demand"`           // List of Commodity Keys this planet BUYS (at a premium)

	// Economy Configuration: Determines the "Inventory Level" this planet tries to maintain.
	MinCargo      int `json:"min_cargo" yaml:"min_cargo"`           // Minimum cargo contracts available
	MaxCargo      int `json:"max_cargo" yaml:"max_cargo"`           // Maximum cargo contracts available
	MinPassengers int `json:"min_passengers" yaml:"min_passengers"` // Minimum passenger contracts available
	MaxPassengers int `json:"max_passengers" yaml:"max_passengers"` // Maximum passenger contracts available
}

// Ship represents the player's vessel, including its current state and configuration.
type Ship struct {
	Name        string `json:"name" yaml:"name"`         // Ship Name
	LocationKey string `json:"location_key"`             // Current Planet Key where the ship is docked
	Credits     int    `json:"credits"`                  // Current wallet balance
	Fuel        int64  `json:"fuel"`                     // Current Fuel Level
	MaxFuel     int64  `json:"max_fuel" yaml:"max_fuel"` // Fuel Tank Capacity

	// Engine / Physics Stats
	BaseBurnRate int64 `json:"base_burn_rate" yaml:"base_burn_rate"` // Fuel consumed per LY at Reference Mass
	BurnDamping  int64 `json:"burn_damping" yaml:"burn_damping"`     // Resistance to mass penalties (Higher = Mass matters less)
	BaseMass     int64 `json:"base_mass" yaml:"base_mass"`           // Weight of the empty chassis

	// Capacity Stats
	CargoCapacity  int `json:"cargo_capacity" yaml:"cargo_capacity"`     // Max units of cargo allowed
	PassengerSlots int `json:"passenger_slots" yaml:"passenger_slots"`   // Max passengers allowed
	MaxModuleSlots int `json:"max_module_slots" yaml:"max_module_slots"` // Max installed modules

	// Dynamic Lists
	InstalledModules []ShipModule `json:"installed_modules"` // List of currently installed upgrades
	ActiveContracts  []Contract   `json:"active_contracts"`  // List of jobs currently on board
}

// PassengerConfig defines the baseline variables for generating passenger jobs.
type PassengerConfig struct {
	BaseTicketPrice  int `yaml:"base_ticket_price"`  // Flat fee added to distance calculation
	MassPerPassenger int `yaml:"mass_per_passenger"` // Standard weight of a passenger + luggage
}

// Commodity represents a tradeable good.
type Commodity struct {
	Key       string `yaml:"key" json:"key"`               // Unique ID (e.g., "item_water")
	Name      string `yaml:"name" json:"name"`             // Display Name
	BaseValue int    `yaml:"base_value" json:"base_value"` // Baseline price before market multipliers
	Mass      int    `yaml:"mass" json:"mass"`             // Weight per unit
}

// Universe is the root configuration struct, mapping to the entire 'universe.yaml' file.
type Universe struct {
	BalanceConfig    GameBalance     `yaml:"game_balance"`
	PlayerShipConfig Ship            `yaml:"player_ship"`
	Commodities      []Commodity     `yaml:"commodities"`
	Planets          []Planet        `yaml:"planets"`
	ShipModules      []ShipModule    `yaml:"ship_modules"`
	PassengerConfig  PassengerConfig `yaml:"passenger_config"`
}

// MarketState tracks the dynamic "Heat" (Supply/Demand pressure) of the economy.
// This is not stored in YAML but generated at runtime.
type MarketState struct {
	// SourceHeat maps PlanetKey -> CommodityKey -> Scarcity Multiplier.
	// > 1.0 means the resource is over-mined (scarcity).
	SourceHeat map[string]map[string]float64

	// DestHeat maps PlanetKey -> CommodityKey -> Saturation Multiplier.
	// > 1.0 means the market is flooded (low prices).
	DestHeat map[string]map[string]float64
}
