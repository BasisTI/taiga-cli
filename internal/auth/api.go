package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/BasisTI/taiga-cli/internal/output"
)

type LoginResult struct {
	AuthToken string `json:"auth_token"`
	Refresh   string `json:"refresh"`
	UserID    int64  `json:"id"`
	Username  string `json:"username"`
}

func post(ctx context.Context, hc *http.Client, url string, payload any) (int, []byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Never follow redirects: a 307/308 would resend the password or refresh token elsewhere.
	noRedirect := *hc
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noRedirect.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, body, err
}

func netErr(stage string, err error) error {
	return &output.Error{Code: "network_error", Source: "network", Stage: stage, Cause: err.Error(), Recovery: "check connectivity to the Taiga URL (sandboxed agents need network access)", Exit: output.ExitNetwork}
}

// Auth responses are never copied into errors: a server that echoes the request
// would otherwise put the password or the refresh token in the envelope.

// Login exchanges username/password for tokens.
func Login(ctx context.Context, hc *http.Client, baseURL, username string, password []byte) (LoginResult, error) {
	var res LoginResult
	status, body, err := post(ctx, hc, baseURL+"/api/v1/auth", map[string]string{"type": "normal", "username": username, "password": string(password)})
	if err != nil {
		return res, netErr("login", err)
	}
	if status == 400 || status == 401 {
		return res, &output.Error{Code: "auth_invalid_credentials", Source: "api", Stage: "login", Cause: fmt.Sprintf("the server rejected the login (HTTP %d)", status), Recovery: "check the username and the secret source", Exit: output.ExitAuth}
	}
	if status != 200 {
		return res, &output.Error{Code: "server_error", Source: "api", Stage: "login", Cause: fmt.Sprintf("unexpected HTTP %d from the login endpoint", status), Exit: output.ExitNetwork}
	}
	if err := json.Unmarshal(body, &res); err != nil || res.AuthToken == "" {
		return res, &output.Error{Code: "server_error", Source: "api", Stage: "login", Cause: "unexpected login response", Exit: output.ExitNetwork}
	}
	return res, nil
}

// RefreshToken renews the session; Taiga may rotate the refresh token.
func RefreshToken(ctx context.Context, hc *http.Client, baseURL, refresh string) (string, string, error) {
	status, body, err := post(ctx, hc, baseURL+"/api/v1/auth/refresh", map[string]string{"refresh": refresh})
	if err != nil {
		return "", "", netErr("refresh", err)
	}
	if status == 400 || status == 401 {
		return "", "", &output.Error{Code: "session_expired", Source: "session_cache", Stage: "refresh", Cause: fmt.Sprintf("the server rejected the refresh token (HTTP %d)", status), Recovery: "run `taiga auth login` (or `taiga auth refresh` outside the sandbox)", Exit: output.ExitAuth}
	}
	var out struct {
		AuthToken string `json:"auth_token"`
		Refresh   string `json:"refresh"`
	}
	if status != 200 {
		return "", "", &output.Error{Code: "server_error", Source: "api", Stage: "refresh", Cause: fmt.Sprintf("unexpected HTTP %d from the refresh endpoint", status), Exit: output.ExitNetwork}
	}
	if json.Unmarshal(body, &out) != nil || out.AuthToken == "" {
		return "", "", &output.Error{Code: "server_error", Source: "api", Stage: "refresh", Cause: "unexpected refresh response", Exit: output.ExitNetwork}
	}
	if out.Refresh == "" {
		out.Refresh = refresh
	}
	return out.AuthToken, out.Refresh, nil
}
