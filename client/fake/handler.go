package fake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// jsonRPCRequest mirrors client.JSONRPCRequest. Duplicated rather than
// imported to keep this package free of an import cycle back into client,
// which imports nothing from here but is the package under test.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      string          `json:"id"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    *struct {
		Reason string `json:"reason"`
		Error  int    `json:"error"`
	} `json:"data,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
	ID      string          `json:"id"`
}

// errNotFound mirrors the shape the client's isNotFoundError matches on, so a
// missing record surfaces as "not found" rather than as an opaque failure.
func errNotFound(what string) *jsonRPCError {
	return &jsonRPCError{
		Code:    -32001,
		Message: fmt.Sprintf("[ENOENT] %s does not exist", what),
		Data: &struct {
			Reason string `json:"reason"`
			Error  int    `json:"error"`
		}{Reason: fmt.Sprintf("[ENOENT] %s does not exist", what), Error: 2},
	}
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var req jsonRPCRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			continue
		}

		s.mu.Lock()
		s.calls = append(s.calls, req.Method)
		isJob := s.jobs[req.Method]
		s.mu.Unlock()

		result, rpcErr := s.dispatch(req)

		// A job-flagged method returns a job ID inline and reports completion
		// out of band, so a client that treats it as synchronous reads the ID
		// as if it were the result — the exact confusion the real middleware
		// produces.
		if isJob && rpcErr == nil {
			s.writeJob(conn, req, result)
			continue
		}
		s.write(conn, jsonRPCResponse{JSONRPC: "2.0", Result: result, Error: rpcErr, ID: req.ID})
	}
}

func (s *Server) write(conn *websocket.Conn, resp jsonRPCResponse) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = conn.WriteJSON(resp)
}

// writeJob answers with a job ID, then pushes a core.get_jobs collection
// update carrying the real result — the protocol the JobPoller expects.
func (s *Server) writeJob(conn *websocket.Conn, req jsonRPCRequest, result json.RawMessage) {
	s.mu.Lock()
	s.nextID["__jobs"]++
	jobID := s.nextID["__jobs"]
	s.mu.Unlock()

	s.write(conn, jsonRPCResponse{
		JSONRPC: "2.0",
		Result:  json.RawMessage(fmt.Sprintf("%d", jobID)),
		ID:      req.ID,
	})

	go func() {
		// A small delay so the client has registered its subscription. Racing
		// the event ahead of the caller is not a scenario the real middleware
		// produces, and emulating it would only make tests flaky.
		time.Sleep(10 * time.Millisecond)
		s.writeMu.Lock()
		defer s.writeMu.Unlock()
		_ = conn.WriteJSON(map[string]any{
			"jsonrpc": "2.0",
			"method":  "collection_update",
			"params": map[string]any{
				"msg":        "changed",
				"collection": "core.get_jobs",
				"id":         jobID,
				"fields": map[string]any{
					"id":     jobID,
					"state":  "SUCCESS",
					"result": json.RawMessage(result),
				},
			},
		})
	}()
}

func (s *Server) dispatch(req jsonRPCRequest) (json.RawMessage, *jsonRPCError) {
	switch req.Method {
	case "auth.login_ex":
		return json.RawMessage(`{"response_type":"SUCCESS"}`), nil
	case "core.subscribe":
		return json.RawMessage(`true`), nil
	case "system.version":
		b, _ := json.Marshal(s.version)
		return b, nil
	case "core.ping":
		return json.RawMessage(`"pong"`), nil
	}

	namespace, verb := splitMethod(req.Method)
	switch verb {
	case "create":
		return s.doCreate(namespace, req.Params)
	case "query":
		return s.doQuery(namespace, req.Params)
	case "get_instance":
		return s.doGetInstance(namespace, req.Params)
	case "update":
		return s.doUpdate(namespace, req.Params)
	case "delete":
		return s.doDelete(namespace, req.Params)
	}

	// A method explicitly registered via WithJobMethods but matching no CRUD
	// verb — filesystem.setacl and service.control are the real examples — is
	// treated as an opaque operation that succeeds. Registering it is the
	// caller stating it exists; refusing it here would make the job protocol
	// testable only for namespaces that happen to be CRUD-shaped, which is
	// exactly the set that needs it least.
	s.mu.Lock()
	registered := s.jobs[req.Method]
	s.mu.Unlock()
	if registered {
		return json.RawMessage(`true`), nil
	}

	return nil, &jsonRPCError{
		Code:    -32601,
		Message: fmt.Sprintf("Method %q not found", req.Method),
	}
}

// firstParam unwraps the positional params array TrueNAS uses. A create takes
// [payload]; an update takes [id, payload]; a delete takes [id].
func firstParam(raw json.RawMessage) (json.RawMessage, []json.RawMessage) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) == 0 {
		// Some callers pass a bare value rather than a one-element array.
		return raw, nil
	}
	return arr[0], arr
}

func (s *Server) doCreate(namespace string, params json.RawMessage) (json.RawMessage, *jsonRPCError) {
	first, _ := firstParam(params)
	var payload map[string]any
	if err := json.Unmarshal(first, &payload); err != nil {
		return nil, &jsonRPCError{Code: -32602, Message: "invalid create payload"}
	}

	s.mu.Lock()
	if hook := s.hooks[namespace]; hook != nil {
		payload = hook(payload)
	}
	id := s.insertLocked(namespace, payload)
	rec := copyMap(s.records[namespace][id])
	s.mu.Unlock()

	b, _ := json.Marshal(rec)
	return b, nil
}

func (s *Server) doQuery(namespace string, params json.RawMessage) (json.RawMessage, *jsonRPCError) {
	s.mu.Lock()
	defer s.mu.Unlock()

	wantID, hasFilter := idFilter(params)
	out := []map[string]any{}
	for _, rec := range s.records[namespace] {
		if hasFilter && toInt64(rec["id"]) != wantID {
			continue
		}
		out = append(out, copyMap(rec))
	}
	// Sorted by id so query results are deterministic; map iteration is not.
	sortByID(out)
	b, _ := json.Marshal(out)
	return b, nil
}

func (s *Server) doGetInstance(namespace string, params json.RawMessage) (json.RawMessage, *jsonRPCError) {
	first, _ := firstParam(params)
	var id int64
	if err := json.Unmarshal(first, &id); err != nil {
		return nil, &jsonRPCError{Code: -32602, Message: "invalid id"}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[namespace][id]
	if !ok {
		return nil, errNotFound(fmt.Sprintf("%s %d", namespace, id))
	}
	b, _ := json.Marshal(copyMap(rec))
	return b, nil
}

func (s *Server) doUpdate(namespace string, params json.RawMessage) (json.RawMessage, *jsonRPCError) {
	_, arr := firstParam(params)
	if len(arr) < 2 {
		return nil, &jsonRPCError{Code: -32602, Message: "update expects [id, payload]"}
	}
	var id int64
	if err := json.Unmarshal(arr[0], &id); err != nil {
		return nil, &jsonRPCError{Code: -32602, Message: "invalid id"}
	}
	var patch map[string]any
	if err := json.Unmarshal(arr[1], &patch); err != nil {
		return nil, &jsonRPCError{Code: -32602, Message: "invalid update payload"}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[namespace][id]
	if !ok {
		return nil, errNotFound(fmt.Sprintf("%s %d", namespace, id))
	}
	// Normalization applies to updates too. A server that canonicalizes on
	// create but not on update would be a strictly easier target than the real
	// one, which is the wrong direction for a test double to be wrong in.
	if hook := s.hooks[namespace]; hook != nil {
		patch = hook(patch)
	}
	for k, v := range patch {
		if k == "id" {
			continue // ids are server-owned
		}
		rec[k] = v
	}
	b, _ := json.Marshal(copyMap(rec))
	return b, nil
}

func (s *Server) doDelete(namespace string, params json.RawMessage) (json.RawMessage, *jsonRPCError) {
	first, _ := firstParam(params)
	var id int64
	if err := json.Unmarshal(first, &id); err != nil {
		return nil, &jsonRPCError{Code: -32602, Message: "invalid id"}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[namespace][id]; !ok {
		return nil, errNotFound(fmt.Sprintf("%s %d", namespace, id))
	}
	delete(s.records[namespace], id)
	return json.RawMessage(`true`), nil
}
