package trace

import "strings"

// ParseClientRequest reads only named protocol fields. Account/user/cache keys
// are not task identities. Native identifiers are digested before persistence.
func ParseClientRequest(path string, raw []byte, headers map[string][]string) Features {
	f := ParseRequest(path, raw)
	if !f.Valid {
		return f
	}
	header := func(name string) string {
		for k, values := range headers {
			if strings.EqualFold(k, name) && len(values) == 1 && len(values[0]) <= 16384 {
				return values[0]
			}
		}
		return ""
	}
	identity := func(client, session, turn string) {
		if session == "" || len(session) > 1024 || len(turn) > 1024 {
			return
		}
		f.Client = client
		f.NativeSession = digest([]string{client, session})
		if turn != "" {
			f.NativeTurn = digest([]string{client, session, turn})
		}
	}
	if f.Format == "openai-responses" {
		var doc map[string]any
		_ = decodeJSON(raw, &doc)
		metadata := object(doc["client_metadata"])
		// Current Codex sends canonical metadata per request, including WebSocket
		// calls. Prefer it over connection headers, which can outlive a turn.
		blob := metadata["x-codex-turn-metadata"]
		if blob == nil {
			if value := header("x-codex-turn-metadata"); value != "" {
				blob = value
			}
		}
		m := object(blob)
		if value := text(blob); value != "" && len(value) <= 16384 {
			if decodeJSON([]byte(value), &m) != nil {
				m = nil
			}
		}
		if blob == nil && text(metadata["thread_id"]) != "" {
			// Earlier Codex versions emit only the flat compatibility fields.
			m = metadata
		}
		session := text(m["thread_id"])
		if session == "" {
			session = text(m["session_id"])
		}
		identity("codex", session, text(m["turn_id"]))
		if f.NativeSession == "" {
			// thread-id is actual thread identity; session-id can be overridden
			// for cache affinity. Do not interpret request IDs or cache keys.
			identity("codex", header("thread-id"), "")
		}
	}
	if f.NativeSession == "" {
		identity("opencode", header("x-opencode-session"), "")
	}
	if f.NativeSession == "" {
		// Pi and compatible providers may expose these session affinity headers.
		// They partition sessions only; they never directly join task traces.
		for _, name := range []string{"x-session-id", "x-session-affinity"} {
			if value := header(name); value != "" {
				identity("session-affinity", value, "")
				break
			}
		}
	}
	return f
}
