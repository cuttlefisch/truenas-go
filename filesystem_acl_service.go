package truenas

import (
	"context"
	"encoding/json"
	"fmt"
)

// aclResponse is the wire form of a filesystem.getacl result. The three
// result variants (NFS4, POSIX1E, DISABLED) share every field but `acl`, so
// one struct decodes all of them and the entries are split by acltype.
type aclResponse struct {
	Path     string          `json:"path"`
	ACLType  string          `json:"acltype"`
	Trivial  bool            `json:"trivial"`
	User     *string         `json:"user"`
	Group    *string         `json:"group"`
	UID      *int64          `json:"uid"`
	GID      *int64          `json:"gid"`
	ACLFlags map[string]bool `json:"aclflags"`
	ACL      json.RawMessage `json:"acl"`
}

type nfs4ACEWire struct {
	Tag   string    `json:"tag"`
	Type  string    `json:"type"`
	ID    *int64    `json:"id"`
	Who   *string   `json:"who"`
	Perms NFS4Perms `json:"perms"`
	Flags NFS4Flags `json:"flags"`
}

type posixACEWire struct {
	Tag     string  `json:"tag"`
	Default bool    `json:"default"`
	ID      *int64  `json:"id"`
	Who     *string `json:"who"`
	Perms   struct {
		Read    bool `json:"READ"`
		Write   bool `json:"WRITE"`
		Execute bool `json:"EXECUTE"`
	} `json:"perms"`
}

// GetACLOpts controls how an ACL is read.
type GetACLOpts struct {
	Path string
	// ResolveIDs asks the server to populate `who` with the resolved account
	// name alongside the numeric id. Without it, an ACL naming a user is
	// readable only as a uid, which makes a diff against a config written in
	// terms of names impossible.
	ResolveIDs bool
	// Simplified requests the basic (preset) form of perms and flags where
	// one applies. The server may return the basic form regardless, so a
	// caller must handle both either way.
	Simplified bool
}

// SetACLOpts describes an ACL to apply.
//
// Exactly one of NFS4 or POSIX should be populated, matching ACLType. Leaving
// both empty with StripACL set removes the ACL entirely.
type SetACLOpts struct {
	Path string
	// ACLType is NFS4 or POSIX1E. Empty asks the server to auto-detect from
	// the filesystem's capabilities.
	ACLType string

	NFS4  []NFS4ACE
	POSIX []POSIXACE

	// UID/GID and User/Group set ownership. Nil or empty preserves what is
	// already there rather than resetting it.
	UID   *int64
	GID   *int64
	User  *string
	Group *string

	Recursive bool
	Traverse  bool
	// StripACL removes the ACL and reverts the path to plain POSIX mode bits.
	// This is destructive: every ACE is discarded.
	StripACL bool
}

// GetACL reads the ACL at a path. filesystem.getacl is not a job.
//
// Perms and flags come back in either the basic preset form or the advanced
// named-bit form, and the schema permits both in every version. Callers must
// handle both: comparing a locally-constructed basic set against an advanced
// set the server returned will report a difference that is not real. The
// expansion of each preset is defined by the middleware and is not described
// in the API schema, so this library deliberately does not guess it.
func (s *FilesystemService) GetACL(ctx context.Context, opts GetACLOpts) (*ACL, error) {
	// Positional parameters. wrapParams only wraps a non-slice value, so a
	// multi-argument call has to build the []any itself.
	result, err := s.client.Call(ctx, "filesystem.getacl",
		[]any{opts.Path, opts.Simplified, opts.ResolveIDs})
	if err != nil {
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, err
	}

	var resp aclResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse getacl response: %w", err)
	}

	acl := &ACL{
		Path:     resp.Path,
		ACLType:  resp.ACLType,
		Trivial:  resp.Trivial,
		User:     resp.User,
		Group:    resp.Group,
		UID:      resp.UID,
		GID:      resp.GID,
		ACLFlags: resp.ACLFlags,
	}

	if len(resp.ACL) == 0 || string(resp.ACL) == "null" {
		return acl, nil
	}

	switch resp.ACLType {
	case ACLTypeNFS4:
		var wire []nfs4ACEWire
		if err := json.Unmarshal(resp.ACL, &wire); err != nil {
			return nil, fmt.Errorf("parse NFS4 ACL entries: %w", err)
		}
		acl.NFS4 = make([]NFS4ACE, 0, len(wire))
		for _, w := range wire {
			acl.NFS4 = append(acl.NFS4, NFS4ACE{
				Tag: w.Tag, Type: w.Type, ID: w.ID, Who: w.Who,
				Perms: w.Perms, Flags: w.Flags,
			})
		}
	case ACLTypePOSIX1E:
		var wire []posixACEWire
		if err := json.Unmarshal(resp.ACL, &wire); err != nil {
			return nil, fmt.Errorf("parse POSIX1E ACL entries: %w", err)
		}
		acl.POSIX = make([]POSIXACE, 0, len(wire))
		for _, w := range wire {
			acl.POSIX = append(acl.POSIX, POSIXACE{
				Tag: w.Tag, Default: w.Default, ID: w.ID, Who: w.Who,
				Perms: POSIXPerms{
					Read: w.Perms.Read, Write: w.Perms.Write, Execute: w.Perms.Execute,
				},
			})
		}
	case ACLTypeDisabled:
		// No entries by definition; the path carries only mode bits.
	default:
		return nil, fmt.Errorf("unknown acltype %q at %s", resp.ACLType, resp.Path)
	}

	return acl, nil
}

// SetACL applies an ACL. filesystem.setacl IS a job, so this blocks until it
// completes rather than returning a job id the caller has to poll.
func (s *FilesystemService) SetACL(ctx context.Context, opts SetACLOpts) error {
	params, err := buildSetACLParams(opts)
	if err != nil {
		return err
	}
	_, err = s.client.CallAndWait(ctx, "filesystem.setacl", params)
	return err
}

// buildSetACLParams converts SetACLOpts into the single filesystem_acl object
// the API accepts.
func buildSetACLParams(opts SetACLOpts) (map[string]any, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if len(opts.NFS4) > 0 && len(opts.POSIX) > 0 {
		return nil, fmt.Errorf("set NFS4 or POSIX entries, not both")
	}

	params := map[string]any{"path": opts.Path}

	// acltype is nullable and means "auto-detect", so an empty value is
	// omitted rather than sent as "".
	if opts.ACLType != "" {
		params["acltype"] = opts.ACLType
	}

	switch {
	case len(opts.NFS4) > 0:
		dacl := make([]map[string]any, 0, len(opts.NFS4))
		for _, ace := range opts.NFS4 {
			entry := map[string]any{
				"tag":   ace.Tag,
				"type":  ace.Type,
				"perms": ace.Perms,
				"flags": ace.Flags,
			}
			if ace.ID != nil {
				entry["id"] = *ace.ID
			}
			if ace.Who != nil {
				entry["who"] = *ace.Who
			}
			dacl = append(dacl, entry)
		}
		params["dacl"] = dacl
	case len(opts.POSIX) > 0:
		dacl := make([]map[string]any, 0, len(opts.POSIX))
		for _, ace := range opts.POSIX {
			entry := map[string]any{
				"tag":     ace.Tag,
				"default": ace.Default,
				"perms": map[string]bool{
					"READ":    ace.Perms.Read,
					"WRITE":   ace.Perms.Write,
					"EXECUTE": ace.Perms.Execute,
				},
			}
			if ace.ID != nil {
				entry["id"] = *ace.ID
			}
			if ace.Who != nil {
				entry["who"] = *ace.Who
			}
			dacl = append(dacl, entry)
		}
		params["dacl"] = dacl
	default:
		// An empty dacl is only meaningful alongside stripacl. Sending one
		// without it would ask the server to apply an ACL granting nobody
		// anything, which is a lockout rather than a no-op.
		if !opts.StripACL {
			return nil, fmt.Errorf("no ACL entries and stripacl not set: " +
				"applying an empty ACL would remove all access")
		}
		params["dacl"] = []map[string]any{}
	}

	// uid/gid default to -1 and user/group to null, all meaning "leave it
	// alone", so an unset field is omitted rather than sent as a zero value.
	// Sending uid=0 would silently chown the path to root.
	if opts.UID != nil {
		params["uid"] = *opts.UID
	}
	if opts.GID != nil {
		params["gid"] = *opts.GID
	}
	if opts.User != nil {
		params["user"] = *opts.User
	}
	if opts.Group != nil {
		params["group"] = *opts.Group
	}

	options := map[string]any{}
	if opts.Recursive {
		options["recursive"] = true
	}
	if opts.Traverse {
		options["traverse"] = true
	}
	if opts.StripACL {
		options["stripacl"] = true
	}
	if len(options) > 0 {
		params["options"] = options
	}

	return params, nil
}
