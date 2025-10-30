package manager

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Output types
type RSLanguage struct {
	FileName    string
	DisplayName string
}

type RSStartCondition struct {
	ID            string
	DisplayName   string
	Description   string
	PreviewButton string
	IsDefault     bool
}

type RSStartLocation struct {
	ID          string
	Name        string
	Description string
}

type RSDifficulty struct {
	ID            string
	Name          string
	Description   string
	PreviewButton string
	IsDefault     bool
}

type RSWorldDefinition struct {
	Directory        string
	ID               string
	Name             string
	Description      string
	ShortDescription string
	Priority         int
	Hidden           bool
	Rating           string
	RatingColor      string
	StartConditions  []RSStartCondition
	StartLocations   []RSStartLocation
	Image            string
}

// Helper to read and unmarshal XML
func readXML(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return xml.Unmarshal(data, v)
}

// Load translations from language file
func LoadLanguageTranslations(filePath string) (map[string]string, error) {
	var langData struct {
		Name      string `xml:"Name"`
		Interface struct {
			Records []struct {
				Key   string `xml:"Key"`
				Value string `xml:"Value"`
			} `xml:"Record"`
		} `xml:"Interface"`
	}

	if err := readXML(filePath, &langData); err != nil {
		return nil, err
	}

	translations := make(map[string]string)
	for _, record := range langData.Interface.Records {
		translations[record.Key] = record.Value
	}
	return translations, nil
}

// Load start condition definitions
func LoadStartConditionDefs(basePath string) (map[string]struct {
	Name          string
	Description   string
	PreviewButton string
}, error) {
	var data struct {
		StartConditions []struct {
			Id   string `xml:"Id,attr"`
			Name struct {
				Key string `xml:"Key,attr"`
			} `xml:"Name"`
			Description struct {
				Key string `xml:"Key,attr"`
			} `xml:"Description"`
			PreviewButton struct {
				Path string `xml:"Path,attr"`
			} `xml:"PreviewButton"`
		} `xml:"StartCondition"`
	}

	// basePath should be the rocketstation_DedicatedServer_Data directory
	path := filepath.Join(basePath, "StreamingAssets", "Data", "startconditions.xml")
	if err := readXML(path, &data); err != nil {
		return nil, err
	}

	defs := make(map[string]struct {
		Name          string
		Description   string
		PreviewButton string
	})

	for _, sc := range data.StartConditions {
		// StreamingAssets/Localization. FileName is the XML filename (e.g. english.xml).
		defs[sc.Id] = struct {
			Name          string
			Description   string
			PreviewButton string
		}{sc.Name.Key, sc.Description.Key, sc.PreviewButton.Path}
	}
	return defs, nil
	// DisplayName and Description are localized using the selected language file.
}

// Scan world definitions
func ScanWorldDefinitions(basePath string, languageFile string) ([]RSWorldDefinition, error) {
	translations, err := LoadLanguageTranslations(filepath.Join(basePath, "StreamingAssets", "Language", languageFile))
	if err != nil {
		return nil, err
	}

	// Name and Description are localized if available.
	scDefs, err := LoadStartConditionDefs(basePath)
	if err != nil {
		return nil, err
	}

	// with ID being the value the dedicated server accepts.
	worldsPath := filepath.Join(basePath, "StreamingAssets", "Worlds")
	entries, err := os.ReadDir(worldsPath)
	if err != nil {
		return nil, err
	}

	// including localized metadata, start conditions/locations, and image path.
	var worlds []RSWorldDefinition
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// localization keys to translated strings.
		xmlFiles, _ := filepath.Glob(filepath.Join(worldsPath, entry.Name(), "*.xml"))
		for _, xmlFile := range xmlFiles {
			var data struct {
				WorldSettings struct {
					World struct {
						Id       string `xml:"Id,attr"`
						Priority int    `xml:"Priority,attr"`
						Hidden   bool   `xml:"Hidden,attr"`
						Name     struct {
							Key string `xml:"Key,attr"`
						} `xml:"Name"`
						Description struct {
							Key string `xml:"Key,attr"`
						} `xml:"Description"`
						ShortDescription struct {
							Key string `xml:"Key,attr"`
						} `xml:"ShortDescription"`
						Rating struct {
							Key   string `xml:"Key,attr"`
							Color string `xml:"Color,attr"`
						} `xml:"Rating"`
						StartCondition []struct {
							Id        string `xml:"Id,attr"`
							IsDefault bool   `xml:"IsDefault,attr"`
						} `xml:"StartCondition"`
						StartLocation []struct {
							Id string `xml:"Id,attr"`
						} `xml:"StartLocation"`
					} `xml:"World"`
				} `xml:"WorldSettings"`
			}

			// data folder and returns a map keyed by condition ID.
			if err := readXML(xmlFile, &data); err != nil || data.WorldSettings.World.Id == "" {
				continue
			}

			w := data.WorldSettings.World
			world := RSWorldDefinition{
				Directory:        entry.Name(),
				ID:               w.Id,
				Priority:         w.Priority,
				Hidden:           w.Hidden,
				Name:             translations[w.Name.Key],
				Description:      translations[w.Description.Key],
				ShortDescription: translations[w.ShortDescription.Key],
				Rating:           translations[w.Rating.Key],
				RatingColor:      w.Rating.Color,
			}

			// StreamingAssets using the provided language file for localization.
			// Attach world image relative path when known
			if img := WorldImageFileName(world.ID); img != "" {
				world.Image = filepath.Join("Images", "SpaceMapImages", "Planets", img)
			}

			// WorldImageFileName returns the map image file name for a world ID prefix.
			for _, sc := range w.StartCondition {
				if def, ok := scDefs[sc.Id]; ok {
					world.StartConditions = append(world.StartConditions, RSStartCondition{
						ID:            sc.Id,
						DisplayName:   translations[def.Name],
						Description:   translations[def.Description],
						PreviewButton: def.PreviewButton,
						IsDefault:     sc.IsDefault,
					})
				}
			}
			// from the StreamingAssets path based on its world ID.

			for _, sl := range w.StartLocation {
				id := strings.TrimSpace(sl.Id)
				if id == "" {
					continue
				}
				name := translations[id+"Name"]
				if strings.TrimSpace(name) == "" {
					name = id
				}
				desc := translations[id+"Description"]
				world.StartLocations = append(world.StartLocations, RSStartLocation{ID: id, Name: name, Description: desc})
			}

			worlds = append(worlds, world)
		}
	}
	return worlds, nil
}

// WorldImageFileName returns the map image file name for a world ID prefix.
func WorldImageFileName(worldId string) string {
	if len(worldId) < 2 {
		return ""
	}
	switch worldId[0:2] {
	case "Ma":
		return "StatMars.png"
	case "Eu":
		return "StatEuropa.png"
	case "Mi":
		return "StatMimas.png"
	case "Lu":
		return "StatMoon.png"
	case "Ve":
		return "StatVenus.png"
	case "Vu":
		return "StatVulkan.png"
	default:
		return ""
	}
}

// GetWorldImage reads the planet image PNG for a world from the game data base path.
func GetWorldImage(basePath string, worldId string) ([]byte, error) {
	fileName := WorldImageFileName(worldId)
	if fileName == "" {
		return nil, fmt.Errorf("world image not found for %s", worldId)
	}
	imagePath := filepath.Join(basePath, "StreamingAssets", "Images", "SpaceMapImages", "Planets", fileName)
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("world image not found for %s", worldId)
	}
	return data, nil
}

// Scan languages
// ScanLanguages enumerates available language XML files under Localization.
func ScanLanguages(basePath string) ([]RSLanguage, error) {
	languagePath := filepath.Join(basePath, "StreamingAssets", "Language")
	xmlFiles, err := filepath.Glob(filepath.Join(languagePath, "*.xml"))
	if err != nil {
		return nil, err
	}

	var langs []RSLanguage
	for _, xmlFile := range xmlFiles {
		fileName := filepath.Base(xmlFile)
		if strings.Contains(fileName, "_") {
			continue
		}

		var langData struct {
			Name string `xml:"Name"`
		}

		if err := readXML(xmlFile, &langData); err != nil || langData.Name == "" {
			continue
		}

		langs = append(langs, RSLanguage{
			FileName:    fileName,
			DisplayName: langData.Name,
		})
	}
	return langs, nil
}

// Scan difficulties
// ScanDifficulties parses difficultySettings.xml and returns localized IDs.
func ScanDifficulties(basePath string, languageFile string) ([]RSDifficulty, error) {
	translations, err := LoadLanguageTranslations(filepath.Join(basePath, "StreamingAssets", "Language", languageFile))
	if err != nil {
		return nil, err
	}

	var data struct {
		DifficultySettings struct {
			Difficulties []struct {
				Id      string `xml:"Id,attr"`
				Default bool   `xml:"Default,attr"`
				Name    struct {
					Key string `xml:"Key,attr"`
				} `xml:"Name"`
				Description struct {
					Key string `xml:"Key,attr"`
				} `xml:"Description"`
				PreviewButton struct {
					Path string `xml:"Path,attr"`
				} `xml:"PreviewButton"`
			} `xml:"DifficultySetting"`
		} `xml:"DifficultySettings"`
	}

	path := filepath.Join(basePath, "StreamingAssets", "Data", "difficultySettings.xml")
	if err := readXML(path, &data); err != nil {
		return nil, fmt.Errorf("difficultySettings.xml not found: %s", path)
	}

	var difficulties []RSDifficulty
	for _, d := range data.DifficultySettings.Difficulties {
		difficulties = append(difficulties, RSDifficulty{
			ID:            d.Id,
			Name:          translations[d.Name.Key],
			Description:   translations[d.Description.Key],
			PreviewButton: d.PreviewButton.Path,
			IsDefault:     d.Default,
		})
	}
	return difficulties, nil
}

// Scan start locations for a specific world directory
// ScanStartLocations parses the start locations for a given world directory.
func ScanStartLocations(basePath string, languageFile string, worldDir string) ([]RSStartLocation, error) {
	translations, err := LoadLanguageTranslations(filepath.Join(basePath, "StreamingAssets", "Language", languageFile))
	if err != nil {
		return nil, err
	}

	// Look for primary world XML first; if not present, any XML under the worldDir
	worldsPath := filepath.Join(basePath, "StreamingAssets", "Worlds", worldDir)
	candidates := []string{filepath.Join(worldsPath, worldDir+".xml")}
	if fi, err := os.Stat(candidates[0]); err != nil || fi.IsDir() {
		matches, _ := filepath.Glob(filepath.Join(worldsPath, "*.xml"))
		candidates = matches
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("world xml not found for %s", worldDir)
	}

	type startLocation struct {
		Id string `xml:"Id,attr"`
	}
	var out []RSStartLocation
	seen := make(map[string]struct{})

	for _, path := range candidates {
		var data struct {
			WorldSettings struct {
				World struct {
					StartLocation []startLocation `xml:"StartLocation"`
				} `xml:"World"`
			} `xml:"WorldSettings"`
		}
		if err := readXML(path, &data); err != nil {
			continue
		}
		for _, sl := range data.WorldSettings.World.StartLocation {
			id := strings.TrimSpace(sl.Id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			name := translations[id+"Name"]
			if strings.TrimSpace(name) == "" {
				name = id
			}
			desc := translations[id+"Description"]
			out = append(out, RSStartLocation{ID: id, Name: name, Description: desc})
		}
	}

	return out, nil
}
