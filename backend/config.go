package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/alice-lg/alice-lg/backend/sources"
	"github.com/alice-lg/alice-lg/backend/sources/bioris"
	"github.com/alice-lg/alice-lg/backend/sources/birdwatcher"
	"github.com/alice-lg/alice-lg/backend/sources/gobgp"

	"github.com/go-ini/ini"
)

const SOURCE_UNKNOWN = 0
const SOURCE_BIRDWATCHER = 1
const SOURCE_GOBGP = 2
const SOURCE_BIORIS = 3

type ServerConfig struct {
	Listen                         string `ini:"listen_http"`
	EnablePrefixLookup             bool   `ini:"enable_prefix_lookup"`
	NeighboursStoreRefreshInterval int    `ini:"neighbours_store_refresh_interval"`
	RoutesStoreRefreshInterval     int    `ini:"routes_store_refresh_interval"`
	Asn                            int    `ini:"asn"`
	EnableNeighborsStatusRefresh   bool   `ini:"enable_neighbors_status_refresh"`
}

type HousekeepingConfig struct {
	Interval           int  `ini:"interval"`
	ForceReleaseMemory bool `ini:"force_release_memory"`
}

type RejectionsConfig struct {
	Reasons BgpCommunities
}

type NoexportsConfig struct {
	Reasons      BgpCommunities
	LoadOnDemand bool `ini:"load_on_demand"`
}

type RejectCandidatesConfig struct {
	Communities BgpCommunities
}

type RpkiConfig struct {
	// Define communities
	Enabled    bool     `ini:"enabled"`
	Valid      []string `ini:"valid"`
	Unknown    []string `ini:"unknown"`
	NotChecked []string `ini:"not_checked"`
	Invalid    []string `ini:"invalid"`
}

type UiConfig struct {
	RoutesColumns      map[string]string
	RoutesColumnsOrder []string

	NeighboursColumns      map[string]string
	NeighboursColumnsOrder []string

	LookupColumns      map[string]string
	LookupColumnsOrder []string

	RoutesRejections       RejectionsConfig
	RoutesNoexports        NoexportsConfig
	RoutesRejectCandidates RejectCandidatesConfig

	BgpCommunities BgpCommunities
	Rpki           RpkiConfig

	Theme ThemeConfig

	Pagination PaginationConfig
}

type ThemeConfig struct {
	Path     string `ini:"path"`
	BasePath string `ini:"url_base"` // Optional, default: /theme
}

type PaginationConfig struct {
	RoutesFilteredPageSize    int `ini:"routes_filtered_page_size"`
	RoutesAcceptedPageSize    int `ini:"routes_accepted_page_size"`
	RoutesNotExportedPageSize int `ini:"routes_not_exported_page_size"`
}

type SourceConfig struct {
	Id    string
	Order int
	Name  string
	Group string

	// Blackhole IPs
	Blackholes []string

	// Source configurations
	Type        int
	Birdwatcher birdwatcher.Config
	GoBGP       gobgp.Config
	BioRIS      bioris.Config

	// Source instance
	instance sources.Source
}

type Config struct {
	Server       ServerConfig
	Housekeeping HousekeepingConfig
	Ui           UiConfig
	Sources      []*SourceConfig
	File         string
}

// Get source by id
func (self *Config) SourceById(sourceId string) *SourceConfig {
	for _, sourceConfig := range self.Sources {
		if sourceConfig.Id == sourceId {
			return sourceConfig
		}
	}
	return nil
}

// Get instance by id
func (self *Config) SourceInstanceById(sourceId string) sources.Source {
	sourceConfig := self.SourceById(sourceId)
	if sourceConfig == nil {
		return nil // Nothing to do here.
	}

	// Get instance from config
	return sourceConfig.getInstance()
}

// Get sources keys form ini
func getSourcesKeys(config *ini.File) []string {
	sources := []string{}
	sections := config.SectionStrings()
	for _, section := range sections {
		if strings.HasPrefix(section, "source") {
			sources = append(sources, section)
		}
	}
	return sources
}

func isSourceBase(section *ini.Section) bool {
	return len(strings.Split(section.Name(), ".")) == 2
}

// Get backend configuration type
func getBackendType(section *ini.Section) int {
	name := section.Name()
	if strings.HasSuffix(name, "birdwatcher") {
		return SOURCE_BIRDWATCHER
	} else if strings.HasSuffix(name, "gobgp") {
		return SOURCE_GOBGP
	} else if strings.HasSuffix(name, "bioris") {
		return SOURCE_BIORIS
	}

	return SOURCE_UNKNOWN
}

// Get UI config: Routes Columns Default
func getRoutesColumnsDefaults() (map[string]string, []string, error) {
	columns := map[string]string{
		"network":     "Network",
		"bgp.as_path": "AS Path",
		"gateway":     "Gateway",
		"interface":   "Interface",
	}

	order := []string{"network", "bgp.as_path", "gateway", "interface"}

	return columns, order, nil
}

// Get UI config: Routes Columns
// The columns displayed in the frontend.
// The columns are ordered as in the config file.
//
// In case the configuration is empty, fall back to
// the defaults as defined in getRoutesColumnsDefault()
//
func getRoutesColumns(config *ini.File) (map[string]string, []string, error) {
	columns := make(map[string]string)
	order := []string{}

	section := config.Section("routes_columns")
	keys := section.Keys()

	if len(keys) == 0 {
		return getRoutesColumnsDefaults()
	}

	for _, key := range keys {
		columns[key.Name()] = section.Key(key.Name()).MustString("")
		order = append(order, key.Name())
	}

	return columns, order, nil
}

// Get UI config: Get Neighbours Columns Defaults
func getNeighboursColumnsDefaults() (map[string]string, []string, error) {
	columns := map[string]string{
		"address":         "Neighbour",
		"asn":             "ASN",
		"state":           "State",
		"Uptime":          "Uptime",
		"Description":     "Description",
		"routes_received": "Routes Recv.",
		"routes_filtered": "Routes Filtered",
	}

	order := []string{
		"address", "asn", "state",
		"Uptime", "Description", "routes_received", "routes_filtered",
	}

	return columns, order, nil
}

// Get UI config: Get Neighbours Columns
// basically the same as with the routes columns.
func getNeighboursColumns(config *ini.File) (
	map[string]string,
	[]string,
	error,
) {
	columns := make(map[string]string)
	order := []string{}

	section := config.Section("neighbours_columns")
	keys := section.Keys()

	if len(keys) == 0 {
		return getNeighboursColumnsDefaults()
	}

	for _, key := range keys {
		columns[key.Name()] = section.Key(key.Name()).MustString("")
		order = append(order, key.Name())
	}

	return columns, order, nil
}

// Get UI config: Get Prefix search / Routes lookup columns
// As these differ slightly from our routes in the response
// (e.g. the neighbor and source rs is referenced as a nested object)
// we provide an additional configuration for this
func getLookupColumnsDefaults() (map[string]string, []string, error) {
	columns := map[string]string{
		"network":               "Network",
		"gateway":               "Gateway",
		"neighbour.asn":         "ASN",
		"neighbour.description": "Neighbor",
		"bgp.as_path":           "AS Path",
		"routeserver.name":      "RS",
	}

	order := []string{
		"network",
		"gateway",
		"bgp.as_path",
		"neighbour.asn",
		"neighbour.description",
		"routeserver.name",
	}

	return columns, order, nil
}

func getLookupColumns(config *ini.File) (
	map[string]string,
	[]string,
	error,
) {
	columns := make(map[string]string)
	order := []string{}

	section := config.Section("lookup_columns")
	keys := section.Keys()

	if len(keys) == 0 {
		return getLookupColumnsDefaults()
	}

	for _, key := range keys {
		columns[key.Name()] = section.Key(key.Name()).MustString("")
		order = append(order, key.Name())
	}

	return columns, order, nil
}

// Helper parse communities from a section body
func parseAndMergeCommunities(
	communities BgpCommunities, body string,
) BgpCommunities {

	// Parse and merge communities
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			log.Println("Skipping malformed BGP community:", line)
			continue
		}

		community := strings.TrimSpace(kv[0])
		label := strings.TrimSpace(kv[1])
		communities.Set(community, label)
	}

	return communities
}

// Get UI config: Bgp Communities
func getBgpCommunities(config *ini.File) BgpCommunities {
	// Load defaults
	communities := MakeWellKnownBgpCommunities()
	communitiesConfig := config.Section("bgp_communities")
	if communitiesConfig == nil {
		return communities // nothing else to do here, go with the default
	}

	return parseAndMergeCommunities(communities, communitiesConfig.Body())
}

// Get UI config: Get rejections
func getRoutesRejections(config *ini.File) (RejectionsConfig, error) {
	reasonsConfig := config.Section("rejection_reasons")
	if reasonsConfig == nil {
		return RejectionsConfig{}, nil
	}

	reasons := parseAndMergeCommunities(
		make(BgpCommunities),
		reasonsConfig.Body())

	rejectionsConfig := RejectionsConfig{
		Reasons: reasons,
	}

	return rejectionsConfig, nil
}

// Get UI config: Get no export config
func getRoutesNoexports(config *ini.File) (NoexportsConfig, error) {
	baseConfig := config.Section("noexport")
	reasonsConfig := config.Section("noexport_reasons")

	// Map base configuration
	noexportsConfig := NoexportsConfig{}
	baseConfig.MapTo(&noexportsConfig)

	reasons := parseAndMergeCommunities(
		make(BgpCommunities),
		reasonsConfig.Body())

	noexportsConfig.Reasons = reasons

	return noexportsConfig, nil
}

// Get UI config: Reject candidates
func getRejectCandidatesConfig(config *ini.File) (RejectCandidatesConfig, error) {
	candidateCommunities := config.Section(
		"rejection_candidates").Key("communities").String()

	if candidateCommunities == "" {
		return RejectCandidatesConfig{}, nil
	}

	communities := BgpCommunities{}
	for i, c := range strings.Split(candidateCommunities, ",") {
		communities.Set(c, fmt.Sprintf("reject-candidate-%d", i+1))
	}

	conf := RejectCandidatesConfig{
		Communities: communities,
	}

	return conf, nil
}

// Get UI config: RPKI configuration
func getRpkiConfig(config *ini.File) (RpkiConfig, error) {
	var rpki RpkiConfig
	// Defaults taken from:
	//   https://www.euro-ix.net/en/forixps/large-bgp-communities/
	section := config.Section("rpki")
	section.MapTo(&rpki)

	fallbackAsn, err := getOwnASN(config)
	if err != nil {
		log.Println(
			"Own ASN is not configured.",
			"This might lead to unexpected behaviour with BGP large communities",
		)
	}
	ownAsn := fmt.Sprintf("%d", fallbackAsn)

	// Fill in defaults or postprocess config value
	if len(rpki.Valid) == 0 {
		rpki.Valid = []string{ownAsn, "1000", "1"}
	} else {
		rpki.Valid = strings.SplitN(rpki.Valid[0], ":", 3)
	}

	if len(rpki.Unknown) == 0 {
		rpki.Unknown = []string{ownAsn, "1000", "2"}
	} else {
		rpki.Unknown = strings.SplitN(rpki.Unknown[0], ":", 3)
	}

	if len(rpki.NotChecked) == 0 {
		rpki.NotChecked = []string{ownAsn, "1000", "3"}
	} else {
		rpki.NotChecked = strings.SplitN(rpki.NotChecked[0], ":", 3)
	}

	// As the euro-ix document states, this can be a range.
	if len(rpki.Invalid) == 0 {
		rpki.Invalid = []string{ownAsn, "1000", "4", "*"}
	} else {
		// Preprocess
		rpki.Invalid = strings.SplitN(rpki.Invalid[0], ":", 3)
		tokens := []string{}
		if len(rpki.Invalid) != 3 {
			// This is wrong, we should have three parts (RS):1000:[range]
			return rpki, fmt.Errorf("Unexpected rpki.Invalid configuration: %v", rpki.Invalid)
		} else {
			tokens = strings.Split(rpki.Invalid[2], "-")
		}

		rpki.Invalid = append([]string{rpki.Invalid[0], rpki.Invalid[1]}, tokens...)
	}

	return rpki, nil
}

// Helper: Get own ASN from ini
// This is now easy, since we enforce an ASN in
// the [server] section.
func getOwnASN(config *ini.File) (int, error) {
	server := config.Section("server")
	asn := server.Key("asn").MustInt(-1)

	if asn == -1 {
		return 0, fmt.Errorf("Could not get own ASN from config")
	}

	return asn, nil
}

// Get UI config: Theme settings
func getThemeConfig(config *ini.File) ThemeConfig {
	baseConfig := config.Section("theme")

	themeConfig := ThemeConfig{}
	baseConfig.MapTo(&themeConfig)

	if themeConfig.BasePath == "" {
		themeConfig.BasePath = "/theme"
	}

	return themeConfig
}

// Get UI config: Pagination settings
func getPaginationConfig(config *ini.File) PaginationConfig {
	baseConfig := config.Section("pagination")

	paginationConfig := PaginationConfig{}
	baseConfig.MapTo(&paginationConfig)

	return paginationConfig
}

// Get the UI configuration from the config file
func getUiConfig(config *ini.File) (UiConfig, error) {
	uiConfig := UiConfig{}

	// Get route columns
	routesColumns, routesColumnsOrder, err := getRoutesColumns(config)
	if err != nil {
		return uiConfig, err
	}

	// Get neighbours table columns
	neighboursColumns,
		neighboursColumnsOrder,
		err := getNeighboursColumns(config)
	if err != nil {
		return uiConfig, err
	}

	// Lookup table columns
	lookupColumns, lookupColumnsOrder, err := getLookupColumns(config)
	if err != nil {
		return uiConfig, err
	}

	// Get rejections and reasons
	rejections, err := getRoutesRejections(config)
	if err != nil {
		return uiConfig, err
	}

	noexports, err := getRoutesNoexports(config)
	if err != nil {
		return uiConfig, err
	}

	// Get reject candidates
	rejectCandidates, _ := getRejectCandidatesConfig(config)

	// RPKI filter config
	rpki, err := getRpkiConfig(config)
	if err != nil {
		return uiConfig, err
	}

	// Theme configuration: Theming is optional, if no settings
	// are found, it will be ignored
	themeConfig := getThemeConfig(config)

	// Pagination
	paginationConfig := getPaginationConfig(config)

	// Make config
	uiConfig = UiConfig{
		RoutesColumns:      routesColumns,
		RoutesColumnsOrder: routesColumnsOrder,

		NeighboursColumns:      neighboursColumns,
		NeighboursColumnsOrder: neighboursColumnsOrder,

		LookupColumns:      lookupColumns,
		LookupColumnsOrder: lookupColumnsOrder,

		RoutesRejections:       rejections,
		RoutesNoexports:        noexports,
		RoutesRejectCandidates: rejectCandidates,

		BgpCommunities: getBgpCommunities(config),
		Rpki:           rpki,

		Theme: themeConfig,

		Pagination: paginationConfig,
	}

	return uiConfig, nil
}

func getSources(config *ini.File) ([]*SourceConfig, error) {
	sources := []*SourceConfig{}

	order := 0
	sourceSections := config.ChildSections("source")
	for _, section := range sourceSections {
		if !isSourceBase(section) {
			continue
		}

		// Derive source-id from name
		sourceId := section.Name()[len("source:"):]

		// Try to get child configs and determine
		// Source type
		sourceConfigSections := section.ChildSections()
		if len(sourceConfigSections) == 0 {
			// This source has no configured backend
			return sources, fmt.Errorf("%s has no backend configuration", section.Name())
		}

		if len(sourceConfigSections) > 1 {
			// The source is ambiguous
			return sources, fmt.Errorf("%s has ambigous backends", section.Name())
		}

		// Configure backend
		backendConfig := sourceConfigSections[0]
		backendType := getBackendType(backendConfig)

		if backendType == SOURCE_UNKNOWN {
			return sources, fmt.Errorf("%s has an unsupported backend", section.Name())
		}

		// Make config
		sourceName := section.Key("name").MustString("Unknown Source")
		sourceGroup := section.Key("group").MustString("")
		sourceBlackholes := TrimmedStringList(
			section.Key("blackholes").MustString(""))

		config := &SourceConfig{
			Id:         sourceId,
			Order:      order,
			Name:       sourceName,
			Group:      sourceGroup,
			Blackholes: sourceBlackholes,
			Type:       backendType,
		}

		// Set backend
		switch backendType {
		case SOURCE_BIRDWATCHER:
			sourceType := backendConfig.Key("type").MustString("")
			peerTablePrefix := backendConfig.Key("peer_table_prefix").MustString("T")
			pipeProtocolPrefix := backendConfig.Key("pipe_protocol_prefix").MustString("M")

			if sourceType != "single_table" &&
				sourceType != "multi_table" {
				log.Fatal("Configuration error (birdwatcher source) unknown birdwatcher type:", sourceType)
			}

			log.Println("Adding birdwatcher source of type", sourceType,
				"with peer_table_prefix", peerTablePrefix,
				"and pipe_protocol_prefix", pipeProtocolPrefix)

			c := birdwatcher.Config{
				Id:   config.Id,
				Name: config.Name,

				Timezone:        "UTC",
				ServerTime:      "2006-01-02T15:04:05.999999999Z07:00",
				ServerTimeShort: "2006-01-02",
				ServerTimeExt:   "Mon, 02 Jan 2006 15:04:05 -0700",

				Type:               sourceType,
				PeerTablePrefix:    peerTablePrefix,
				PipeProtocolPrefix: pipeProtocolPrefix,
			}

			backendConfig.MapTo(&c)
			config.Birdwatcher = c

		case SOURCE_GOBGP:
			c := gobgp.Config{
				Id:   config.Id,
				Name: config.Name,
			}

			backendConfig.MapTo(&c)
			config.GoBGP = c
		case SOURCE_BIORIS:
			c := bioris.Config{
				Id:   config.Id,
				Name: config.Name,
			}

			backendConfig.MapTo(&c)
			config.BioRIS = c
			//err := config.(*bioris.Config).Verify()
			//if err != nil {
			//	return sources, fmt.Errorf("Cout not configure %s", section.Name())
			//}
		}

		// Add to list of sources
		sources = append(sources, config)
		order++
	}

	return sources, nil
}

// Try to load configfiles as specified in the files
// list. For example:
//
//    ./etc/alice-lg/alice.conf
//    /etc/alice-lg/alice.conf
//    ./etc/alice-lg/alice.local.conf
//
func loadConfig(file string) (*Config, error) {

	// Try to get config file, fallback to alternatives
	file, err := getConfigFile(file)
	if err != nil {
		return nil, err
	}

	// Load configuration, but handle bgp communities section
	// with our own parser
	parsedConfig, err := ini.LoadSources(ini.LoadOptions{
		UnparseableSections: []string{
			"bgp_communities",
			"rejection_reasons",
			"noexport_reasons",
		},
	}, file)
	if err != nil {
		return nil, err
	}

	// Map sections
	server := ServerConfig{}
	parsedConfig.Section("server").MapTo(&server)

	housekeeping := HousekeepingConfig{}
	parsedConfig.Section("housekeeping").MapTo(&housekeeping)

	// Get all sources
	sources, err := getSources(parsedConfig)
	if err != nil {
		return nil, err
	}

	// Get UI configurations
	ui, err := getUiConfig(parsedConfig)
	if err != nil {
		return nil, err
	}

	config := &Config{
		Server:       server,
		Housekeeping: housekeeping,
		Ui:           ui,
		Sources:      sources,
		File:         file,
	}

	return config, nil
}

// Get source instance from config
func (self *SourceConfig) getInstance() sources.Source {
	if self.instance != nil {
		return self.instance
	}

	var instance sources.Source
	switch self.Type {
	case SOURCE_BIRDWATCHER:
		instance = birdwatcher.NewBirdwatcher(self.Birdwatcher)
	case SOURCE_GOBGP:
		instance = gobgp.NewGoBGP(self.GoBGP)
	case SOURCE_BIORIS:
		instance = bioris.NewBioRIS(self.BioRIS)
	}

	self.instance = instance
	return instance
}

// Get configuration file with fallbacks
func getConfigFile(filename string) (string, error) {
	// Check if requested file is present
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		// Fall back to local filename
		filename = ".." + filename
	}

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		filename = strings.Replace(filename, ".conf", ".local.conf", 1)
	}

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return "not_found", fmt.Errorf("could not find any configuration file")
	}

	return filename, nil
}
