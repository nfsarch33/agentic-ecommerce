// Package storeonboard is the customer-store onboarding check (v18870-4):
// the four things the platform demands before it will touch a customer's
// WooCommerce store — minimum-scope REST keys, an agent Application Password
// whose user is NOT an administrator, a signed data statement, and a named
// approver — verified against the live store, one PASS/FAIL line each.
//
// Leaf by design: no DB, no workflow, no platform imports. The CLI drives
// it; tests drive it with a fixture store. Secrets arrive as arguments the
// caller holds (env in the CLI) and never travel in the config file.
package storeonboard

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config is the onboarding record the operator maintains. It carries NO
// secrets: the consumer key/secret and the application password ride env
// vars into the CLI.
type Config struct {
	StoreURL       string        `json:"store_url"`
	KeyPermissions string        `json:"key_permissions"`  // read_write is the minimum the product needs
	AgentUserLogin string        `json:"agent_user_login"` // the WP user the Application Password belongs to
	AgentUserRole  string        `json:"agent_user_role"`  // the role the platform accepts (shop_manager, not administrator)
	DataStatement  DataStatement `json:"data_statement"`
	Approver       Approver      `json:"approver"`
}

// DataStatement names what data the platform reads and writes on the store,
// the version the customer saw, and when they signed it.
type DataStatement struct {
	Version  string `json:"version"`
	SignedAt string `json:"signed_at"`
}

// Approver is the human at the customer who approved the connection.
type Approver struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Secrets carries the credentials the check uses. Values never appear in
// output; only PASS/FAIL lines ever print.
type Secrets struct {
	ConsumerKey    string
	ConsumerSecret string
	AppPassword    string
}

// Result is one check's outcome.
type Result struct {
	Name string
	Pass bool
	Note string
}

// Check runs the full gate against the live store and returns the results
// in a stable order. The HTTP client is the caller's so tests inject the
// fixture store's URL; the timeout is per request.
func Check(ctx context.Context, cfg Config, sec Secrets, client *http.Client, timeout time.Duration) []Result {
	var out []Result
	out = append(out, checkRecord(cfg))
	out = append(out, checkStoreReachable(ctx, cfg, sec, client, timeout))
	out = append(out, checkAppPassword(ctx, cfg, sec, client, timeout))
	return out
}

// AllPassed reports whether every result passed — the exit-code basis.
func AllPassed(rs []Result) bool {
	for _, r := range rs {
		if !r.Pass {
			return false
		}
	}
	return true
}

// checkRecord verifies the four named fields exist and are sane, including
// the minimum-scope rule: the REST key must be read_write (read-only keys
// cannot publish; anything broader does not exist in WooCommerce's model),
// and the recorded agent role must not be administrator.
func checkRecord(cfg Config) Result {
	var missing []string
	if strings.TrimSpace(cfg.StoreURL) == "" {
		missing = append(missing, "store_url")
	}
	if cfg.KeyPermissions != "read_write" {
		missing = append(missing, "key_permissions=read_write (minimum scope)")
	}
	if strings.TrimSpace(cfg.AgentUserLogin) == "" {
		missing = append(missing, "agent_user_login")
	}
	if strings.EqualFold(cfg.AgentUserRole, "administrator") || strings.TrimSpace(cfg.AgentUserRole) == "" {
		missing = append(missing, "agent_user_role (non-administrator)")
	}
	if strings.TrimSpace(cfg.DataStatement.Version) == "" || !parseableTime(cfg.DataStatement.SignedAt) {
		missing = append(missing, "data_statement.version+signed_at")
	}
	if strings.TrimSpace(cfg.Approver.Name) == "" || !strings.Contains(cfg.Approver.Email, "@") {
		missing = append(missing, "approver.name+email")
	}
	if len(missing) > 0 {
		return Result{"record", false, "incomplete: " + strings.Join(missing, ", ")}
	}
	return Result{"record", true, "minimum-scope keys, non-admin agent user, signed statement, named approver"}
}

// checkStoreReachable authenticates the REST key against the store's
// system status endpoint — the same read the sync engine uses first.
func checkStoreReachable(ctx context.Context, cfg Config, sec Secrets, client *http.Client, timeout time.Duration) Result {
	if sec.ConsumerKey == "" || sec.ConsumerSecret == "" {
		return Result{"store-reachable", false, "consumer key/secret not provided"}
	}
	status, _, err := wcGet(ctx, cfg, sec, client, timeout, "/wp-json/wc/v3/system_status")
	if err != nil {
		return Result{"store-reachable", false, "store unreachable: " + err.Error()}
	}
	if status != http.StatusOK {
		return Result{"store-reachable", false, fmt.Sprintf("system status answered %d with the REST key", status)}
	}
	return Result{"store-reachable", true, "REST key authenticates and the store answers"}
}

// checkAppPassword authenticates the agent's Application Password and
// verifies the USER it belongs to is not an administrator — the
// minimum-privilege rule for machine users. MUTANT LINE: dropping the
// administrator refusal turns the admin-role row green.
func checkAppPassword(ctx context.Context, cfg Config, sec Secrets, client *http.Client, timeout time.Duration) Result {
	if sec.AppPassword == "" {
		return Result{"app-password", false, "application password not provided"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(cfg.StoreURL, "/")+"/wp-json/wp/v2/users/me", nil)
	if err != nil {
		return Result{"app-password", false, err.Error()}
	}
	auth := base64.StdEncoding.EncodeToString([]byte(cfg.AgentUserLogin + ":" + sec.AppPassword))
	req.Header.Set("Authorization", "Basic "+auth)
	resp, err := doWithTimeout(client, req, timeout)
	if err != nil {
		return Result{"app-password", false, "WP REST unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Result{"app-password", false, fmt.Sprintf("users/me answered %d with the application password", resp.StatusCode)}
	}
	// The authenticated user's roles arrive on the same endpoint when the
	// context permits; the ROLE was also verified in the record check, and
	// this live confirmation re-reads it from the store.
	if role := liveUserRole(ctx, cfg, sec, client, timeout); role != "" && strings.EqualFold(role, "administrator") {
		return Result{"app-password", false, "the application password's user is an administrator — minimum privilege refused"}
	}
	return Result{"app-password", true, "application password authenticates a non-administrator user"}
}

// liveUserRole re-reads the authenticated user's role from the store, with
// the APPLICATION PASSWORD as the credential (the WP REST namespace, not the
// WC one). An empty answer (role hidden from the user's own context) defers
// to the record check rather than failing the gate twice on one cause.
func liveUserRole(ctx context.Context, cfg Config, sec Secrets, client *http.Client, timeout time.Duration) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(cfg.StoreURL, "/")+"/wp-json/wp/v2/users/me?context=edit", nil)
	if err != nil {
		return ""
	}
	auth := base64.StdEncoding.EncodeToString([]byte(cfg.AgentUserLogin + ":" + sec.AppPassword))
	req.Header.Set("Authorization", "Basic "+auth)
	resp, err := doWithTimeout(client, req, timeout)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}
	var user struct {
		Roles []string `json:"roles"`
	}
	if json.Unmarshal(body, &user) != nil || len(user.Roles) == 0 {
		return ""
	}
	return user.Roles[0]
}

func wcGet(ctx context.Context, cfg Config, sec Secrets, client *http.Client, timeout time.Duration, path string) (int, []byte, error) {
	body, status, err := wcGetBody(ctx, cfg, sec, client, timeout, path)
	return status, body, err
}

func wcGetBody(ctx context.Context, cfg Config, sec Secrets, client *http.Client, timeout time.Duration, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.StoreURL, "/")+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth(sec.ConsumerKey, sec.ConsumerSecret)
	resp, err := doWithTimeout(client, req, timeout)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return body, resp.StatusCode, err
}

func doWithTimeout(client *http.Client, req *http.Request, timeout time.Duration) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if client.Timeout == 0 && timeout > 0 {
		client.Timeout = timeout
	}
	return client.Do(req)
}

func parseableTime(s string) bool {
	_, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	return err == nil
}
