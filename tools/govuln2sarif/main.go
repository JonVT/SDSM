package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Structures approximate govulncheck -json output. We keep them loose so the tool
// continues working if upstream adds fields.
type GovulncheckReport struct {
	Vulns []Govuln `json:"vulnerabilities"` // newer schema (if present)
	// Alternate field names observed in some versions
	Vulnerabilities []Govuln `json:"vulnerability"`
	// Fallback: some versions output an array of events instead of a single object
}

type Govuln struct {
	ID         string      `json:"id"`
	Summary    string      `json:"summary"`
	Details    string      `json:"details"`
	Aliases    []string    `json:"aliases"`
	Modified   string      `json:"modified"`
	Published  string      `json:"published"`
	Withdrawn  string      `json:"withdrawn"`
	Affected   []Affected  `json:"affected"`
	References []Reference `json:"references"`
}

type Affected struct {
	Package  Package    `json:"package"`
	Severity []Severity `json:"severity"`
	// Ranges omitted
}

type Package struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
	PURL      string `json:"purl"`
}

type Severity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

type Reference struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// SARIF minimal structures
type Sarif struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []SarifRun `json:"runs"`
}

type SarifRun struct {
	Tool    SarifTool     `json:"tool"`
	Results []SarifResult `json:"results"`
}

type SarifTool struct {
	Driver SarifDriver `json:"driver"`
}

type SarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Rules          []SarifRule `json:"rules"`
}

type SarifRule struct {
	ID         string            `json:"id"`
	ShortDesc  SarifMultiformat  `json:"shortDescription"`
	FullDesc   SarifMultiformat  `json:"fullDescription"`
	HelpURI    string            `json:"helpUri,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type SarifResult struct {
	RuleID     string            `json:"ruleId"`
	Level      string            `json:"level"`
	Message    SarifMultiformat  `json:"message"`
	Locations  []SarifLocation   `json:"locations,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type SarifMultiformat struct {
	Text string `json:"text"`
}

type SarifLocation struct {
	PhysicalLocation SarifPhysicalLocation `json:"physicalLocation"`
}

type SarifPhysicalLocation struct {
	ArtifactLocation SarifArtifactLocation `json:"artifactLocation"`
}

type SarifArtifactLocation struct {
	URI string `json:"uri"`
}

func main() {
	inPath := flag.String("in", "govulncheck.json", "Input govulncheck JSON file")
	outPath := flag.String("out", "govulncheck.sarif", "Output SARIF file")
	flag.Parse()

	input, err := os.ReadFile(*inPath)
	if err != nil {
		fatal(fmt.Errorf("read input: %w", err))
	}

	// Attempt to unmarshal a single object; if that fails try line-oriented events.
	var report GovulncheckReport
	if err := json.Unmarshal(input, &report); err != nil {
		// Try NDJSON/array fallback
		var arr []Govuln
		dec := json.NewDecoder(strings.NewReader(string(input)))
		for {
			var v Govuln
			if derr := dec.Decode(&v); derr != nil {
				if errors.Is(derr, io.EOF) {
					break
				}
				fatal(fmt.Errorf("decode stream: %w", derr))
			}
			if strings.TrimSpace(v.ID) != "" {
				arr = append(arr, v)
			}
		}
		if len(arr) > 0 {
			report.Vulns = arr
		}
	}

	vulns := report.Vulns
	if len(vulns) == 0 && len(report.Vulnerabilities) > 0 {
		vulns = report.Vulnerabilities
	}
	if len(vulns) == 0 {
		// Produce empty SARIF with explicit empty arrays for rules/results (not null)
		writeSarif(*outPath, Sarif{
			Version: "2.1.0",
			Schema:  sarifSchema(),
			Runs: []SarifRun{{
				Tool: SarifTool{Driver: SarifDriver{
					Name:           "govulncheck",
					InformationURI: toolURI(),
					Rules:          []SarifRule{},
				}},
				Results: []SarifResult{},
			}},
		})
		return
	}

	rules := make([]SarifRule, 0, len(vulns))
	results := make([]SarifResult, 0, len(vulns))
	seen := map[string]struct{}{}

	for _, v := range vulns {
		ruleID := strings.TrimSpace(v.ID)
		if ruleID == "" {
			// Fallback to first alias or summary hash
			if len(v.Aliases) > 0 {
				ruleID = v.Aliases[0]
			} else {
				ruleID = fmt.Sprintf("VULN-%x", hashString(v.Summary))
			}
		}
		if _, ok := seen[ruleID]; !ok {
			rules = append(rules, SarifRule{
				ID:        ruleID,
				ShortDesc: SarifMultiformat{Text: truncate(v.Summary, 140)},
				FullDesc:  SarifMultiformat{Text: truncate(cleanMultiline(v.Details), 4000)},
				HelpURI:   firstReference(v.References),
				Properties: map[string]string{
					"aliases": strings.Join(v.Aliases, ","),
				},
			})
			seen[ruleID] = struct{}{}
		}
		level := severityToLevel(v)
		pkgURI := affectedPackageURI(v)
		results = append(results, SarifResult{
			RuleID:    ruleID,
			Level:     level,
			Message:   SarifMultiformat{Text: composeMessage(v)},
			Locations: locationIf(pkgURI),
			Properties: map[string]string{
				"published": v.Published,
				"modified":  v.Modified,
			},
		})
	}

	sarif := Sarif{
		Version: "2.1.0",
		Schema:  sarifSchema(),
		Runs: []SarifRun{{
			Tool: SarifTool{Driver: SarifDriver{
				Name:           "govulncheck",
				InformationURI: toolURI(),
				Rules:          rules,
			}},
			Results: results,
		}},
	}
	writeSarif(*outPath, sarif)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "govuln2sarif error: %v\n", err)
	os.Exit(1)
}

func writeSarif(path string, s Sarif) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		fatal(fmt.Errorf("marshal sarif: %w", err))
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fatal(fmt.Errorf("write sarif: %w", err))
	}
}

func sarifSchema() string {
	return "https://schemastore.azurewebsites.net/schemas/json/sarif-2.1.0.json"
}
func toolURI() string { return "https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck" }

func hashString(s string) uint64 {
	// Simple FNV-1a 64
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func cleanMultiline(s string) string {
	// Collapse CRLF, trim excessive whitespace
	out := strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\t", " ")
	lines := strings.Split(out, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimSpace(ln)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func firstReference(refs []Reference) string {
	for _, r := range refs {
		if strings.TrimSpace(r.URL) != "" {
			return r.URL
		}
	}
	return ""
}

func severityToLevel(v Govuln) string {
	// Default to warning unless explicit high severity observed.
	for _, a := range v.Affected {
		for _, sev := range a.Severity {
			s := strings.ToLower(strings.TrimSpace(sev.Type + " " + sev.Score))
			if strings.Contains(s, "high") || strings.Contains(s, "critical") {
				return "error"
			}
		}
	}
	return "warning"
}

func affectedPackageURI(v Govuln) string {
	for _, a := range v.Affected {
		if strings.TrimSpace(a.Package.Name) != "" {
			return a.Package.Name
		}
	}
	return ""
}

func locationIf(uri string) []SarifLocation {
	if uri == "" {
		return nil
	}
	return []SarifLocation{{PhysicalLocation: SarifPhysicalLocation{ArtifactLocation: SarifArtifactLocation{URI: uri}}}}
}

func composeMessage(v Govuln) string {
	pkg := affectedPackageURI(v)
	parts := []string{strings.TrimSpace(v.Summary)}
	if pkg != "" {
		parts = append(parts, fmt.Sprintf("package: %s", pkg))
	}
	if v.Published != "" {
		parts = append(parts, "published: "+v.Published)
	}
	if v.Modified != "" {
		parts = append(parts, "modified: "+v.Modified)
	}
	if v.Withdrawn != "" {
		parts = append(parts, "withdrawn: "+v.Withdrawn)
	}
	return strings.Join(parts, " | ")
}

// _ build tag marker to aid staticcheck/unused warnings if executed on older outputs
var _ = time.Now
