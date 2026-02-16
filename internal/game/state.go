/*
Package game
File: state.go
Description:
    Manages the runtime state of the application.
    It holds the Global Variables that represent the current universe,
    the player's ship, and the active job board.

    It also handles the initialization (LoadConfig) logic.
*/

package game

import (
	"math/rand"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	// DataLock protects all global variables below from concurrent read/write issues.
	// Any API handler reading or writing to these globals MUST hold this lock.
	DataLock sync.RWMutex

	// CurrentUniverse holds the static configuration loaded from YAML.
	CurrentUniverse Universe

	// PlayerShip represents the specific instance of the player's vessel.
	// This is modified heavily during runtime (travel, trading, upgrades).
	PlayerShip Ship

	// AvailableContracts maps PlanetKey -> List of Contracts.
	// These are the jobs currently sitting on the "Job Board" at each planet.
	AvailableContracts = make(map[string][]Contract)

	// Market represents the global supply/demand simulation state.
	Market = MarketState{
		SourceHeat: make(map[string]map[string]float64),
		DestHeat:   make(map[string]map[string]float64),
	}
)

// LoadConfig reads 'universe.yaml' and initializes the game state.
// It resets the player to the default configuration if they don't have a location.
func LoadConfig() error {
	DataLock.Lock()
	defer DataLock.Unlock()

	// 1. Read the YAML file
	f, err := os.ReadFile("universe.yaml")
	if err != nil {
		return err
	}

	// 2. Unmarshal into the Universe struct
	var newUni Universe
	if err := yaml.Unmarshal(f, &newUni); err != nil {
		return err
	}
	CurrentUniverse = newUni

	// 3. Initialize the Market Heat Maps
	InitMarket() // Defined in economy.go

	// 4. Initialize Random Seed for procedural generation
	// We do this once here to ensure random distribution throughout the session.
	rand.Seed(time.Now().UnixNano())

	// 5. Initialize Player Ship (New Game Logic)
	// If the ship has no location, we assume it's a fresh boot and apply defaults.
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
