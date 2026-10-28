// Minimal MCP server (JSON-RPC over stdio) for the Cloud beta.
// It never touches SQL and never sees a database password. Every tool
// calls the gateway HTTP API with the operator's JWT (MCP_TOKEN env).
// Tools: create_app, deploy_app, check_permissions, view_audit_log.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type toolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func gatewayCall(method, path string, body any) (string, error) {
	url := os.Getenv("GATEWAY_URL")
	if url == "" {
		url = "http://localhost:8080"
	}
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimSuffix(url, "/")+path, rdr)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := os.Getenv("MCP_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if cmp := os.Getenv("MCP_COMPANY_ID"); cmp != "" {
		req.Header.Set("X-Company-ID", cmp)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("gateway %d: %s", resp.StatusCode, string(b))
	}
	return string(b), nil
}

func callTool(name string, args map[string]any) (string, error) {
	switch name {
	case "create_app":
		return gatewayCall("POST", "/api/v1/apps", map[string]any{"name": args["name"], "repo": args["repo"]})
	case "deploy_app":
		id, _ := args["app_id"].(string)
		return gatewayCall("POST", "/api/v1/apps/"+id+"/deploy", map[string]any{})
	case "check_permissions":
		return gatewayCall("GET", "/api/v1/policy", nil)
	case "view_audit_log":
		return gatewayCall("GET", "/api/v1/audit?limit=50", nil)
	default:
		return "", fmt.Errorf("unknown tool %q (allowed: create_app, deploy_app, check_permissions, view_audit_log)", name)
	}
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "initialize":
			resp["result"] = map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "cloud-beta-mcp", "version": "0.1.0"},
			}
		case "tools/list":
			resp["result"] = map[string]any{"tools": []map[string]any{
				{"name": "create_app", "description": "Register an app in the workspace via the gateway", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}, "repo": map[string]any{"type": "string"}}}},
				{"name": "deploy_app", "description": "Deploy an app via the gateway: starts a real per-app container (template deploy)", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"app_id": map[string]any{"type": "string"}}}},
				{"name": "check_permissions", "description": "Return the active tenant policy", "inputSchema": map[string]any{"type": "object"}},
				{"name": "view_audit_log", "description": "Return recent audit rows via the gateway", "inputSchema": map[string]any{"type": "object"}},
			}}
		case "tools/call":
			var tc toolCall
			_ = json.Unmarshal(req.Params, &tc)
			out, err := callTool(tc.Name, tc.Arguments)
			if err != nil {
				resp["result"] = map[string]any{"content": []map[string]any{{"type": "text", "text": "error: " + err.Error()}}, "isError": true}
			} else {
				resp["result"] = map[string]any{"content": []map[string]any{{"type": "text", "text": out}}}
			}
		case "notifications/initialized":
			continue
		default:
			resp["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		_ = enc.Encode(resp)
	}
}
