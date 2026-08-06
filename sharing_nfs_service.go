package truenas

import (
	"context"
	"encoding/json"
	"fmt"
)

// SharingNFS is the user-facing representation of a TrueNAS NFS export.
//
// Field names follow the API's, deliberately: unlike CronJob — which renames
// the API's inverted stdout/stderr to CaptureStdout/CaptureStderr — nothing
// here has semantics worth renaming away from, and a name that matches the
// wire is one less mapping for a reader to hold.
type SharingNFS struct {
	ID              int64
	Path            string
	Aliases         []string
	Comment         string
	Networks        []string
	Hosts           []string
	ReadOnly        bool
	MaprootUser     *string
	MaprootGroup    *string
	MapallUser      *string
	MapallGroup     *string
	Security        []string
	Enabled         bool
	ExposeSnapshots bool
	Locked          bool
}

// CreateSharingNFSOpts contains options for creating an NFS export.
//
// Only Path is required by the API. The pointer fields are nil-means-unset,
// matching the wire contract — see SharingNFSResponse for why that distinction
// cannot be collapsed.
type CreateSharingNFSOpts struct {
	Path            string
	Aliases         []string
	Comment         string
	Networks        []string
	Hosts           []string
	ReadOnly        bool
	MaprootUser     *string
	MaprootGroup    *string
	MapallUser      *string
	MapallGroup     *string
	Security        []string
	Enabled         *bool // nil means "take the server default", which is true
	ExposeSnapshots bool
}

// UpdateSharingNFSOpts contains options for updating an NFS export.
// All fields are always sent, matching the CronService convention.
type UpdateSharingNFSOpts = CreateSharingNFSOpts

// SharingNFSService provides typed methods for the sharing.nfs.* API namespace.
//
// Takes a Caller rather than an AsyncCaller: all five sharing.nfs methods are
// job: false in both 25.04 and 25.10, verified against both embedded schemas.
// Requesting the narrower interface is what keeps a caller from having to
// supply job-polling machinery this service will never use.
//
// There is no version resolution here either. sharing.nfs.create is
// byte-identical across 25.04 and 25.10 — the same thirteen accepted fields —
// which is why this namespace, rather than sharing.smb, is the one that proves
// the pipeline.
type SharingNFSService struct {
	client  Caller
	version Version
}

// NewSharingNFSService creates a new SharingNFSService.
func NewSharingNFSService(c Caller, v Version) *SharingNFSService {
	return &SharingNFSService{client: c, version: v}
}

// Create creates an NFS export and returns the full object.
func (s *SharingNFSService) Create(ctx context.Context, opts CreateSharingNFSOpts) (*SharingNFS, error) {
	result, err := s.client.Call(ctx, "sharing.nfs.create", []any{sharingNFSOptsToParams(opts)})
	if err != nil {
		return nil, err
	}

	var createResp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(result, &createResp); err != nil {
		return nil, fmt.Errorf("parse create response: %w", err)
	}

	return s.Get(ctx, createResp.ID)
}

// Get returns an NFS export by ID, or nil if not found.
func (s *SharingNFSService) Get(ctx context.Context, id int64) (*SharingNFS, error) {
	result, err := s.client.Call(ctx, "sharing.nfs.get_instance", []any{id})
	if err != nil {
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, err
	}

	var resp SharingNFSResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse get_instance response: %w", err)
	}

	share := sharingNFSFromResponse(resp)
	return &share, nil
}

// List returns all NFS exports.
func (s *SharingNFSService) List(ctx context.Context) ([]SharingNFS, error) {
	result, err := s.client.Call(ctx, "sharing.nfs.query", []any{})
	if err != nil {
		return nil, err
	}

	var resps []SharingNFSResponse
	if err := json.Unmarshal(result, &resps); err != nil {
		return nil, fmt.Errorf("parse query response: %w", err)
	}

	shares := make([]SharingNFS, 0, len(resps))
	for _, r := range resps {
		shares = append(shares, sharingNFSFromResponse(r))
	}
	return shares, nil
}

// Update updates an NFS export and returns the full object.
func (s *SharingNFSService) Update(ctx context.Context, id int64, opts UpdateSharingNFSOpts) (*SharingNFS, error) {
	result, err := s.client.Call(ctx, "sharing.nfs.update", []any{id, sharingNFSOptsToParams(opts)})
	if err != nil {
		return nil, err
	}

	var resp SharingNFSResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse update response: %w", err)
	}

	share := sharingNFSFromResponse(resp)
	return &share, nil
}

// Delete deletes an NFS export.
func (s *SharingNFSService) Delete(ctx context.Context, id int64) error {
	_, err := s.client.Call(ctx, "sharing.nfs.delete", []any{id})
	return err
}

// sharingNFSOptsToParams builds the create/update payload.
//
// Slices are sent as empty arrays rather than omitted when nil. The API
// defaults them to [], so the two are equivalent on create — but on UPDATE an
// omitted field keeps its previous value while an empty array clears it, and
// silently keeping a value the caller cleared is the harder bug to notice.
func sharingNFSOptsToParams(opts CreateSharingNFSOpts) map[string]any {
	params := map[string]any{
		"path":             opts.Path,
		"comment":          opts.Comment,
		"aliases":          nonNilSlice(opts.Aliases),
		"networks":         nonNilSlice(opts.Networks),
		"hosts":            nonNilSlice(opts.Hosts),
		"security":         nonNilSlice(opts.Security),
		"ro":               opts.ReadOnly,
		"expose_snapshots": opts.ExposeSnapshots,
	}

	// The map* fields are sent only when set. Sending an explicit null would
	// also be correct, but omitting keeps the payload closer to what the UI
	// produces and avoids relying on null handling that differs per field.
	if opts.MaprootUser != nil {
		params["maproot_user"] = *opts.MaprootUser
	}
	if opts.MaprootGroup != nil {
		params["maproot_group"] = *opts.MaprootGroup
	}
	if opts.MapallUser != nil {
		params["mapall_user"] = *opts.MapallUser
	}
	if opts.MapallGroup != nil {
		params["mapall_group"] = *opts.MapallGroup
	}
	if opts.Enabled != nil {
		params["enabled"] = *opts.Enabled
	}

	return params
}

// nonNilSlice returns an empty slice for nil, so the payload carries [] rather
// than null. TrueNAS accepts both, but [] is what its own UI sends and what the
// schema documents as the default.
func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func sharingNFSFromResponse(resp SharingNFSResponse) SharingNFS {
	return SharingNFS{
		ID:              resp.ID,
		Path:            resp.Path,
		Aliases:         resp.Aliases,
		Comment:         resp.Comment,
		Networks:        resp.Networks,
		Hosts:           resp.Hosts,
		ReadOnly:        resp.RO,
		MaprootUser:     resp.MaprootUser,
		MaprootGroup:    resp.MaprootGroup,
		MapallUser:      resp.MapallUser,
		MapallGroup:     resp.MapallGroup,
		Security:        resp.Security,
		Enabled:         resp.Enabled,
		ExposeSnapshots: resp.ExposeSnapshots,
		Locked:          resp.Locked,
	}
}
