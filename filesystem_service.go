package truenas

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// StatResult is the user-facing representation of a filesystem stat.
// Mode contains only permission bits (masked with 0o777).
type StatResult struct {
	Mode int64
	UID  int64
	GID  int64
}

// SetPermOpts contains options for setting filesystem permissions.
type SetPermOpts struct {
	Path      string
	UID       *int64
	GID       *int64
	Mode      string // Octal string e.g. "755", empty omits
	Recursive bool
	StripACL  bool
	Traverse  bool
}

// FilesystemService provides typed methods for the filesystem.* API namespace.
type FilesystemService struct {
	client  FileCaller
	version Version
}

// NewFilesystemService creates a new FilesystemService.
func NewFilesystemService(c FileCaller, v Version) *FilesystemService {
	return &FilesystemService{client: c, version: v}
}

// Client returns the underlying FileCaller.
func (s *FilesystemService) Client() FileCaller {
	return s.client
}

// WriteFile writes content to a file on the remote system via filesystem.file_receive.
func (s *FilesystemService) WriteFile(ctx context.Context, path string, params WriteFileParams) error {
	b64Content := base64.StdEncoding.EncodeToString(params.Content)

	uid := -1
	if params.UID != nil {
		uid = *params.UID
	}
	gid := -1
	if params.GID != nil {
		gid = *params.GID
	}

	apiParams := []any{
		path,
		b64Content,
		map[string]any{
			"mode": int(params.Mode),
			"uid":  uid,
			"gid":  gid,
		},
	}

	_, err := s.client.Call(ctx, "filesystem.file_receive", apiParams)
	if err != nil {
		return fmt.Errorf("write file %q: %w", path, err)
	}
	return nil
}

// Stat returns filesystem stat information for the given path.
// Mode is masked with 0o777 to strip file type bits.
func (s *FilesystemService) Stat(ctx context.Context, path string) (*StatResult, error) {
	result, err := s.client.Call(ctx, "filesystem.stat", path)
	if err != nil {
		return nil, err
	}

	var resp StatResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse stat response: %w", err)
	}

	return &StatResult{
		Mode: resp.Mode & 0o777,
		UID:  resp.UID,
		GID:  resp.GID,
	}, nil
}

// SetPermissions sets filesystem permissions via the filesystem.setperm API.
// This is a job-based operation that blocks until complete.
//
// The middleware refuses this call when an extended ACL is present on the path
// unless StripACL is set — that guard is the SERVER's, documented on
// filesystem.setperm in both 25.04 and 25.10, and this library deliberately
// does not duplicate it. A second implementation of the same rule would be one
// more thing to keep in step with the middleware's own idea of "extended", and
// would be wrong the moment the two diverged.
//
// What the server cannot do is fail early: the refusal arrives when the job
// runs. Callers that want to know beforehand — a Terraform provider deciding
// at plan time, for instance — should call PreflightSetPerm.
//
// StripACL is destructive. With it set, every ACE on the path is discarded; if
// Mode is also unset, non-trivial ACLs are flattened to their trivial
// equivalent. Traverse extends that across dataset boundaries.
func (s *FilesystemService) SetPermissions(ctx context.Context, opts SetPermOpts) error {
	params := buildSetPermParams(opts)
	_, err := s.client.CallAndWait(ctx, "filesystem.setperm", params)
	return err
}

// ErrExtendedACLPresent reports that a path carries an ACL that setperm would
// have to discard. Callers can test for it with errors.Is.
var ErrExtendedACLPresent = errors.New("extended ACL present; setperm would discard it")

// PreflightSetPerm reports whether SetPermissions would be refused, without
// changing anything.
//
// The decision uses the ACL's `trivial` flag, which the server computes: an
// ACL is trivial when it can be expressed as a file mode without losing any
// access rule. That is the same question setperm asks, answered by the same
// authority, so this does not restate the rule — it asks the server for it.
//
// Returns nil when the call would proceed. Returns an error wrapping
// ErrExtendedACLPresent when it would be refused. A path that does not exist
// is not a preflight failure: setperm will report that itself, and guessing
// here would turn a clear error into a confusing one.
//
// This costs one filesystem.getacl call, which is not a job.
func (s *FilesystemService) PreflightSetPerm(ctx context.Context, opts SetPermOpts) error {
	// StripACL is the caller stating outright that discarding the ACL is
	// intended, which is exactly what the server's own guard accepts.
	if opts.StripACL {
		return nil
	}

	acl, err := s.GetACL(ctx, GetACLOpts{Path: opts.Path})
	if err != nil {
		return err
	}
	if acl == nil {
		return nil
	}
	if acl.Trivial || acl.ACLType == ACLTypeDisabled {
		return nil
	}

	n := acl.entryCount()
	return fmt.Errorf("%w: %s carries a non-trivial %s ACL with %d %s. "+
		"Set stripacl to discard it, or manage the ACL directly instead of "+
		"setting a mode", ErrExtendedACLPresent, opts.Path, acl.ACLType, n, plural(n, "entry", "entries"))
}

// plural picks the right noun form for a count.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// buildSetPermParams converts SetPermOpts to API parameters.
// Only includes fields that are set (non-nil/non-empty/non-false).
func buildSetPermParams(opts SetPermOpts) map[string]any {
	params := map[string]any{
		"path": opts.Path,
	}

	if opts.UID != nil {
		params["uid"] = *opts.UID
	}
	if opts.GID != nil {
		params["gid"] = *opts.GID
	}
	if opts.Mode != "" {
		params["mode"] = opts.Mode
	}

	options := map[string]any{}
	if opts.Recursive {
		options["recursive"] = true
	}
	if opts.StripACL {
		options["stripacl"] = true
	}
	if opts.Traverse {
		options["traverse"] = true
	}
	if len(options) > 0 {
		params["options"] = options
	}

	return params
}
