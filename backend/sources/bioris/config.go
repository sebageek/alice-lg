package bioris

import (
	"fmt"
)

// Config holds all configuration variables to connect to the BioRIS
type Config struct {
	Id   string
	Name string

	API    string `ini:"api"`
	Router string `ini:"router"`
	VRFID  uint64 `ini:"vrf_id"`
}

// Verify verifies that required fields in the config are set
func (config *Config) Verify() error {
	if config.API == "" {
		return fmt.Errorf("Missing api configuration")
	}

	if config.Router == "" {
		return fmt.Errorf("A router needs to be specified")
	}

	return nil
}
