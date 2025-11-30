package cards

import (
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"sdsm/app/backend/internal/manager"
	"sdsm/app/backend/internal/models"
)

// Datasets caches shared data used across multiple cards so expensive manager lookups only happen once per request.
type Datasets struct {
	manager *manager.Manager
	server  *models.Server

	summaryOnce sync.Once
	summary     *ServerSummaryDataset

	rosterOnce sync.Once
	roster     *PlayerRosterDataset

	configOnce sync.Once
	config     *ServerConfigDataset
}

// ServerSummaryDataset aggregates metadata shown in the server status overview card.
type ServerSummaryDataset struct {
	Server             *models.Server
	WorldInfo          manager.WorldInfo
	StartLocationInfo  gin.H
	StartConditionInfo gin.H
	LanguageOptions    []string
	ResolvedWorldID    string
}

// PlayerRosterDataset bundles live/history players plus ban lists for reuse across cards.
type PlayerRosterDataset struct {
	LiveClients    []*models.Client
	HistoryClients []*models.Client
	BannedEntries  []models.BannedEntry
	BannedIDs      []string
}

// ServerConfigDataset exposes localized world/difficulty/language options.
type ServerConfigDataset struct {
	WorldIDs            map[string][]string
	Worlds              map[string][]gin.H
	WorldData           map[string]map[string]gin.H
	ReleaseDifficulties []string
	BetaDifficulties    []string
	ReleaseLanguages    []string
	BetaLanguages       []string
}

// NewDatasets constructs a dataset cache for the provided manager and server.
func NewDatasets(mgr *manager.Manager, srv *models.Server) *Datasets {
	if mgr == nil || srv == nil {
		return nil
	}
	return &Datasets{manager: mgr, server: srv}
}

// ServerSummary returns cached server summary metadata, building it on first access.
func (d *Datasets) ServerSummary() *ServerSummaryDataset {
	if d == nil || d.manager == nil || d.server == nil {
		return nil
	}
	d.summaryOnce.Do(func() {
		server := d.server
		mgr := d.manager

		worldInfo := mgr.GetWorldInfoWithLanguage(server.World, server.Beta, server.Language)
		startLocation := buildStartLocationInfo(
			mgr.GetStartLocationsForWorldVersionWithLanguage(server.WorldID, server.Beta, server.Language),
			server.StartLocation,
		)
		startCondition := buildStartConditionInfo(
			mgr.GetStartConditionsForWorldVersionWithLanguage(server.WorldID, server.Beta, server.Language),
			server.StartCondition,
		)

		languageOptions := mgr.GetLanguagesForVersion(server.Beta)
		if len(languageOptions) == 0 {
			languageOptions = mgr.GetLanguagesForVersion(false)
		}

		resolvedWorldID := mgr.ResolveWorldID(server.WorldID, server.Beta)

		d.summary = &ServerSummaryDataset{
			Server:             server,
			WorldInfo:          worldInfo,
			StartLocationInfo:  startLocation,
			StartConditionInfo: startCondition,
			LanguageOptions:    languageOptions,
			ResolvedWorldID:    resolvedWorldID,
		}
	})
	return d.summary
}

// PlayerRoster returns cached player roster + ban metadata.
func (d *Datasets) PlayerRoster() *PlayerRosterDataset {
	if d == nil || d.manager == nil || d.server == nil {
		return nil
	}
	d.rosterOnce.Do(func() {
		server := d.server
		liveSorted := append([]*models.Client(nil), server.LiveClients()...)
		if len(liveSorted) > 1 {
			sort.SliceStable(liveSorted, func(i, j int) bool {
				return liveSorted[i].ConnectDatetime.After(liveSorted[j].ConnectDatetime)
			})
		}
		historySorted := append([]*models.Client(nil), server.Clients...)
		if len(historySorted) > 1 {
			sort.SliceStable(historySorted, func(i, j int) bool {
				return historySorted[i].ConnectDatetime.After(historySorted[j].ConnectDatetime)
			})
		}

		d.roster = &PlayerRosterDataset{
			LiveClients:    liveSorted,
			HistoryClients: historySorted,
			BannedEntries:  server.BannedEntries(),
			BannedIDs:      server.ReadBlacklistIDs(),
		}
	})
	return d.roster
}

// ServerConfig returns cached localized server configuration metadata for selection controls.
func (d *Datasets) ServerConfig() *ServerConfigDataset {
	if d == nil || d.manager == nil || d.server == nil {
		return nil
	}
	d.configOnce.Do(func() {
		mgr := d.manager
		language := strings.TrimSpace(d.server.Language)
		if language == "" {
			language = strings.TrimSpace(mgr.Language)
		}
		if language == "" {
			language = "english"
		}

		worldIDs := map[string][]string{
			"release": mgr.GetWorldIDsByVersion(false),
			"beta":    mgr.GetWorldIDsByVersion(true),
		}

		worldDisplays := map[string][]gin.H{
			"release": {},
			"beta":    {},
		}
		for _, id := range worldIDs["release"] {
			info := mgr.GetWorldInfoWithLanguage(id, false, language)
			name := id
			if strings.TrimSpace(info.Name) != "" {
				name = info.Name
			}
			worldDisplays["release"] = append(worldDisplays["release"], gin.H{"id": id, "name": name})
		}
		for _, id := range worldIDs["beta"] {
			info := mgr.GetWorldInfoWithLanguage(id, true, language)
			name := id
			if strings.TrimSpace(info.Name) != "" {
				name = info.Name
			}
			worldDisplays["beta"] = append(worldDisplays["beta"], gin.H{"id": id, "name": name})
		}

		worldData := map[string]map[string]gin.H{
			"release": {},
			"beta":    {},
		}
		for _, id := range worldIDs["release"] {
			worldData["release"][id] = gin.H{
				"locations":  mgr.GetStartLocationsForWorldVersionWithLanguage(id, false, language),
				"conditions": mgr.GetStartConditionsForWorldVersionWithLanguage(id, false, language),
			}
		}
		for _, id := range worldIDs["beta"] {
			worldData["beta"][id] = gin.H{
				"locations":  mgr.GetStartLocationsForWorldVersionWithLanguage(id, true, language),
				"conditions": mgr.GetStartConditionsForWorldVersionWithLanguage(id, true, language),
			}
		}

		d.config = &ServerConfigDataset{
			WorldIDs:            worldIDs,
			Worlds:              worldDisplays,
			WorldData:           worldData,
			ReleaseDifficulties: mgr.GetDifficultiesForVersionWithLanguage(false, language),
			BetaDifficulties:    mgr.GetDifficultiesForVersionWithLanguage(true, language),
			ReleaseLanguages:    mgr.GetLanguagesForVersion(false),
			BetaLanguages:       mgr.GetLanguagesForVersion(true),
		}
	})
	return d.config
}

func buildStartLocationInfo(locations []manager.LocationInfo, selected string) gin.H {
	if len(locations) == 0 {
		return nil
	}
	selected = strings.TrimSpace(selected)
	match := locations[0]
	for _, loc := range locations {
		if selected != "" && strings.EqualFold(strings.TrimSpace(loc.ID), selected) {
			match = loc
			break
		}
	}
	name := strings.TrimSpace(match.Name)
	if name == "" {
		name = match.ID
	}
	return gin.H{
		"id":          match.ID,
		"name":        name,
		"description": match.Description,
	}
}

func buildStartConditionInfo(conditions []manager.ConditionInfo, selected string) gin.H {
	if len(conditions) == 0 {
		return nil
	}
	selected = strings.TrimSpace(selected)
	match := conditions[0]
	for _, cond := range conditions {
		if selected != "" && strings.EqualFold(strings.TrimSpace(cond.ID), selected) {
			match = cond
			break
		}
	}
	name := strings.TrimSpace(match.Name)
	if name == "" {
		name = match.ID
	}
	return gin.H{
		"id":          match.ID,
		"name":        name,
		"description": match.Description,
	}
}
