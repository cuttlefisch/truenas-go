package truenas

import "testing"

// Each predicate is tested at its boundary — the release before, the release
// that introduced it, and one after — rather than at a single convenient
// version. A predicate that is only ever exercised on one side of its boundary
// is not tested, it is asserted.
func TestCapabilityPredicates(t *testing.T) {
	ver := func(major, minor int) Version {
		return Version{Major: major, Minor: minor, Raw: "test"}
	}

	tests := []struct {
		name string
		fn   func(Version) bool
		// version → expected
		cases map[string]bool
		vers  map[string]Version
	}{
		{
			name: "SupportsJSONRPCAPI",
			fn:   Version.SupportsJSONRPCAPI,
			vers: map[string]Version{"24.10": ver(24, 10), "25.04": ver(25, 4), "25.10": ver(25, 10), "26.0": ver(26, 0)},
			cases: map[string]bool{
				"24.10": false, // legacy DDP /websocket only
				"25.04": true,
				"25.10": true,
				"26.0":  true,
			},
		},
		{
			name: "SupportsCoreSubscribe",
			fn:   Version.SupportsCoreSubscribe,
			vers: map[string]Version{"24.10": ver(24, 10), "25.04": ver(25, 4), "26.0": ver(26, 0)},
			cases: map[string]bool{
				"24.10": false, // falls back to polling core.get_jobs
				"25.04": true,
				"26.0":  true,
			},
		},
		{
			name: "SupportsCloudSyncProviderObject",
			fn:   Version.SupportsCloudSyncProviderObject,
			vers: map[string]Version{"24.10": ver(24, 10), "25.04": ver(25, 4)},
			cases: map[string]bool{
				"24.10": false, // provider is a bare string + separate attributes
				"25.04": true,
			},
		},
		{
			name: "UsesPoolSnapshotNamespace",
			fn:   Version.UsesPoolSnapshotNamespace,
			vers: map[string]Version{"25.04": ver(25, 4), "25.09": ver(25, 9), "25.10": ver(25, 10), "26.0": ver(26, 0)},
			cases: map[string]bool{
				// The boundary that matters: 25.09 is still zfs.snapshot, and
				// 25.10 removed it outright rather than aliasing it.
				"25.04": false,
				"25.09": false,
				"25.10": true,
				"26.0":  true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for label, want := range tt.cases {
				if got := tt.fn(tt.vers[label]); got != want {
					t.Errorf("%s on %s = %v, want %v", tt.name, label, got, want)
				}
			}
		})
	}
}

// A zero Version means detection has not happened yet or failed. Every
// predicate must report false for it: assuming a capability we have not
// confirmed is how a client sends a payload the appliance rejects opaquely.
func TestCapabilityPredicates_ZeroVersionIsNeverCapable(t *testing.T) {
	var zero Version
	if !zero.IsZero() {
		t.Fatal("expected the zero Version to report IsZero")
	}
	checks := map[string]bool{
		"SupportsJSONRPCAPI":              zero.SupportsJSONRPCAPI(),
		"SupportsCoreSubscribe":           zero.SupportsCoreSubscribe(),
		"SupportsCloudSyncProviderObject": zero.SupportsCloudSyncProviderObject(),
		"UsesPoolSnapshotNamespace":       zero.UsesPoolSnapshotNamespace(),
	}
	for name, got := range checks {
		if got {
			t.Errorf("%s reported true for an undetected version", name)
		}
	}
}
