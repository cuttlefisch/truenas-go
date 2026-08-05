module github.com/deevus/truenas-go

go 1.25.0

// Build toolchain, distinct from the `go` directive above (which is the
// minimum language version consumers must support — deliberately left at
// 1.25.0 so this library does not force a floor on anyone importing it).
//
// Pinned to a patch release because go1.25.0's standard library carries
// vulnerabilities reachable from this code (crypto/tls, crypto/x509,
// net/textproto), fixed across 1.25.1–1.25.12. actions/setup-go reads this
// line in preference to the `go` directive, so CI builds and scans with it.
toolchain go1.25.12

require (
	al.essio.dev/pkg/shellescape v1.6.0
	github.com/dustin/go-humanize v1.0.1
	github.com/gorilla/websocket v1.5.3
	golang.org/x/crypto v0.52.0
	golang.org/x/time v0.14.0
)

require golang.org/x/sys v0.45.0 // indirect
