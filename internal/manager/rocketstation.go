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
		defs[sc.Id] = struct {
			Name          string
			Description   string
			PreviewButton string
		}{sc.Name.Key, sc.Description.Key, sc.PreviewButton.Path}
	}
	return defs, nil
}

// Scan world definitions
func ScanWorldDefinitions(basePath string, languageFile string) ([]RSWorldDefinition, error) {
	translations, err := LoadLanguageTranslations(filepath.Join(basePath, "StreamingAssets", "Language", languageFile))
	if err != nil {
		return nil, err
	}

	scDefs, err := LoadStartConditionDefs(basePath)
	if err != nil {
		return nil, err
	}

	worldsPath := filepath.Join(basePath, "StreamingAssets", "Worlds")
	entries, err := os.ReadDir(worldsPath)
	if err != nil {
		return nil, err
	}

	var worlds []RSWorldDefinition
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

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
					} `xml:"World"`
				} `xml:"WorldSettings"`
			}

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

			worlds = append(worlds, world)
		}
	}
	return worlds, nil
}

// Scan languages
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
