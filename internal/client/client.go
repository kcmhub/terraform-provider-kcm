// Package client calls the KCM as code API (/api/v1/iac) of a KCM instance.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// APIVersion of the documents.
const APIVersion = "kcm.kcmhub.io/v1"

// Document is a declared object: the same shape as the YAML documents of KCM as code.
type Document struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Metadata   Metadata               `json:"metadata"`
	Spec       map[string]interface{} `json:"spec"`
	// Status is set by KCM on the read of one object (never sent): the uids of the object and of what it belongs to.
	Status *Status `json:"status,omitempty"`
}

// Status holds the stable uids of an object, outside its spec so that a declaration stays portable: UID is the uid
// of the object itself (environments, Kafka clusters, connections), ClusterUID and EnvironmentUID those of what it
// belongs to.
type Status struct {
	UID            string `json:"uid,omitempty"`
	EnvironmentUID string `json:"environmentUid,omitempty"`
	ClusterUID     string `json:"clusterUid,omitempty"`
}

// Metadata names the object within its kind; ManagedBy is set by KCM on read.
type Metadata struct {
	Name      string `json:"name"`
	ManagedBy string `json:"managedBy,omitempty"`
}

// FieldChange is one changed field of a planned change; secrets show as "(sensitive)".
type FieldChange struct {
	Field  string  `json:"field"`
	Before *string `json:"before"`
	After  *string `json:"after"`
}

// Change is one change of a plan.
type Change struct {
	Action      string        `json:"action"`
	Kind        string        `json:"kind"`
	Name        string        `json:"name"`
	Detail      string        `json:"detail"`
	Destructive bool          `json:"destructive"`
	Diff        []FieldChange `json:"diff"`
}

// Plan is the result of a plan or an apply.
type Plan struct {
	ManagedBy          string   `json:"managedBy"`
	Applied            bool     `json:"applied"`
	PlanID             string   `json:"planId"`
	DestructiveChanges int      `json:"destructiveChanges"`
	Changes            []Change `json:"changes"`
	Errors             []string `json:"errors"`
}

// HasChanges tells whether the plan changes something (anything but NOOP).
func (p *Plan) HasChanges() bool {
	for _, c := range p.Changes {
		if c.Action != "NOOP" {
			return true
		}
	}
	return false
}

// Client of one KCM instance.
type Client struct {
	Endpoint   string
	Token      string
	Source     string
	HTTPClient *http.Client
	UserAgent  string
}

// APIError is an error answered by KCM.
type APIError struct {
	Status    int
	ErrorCode string
	Message   string
}

func (e *APIError) Error() string {
	if e.ErrorCode != "" {
		return fmt.Sprintf("KCM answered %d (%s): %s", e.Status, e.ErrorCode, e.Message)
	}
	return fmt.Sprintf("KCM answered %d: %s", e.Status, e.Message)
}

// IsNotFound tells whether err is a 404 of KCM.
func IsNotFound(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.Status == http.StatusNotFound
}

// IsUnreachable tells whether KCM could not reach the system behind the object (KCM-1200: Kafka connection error).
func IsUnreachable(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.ErrorCode == "KCM-1200"
}

// New returns a client; endpoint is the base URL of KCM (https://kcm.example.com).
func New(endpoint, token, source string) *Client {
	return &Client{
		Endpoint:   strings.TrimRight(endpoint, "/"),
		Token:      token,
		Source:     source,
		HTTPClient: &http.Client{Timeout: 2 * time.Minute},
		UserAgent:  "terraform-provider-kcm",
	}
}

func (c *Client) objectPath(kind, name string) string {
	return "/api/v1/iac/objects/" + url.PathEscape(kind) + "/" + url.PathEscape(name)
}

// Get reads one object, managed or not.
func (c *Client) Get(ctx context.Context, kind, name string) (*Document, error) {
	var doc Document
	if err := c.do(ctx, http.MethodGet, c.objectPath(kind, name), nil, nil, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Put declares one object of the source (applies it, or only plans it with dryRun).
func (c *Client) Put(ctx context.Context, doc *Document, allowDestroy, dryRun bool) (*Plan, error) {
	query := url.Values{}
	query.Set("source", c.Source)
	query.Set("allowDestroy", strconv.FormatBool(allowDestroy))
	query.Set("dryRun", strconv.FormatBool(dryRun))
	var plan Plan
	if err := c.do(ctx, http.MethodPut, c.objectPath(doc.Kind, doc.Metadata.Name), query, doc, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// Delete removes one managed object of the source: deleted with deletionPolicy Delete and allowDestroy, otherwise
// kept and no longer managed.
func (c *Client) Delete(ctx context.Context, kind, name string, allowDestroy bool) (*Plan, error) {
	query := url.Values{}
	query.Set("source", c.Source)
	query.Set("allowDestroy", strconv.FormatBool(allowDestroy))
	var plan Plan
	if err := c.do(ctx, http.MethodDelete, c.objectPath(kind, name), query, nil, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// PlanDocuments plans documents of the source without pruning (a read operation: nothing changes, not audited as a
// change).
func (c *Client) PlanDocuments(ctx context.Context, docs []*Document) (*Plan, error) {
	query := url.Values{}
	query.Set("source", c.Source)
	query.Set("prune", "false")
	var plan Plan
	if err := c.do(ctx, http.MethodPost, "/api/v1/iac/plan", query, docs, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// Environment is a KCM group as the REST API of KCM lists it.
type Environment struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Color          string `json:"color"`
	EnvironmentUID string `json:"environmentUid"`
}

// Cluster is a Kafka cluster as the REST API of KCM lists it.
type Cluster struct {
	ID              int64  `json:"id"`
	ClusterUID      string `json:"clusterUid"`
	ClusterName     string `json:"clusterName"`
	KcmGroupID      int64  `json:"kcmGroupId"`
	BootstrapServer string `json:"bootstrapServer"`
}

// Environments lists the environments (KCM groups) the token may see.
func (c *Client) Environments(ctx context.Context) ([]Environment, error) {
	var environments []Environment
	if err := c.do(ctx, http.MethodGet, "/api/v1/groups", nil, nil, &environments); err != nil {
		return nil, err
	}
	return environments, nil
}

// Clusters lists the Kafka clusters of an environment the token may see.
func (c *Client) Clusters(ctx context.Context, environmentID int64) ([]Cluster, error) {
	var clusters []Cluster
	if err := c.do(ctx, http.MethodGet, "/api/v1/groups/"+strconv.FormatInt(environmentID, 10)+"/kafka-clusters", nil, nil, &clusters); err != nil {
		return nil, err
	}
	return clusters, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out interface{}) error {
	target := c.Endpoint + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling KCM: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode, Message: strings.TrimSpace(string(data))}
		var parsed struct {
			ErrorCode string `json:"errorCode"`
			Message   string `json:"message"`
		}
		if json.Unmarshal(data, &parsed) == nil && parsed.Message != "" {
			apiErr.ErrorCode = parsed.ErrorCode
			apiErr.Message = parsed.Message
		}
		return apiErr
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("unreadable answer of KCM: %w", err)
	}
	return nil
}
