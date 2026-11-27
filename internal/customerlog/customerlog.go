// Package customerlog writes the customer's log line from this service: one JSON line per tool call, read by the org in
// Monitoramento > Logs.
//
// Same schema as the agent-runtime's, agent-base-tools' and agent-mcp-google's customer_log (the reader selects
// {audience="org", org_id="<org>"} and reads kind/status/code/origin/request_id): the line goes to stdout as JSON and
// Alloy promotes audience and org_id to Loki labels.
//
// Hard rules, kept here and not in the callers:
//
//   - No org, no customer line. A line without a resolved org_id leaves with audience="internal" and no org_id field, so
//     it can never match an org's selector. The org is a LABEL only: it comes from the platform's X-Org-Id header (sent
//     by the agent runtime to platform servers), never authorises anything, and must look like an identifier.
//   - Metadata only. No tool arguments, no tool output, no error text: only the tool's name, status, duration and a code.
//   - Best-effort. A failure writing the line is swallowed: logging is diagnostics, never a dependency of the call.
package customerlog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// OrgHeader carries the org the runtime is serving; ConversationHeader the (unsigned) conversation id, used only
	// to group lines.
	OrgHeader          = "X-Org-Id"
	ConversationHeader = "X-Conversation-Id"

	origin      = "feegow-mcp"
	unknownTool = "unknown_tool"

	// Codes that the org can act on: the tool call's own line says why the integration refused.
	CodeAuthFailed    = "auth_failed"
	CodeQuotaExceeded = "quota_exceeded"
	CodeToolError     = "tool_error"
	CodeCancelled     = "cancelled"
)

// idRe: an org, a conversation and a tool name enter the line as they are, so they must look like identifiers.
var idRe = regexp.MustCompile(`^[A-Za-z0-9_.:\-]{1,64}$`)

type ctxKey int

const (
	orgKey ctxKey = iota
	conversationKey
)

var (
	outMu sync.Mutex
	out   io.Writer = os.Stdout
)

// SetWriter redirects the lines (stdout by default) and returns the function that restores the previous writer. It
// exists for tests that read what the org would read.
func SetWriter(w io.Writer) (restore func()) {
	outMu.Lock()
	defer outMu.Unlock()
	prev := out
	out = w
	return func() {
		outMu.Lock()
		defer outMu.Unlock()
		out = prev
	}
}

func ident(v string) string {
	v = strings.TrimSpace(v)
	if idRe.MatchString(v) {
		return v
	}
	return ""
}

// Middleware records, on each request's own context, the org and the conversation the headers name (id-shaped values
// only). It always sets both, so nothing carried by an earlier context can name an org for a request without the
// header. It never rejects a request. Exactly one header line counts: repeated lines (a proxy that appends) name no org.
//
// It sits OUTSIDE the bearer check: a request the check refuses with 401 never reaches a tool. That refusal leaves one
// auth_failed line, but always INTERNAL (no org_id): the request carried no credential, so its X-Org-Id is unproven and
// must never write into an org's Monitoramento (anyone reaching the service could forge or flood another org's logs).
// The org's own credential refusals are the tool calls' lines, which carry the org.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org, conv := single(r.Header, OrgHeader), single(r.Header, ConversationHeader)
		ctx := context.WithValue(r.Context(), orgKey, org)
		ctx = context.WithValue(ctx, conversationKey, conv)
		rec := &statusRecorder{ResponseWriter: w}
		started := time.Now()
		next.ServeHTTP(rec, r.WithContext(ctx))
		if rec.status == http.StatusUnauthorized {
			write(buildLine("", conv, edgeOperation, "erro", CodeAuthFailed, time.Since(started).Milliseconds()))
		}
	})
}

// edgeOperation names a refusal that happened before any tool was chosen.
const edgeOperation = "mcp_request"

// statusRecorder keeps the first status written; Flush is passed through so streamed (SSE) responses still stream.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func single(h http.Header, name string) string {
	vs := h.Values(name)
	if len(vs) != 1 {
		return ""
	}
	return ident(vs[0])
}

func fromContext(ctx context.Context, key ctxKey) string {
	v, _ := ctx.Value(key).(string)
	return v
}

// Line is the customer line's fields.
func buildLine(org, conversation, tool, status, code string, durationMS int64) map[string]any {
	name := tool
	if ident(name) == "" {
		name = unknownTool
	}
	audience := "internal"
	if org != "" {
		audience = "org"
	}
	level, message := "INFO", ""
	if status == "erro" {
		level = "ERROR"
		message = "tool " + name + " failed (" + code + ")"
	}
	line := map[string]any{"level": level, "audience": audience}
	if org != "" {
		line["org_id"] = org
	}
	line["kind"] = "tool"
	line["status"] = status
	line["operation"] = name
	line["origin"] = origin
	line["code"] = nil
	if code != "" {
		line["code"] = code
	}
	line["message"] = message
	line["duration_ms"] = durationMS
	line["request_id"] = nil
	line["step"] = nil
	line["request"] = nil
	line["response"] = nil
	line["group_key"] = nil
	if conversation != "" {
		line["group_key"] = conversation
	}
	line["retry_of"] = nil
	return line
}

func write(line map[string]any) {
	defer func() { _ = recover() }() // best-effort: never break the call being logged
	raw, err := json.Marshal(line)
	if err != nil {
		return
	}
	outMu.Lock()
	defer outMu.Unlock()
	_, _ = out.Write(append(raw, '\n'))
}

// ReceivingMiddleware leaves ONE customer line per tools/call: the tool's name, status, duration and conversation,
// and for an integration refusal the code (auth_failed, quota_exceeded). It reads the outcome from the result's
// IsError flag and classifies from the error text, which is never logged.
func ReceivingMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method != "tools/call" {
			return next(ctx, method, req)
		}
		started := time.Now()
		name := toolName(req)
		// A panic in the tool still leaves its line (the org most wants to see that one); the panic goes on.
		defer func() {
			if rec := recover(); rec != nil {
				write(buildLine(
					fromContext(ctx, orgKey), fromContext(ctx, conversationKey), name,
					"erro", CodeToolError, time.Since(started).Milliseconds(),
				))
				panic(rec)
			}
		}()
		res, err := next(ctx, method, req)
		status, code := "ok", ""
		switch {
		case ctx.Err() != nil:
			// the caller gave up (cancel or deadline): not the integration's failure
			status, code = "aviso", CodeCancelled
		case err != nil:
			status, code = "erro", CodeToolError
		default:
			if r, ok := res.(*mcp.CallToolResult); ok && r != nil && r.IsError {
				status, code = "erro", classify(textOf(r))
			}
		}
		write(buildLine(
			fromContext(ctx, orgKey), fromContext(ctx, conversationKey), name,
			status, code, time.Since(started).Milliseconds(),
		))
		return res, err
	}
}

func toolName(req mcp.Request) string {
	if r, ok := req.(*mcp.CallToolRequest); ok && r != nil && r.Params != nil {
		return r.Params.Name
	}
	return ""
}

func textOf(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// classify maps the integration's refusal to a code from OUR OWN error texts (feegow/errors.go and the auth
// middleware), never from Feegow's body: the 403 "credencial INATIVA" and 401 "chave de API ausente" of
// feegow.CredentialError, the expired-credential refusal, and the (undocumented) HTTP 429 of UnexpectedStatusError are
// the org-actionable ones; everything else is a plain tool error.
func classify(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "status http 429"):
		return CodeQuotaExceeded
	case strings.Contains(lower, "credencial da clínica está inativa"),
		strings.Contains(lower, "chave de api ausente"),
		strings.Contains(lower, "credencial da clínica expirada"):
		return CodeAuthFailed
	}
	return CodeToolError
}
