// Package platform implements the ApexVoid Enterprise external-service v1 wire contract.
// This package deliberately has no dependency on the Enterprise source code or its database.
package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const AssertionHeader = "X-ApexVoid-Identity-Assertion"
const GatewayHeader = "X-ApexVoid-Gateway"
const ApplicationDisplayNameHeader = "X-ApexVoid-Application-Display-Name"

type Decision struct {
	UserID      string `json:"user_id"`
	WorkspaceID string `json:"workspace_id"`
	Permission  string `json:"permission"`
	Allowed     bool   `json:"allowed"`
}
type Client struct {
	base       string
	appID      string
	credential string
	http       *http.Client
}

func New(base, appID, credential string) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid enterprise endpoint")
	}
	if appID == "" {
		return nil, errors.New("missing application ID")
	}
	return &Client{base: strings.TrimRight(base, "/"), appID: appID, credential: credential, http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Ready() bool { return c.credential != "" }
func (c *Client) Introspect(ctx context.Context, assertion, permission string) (Decision, error) {
	if assertion == "" || permission == "" || !c.Ready() {
		return Decision{}, errors.New("authorization unavailable")
	}
	body, err := json.Marshal(map[string]string{"identity_assertion": assertion, "permission": permission})
	if err != nil {
		return Decision{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/integrations/v1/session/introspect", bytes.NewReader(body))
	if err != nil {
		return Decision{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-ApexVoid-Application-ID", c.appID)
	req.Header.Set("X-ApexVoid-Service-Credential", c.credential)
	resp, err := c.http.Do(req)
	if err != nil {
		return Decision{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return Decision{}, fmt.Errorf("platform authorization denied: HTTP %d", resp.StatusCode)
	}
	var decision Decision
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&decision); err != nil {
		return Decision{}, err
	}
	if decision.Permission != permission || decision.UserID == "" || decision.WorkspaceID == "" {
		return Decision{}, errors.New("invalid introspection decision")
	}
	return decision, nil
}
