package truenas

import (
	"encoding/json"
	"testing"

	"github.com/deevus/truenas-go/api"
)

// --- create params -----------------------------------------------------------

func TestDatasetCreateParams_SMBShape(t *testing.T) {
	params := datasetCreateParams(CreateDatasetOpts{
		Name:            "tank/smb",
		ACLType:         "NFSV4",
		ACLMode:         "RESTRICTED",
		CaseSensitivity: "INSENSITIVE",
		ShareType:       "SMB",
	})

	for key, want := range map[string]string{
		"acltype":         "NFSV4",
		"aclmode":         "RESTRICTED",
		"casesensitivity": "INSENSITIVE",
		"share_type":      "SMB",
	} {
		got, ok := params[key]
		if !ok {
			t.Errorf("%s missing from create params", key)
			continue
		}
		if got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}

// An unset field must be absent rather than sent as "". The API treats an
// empty string as an invalid enum value, so sending one turns "leave it to the
// server default" into a create failure.
func TestDatasetCreateParams_OmitsUnsetSMBFields(t *testing.T) {
	params := datasetCreateParams(CreateDatasetOpts{Name: "tank/plain"})

	for _, key := range []string{"acltype", "aclmode", "casesensitivity", "share_type"} {
		if v, ok := params[key]; ok {
			t.Errorf("%s present as %q for an unset field; it must be omitted", key, v)
		}
	}
}

// --- update params -----------------------------------------------------------

func TestDatasetUpdateParams_SendsMutableACLFields(t *testing.T) {
	params := datasetUpdateParams(UpdateDatasetOpts{
		ACLType: "POSIX",
		ACLMode: "PASSTHROUGH",
	})

	if params["acltype"] != "POSIX" {
		t.Errorf("acltype = %v, want POSIX", params["acltype"])
	}
	if params["aclmode"] != "PASSTHROUGH" {
		t.Errorf("aclmode = %v, want PASSTHROUGH", params["aclmode"])
	}
}

// UpdateDatasetOpts has no CaseSensitivity or ShareType field at all, so the
// update path cannot send them even by mistake. This asserts the resulting
// behaviour rather than the absence of a field, because the absence is what a
// future edit would quietly undo.
func TestDatasetUpdateParams_NeverSendsImmutableFields(t *testing.T) {
	params := datasetUpdateParams(UpdateDatasetOpts{
		Compression: "lz4",
		ACLType:     "NFSV4",
	})

	for _, key := range []string{"casesensitivity", "share_type"} {
		if _, ok := params[key]; ok {
			t.Errorf("%s reached update params; pool.dataset.update rejects it", key)
		}
	}
}

// --- response mapping --------------------------------------------------------

func TestDatasetFromResponse_ReadsSMBShape(t *testing.T) {
	raw := `{
		"id": "tank/smb", "name": "tank/smb", "pool": "tank", "type": "FILESYSTEM",
		"mountpoint": "/mnt/tank/smb",
		"acltype": {"value": "NFSV4"},
		"aclmode": {"value": "RESTRICTED"},
		"casesensitivity": {"value": "INSENSITIVE"}
	}`
	var resp DatasetResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	ds := datasetFromResponse(resp)
	if ds.ACLType != "NFSV4" {
		t.Errorf("ACLType = %q, want NFSV4", ds.ACLType)
	}
	if ds.ACLMode != "RESTRICTED" {
		t.Errorf("ACLMode = %q, want RESTRICTED", ds.ACLMode)
	}
	if ds.CaseSensitivity != "INSENSITIVE" {
		t.Errorf("CaseSensitivity = %q, want INSENSITIVE", ds.CaseSensitivity)
	}
}

// A dataset created before these fields existed, or a server that omits them,
// must map to empty strings rather than panicking.
func TestDatasetFromResponse_AbsentSMBShape(t *testing.T) {
	var resp DatasetResponse
	if err := json.Unmarshal([]byte(`{"id":"tank/x","name":"tank/x"}`), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ds := datasetFromResponse(resp)
	if ds.ACLType != "" || ds.ACLMode != "" || ds.CaseSensitivity != "" {
		t.Errorf("expected empty SMB shape, got %q/%q/%q",
			ds.ACLType, ds.ACLMode, ds.CaseSensitivity)
	}
}

// --- the schema facts this design rests on -----------------------------------

// objectProps returns the property names of a JSON-schema node.
//
// 25.10 restructured pool.dataset.create: where 25.04 had one flat object
// carrying both filesystem and volume fields, 25.10 has an anyOf of
// PoolDatasetCreateFilesystem and PoolDatasetCreateVolume, discriminated by
// the "type" enum. The zvol-only fields did not disappear, they moved into the
// volume branch — and casesensitivity is filesystem-only there.
//
// wantType selects a branch by its discriminator so the assertions describe
// the variant the client actually sends. Merging the branches instead would
// report a union that no single call accepts.
func objectProps(node any, wantType string) map[string]bool {
	obj, ok := node.(map[string]any)
	if !ok {
		return nil
	}

	if branches, ok := obj["anyOf"].([]any); ok {
		for _, b := range branches {
			if branchIsType(b, wantType) {
				return objectProps(b, wantType)
			}
		}
		return nil
	}

	props, ok := obj["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return nil
	}
	out := map[string]bool{}
	for k := range props {
		out[k] = true
	}
	return out
}

// branchIsType reports whether an anyOf branch is the variant for wantType,
// read from its "type" property enum.
func branchIsType(branch any, wantType string) bool {
	obj, ok := branch.(map[string]any)
	if !ok {
		return false
	}
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		return false
	}
	typeProp, ok := props["type"].(map[string]any)
	if !ok {
		return false
	}
	values, ok := typeProp["enum"].([]any)
	if !ok {
		return false
	}
	for _, v := range values {
		if v == wantType {
			return true
		}
	}
	return false
}

// datasetMethodSchema loads one method's raw schema entry for a version.
func datasetMethodSchema(t *testing.T, version, method string) map[string]any {
	t.Helper()

	methods, err := api.RawMethods(version)
	if err != nil {
		t.Fatalf("RawMethods(%s): %v", version, err)
	}
	raw, ok := methods[method]
	if !ok {
		t.Fatalf("%s not found in %s schema", method, version)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", method, err)
	}
	return m
}

// datasetAcceptedProps returns the fields a method accepts. The parameter
// object is the last entry of "accepts" that carries properties: create takes
// only the data object, while update and query take an id or filter first.
func datasetAcceptedProps(t *testing.T, version, method string) map[string]bool {
	t.Helper()

	accepts, ok := datasetMethodSchema(t, version, method)["accepts"].([]any)
	if !ok {
		t.Fatalf("%s (%s) has no accepts list", method, version)
	}
	var props map[string]bool
	for _, a := range accepts {
		if p := objectProps(a, "FILESYSTEM"); p != nil {
			props = p
		}
	}
	if props == nil {
		t.Fatalf("no parameter object found for %s (%s)", method, version)
	}
	return props
}

// datasetReturnedProps returns the fields a method's result carries. The
// dataset object comes back from create and update; query's own return is an
// anyOf wrapper that does not restate it.
func datasetReturnedProps(t *testing.T, version, method string) map[string]bool {
	t.Helper()

	returns, ok := datasetMethodSchema(t, version, method)["returns"].([]any)
	if !ok || len(returns) == 0 {
		t.Fatalf("%s (%s) has no returns list", method, version)
	}
	props := objectProps(returns[0], "FILESYSTEM")
	if props == nil {
		t.Fatalf("no result object found for %s (%s)", method, version)
	}
	return props
}

// The navigation above is only trustworthy if it lands on the object we think
// it does, so pin the sizes. These are the counts read directly from the
// embedded schemas; a mismatch means the helper is reading the wrong node, not
// that the assertion needs relaxing.
func TestDatasetSchemaNavigation_LandsOnTheRightObjects(t *testing.T) {
	for _, tc := range []struct {
		version       string
		createAccepts int
		updateAccepts int
		createReturns int
	}{
		{"25.04", 37, 29, 52},
		{"25.10", 33, 29, 52},
	} {
		t.Run(tc.version, func(t *testing.T) {
			if n := len(datasetAcceptedProps(t, tc.version, "pool.dataset.create")); n != tc.createAccepts {
				t.Errorf("create accepts %d properties, want %d", n, tc.createAccepts)
			}
			if n := len(datasetAcceptedProps(t, tc.version, "pool.dataset.update")); n != tc.updateAccepts {
				t.Errorf("update accepts %d properties, want %d", n, tc.updateAccepts)
			}
			if n := len(datasetReturnedProps(t, tc.version, "pool.dataset.create")); n != tc.createReturns {
				t.Errorf("create returns %d properties, want %d", n, tc.createReturns)
			}
		})
	}
}

// casesensitivity and share_type are create-only. If a future TrueNAS makes
// either updatable, this fails and the RequiresReplace plan modifier in the
// provider should be reconsidered rather than left in place out of habit.
func TestSchema_CaseSensitivityAndShareTypeAreCreateOnly(t *testing.T) {
	for _, version := range []string{"25.04", "25.10"} {
		t.Run(version, func(t *testing.T) {
			create := datasetAcceptedProps(t, version, "pool.dataset.create")
			update := datasetAcceptedProps(t, version, "pool.dataset.update")

			for _, field := range []string{"casesensitivity", "share_type"} {
				if !create[field] {
					t.Errorf("%s missing from pool.dataset.create", field)
				}
				if update[field] {
					t.Errorf("%s accepted by pool.dataset.update; it was create-only when "+
						"RequiresReplace was chosen for it", field)
				}
			}

			// The counterpart: these two must stay updatable, or forcing
			// replacement on them would be correct after all.
			for _, field := range []string{"acltype", "aclmode"} {
				if !update[field] {
					t.Errorf("%s no longer accepted by pool.dataset.update", field)
				}
			}
		})
	}
}

// share_type is write-only: accepted at create, never returned by query. This
// is why Dataset has no ShareType field and why the provider must not mark the
// attribute Computed — there is nothing to refresh it from.
func TestSchema_ShareTypeIsWriteOnly(t *testing.T) {
	for _, version := range []string{"25.04", "25.10"} {
		t.Run(version, func(t *testing.T) {
			create := datasetAcceptedProps(t, version, "pool.dataset.create")
			query := datasetReturnedProps(t, version, "pool.dataset.create")

			if !create["share_type"] {
				t.Error("share_type missing from pool.dataset.create")
			}
			if query["share_type"] {
				t.Error("the dataset object now carries share_type; it can become a " +
					"readable, Computed attribute instead of a write-only one")
			}

			// These three are readable, which is what lets them round-trip.
			for _, field := range []string{"acltype", "aclmode", "casesensitivity"} {
				if !query[field] {
					t.Errorf("the dataset object no longer carries %s", field)
				}
			}
		})
	}
}
