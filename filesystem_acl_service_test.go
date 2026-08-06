package truenas

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func strPtr(s string) *string { return &s }
func i64Ptr(i int64) *int64   { return &i }

// --- the basic/advanced union ------------------------------------------------

func TestNFS4Perms_UnmarshalBasic(t *testing.T) {
	var p NFS4Perms
	if err := json.Unmarshal([]byte(`{"BASIC":"FULL_CONTROL"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !p.IsBasic() {
		t.Error("IsBasic = false for a BASIC payload")
	}
	if p.Basic != "FULL_CONTROL" {
		t.Errorf("Basic = %q", p.Basic)
	}
	if p.Advanced != nil {
		t.Errorf("Advanced = %v, want nil for the basic form", p.Advanced)
	}
}

func TestNFS4Perms_UnmarshalAdvanced(t *testing.T) {
	var p NFS4Perms
	raw := `{"READ_DATA":true,"WRITE_DATA":false,"EXECUTE":true}`
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.IsBasic() {
		t.Error("IsBasic = true for an advanced payload")
	}
	if !p.Advanced["READ_DATA"] || p.Advanced["WRITE_DATA"] || !p.Advanced["EXECUTE"] {
		t.Errorf("Advanced = %v", p.Advanced)
	}
}

// Whichever form went in must come back out. Silently converting would change
// what gets sent to the server relative to what the operator wrote.
func TestNFS4Perms_RoundTrip(t *testing.T) {
	for _, raw := range []string{
		`{"BASIC":"MODIFY"}`,
		`{"EXECUTE":true,"READ_DATA":true}`,
	} {
		var p NFS4Perms
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		out, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var a, b any
		_ = json.Unmarshal([]byte(raw), &a)
		_ = json.Unmarshal(out, &b)
		if !jsonEqual(a, b) {
			t.Errorf("round trip changed %s into %s", raw, out)
		}
	}
}

func TestNFS4Flags_BasicAndAdvanced(t *testing.T) {
	var f NFS4Flags
	if err := json.Unmarshal([]byte(`{"BASIC":"INHERIT"}`), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !f.IsBasic() || f.Basic != "INHERIT" {
		t.Errorf("Basic = %q, IsBasic = %v", f.Basic, f.IsBasic())
	}

	var g NFS4Flags
	if err := json.Unmarshal([]byte(`{"FILE_INHERIT":true}`), &g); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if g.IsBasic() || !g.Advanced["FILE_INHERIT"] {
		t.Errorf("Advanced = %v", g.Advanced)
	}
}

// The comparison rule the whole design turns on. A basic set and an advanced
// set are never equal, even when the preset plausibly expands to those bits,
// because the expansion is defined by the middleware and is not in the schema.
// Claiming equality would let a real difference pass as a no-op.
func TestNFS4Perms_EqualNeverCrossesForms(t *testing.T) {
	basic := NFS4Perms{Basic: "FULL_CONTROL"}
	advanced := NFS4Perms{Advanced: map[string]bool{
		"READ_DATA": true, "WRITE_DATA": true, "EXECUTE": true,
	}}

	if basic.Equal(advanced) || advanced.Equal(basic) {
		t.Error("a basic set compared equal to an advanced one")
	}
	if !basic.Equal(NFS4Perms{Basic: "FULL_CONTROL"}) {
		t.Error("identical basic sets compared unequal")
	}
	if !advanced.Equal(NFS4Perms{Advanced: map[string]bool{
		"READ_DATA": true, "WRITE_DATA": true, "EXECUTE": true,
	}}) {
		t.Error("identical advanced sets compared unequal")
	}
}

// Every advanced bit defaults to false, so an absent key and an explicit false
// mean the same thing. Treating them as different would produce a diff every
// time the server omitted a bit the config spelled out.
func TestNFS4Perms_EqualTreatsAbsentAsFalse(t *testing.T) {
	sparse := NFS4Perms{Advanced: map[string]bool{"READ_DATA": true}}
	explicit := NFS4Perms{Advanced: map[string]bool{
		"READ_DATA": true, "WRITE_DATA": false, "EXECUTE": false,
	}}
	if !sparse.Equal(explicit) {
		t.Error("an omitted bit compared unequal to an explicit false")
	}

	differing := NFS4Perms{Advanced: map[string]bool{
		"READ_DATA": true, "WRITE_DATA": true,
	}}
	if sparse.Equal(differing) {
		t.Error("a genuinely different bit compared equal")
	}
}

// Map iteration order is random, so anything rendering an advanced set has to
// sort. An unsorted rendering produces spurious diffs at random.
func TestNFS4Perms_SetNamesIsSorted(t *testing.T) {
	p := NFS4Perms{Advanced: map[string]bool{
		"WRITE_DATA": true, "READ_DATA": true, "EXECUTE": true, "DELETE": false,
	}}
	got := p.SetNames()
	want := []string{"EXECUTE", "READ_DATA", "WRITE_DATA"}

	if len(got) != len(want) {
		t.Fatalf("SetNames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SetNames = %v, want %v", got, want)
		}
	}

	// Repeated calls must agree, which is the property that actually matters.
	for i := 0; i < 20; i++ {
		again := p.SetNames()
		for j := range got {
			if again[j] != got[j] {
				t.Fatalf("SetNames unstable across calls: %v then %v", got, again)
			}
		}
	}
}

// --- GetACL ------------------------------------------------------------------

func aclService(fn func(method string, params any) (json.RawMessage, error)) *FilesystemService {
	return NewFilesystemService(&mockFileCaller{
		mockAsyncCaller: mockAsyncCaller{
			mockCaller: mockCaller{
				callFunc: func(_ context.Context, method string, params any) (json.RawMessage, error) {
					return fn(method, params)
				},
			},
			callAndWaitFunc: func(_ context.Context, method string, params any) (json.RawMessage, error) {
				return fn(method, params)
			},
		},
	}, Version{Major: 25, Minor: 10})
}

func TestFilesystemService_GetACL_NFS4(t *testing.T) {
	var gotParams any
	s := aclService(func(method string, params any) (json.RawMessage, error) {
		if method != "filesystem.getacl" {
			t.Errorf("method = %q", method)
		}
		gotParams = params
		return json.RawMessage(`{
			"path": "/mnt/tank/share", "acltype": "NFS4", "trivial": false,
			"user": "root", "group": "wheel", "uid": 0, "gid": 0,
			"aclflags": {"protected": true},
			"acl": [
				{"tag":"owner@","type":"ALLOW","perms":{"BASIC":"FULL_CONTROL"},"flags":{"BASIC":"INHERIT"}},
				{"tag":"USER","type":"ALLOW","id":3001,"who":"alice",
				 "perms":{"READ_DATA":true,"EXECUTE":true},"flags":{"FILE_INHERIT":true}}
			]
		}`), nil
	})

	acl, err := s.GetACL(context.Background(), GetACLOpts{
		Path: "/mnt/tank/share", ResolveIDs: true,
	})
	if err != nil {
		t.Fatalf("GetACL: %v", err)
	}

	// Positional params: path, simplified, resolve_ids.
	list, ok := gotParams.([]any)
	if !ok || len(list) != 3 {
		t.Fatalf("params = %#v, want a 3-element slice", gotParams)
	}
	if list[0] != "/mnt/tank/share" || list[1] != false || list[2] != true {
		t.Errorf("params = %v, want [path false true]", list)
	}

	if acl.ACLType != ACLTypeNFS4 {
		t.Errorf("ACLType = %q", acl.ACLType)
	}
	if acl.Trivial {
		t.Error("Trivial = true for an ACL with a named-user ACE")
	}
	if len(acl.NFS4) != 2 {
		t.Fatalf("got %d ACEs, want 2", len(acl.NFS4))
	}
	if len(acl.POSIX) != 0 {
		t.Errorf("POSIX populated for an NFS4 ACL: %v", acl.POSIX)
	}
	if !acl.NFS4[0].Perms.IsBasic() || acl.NFS4[0].Perms.Basic != "FULL_CONTROL" {
		t.Errorf("ACE 0 perms = %+v", acl.NFS4[0].Perms)
	}
	if acl.NFS4[1].Perms.IsBasic() {
		t.Error("ACE 1 perms read as basic; the payload was advanced")
	}
	if acl.NFS4[1].Who == nil || *acl.NFS4[1].Who != "alice" {
		t.Errorf("ACE 1 who = %v, want alice", acl.NFS4[1].Who)
	}
	if acl.NFS4[1].ID == nil || *acl.NFS4[1].ID != 3001 {
		t.Errorf("ACE 1 id = %v, want 3001", acl.NFS4[1].ID)
	}
	if !acl.ACLFlags["protected"] {
		t.Errorf("ACLFlags = %v", acl.ACLFlags)
	}
}

func TestFilesystemService_GetACL_POSIX(t *testing.T) {
	s := aclService(func(string, any) (json.RawMessage, error) {
		return json.RawMessage(`{
			"path": "/mnt/tank/p", "acltype": "POSIX1E", "trivial": false,
			"acl": [
				{"tag":"USER_OBJ","default":false,"perms":{"READ":true,"WRITE":true,"EXECUTE":false}},
				{"tag":"USER","default":true,"id":3001,"who":"alice",
				 "perms":{"READ":true,"WRITE":false,"EXECUTE":true}}
			]
		}`), nil
	})

	acl, err := s.GetACL(context.Background(), GetACLOpts{Path: "/mnt/tank/p"})
	if err != nil {
		t.Fatalf("GetACL: %v", err)
	}
	if len(acl.POSIX) != 2 {
		t.Fatalf("got %d ACEs, want 2", len(acl.POSIX))
	}
	if len(acl.NFS4) != 0 {
		t.Errorf("NFS4 populated for a POSIX ACL: %v", acl.NFS4)
	}
	if !acl.POSIX[0].Perms.Read || !acl.POSIX[0].Perms.Write || acl.POSIX[0].Perms.Execute {
		t.Errorf("ACE 0 perms = %+v", acl.POSIX[0].Perms)
	}
	if !acl.POSIX[1].Default {
		t.Error("ACE 1 default = false; the payload said true")
	}
}

// A DISABLED path has no entries at all. Reporting it as an error, or as an
// empty NFS4 ACL, would both be wrong.
func TestFilesystemService_GetACL_Disabled(t *testing.T) {
	s := aclService(func(string, any) (json.RawMessage, error) {
		return json.RawMessage(`{
			"path": "/mnt/tank/plain", "acltype": "DISABLED", "trivial": true, "acl": []
		}`), nil
	})

	acl, err := s.GetACL(context.Background(), GetACLOpts{Path: "/mnt/tank/plain"})
	if err != nil {
		t.Fatalf("GetACL: %v", err)
	}
	if acl.ACLType != ACLTypeDisabled {
		t.Errorf("ACLType = %q", acl.ACLType)
	}
	if !acl.Trivial {
		t.Error("Trivial = false for a DISABLED ACL")
	}
	if len(acl.NFS4) != 0 || len(acl.POSIX) != 0 {
		t.Error("entries populated for a DISABLED ACL")
	}
}

// An unrecognised acltype must fail loudly. Falling through silently would
// return an ACL with no entries, which reads as "this path grants nothing".
func TestFilesystemService_GetACL_UnknownACLType(t *testing.T) {
	s := aclService(func(string, any) (json.RawMessage, error) {
		return json.RawMessage(`{"path":"/mnt/x","acltype":"NFS5","acl":[{"tag":"owner@"}]}`), nil
	})

	_, err := s.GetACL(context.Background(), GetACLOpts{Path: "/mnt/x"})
	if err == nil {
		t.Fatal("expected an error for an unknown acltype")
	}
	if !strings.Contains(err.Error(), "NFS5") {
		t.Errorf("error should name the unknown type, got %v", err)
	}
}

func TestFilesystemService_GetACL_NotFound(t *testing.T) {
	s := aclService(func(string, any) (json.RawMessage, error) {
		return nil, errors.New("[ENOENT] path does not exist")
	})

	acl, err := s.GetACL(context.Background(), GetACLOpts{Path: "/mnt/gone"})
	if err != nil {
		t.Fatalf("a missing path should not be an error: %v", err)
	}
	if acl != nil {
		t.Errorf("acl = %+v, want nil", acl)
	}
}

func TestFilesystemService_GetACL_CallError(t *testing.T) {
	s := aclService(func(string, any) (json.RawMessage, error) {
		return nil, errors.New("connection refused")
	})
	if _, err := s.GetACL(context.Background(), GetACLOpts{Path: "/mnt/x"}); err == nil {
		t.Fatal("expected the transport error to surface")
	}
}

// --- SetACL ------------------------------------------------------------------

// setacl is job: true in both 25.04 and 25.10, so it must go through
// CallAndWait. Using Call would return before the ACL was actually applied and
// a read-back immediately afterwards would race.
func TestFilesystemService_SetACL_UsesCallAndWait(t *testing.T) {
	usedCall := false
	usedCallAndWait := false

	s := NewFilesystemService(&mockFileCaller{
		mockAsyncCaller: mockAsyncCaller{
			mockCaller: mockCaller{
				callFunc: func(context.Context, string, any) (json.RawMessage, error) {
					usedCall = true
					return json.RawMessage(`null`), nil
				},
			},
			callAndWaitFunc: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
				usedCallAndWait = true
				if method != "filesystem.setacl" {
					t.Errorf("method = %q", method)
				}
				return json.RawMessage(`null`), nil
			},
		},
	}, Version{Major: 25, Minor: 10})

	err := s.SetACL(context.Background(), SetACLOpts{
		Path:    "/mnt/tank/share",
		ACLType: ACLTypeNFS4,
		NFS4: []NFS4ACE{{
			Tag: ACETagOwner, Type: ACETypeAllow,
			Perms: NFS4Perms{Basic: "FULL_CONTROL"},
			Flags: NFS4Flags{Basic: "INHERIT"},
		}},
	})
	if err != nil {
		t.Fatalf("SetACL: %v", err)
	}
	if usedCall {
		t.Error("SetACL used Call; filesystem.setacl is a job and needs CallAndWait")
	}
	if !usedCallAndWait {
		t.Error("SetACL did not use CallAndWait")
	}
}

func TestFilesystemService_SetACL_ParamsNFS4(t *testing.T) {
	params, err := buildSetACLParams(SetACLOpts{
		Path:    "/mnt/tank/share",
		ACLType: ACLTypeNFS4,
		NFS4: []NFS4ACE{
			{
				Tag: ACETagOwner, Type: ACETypeAllow,
				Perms: NFS4Perms{Basic: "FULL_CONTROL"},
				Flags: NFS4Flags{Basic: "INHERIT"},
			},
			{
				Tag: ACETagUser, Type: ACETypeAllow, ID: i64Ptr(3001), Who: strPtr("alice"),
				Perms: NFS4Perms{Advanced: map[string]bool{"READ_DATA": true}},
				Flags: NFS4Flags{Advanced: map[string]bool{"FILE_INHERIT": true}},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildSetACLParams: %v", err)
	}

	if params["path"] != "/mnt/tank/share" {
		t.Errorf("path = %v", params["path"])
	}
	if params["acltype"] != ACLTypeNFS4 {
		t.Errorf("acltype = %v", params["acltype"])
	}

	dacl, ok := params["dacl"].([]map[string]any)
	if !ok || len(dacl) != 2 {
		t.Fatalf("dacl = %#v", params["dacl"])
	}
	if _, present := dacl[0]["id"]; present {
		t.Error("id emitted for an owner@ ACE that has none")
	}
	if dacl[1]["id"] != int64(3001) || dacl[1]["who"] != "alice" {
		t.Errorf("ACE 1 id/who = %v/%v", dacl[1]["id"], dacl[1]["who"])
	}

	// The union must survive marshalling, since that is what reaches the wire.
	encoded, err := json.Marshal(dacl[0]["perms"])
	if err != nil {
		t.Fatalf("marshal perms: %v", err)
	}
	if string(encoded) != `{"BASIC":"FULL_CONTROL"}` {
		t.Errorf("perms encoded as %s", encoded)
	}
}

func TestFilesystemService_SetACL_ParamsPOSIX(t *testing.T) {
	params, err := buildSetACLParams(SetACLOpts{
		Path:    "/mnt/tank/p",
		ACLType: ACLTypePOSIX1E,
		POSIX: []POSIXACE{{
			Tag: "USER", Default: true, ID: i64Ptr(3001),
			Perms: POSIXPerms{Read: true, Execute: true},
		}},
	})
	if err != nil {
		t.Fatalf("buildSetACLParams: %v", err)
	}

	dacl := params["dacl"].([]map[string]any)
	perms := dacl[0]["perms"].(map[string]bool)
	if !perms["READ"] || perms["WRITE"] || !perms["EXECUTE"] {
		t.Errorf("perms = %v", perms)
	}
	if dacl[0]["default"] != true {
		t.Errorf("default = %v", dacl[0]["default"])
	}
}

// Ownership fields all default to "leave alone". Sending a zero value would
// chown the path to root, which is the kind of mistake that is quiet until it
// is catastrophic.
func TestFilesystemService_SetACL_ParamsOmitsUnsetOwnership(t *testing.T) {
	params, err := buildSetACLParams(SetACLOpts{
		Path: "/mnt/tank/share",
		NFS4: []NFS4ACE{{Tag: ACETagOwner, Type: ACETypeAllow}},
	})
	if err != nil {
		t.Fatalf("buildSetACLParams: %v", err)
	}
	for _, key := range []string{"uid", "gid", "user", "group"} {
		if v, present := params[key]; present {
			t.Errorf("%s sent as %v for an unset field; it must be omitted", key, v)
		}
	}
	if _, present := params["acltype"]; present {
		t.Error("acltype sent when empty; empty means auto-detect and must be omitted")
	}
	if _, present := params["options"]; present {
		t.Error("options sent when no option was set")
	}
}

func TestFilesystemService_SetACL_ParamsSendsExplicitOwnership(t *testing.T) {
	params, err := buildSetACLParams(SetACLOpts{
		Path: "/mnt/tank/share",
		NFS4: []NFS4ACE{{Tag: ACETagOwner, Type: ACETypeAllow}},
		UID:  i64Ptr(0),
		User: strPtr("root"),
	})
	if err != nil {
		t.Fatalf("buildSetACLParams: %v", err)
	}
	// uid 0 is a real value, not an absent one — the pointer is what
	// distinguishes them.
	if params["uid"] != int64(0) {
		t.Errorf("uid = %v, want 0", params["uid"])
	}
	if params["user"] != "root" {
		t.Errorf("user = %v", params["user"])
	}
}

// The lockout guard. An empty dacl without stripacl asks the server to apply
// an ACL granting nobody anything — on a production share that is an outage,
// and it is an easy thing to reach by passing an empty slice.
func TestFilesystemService_SetACL_ParamsRefusesEmptyACLWithoutStrip(t *testing.T) {
	_, err := buildSetACLParams(SetACLOpts{Path: "/mnt/tank/share"})
	if err == nil {
		t.Fatal("expected a refusal for an empty ACL with no stripacl")
	}
	if !strings.Contains(err.Error(), "stripacl") {
		t.Errorf("error should name stripacl, got %v", err)
	}
}

func TestFilesystemService_SetACL_ParamsEmptyACLAllowedWithStrip(t *testing.T) {
	params, err := buildSetACLParams(SetACLOpts{Path: "/mnt/tank/share", StripACL: true})
	if err != nil {
		t.Fatalf("stripacl should permit an empty dacl: %v", err)
	}
	options := params["options"].(map[string]any)
	if options["stripacl"] != true {
		t.Errorf("options = %v", options)
	}
}

func TestFilesystemService_SetACL_ParamsRejectsBothFlavours(t *testing.T) {
	_, err := buildSetACLParams(SetACLOpts{
		Path:  "/mnt/tank/share",
		NFS4:  []NFS4ACE{{Tag: ACETagOwner, Type: ACETypeAllow}},
		POSIX: []POSIXACE{{Tag: "USER_OBJ"}},
	})
	if err == nil {
		t.Fatal("expected a refusal when both ACL flavours are set")
	}
}

func TestFilesystemService_SetACL_ParamsRequiresPath(t *testing.T) {
	if _, err := buildSetACLParams(SetACLOpts{StripACL: true}); err == nil {
		t.Fatal("expected a refusal for an empty path")
	}
}

func TestFilesystemService_SetACL_ParamsOptions(t *testing.T) {
	params, err := buildSetACLParams(SetACLOpts{
		Path:      "/mnt/tank/share",
		NFS4:      []NFS4ACE{{Tag: ACETagOwner, Type: ACETypeAllow}},
		Recursive: true,
		Traverse:  true,
	})
	if err != nil {
		t.Fatalf("buildSetACLParams: %v", err)
	}
	options := params["options"].(map[string]any)
	if options["recursive"] != true || options["traverse"] != true {
		t.Errorf("options = %v", options)
	}
}

// --- helpers -----------------------------------------------------------------

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
