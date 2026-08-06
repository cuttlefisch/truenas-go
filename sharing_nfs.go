package truenas

// SharingNFSResponse represents an NFS export from the TrueNAS API.
//
// The four map* fields are *string rather than string because the API models
// them as `{"type": ["string", "null"]}` with a null default, and the
// distinction is load-bearing: "not set" and "set to the empty string" are
// different states server-side. Decoding null into "" would make an unset
// field indistinguishable from a cleared one, and a resource that then sent ""
// back would produce a diff on every plan that never converges.
type SharingNFSResponse struct {
	ID              int64    `json:"id"`
	Path            string   `json:"path"`
	Aliases         []string `json:"aliases"`
	Comment         string   `json:"comment"`
	Networks        []string `json:"networks"`
	Hosts           []string `json:"hosts"`
	RO              bool     `json:"ro"`
	MaprootUser     *string  `json:"maproot_user"`
	MaprootGroup    *string  `json:"maproot_group"`
	MapallUser      *string  `json:"mapall_user"`
	MapallGroup     *string  `json:"mapall_group"`
	Security        []string `json:"security"`
	Enabled         bool     `json:"enabled"`
	ExposeSnapshots bool     `json:"expose_snapshots"`

	// Locked is returned but never accepted. It reports whether the export's
	// dataset is locked by encryption, which is server-owned state.
	Locked bool `json:"locked"`
}
