// Package fake provides a stateful in-memory TrueNAS middleware server
// speaking real JSON-RPC 2.0 over a websocket.
//
// It exists to make a whole class of provider bug catchable in CI, with no
// appliance anywhere: the class where the SERVER normalizes what you sent, the
// client stores what it MEANT to send, and every subsequent `terraform plan`
// reports a change that will never converge.
//
// The existing test servers in client/*_test.go are stateless — request in,
// canned response out. They cannot express "create an object, then read back
// what the server stored", which is exactly the loop an idempotency bug lives
// in. This one keeps a store, so a create is observable by a later query.
//
// # Normalization hooks are the point
//
// A fake that echoes its input back agrees with the client by construction and
// proves nothing. Register a NormalizeFunc per namespace to reproduce what the
// real middleware does to a payload — upper-case an enum, canonicalize a path,
// apply a default — and a resource that fails to account for it produces a
// non-empty second plan, in CI, on every PR.
//
// # Deliberately not a TrueNAS emulator
//
// This models the SHAPE of the middleware's CRUD and job protocols, not its
// semantics. It does not validate payloads against the API schema, enforce
// referential integrity, or implement query-filters beyond a simple id match.
// Anything that turns on real server behaviour still needs the acceptance tier
// (ADR-005) — this is a filter, not a substitute, and treating a green fake as
// proof that a resource works against TrueNAS is exactly the mistake it is
// meant to make cheaper to avoid, not to enable.
//
// # No testing dependency, on purpose
//
// Nothing here takes a testing.TB or imports "testing". The provider repo
// consumes this package, and a test-only dependency reaching its production
// build would be a real cost. Callers close the server themselves.
package fake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// NormalizeFunc reproduces a server-side mutation of a create or update
// payload. It receives the payload as sent and returns what the server would
// actually store.
//
// Implementations must not mutate the input map — return a new one, or copy
// first. The caller retains the original for its own assertions.
type NormalizeFunc func(payload map[string]any) map[string]any

// Server is a stateful in-memory middleware.
type Server struct {
	ts *httptest.Server

	mu      sync.Mutex
	records map[string]map[int64]map[string]any // namespace -> id -> record
	nextID  map[string]int64
	hooks   map[string]NormalizeFunc
	jobs    map[string]bool // methods that behave as jobs
	calls   []string        // every method called, in order

	writeMu sync.Mutex // serializes websocket writes; gorilla forbids concurrent writers
	version string
}

// Option configures a Server at construction.
type Option func(*Server)

// WithVersion sets what system.version reports. Defaults to a 25.10.5 string,
// the platform this library targets.
func WithVersion(v string) Option {
	return func(s *Server) { s.version = v }
}

// WithJobMethods marks methods that return a job ID and complete
// asynchronously, rather than returning their result inline.
func WithJobMethods(methods ...string) Option {
	return func(s *Server) {
		for _, m := range methods {
			s.jobs[m] = true
		}
	}
}

// New starts a fake middleware. The caller must Close it.
func New(opts ...Option) *Server {
	s := &Server{
		records: map[string]map[int64]map[string]any{},
		nextID:  map[string]int64{},
		hooks:   map[string]NormalizeFunc{},
		jobs:    map[string]bool{},
		version: "TrueNAS-25.10.5",
	}
	for _, o := range opts {
		o(s)
	}
	// TLS, not plain HTTP, deliberately. The client picks wss:// unless an
	// unexported test-only flag is set, and reaching that flag from this
	// package would mean widening client's public API for a test knob — a
	// permanent cost to merge-compatibility with upstream. Serving TLS instead
	// lets a caller use the real InsecureSkipVerify config field, and has the
	// side benefit of exercising the TLS dial path rather than bypassing it.
	s.ts = httptest.NewTLSServer(http.HandlerFunc(s.handle))
	return s
}

// Close shuts the server down.
func (s *Server) Close() { s.ts.Close() }

// URL returns the https:// base URL. Use Host/Port for a WebSocketConfig.
func (s *Server) URL() string { return s.ts.URL }

// Host returns the host:port the server is listening on.
func (s *Server) Host() string { return strings.TrimPrefix(s.ts.URL, "https://") }

// HostPort splits the listener address, saving every caller the same parse.
func (s *Server) HostPort() (string, int) {
	hp := s.Host()
	i := strings.LastIndex(hp, ":")
	if i < 0 {
		return hp, 443
	}
	port, _ := strconv.Atoi(hp[i+1:])
	return hp[:i], port
}

// Normalize registers a server-side mutation for a namespace's create and
// update payloads. This is what makes the fake capable of catching an
// idempotency defect rather than merely exercising CRUD.
func (s *Server) Normalize(namespace string, fn NormalizeFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks[namespace] = fn
}

// Seed inserts a record directly, bypassing normalization, and returns its id.
// Use it to set up preconditions a test does not want to create through the
// API.
func (s *Server) Seed(namespace string, record map[string]any) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.insertLocked(namespace, record)
}

// Records returns a copy of everything stored in a namespace, so a test can
// assert on server state rather than only on what the client believes.
func (s *Server) Records(namespace string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.records[namespace]))
	for _, r := range s.records[namespace] {
		out = append(out, copyMap(r))
	}
	return out
}

// Calls returns every method invoked, in order. Useful for asserting that a
// resource did not make a call it should have skipped — an assertion that is
// otherwise invisible.
func (s *Server) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func (s *Server) insertLocked(namespace string, record map[string]any) int64 {
	if s.records[namespace] == nil {
		s.records[namespace] = map[int64]map[string]any{}
	}
	s.nextID[namespace]++
	id := s.nextID[namespace]
	rec := copyMap(record)
	rec["id"] = id
	s.records[namespace][id] = rec
	return id
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// splitMethod separates "sharing.nfs.create" into ("sharing.nfs", "create").
func splitMethod(method string) (namespace, verb string) {
	i := strings.LastIndex(method, ".")
	if i < 0 {
		return "", method
	}
	return method[:i], method[i+1:]
}

// idFilter recognises the one query-filter shape this fake supports:
// [["id", "=", N]]. Anything else is treated as "no filter", which returns
// everything — deliberately permissive, because a fake that silently returned
// an empty set for a filter it did not understand would look like a missing
// record and send someone hunting the wrong bug.
func idFilter(params json.RawMessage) (int64, bool) {
	var outer []json.RawMessage
	if err := json.Unmarshal(params, &outer); err != nil || len(outer) == 0 {
		return 0, false
	}
	var filters [][]any
	if err := json.Unmarshal(outer[0], &filters); err != nil {
		return 0, false
	}
	for _, f := range filters {
		if len(f) == 3 && f[0] == "id" && f[1] == "=" {
			return toInt64(f[2]), true
		}
	}
	return 0, false
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64: // every number decoded from JSON into an `any`
		return int64(n)
	}
	return 0
}

func sortByID(recs []map[string]any) {
	sort.Slice(recs, func(i, j int) bool {
		return toInt64(recs[i]["id"]) < toInt64(recs[j]["id"])
	})
}
