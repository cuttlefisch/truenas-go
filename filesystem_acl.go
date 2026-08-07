package truenas

import (
	"encoding/json"
	"fmt"
	"sort"
)

// ACL flavours as reported by filesystem.getacl and accepted by
// filesystem.setacl.
//
// NOTE these are NOT the same spellings as the dataset acltype property, which
// uses NFSV4/POSIX/OFF. A dataset with acltype=NFSV4 carries an ACL with
// acltype=NFS4. Converting between the two is the caller's job; nothing here
// silently accepts the dataset spelling, because doing so would hide the
// mismatch rather than surface it.
const (
	ACLTypeNFS4     = "NFS4"
	ACLTypePOSIX1E  = "POSIX1E"
	ACLTypeDisabled = "DISABLED"
)

// NFS4 ACE tags.
const (
	ACETagOwner    = "owner@"
	ACETagGroup    = "group@"
	ACETagEveryone = "everyone@"
	ACETagUser     = "USER"
	ACETagGroupID  = "GROUP"
)

// NFS4 ACE types.
const (
	ACETypeAllow = "ALLOW"
	ACETypeDeny  = "DENY"
)

// ACL is a filesystem ACL as returned by filesystem.getacl.
//
// Exactly one of NFS4 or POSIX is populated, selected by ACLType. A DISABLED
// ACL has neither: the path carries only ordinary mode bits.
type ACL struct {
	Path    string
	ACLType string
	// Trivial reports that the ACL expresses nothing a plain POSIX mode
	// cannot. It is what makes it safe to run filesystem.setperm on a path:
	// on a non-trivial ACL, setperm discards information.
	Trivial bool

	User  *string
	Group *string
	UID   *int64
	GID   *int64

	// ACLFlags carries the NFS4 ACL-wide flags (autoinherit, protected,
	// defaulted). Empty for POSIX1E.
	ACLFlags map[string]bool

	NFS4  []NFS4ACE
	POSIX []POSIXACE
}

// NFS4ACE is a single entry in an NFSv4 ACL.
type NFS4ACE struct {
	Tag   string
	Type  string
	ID    *int64
	Who   *string
	Perms NFS4Perms
	Flags NFS4Flags
}

// POSIXACE is a single entry in a POSIX.1e ACL.
type POSIXACE struct {
	Tag     string
	Default bool
	ID      *int64
	Who     *string
	Perms   POSIXPerms
}

// POSIXPerms are the three POSIX permission bits.
type POSIXPerms struct {
	Read    bool
	Write   bool
	Execute bool
}

// NFS4Perms is the permission set of an NFSv4 ACE.
//
// The API accepts and returns this as one of two shapes: a basic preset
// ({"BASIC": "FULL_CONTROL"}) or an advanced map of fourteen named booleans.
// Both are preserved as sent rather than converted, because the expansion of
// each preset is defined by the middleware and is not stated in the schema.
// Guessing it would produce ACLs that look right and grant the wrong access.
//
// Callers that need to compare two permission sets should compare like with
// like; see the note on Normalized.
type NFS4Perms struct {
	// Basic is FULL_CONTROL, MODIFY, READ or TRAVERSE. Empty when the
	// advanced form is in use.
	Basic string
	// Advanced holds the named permission bits. Nil when Basic is set.
	Advanced map[string]bool
}

// NFS4Flags is the inheritance flag set of an NFSv4 ACE, with the same
// basic-or-advanced duality as NFS4Perms.
type NFS4Flags struct {
	// Basic is INHERIT or NOINHERIT. Empty when the advanced form is in use.
	Basic string
	// Advanced holds the named inheritance bits. Nil when Basic is set.
	Advanced map[string]bool
}

// IsBasic reports whether the preset form is in use.
func (p NFS4Perms) IsBasic() bool { return p.Basic != "" }

// IsBasic reports whether the preset form is in use.
func (f NFS4Flags) IsBasic() bool { return f.Basic != "" }

// MarshalJSON emits whichever form the value holds.
func (p NFS4Perms) MarshalJSON() ([]byte, error) {
	if p.Basic != "" {
		return json.Marshal(map[string]string{"BASIC": p.Basic})
	}
	if p.Advanced == nil {
		return []byte("null"), nil
	}
	return json.Marshal(p.Advanced)
}

// UnmarshalJSON accepts either form, keeping whichever the server sent.
func (p *NFS4Perms) UnmarshalJSON(data []byte) error {
	basic, advanced, err := decodeBasicOrAdvanced(data, "perms")
	if err != nil {
		return err
	}
	p.Basic, p.Advanced = basic, advanced
	return nil
}

// MarshalJSON emits whichever form the value holds.
func (f NFS4Flags) MarshalJSON() ([]byte, error) {
	if f.Basic != "" {
		return json.Marshal(map[string]string{"BASIC": f.Basic})
	}
	if f.Advanced == nil {
		return []byte("null"), nil
	}
	return json.Marshal(f.Advanced)
}

// UnmarshalJSON accepts either form, keeping whichever the server sent.
func (f *NFS4Flags) UnmarshalJSON(data []byte) error {
	basic, advanced, err := decodeBasicOrAdvanced(data, "flags")
	if err != nil {
		return err
	}
	f.Basic, f.Advanced = basic, advanced
	return nil
}

// decodeBasicOrAdvanced splits the union. A BASIC key selects the preset form;
// anything else is read as the advanced boolean map.
func decodeBasicOrAdvanced(data []byte, what string) (string, map[string]bool, error) {
	if string(data) == "null" {
		return "", nil, nil
	}

	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return "", nil, fmt.Errorf("parse %s: %w", what, err)
	}

	if raw, ok := probe["BASIC"]; ok {
		var basic string
		if err := json.Unmarshal(raw, &basic); err != nil {
			return "", nil, fmt.Errorf("parse %s BASIC: %w", what, err)
		}
		return basic, nil, nil
	}

	advanced := make(map[string]bool, len(probe))
	for k, raw := range probe {
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return "", nil, fmt.Errorf("parse %s %s: %w", what, k, err)
		}
		advanced[k] = b
	}
	return "", advanced, nil
}

// SetNames returns the advanced keys that are true, sorted.
//
// Map iteration order is random, so any comparison or rendering of an advanced
// permission set has to sort first. A diff built from unsorted keys reports
// changes that are not there.
func (p NFS4Perms) SetNames() []string { return trueKeys(p.Advanced) }

// SetNames returns the advanced keys that are true, sorted.
func (f NFS4Flags) SetNames() []string { return trueKeys(f.Advanced) }

func trueKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Equal compares two permission sets.
//
// A basic set and an advanced set are NEVER reported equal, even when the
// preset expands to exactly those bits, because the expansion is not knowable
// from the schema. Callers that must compare across forms have to ask the
// server; see GetACL's doc comment.
func (p NFS4Perms) Equal(other NFS4Perms) bool {
	if p.IsBasic() != other.IsBasic() {
		return false
	}
	if p.IsBasic() {
		return p.Basic == other.Basic
	}
	return sameBoolMap(p.Advanced, other.Advanced)
}

// Equal compares two flag sets, with the same cross-form rule as NFS4Perms.
func (f NFS4Flags) Equal(other NFS4Flags) bool {
	if f.IsBasic() != other.IsBasic() {
		return false
	}
	if f.IsBasic() {
		return f.Basic == other.Basic
	}
	return sameBoolMap(f.Advanced, other.Advanced)
}

// sameBoolMap treats an absent key and an explicit false as the same thing,
// which is what the API means: every advanced bit defaults to false.
func sameBoolMap(a, b map[string]bool) bool {
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	for k, v := range b {
		if a[k] != v {
			return false
		}
	}
	return true
}

// entryCount returns the number of ACEs, whichever flavour is populated.
func (a *ACL) entryCount() int {
	return len(a.NFS4) + len(a.POSIX)
}
