// Copyright 2026 Jose Vazquez
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pdns

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateZone(t *testing.T) {
	srv, rec := capture(t, http.StatusCreated, "")
	c := NewClient(srv.URL, "secret", srv.Client())

	err := c.CreateZone(context.Background(), Zone{
		Name:        "example.org.",
		Kind:        "Native",
		Nameservers: []string{"ns1.example.org."},
	})
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	if rec.method != http.MethodPost {
		t.Errorf("method = %q, want POST", rec.method)
	}
	if rec.path != "/api/v1/servers/localhost/zones" {
		t.Errorf("path = %q", rec.path)
	}
	if rec.apiKey != "secret" {
		t.Errorf("X-API-Key = %q, want secret", rec.apiKey)
	}
	if rec.contentType != "application/json" {
		t.Errorf("Content-Type = %q", rec.contentType)
	}
	var got Zone
	if err := json.Unmarshal(rec.body, &got); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if got.Name != "example.org." || got.Kind != "Native" {
		t.Errorf("body zone = %+v", got)
	}
}

func TestCreateZoneConflictFallsBackToUpdate(t *testing.T) {
	var methods []string
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.Path)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error": "Conflict"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", srv.Client())
	if err := c.CreateZone(context.Background(), Zone{Name: "example.org.", Kind: "Native"}); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if len(methods) != 2 || methods[0] != http.MethodPost || methods[1] != http.MethodPut {
		t.Errorf("methods = %v, want [POST PUT]", methods)
	}
	if len(paths) != 2 || paths[1] != "/api/v1/servers/localhost/zones/example.org." {
		t.Errorf("paths = %v", paths)
	}
}

func TestUpdateZone(t *testing.T) {
	srv, rec := capture(t, http.StatusNoContent, "")
	c := NewClient(srv.URL, "k", srv.Client())

	err := c.UpdateZone(context.Background(), "example org..Native", Zone{Name: "example.org.", Kind: "Native"})
	if err != nil {
		t.Fatalf("UpdateZone: %v", err)
	}

	if rec.method != http.MethodPut {
		t.Errorf("method = %q, want PUT", rec.method)
	}
	if rec.path != "/api/v1/servers/localhost/zones/example%20org..Native" {
		t.Errorf("path = %q (zone ID not escaped)", rec.path)
	}
	var got Zone
	if err := json.Unmarshal(rec.body, &got); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if got.Kind != "Native" {
		t.Errorf("PUT body kind = %q, want Native", got.Kind)
	}
}

func TestUpdateZoneWithNameserversSendsPutAndPatch(t *testing.T) {
	type request struct {
		method string
		path   string
		body   []byte
	}
	var requests []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		requests = append(requests, request{method: r.Method, path: r.URL.EscapedPath(), body: b})
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", srv.Client())
	err := c.UpdateZone(context.Background(), "example.org.", Zone{
		Name:        "example.org.",
		Kind:        "Native",
		Nameservers: []string{"ns1.example.org.", "ns2.example.org."},
	})
	if err != nil {
		t.Fatalf("UpdateZone: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("got %d requests, want 2: %v", len(requests), requests)
	}
	if requests[0].method != http.MethodPut || requests[1].method != http.MethodPatch {
		t.Errorf("methods = [%s %s], want [PUT PATCH]", requests[0].method, requests[1].method)
	}
	wantPath := "/api/v1/servers/localhost/zones/example.org."
	if requests[0].path != wantPath || requests[1].path != wantPath {
		t.Errorf("paths = [%s %s], want both %s", requests[0].path, requests[1].path, wantPath)
	}

	var got rrsetsPayload
	if err := json.Unmarshal(requests[1].body, &got); err != nil {
		t.Fatalf("decode PATCH body: %v", err)
	}
	if len(got.RRsets) != 1 {
		t.Fatalf("rrsets = %+v, want 1 entry", got.RRsets)
	}
	rr := got.RRsets[0]
	if rr.Name != "example.org." || rr.Type != "NS" || rr.TTL != defaultNSTTL || rr.ChangeType != "REPLACE" {
		t.Errorf("rrset = %+v, want apex NS REPLACE with ttl %d", rr, defaultNSTTL)
	}
	if len(rr.Records) != 2 || rr.Records[0].Content != "ns1.example.org." || rr.Records[1].Content != "ns2.example.org." {
		t.Errorf("records = %+v, want ns1 and ns2", rr.Records)
	}
}

func TestUpdateZoneEmptyNameserversSendsReplaceWithNoRecords(t *testing.T) {
	var bodies []rrsetsPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			b, _ := io.ReadAll(r.Body)
			var p rrsetsPayload
			if err := json.Unmarshal(b, &p); err != nil {
				t.Errorf("decode PATCH body: %v", err)
			}
			bodies = append(bodies, p)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", srv.Client())
	if err := c.UpdateZone(context.Background(), "example.org.", Zone{Kind: "Native", Nameservers: []string{}}); err != nil {
		t.Fatalf("UpdateZone: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("got %d PATCH bodies, want 1", len(bodies))
	}
	if len(bodies[0].RRsets) != 1 || len(bodies[0].RRsets[0].Records) != 0 {
		t.Errorf("rrsets = %+v, want REPLACE with empty records", bodies[0])
	}
}

func TestReplaceRRset(t *testing.T) {
	srv, rec := capture(t, http.StatusNoContent, "")
	c := NewClient(srv.URL, "k", srv.Client())

	err := c.ReplaceRRset(context.Background(), "example.org.", RRset{
		Name:    "www.example.org.",
		Type:    "A",
		TTL:     300,
		Records: []Record{{Content: "192.0.2.1"}, {Content: "192.0.2.2", Disabled: true}},
	})
	if err != nil {
		t.Fatalf("ReplaceRRset: %v", err)
	}

	if rec.method != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", rec.method)
	}
	if rec.path != "/api/v1/servers/localhost/zones/example.org." {
		t.Errorf("path = %q", rec.path)
	}
	if rec.apiKey != "k" {
		t.Errorf("X-API-Key = %q, want k", rec.apiKey)
	}
	if rec.contentType != "application/json" {
		t.Errorf("Content-Type = %q", rec.contentType)
	}
	var got rrsetsPayload
	if err := json.Unmarshal(rec.body, &got); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if len(got.RRsets) != 1 {
		t.Fatalf("rrsets = %+v, want 1 entry", got.RRsets)
	}
	rr := got.RRsets[0]
	if rr.Name != "www.example.org." || rr.Type != "A" || rr.TTL != 300 {
		t.Errorf("rrset = %+v", rr)
	}
	if rr.ChangeType != "REPLACE" {
		t.Errorf("changetype = %q, want REPLACE", rr.ChangeType)
	}
	if len(rr.Records) != 2 || rr.Records[0].Content != "192.0.2.1" || !rr.Records[1].Disabled {
		t.Errorf("records = %+v", rr.Records)
	}
}

func TestReplaceRRsetOverwritesChangeType(t *testing.T) {
	var bodies []rrsetsPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var p rrsetsPayload
		if err := json.Unmarshal(b, &p); err != nil {
			t.Errorf("decode PATCH body: %v", err)
		}
		bodies = append(bodies, p)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", srv.Client())
	err := c.ReplaceRRset(context.Background(), "example.org.", RRset{
		Name:       "www.example.org.",
		Type:       "A",
		ChangeType: "DELETE",
	})
	if err != nil {
		t.Fatalf("ReplaceRRset: %v", err)
	}
	if len(bodies) != 1 || len(bodies[0].RRsets) != 1 || bodies[0].RRsets[0].ChangeType != "REPLACE" {
		t.Errorf("rrsets = %+v, want changetype REPLACE", bodies)
	}
}

func TestDeleteRRset(t *testing.T) {
	srv, rec := capture(t, http.StatusNoContent, "")
	c := NewClient(srv.URL, "k", srv.Client())

	err := c.DeleteRRset(context.Background(), "example.org.", "www.example.org.", "A")
	if err != nil {
		t.Fatalf("DeleteRRset: %v", err)
	}

	if rec.method != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", rec.method)
	}
	if rec.path != "/api/v1/servers/localhost/zones/example.org." {
		t.Errorf("path = %q", rec.path)
	}
	if rec.apiKey != "k" {
		t.Errorf("X-API-Key = %q, want k", rec.apiKey)
	}
	if rec.contentType != "application/json" {
		t.Errorf("Content-Type = %q", rec.contentType)
	}
	var got rrsetsPayload
	if err := json.Unmarshal(rec.body, &got); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if len(got.RRsets) != 1 {
		t.Fatalf("rrsets = %+v, want 1 entry", got.RRsets)
	}
	rr := got.RRsets[0]
	if rr.Name != "www.example.org." || rr.Type != "A" {
		t.Errorf("rrset = %+v", rr)
	}
	if rr.ChangeType != "DELETE" {
		t.Errorf("changetype = %q, want DELETE", rr.ChangeType)
	}
	if len(rr.Records) != 0 {
		t.Errorf("records = %+v, want empty for DELETE", rr.Records)
	}
}

func TestDeleteZone(t *testing.T) {
	srv, rec := capture(t, http.StatusNoContent, "")
	c := NewClient(srv.URL, "k", srv.Client())

	err := c.DeleteZone(context.Background(), "example.org.")
	if err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}
	if rec.method != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", rec.method)
	}
	if rec.path != "/api/v1/servers/localhost/zones/example.org." {
		t.Errorf("path = %q", rec.path)
	}
	if rec.contentType != "" {
		t.Errorf("Content-Type = %q, want empty for bodyless request", rec.contentType)
	}
}

func TestDeleteZoneNotFoundIsIdempotent(t *testing.T) {
	srv, _ := capture(t, http.StatusNotFound, `{"error": "not found"}`)
	c := NewClient(srv.URL, "k", srv.Client())

	if err := c.DeleteZone(context.Background(), "example.org."); err != nil {
		t.Fatalf("DeleteZone on 404: %v", err)
	}
}

func TestNon2xxErrorIncludesStatusAndBody(t *testing.T) {
	srv, _ := capture(t, http.StatusBadRequest, `{"error": "Domain 'example.org.' is not a valid DNS name"}`)
	c := NewClient(srv.URL, "k", srv.Client())

	err := c.CreateZone(context.Background(), Zone{Name: "example.org.", Kind: "Native"})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "not a valid DNS name") {
		t.Errorf("error = %v, want status 400 and body text", err)
	}
}

func TestErrorBodyIsCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(make([]byte, 1<<20)) // 1 MiB of zeros
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", srv.Client())
	err := c.DeleteZone(context.Background(), "example.org.")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	// Body is capped at 4 KiB; allow room for the error message prefix.
	if len(err.Error()) > 4200 {
		t.Errorf("error message too large: %d bytes", len(err.Error()))
	}
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %T is not *apiError", err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
}

func TestDoClosesBodyOnceOnError(t *testing.T) {
	var closes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", srv.Client())
	// Wrap the transport so we can count how many times the response body
	// of a non-2xx response is closed.
	c.httpClient = &http.Client{
		Transport: closeCountTransport{next: srv.Client().Transport, closes: &closes},
	}

	_, err := c.do(context.Background(), http.MethodGet, srv.URL, nil)
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	if closes != 1 {
		t.Errorf("body closed %d times, want 1", closes)
	}
}

type closeCountTransport struct {
	next   http.RoundTripper
	closes *int
}

func (t closeCountTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if resp != nil && resp.Body != nil {
		resp.Body = &countingReadCloser{ReadCloser: resp.Body, closes: t.closes}
	}
	return resp, err
}

type countingReadCloser struct {
	io.ReadCloser
	closes *int
}

func (c *countingReadCloser) Close() error {
	*c.closes++
	return c.ReadCloser.Close()
}

// capture is a helper httptest server that records the request and replies
// with the given status and body.
func capture(t *testing.T, wantStatus int, wantBody string) (*httptest.Server, *record) {
	t.Helper()
	rec := &record{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.method = r.Method
		rec.path = r.URL.EscapedPath()
		rec.apiKey = r.Header.Get("X-API-Key")
		rec.contentType = r.Header.Get("Content-Type")
		rec.body = b
		w.WriteHeader(wantStatus)
		_, _ = w.Write([]byte(wantBody))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

type record struct {
	method      string
	path        string
	apiKey      string
	contentType string
	body        []byte
}
