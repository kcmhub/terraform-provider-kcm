package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPutSendsTheDocumentWithTheSource(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	var gotDoc Document
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotAuth = r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotDoc)
		_, _ = w.Write([]byte(`{"managedBy":"iac:terraform:ws","applied":true,"planId":"abc","changes":[{"action":"CREATE","kind":"Topic","name":"orders"}]}`))
	}))
	defer server.Close()

	c := New(server.URL+"/", "t0k", "terraform:ws")
	plan, err := c.Put(context.Background(), &Document{APIVersion: APIVersion, Kind: "Topic", Metadata: Metadata{Name: "orders"},
		Spec: map[string]interface{}{"partitions": 3}}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/iac/objects/Topic/orders" {
		t.Errorf("path %s", gotPath)
	}
	if gotQuery != "allowDestroy=false&dryRun=false&source=terraform%3Aws" {
		t.Errorf("query %s", gotQuery)
	}
	if gotAuth != "Bearer t0k" {
		t.Errorf("authorization %s", gotAuth)
	}
	if gotDoc.Spec["partitions"].(float64) != 3 || gotDoc.APIVersion != APIVersion {
		t.Errorf("document %+v", gotDoc)
	}
	if !plan.Applied || !plan.HasChanges() || plan.PlanID != "abc" {
		t.Errorf("plan %+v", plan)
	}
}

func TestErrorsOfKcmAreReadable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status":404,"errorCode":"KCM-1004","message":"Topic orders not found"}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":400,"errorCode":"KCM-1600","message":"Team or Enterprise edition"}`))
	}))
	defer server.Close()
	c := New(server.URL, "t", "s")

	_, err := c.Get(context.Background(), "Topic", "orders")
	if !IsNotFound(err) {
		t.Errorf("expected a not found error, got %v", err)
	}
	_, err = c.Delete(context.Background(), "Topic", "orders", true)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.ErrorCode != "KCM-1600" || apiErr.Message != "Team or Enterprise edition" {
		t.Errorf("unexpected error %v", err)
	}
}

func TestPlanDocumentsDoesNotPrune(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/iac/plan" || r.URL.Query().Get("prune") != "false" || r.Method != http.MethodPost {
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"changes":[{"action":"NOOP"}]}`))
	}))
	defer server.Close()

	plan, err := New(server.URL, "t", "s").PlanDocuments(context.Background(), []*Document{{Kind: "TopicMetadata"}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.HasChanges() {
		t.Error("a NOOP plan has no change")
	}
}

func TestNamesArePathEscaped(t *testing.T) {
	c := New("https://kcm", "t", "s")
	if got := c.objectPath("RoleBinding", "a b/c"); got != "/api/v1/iac/objects/RoleBinding/a%20b%2Fc" {
		t.Errorf("path %s", got)
	}
}

func TestAKafkaConnectionErrorIsUnreachableNotMissing(t *testing.T) {
	err := error(&APIError{Status: 502, ErrorCode: "KCM-1200", Message: "the topics cannot be read from Kafka"})
	if !IsUnreachable(err) || IsNotFound(err) {
		t.Fatal("KCM-1200 is unreachable, not missing")
	}
	if IsUnreachable(&APIError{Status: 404, ErrorCode: "KCM-1004"}) {
		t.Fatal("a missing object is not unreachable")
	}
}
