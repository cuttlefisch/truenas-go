package truenas

// Named capability predicates over a detected appliance Version.
//
// Why these exist rather than bare AtLeast(major, minor) calls at each site:
// a version comparison says *which release* introduced something, but not
// *what* is being gated. Six call sites in this library spelled AtLeast(25, 0),
// and they were gating four unrelated things — a wire-format change in
// cloudsync, the JSON-RPC endpoint, core.subscribe availability, and a
// namespace rename. Read individually, each looked like the same check.
//
// Naming the capability makes the call site self-describing, gives the version
// boundary exactly one definition, and means a boundary that later turns out to
// be wrong is corrected in one place instead of grepped for. This mirrors
// bpg/terraform-provider-proxmox's version/capabilities.go, which is the same
// pattern applied to the same problem.
//
// Keep each predicate to a single capability. If two capabilities happen to
// share a boundary today, they still get two predicates — the duplication is
// the point, because the boundaries can diverge later and a shared predicate
// would have to be split under pressure.

// SupportsJSONRPCAPI reports whether the appliance serves JSON-RPC 2.0 at
// /api/current. TrueNAS 24.x offered only the legacy DDP-style /websocket
// endpoint, which this library deliberately does not support.
func (v Version) SupportsJSONRPCAPI() bool { return v.AtLeast(25, 0) }

// SupportsCoreSubscribe reports whether core.subscribe exists, which is what
// lets a job be awaited via events instead of polled. On appliances without it
// the SSH transport falls back to polling core.get_jobs.
func (v Version) SupportsCoreSubscribe() bool { return v.AtLeast(25, 0) }

// SupportsCloudSyncProviderObject reports whether cloudsync.credentials takes
// the provider as an object with a "type" key merged with its attributes.
// Before 25.0 the provider was a bare string alongside a separate "attributes"
// object.
func (v Version) SupportsCloudSyncProviderObject() bool { return v.AtLeast(25, 0) }

// UsesPoolSnapshotNamespace reports whether snapshot methods live under
// pool.snapshot rather than zfs.snapshot. The namespace was renamed in 25.10
// and the old one removed, so this is a rename rather than an addition — both
// halves of the branch are load-bearing.
func (v Version) UsesPoolSnapshotNamespace() bool { return v.AtLeast(25, 10) }
