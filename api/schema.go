// Package api provides embedded TrueNAS API method definitions keyed by version.
package api

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed */methods.json
var methodsFS embed.FS

// MethodDef describes a single TrueNAS API method.
type MethodDef struct {
	Description  *string `json:"description"`
	Job          bool    `json:"job"`
	Filterable   bool    `json:"filterable"`
	Downloadable bool    `json:"downloadable"`
	Uploadable   bool    `json:"uploadable"`
	ItemMethod   bool    `json:"item_method"`
	RequireWS    bool    `json:"require_websocket"`
}

// MinSupportedVersion is the oldest TrueNAS release this library targets. It is
// the provider-wide floor: individual resources may require more (see ADR-009,
// which requires 25.10 for SMB shares), but the library as a whole must connect
// to and operate against an appliance at this version.
const MinSupportedVersion = "25.04"

// Provenance records where an embedded schema came from, and — critically —
// whether it has been verified against a real appliance of that version.
//
// This exists because an unverified schema is indistinguishable from a verified
// one once it is sitting in the repo as JSON. Making the distinction a value in
// code means a test can assert it, and a future re-fetch has an obvious place to
// record that the work was done. A comment in a 4.8 MB JSON file would not
// survive contact with anyone.
type Provenance struct {
	// Version is the embedded schema directory name, e.g. "25.10".
	Version string
	// Source describes where the data came from, in enough detail to reproduce.
	Source string
	// Verified is true only when the schema was fetched from an appliance
	// running exactly this version, by us, over /api/current.
	Verified bool
	// Note carries any caveat a consumer needs before trusting the contents.
	Note string
}

// provenance is the authoritative record for every embedded schema. Adding a
// schema directory without adding an entry here is caught by a test.
var provenance = map[string]Provenance{
	"25.04": {
		Version:  "25.04",
		Source:   "inherited from deevus/truenas-go; present since before the fork",
		Verified: true,
		Note:     "",
	},
	"25.10": {
		Version: "25.10",
		Source:  "converted from a vendored core.get_methods dump taken at 25.10.1",
		// INTERIM. Deliberately false.
		Verified: false,
		Note: "INTERIM AND UNVERIFIED. This is 25.10.1, not the 25.10.5 target, " +
			"and it was captured from the legacy /websocket DDP endpoint rather " +
			"than /api/current. It is embedded so the rest of the programme is " +
			"not blocked on hardware. Re-fetch from a real 25.10.5 appliance and " +
			"diff before relying on any method's presence, absence, or shape.",
	},
}

// ProvenanceFor returns the provenance record for an embedded schema version.
func ProvenanceFor(version string) (Provenance, bool) {
	p, ok := provenance[version]
	return p, ok
}

// parseVersionDir splits an embedded schema directory name like "25.04" into
// its numeric parts. Deliberately not reusing the root package's ParseVersion:
// api is a leaf package with no internal dependencies, which is what lets it be
// embedded and tested in isolation, and inverting that to import the root
// package would be a much larger change than this needs.
func parseVersionDir(dir string) (major, minor int, ok bool) {
	parts := strings.SplitN(dir, ".", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	min, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return maj, min, true
}

// ResolveVersion returns the embedded schema version that best describes an
// appliance at major.minor: the highest embedded version less than or equal to
// it. Returns "" when the appliance predates every embedded schema.
//
// Highest-not-exceeding rather than nearest-match is the point. An appliance on
// 25.10.5 must resolve to the 25.10 schema, never to 25.04, because a method
// present in 25.04 and removed in 25.10 would otherwise read as available.
// Resolving a 26.x appliance to 25.10 is the deliberate, and only safe,
// fallback: it under-reports new methods rather than over-reporting removed
// ones.
func ResolveVersion(major, minor int) string {
	best := ""
	bestMaj, bestMin := -1, -1
	for _, v := range Versions() {
		maj, min, ok := parseVersionDir(v)
		if !ok {
			continue
		}
		if maj > major || (maj == major && min > minor) {
			continue // newer than the appliance
		}
		if maj > bestMaj || (maj == bestMaj && min > bestMin) {
			best, bestMaj, bestMin = v, maj, min
		}
	}
	return best
}

// MethodsForVersion returns the methods for the schema that best describes an
// appliance at major.minor, along with the schema version actually used — which
// callers need in order to report which schema answered a capability question.
func MethodsForVersion(major, minor int) (map[string]MethodDef, string, error) {
	v := ResolveVersion(major, minor)
	if v == "" {
		return nil, "", fmt.Errorf(
			"no embedded API schema for TrueNAS %d.%02d; the oldest supported is %s",
			major, minor, MinSupportedVersion)
	}
	m, err := Methods(v)
	if err != nil {
		return nil, v, err
	}
	return m, v, nil
}

// Methods returns all API methods for a given TrueNAS version (e.g. "25.04").
func Methods(version string) (map[string]MethodDef, error) {
	data, err := methodsFS.ReadFile(version + "/methods.json")
	if err != nil {
		return nil, fmt.Errorf("no methods for version %s: %w", version, err)
	}
	var methods map[string]MethodDef
	if err := json.Unmarshal(data, &methods); err != nil {
		return nil, fmt.Errorf("parsing methods for %s: %w", version, err)
	}
	return methods, nil
}

// LatestVersion returns the highest embedded version string.
func LatestVersion() string {
	vs := Versions()
	if len(vs) == 0 {
		return ""
	}
	return vs[len(vs)-1]
}

// Versions returns all embedded TrueNAS versions, sorted.
func Versions() []string {
	entries, err := methodsFS.ReadDir(".")
	if err != nil {
		return nil
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	sort.Strings(versions)
	return versions
}

// Namespace extracts the service namespace from an API method name.
// e.g. "app.registry.create" → "app.registry", "system.info" → "system"
func Namespace(method string) string {
	i := strings.LastIndex(method, ".")
	if i < 0 {
		return method
	}
	return method[:i]
}

// JobFlagChange records a method whose job flag differs between two schemas.
// It is called out separately from added/removed because it is the change most
// likely to break a caller silently: the method still exists and still accepts
// the same parameters, but a client that treats it as synchronous will read a
// job ID as if it were a result.
type JobFlagChange struct {
	Method string
	WasJob bool
	NowJob bool
}

// SchemaDiff is the method-level delta between two embedded schema versions.
type SchemaDiff struct {
	From, To       string
	Added          []string
	Removed        []string
	JobFlagChanged []JobFlagChange
}

// DiffSchemas reports what changed between two embedded schema versions.
//
// This lives here rather than in cmd/featurematrix because it is a fact about
// the schemas, not about the matrix tool — which means it can be asserted in a
// unit test with no CLI involved. That matters: the point of the diff is to
// pin the delta with exact numbers, and a test that shelled out to a command
// and parsed its prose would be asserting the renderer, not the data.
//
// Slices are sorted, so output is deterministic and diffable.
func DiffSchemas(from, to string) (SchemaDiff, error) {
	fromMethods, err := Methods(from)
	if err != nil {
		return SchemaDiff{}, err
	}
	toMethods, err := Methods(to)
	if err != nil {
		return SchemaDiff{}, err
	}

	d := SchemaDiff{From: from, To: to}
	for name := range toMethods {
		if _, ok := fromMethods[name]; !ok {
			d.Added = append(d.Added, name)
		}
	}
	for name, was := range fromMethods {
		now, ok := toMethods[name]
		if !ok {
			d.Removed = append(d.Removed, name)
			continue
		}
		if was.Job != now.Job {
			d.JobFlagChanged = append(d.JobFlagChanged, JobFlagChange{
				Method: name, WasJob: was.Job, NowJob: now.Job,
			})
		}
	}

	sort.Strings(d.Added)
	sort.Strings(d.Removed)
	sort.Slice(d.JobFlagChanged, func(i, j int) bool {
		return d.JobFlagChanged[i].Method < d.JobFlagChanged[j].Method
	})
	return d, nil
}
