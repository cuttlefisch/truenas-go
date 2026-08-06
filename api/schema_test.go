package api

import (
	"sort"
	"testing"
)

func TestVersions(t *testing.T) {
	vs := Versions()
	if len(vs) == 0 {
		t.Fatal("expected at least one embedded version")
	}
	if vs[0] != "25.04" {
		t.Errorf("expected first version 25.04, got %s", vs[0])
	}
}

func TestLatestVersion(t *testing.T) {
	v := LatestVersion()
	if v == "" {
		t.Fatal("expected non-empty latest version")
	}
	// Updated from 25.04 when the 25.10 schema was embedded. LatestVersion is
	// the FEATURES.md denominator, so this moving is exactly what regenerates
	// that file for every service.
	if v != "25.10" {
		t.Errorf("expected latest version 25.10, got %s", v)
	}
}

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name         string
		major, minor int
		want         string
	}{
		{"exact 25.04", 25, 4, "25.04"},
		{"exact 25.10", 25, 10, "25.10"},
		// The case that matters: a patch release resolves to its own minor,
		// never down to an older one that may still list removed methods.
		{"25.10.5 appliance", 25, 10, "25.10"},
		// Between embedded schemas: falls back to the highest not exceeding.
		{"25.05 between", 25, 5, "25.04"},
		{"25.09 between", 25, 9, "25.04"},
		// Newer than anything embedded: deliberately clamps to the newest,
		// under-reporting new methods rather than over-reporting removed ones.
		{"26.0 future", 26, 0, "25.10"},
		{"27.0 far future", 27, 0, "25.10"},
		// Older than everything embedded: no answer, rather than a wrong one.
		{"24.10 too old", 24, 10, ""},
		{"22.02 far too old", 22, 2, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveVersion(tt.major, tt.minor); got != tt.want {
				t.Errorf("ResolveVersion(%d, %d) = %q, want %q", tt.major, tt.minor, got, tt.want)
			}
		})
	}
}

func TestMethodsForVersion(t *testing.T) {
	m, v, err := MethodsForVersion(25, 10)
	if err != nil {
		t.Fatalf("MethodsForVersion(25, 10) error: %v", err)
	}
	if v != "25.10" {
		t.Errorf("expected schema 25.10, got %s", v)
	}
	// share_precheck is 25.10-only; its presence proves the right schema
	// answered rather than a fallback to 25.04.
	if _, ok := m["sharing.smb.share_precheck"]; !ok {
		t.Error("expected sharing.smb.share_precheck in the 25.10 schema")
	}

	m04, v04, err := MethodsForVersion(25, 4)
	if err != nil {
		t.Fatalf("MethodsForVersion(25, 4) error: %v", err)
	}
	if v04 != "25.04" {
		t.Errorf("expected schema 25.04, got %s", v04)
	}
	if _, ok := m04["sharing.smb.share_precheck"]; ok {
		t.Error("sharing.smb.share_precheck must NOT be in the 25.04 schema")
	}
}

func TestMethodsForVersion_TooOld(t *testing.T) {
	_, _, err := MethodsForVersion(24, 10)
	if err == nil {
		t.Fatal("expected an error for an appliance older than every embedded schema")
	}
}

// Every embedded schema must declare its provenance. Without this, adding a
// directory silently produces a schema nobody can tell is unverified.
func TestProvenanceCoversEveryEmbeddedVersion(t *testing.T) {
	for _, v := range Versions() {
		p, ok := ProvenanceFor(v)
		if !ok {
			t.Errorf("embedded schema %s has no provenance entry", v)
			continue
		}
		if p.Source == "" {
			t.Errorf("provenance for %s has an empty Source", v)
		}
		if !p.Verified && p.Note == "" {
			t.Errorf("provenance for %s is unverified but carries no explanatory Note", v)
		}
	}
}

// The 25.10 schema is knowingly interim. This test is the tripwire: when it is
// re-fetched from a real 25.10.5 appliance and Verified flips to true, this
// test fails and forces the note, the ADR-004 Evidence section, and the
// tracking issue to be closed out together rather than silently drifting.
func TestProvenance_25_10_IsStillInterim(t *testing.T) {
	p, ok := ProvenanceFor("25.10")
	if !ok {
		t.Fatal("no provenance for 25.10")
	}
	if p.Verified {
		t.Fatal("25.10 is now marked verified — update ADR-004's Evidence section, " +
			"close the re-fetch issue, and delete this test")
	}
}

func TestMethods(t *testing.T) {
	methods, err := Methods("25.04")
	if err != nil {
		t.Fatalf("Methods(25.04) error: %v", err)
	}
	if len(methods) == 0 {
		t.Fatal("expected non-empty methods map")
	}

	// Spot-check a known method
	m, ok := methods["system.info"]
	if !ok {
		t.Fatal("expected system.info to be present")
	}
	if m.Description == nil {
		t.Error("expected system.info to have a description")
	}
}

func TestMethods_InvalidVersion(t *testing.T) {
	_, err := Methods("99.99")
	if err == nil {
		t.Fatal("expected error for invalid version")
	}
}

func TestNamespace(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{"app.create", "app"},
		{"app.registry.create", "app.registry"},
		{"system.info", "system"},
		{"pool.dataset.query", "pool.dataset"},
		{"standalone", "standalone"},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got := Namespace(tt.method)
			if got != tt.want {
				t.Errorf("Namespace(%q) = %q, want %q", tt.method, got, tt.want)
			}
		})
	}
}

func TestMethods_KnownFields(t *testing.T) {
	methods, err := Methods("25.04")
	if err != nil {
		t.Fatalf("Methods(25.04) error: %v", err)
	}

	// Check a job method
	if m, ok := methods["app.create"]; ok {
		if !m.Job {
			t.Error("expected app.create to be a job method")
		}
	}

	// Check a filterable method
	if m, ok := methods["app.query"]; ok {
		if !m.Filterable {
			t.Error("expected app.query to be filterable")
		}
	}
}

// The 25.04 → 25.10 delta, pinned to exact numbers.
//
// Asserting "the diff is non-empty" would pass against any two schemas and so
// verifies nothing. These literals are the point: if either embedded schema is
// swapped — most likely when the interim 25.10 dump is replaced by a real
// fetch from a 25.10.5 appliance — this test fails and forces someone to look
// at what actually moved rather than accepting it silently.
//
// When that happens the correct fix is to update these numbers *after*
// reviewing the new delta, not before.
func TestDiffSchemas_25_04_to_25_10(t *testing.T) {
	const (
		wantAdded          = 82
		wantRemoved        = 85
		wantJobFlagChanged = 1
	)

	d, err := DiffSchemas("25.04", "25.10")
	if err != nil {
		t.Fatalf("DiffSchemas: %v", err)
	}
	if got := len(d.Added); got != wantAdded {
		t.Errorf("added = %d, want %d", got, wantAdded)
	}
	if got := len(d.Removed); got != wantRemoved {
		t.Errorf("removed = %d, want %d", got, wantRemoved)
	}
	if got := len(d.JobFlagChanged); got != wantJobFlagChanged {
		t.Errorf("job-flag changed = %d, want %d", got, wantJobFlagChanged)
	}

	// The single job-flag change, named and directional.
	//
	// update.update STOPPED being a job in 25.10 — 25.04 ran the update
	// through it, and 25.10 moved that to update.run. The direction is
	// asserted, not just the fact of a change, because the two directions
	// break a caller differently: a method that becomes a job returns a job ID
	// where a result is expected, while one that stops being a job leaves
	// CallAndWait waiting on a job that will never be reported.
	if len(d.JobFlagChanged) == 1 {
		c := d.JobFlagChanged[0]
		if c.Method != "update.update" {
			t.Errorf("job-flag change on %q, want update.update", c.Method)
		}
		if !c.WasJob || c.NowJob {
			t.Errorf("update.update: job %v → %v, want true → false", c.WasJob, c.NowJob)
		}
	}

	// Spot-check the reshape this programme actually depends on.
	assertContains(t, "removed", d.Removed, "zfs.snapshot.create")
	assertContains(t, "added", d.Added, "pool.snapshot.create")
	assertContains(t, "added", d.Added, "sharing.smb.share_precheck")
	assertContains(t, "added", d.Added, "service.control")
}

func assertContains(t *testing.T, label string, haystack []string, needle string) {
	t.Helper()
	for _, s := range haystack {
		if s == needle {
			return
		}
	}
	t.Errorf("expected %q in the %s set", needle, label)
}

// Sorted output keeps the rendered diff diffable across runs.
func TestDiffSchemas_IsSorted(t *testing.T) {
	d, err := DiffSchemas("25.04", "25.10")
	if err != nil {
		t.Fatalf("DiffSchemas: %v", err)
	}
	if !sort.StringsAreSorted(d.Added) {
		t.Error("Added is not sorted")
	}
	if !sort.StringsAreSorted(d.Removed) {
		t.Error("Removed is not sorted")
	}
}

// Diffing a schema against itself must be empty in all three dimensions — the
// cheapest possible check that the comparison is not reporting spurious drift.
func TestDiffSchemas_Identity(t *testing.T) {
	for _, v := range Versions() {
		d, err := DiffSchemas(v, v)
		if err != nil {
			t.Fatalf("DiffSchemas(%s, %s): %v", v, v, err)
		}
		if len(d.Added) != 0 || len(d.Removed) != 0 || len(d.JobFlagChanged) != 0 {
			t.Errorf("%s vs itself: added=%d removed=%d jobchanged=%d, want all zero",
				v, len(d.Added), len(d.Removed), len(d.JobFlagChanged))
		}
	}
}

func TestDiffSchemas_UnknownVersion(t *testing.T) {
	if _, err := DiffSchemas("25.04", "99.99"); err == nil {
		t.Error("expected an error diffing against an unknown version")
	}
	if _, err := DiffSchemas("99.99", "25.04"); err == nil {
		t.Error("expected an error diffing from an unknown version")
	}
}
