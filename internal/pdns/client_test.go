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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateZone(t *testing.T) {
	srv, rec := capture(t, http.StatusCreated, "")
	c := NewClient(srv.URL, "secret", srv.Client())

	err := c.CreateZone(context.Background(), Zone{
		Name:        "example.org.",
		Kind:        "Native",
		Nameservers: []string{"ns1.example.org."},
	})
	require.NoError(t, err, "CreateZone")

	wantHeader := recordHeader{
		method:      http.MethodPost,
		path:        "/api/v1/servers/localhost/zones",
		apiKey:      "secret",
		contentType: "application/json",
	}
	assert.Equal(t, []recordHeader{wantHeader}, rec.headers())
	require.Len(t, rec.records, 1)
	got := asZone(t, rec.records[0].body)
	want := Zone{Name: "example.org.", Kind: "Native", Nameservers: []string{"ns1.example.org."}}
	assert.Equal(t, want, got)
}

func TestCreateZoneConflictFallsBackToUpdate(t *testing.T) {
	srv, rec := newRecordingServer(t, func(rec *record) response {
		if rec.method == http.MethodPost {
			return response{status: http.StatusConflict, body: `{"error": "Conflict"}`}
		}
		return response{status: http.StatusNoContent}
	})

	c := NewClient(srv.URL, "k", srv.Client())
	require.NoError(t, c.CreateZone(context.Background(),
		Zone{Name: "example.org.", Kind: "Native"}), "CreateZone")

	post := recordHeader{
		method:      http.MethodPost,
		path:        "/api/v1/servers/localhost/zones",
		apiKey:      "k",
		contentType: "application/json",
	}
	put := recordHeader{
		method:      http.MethodPut,
		path:        "/api/v1/servers/localhost/zones/example.org.",
		apiKey:      "k",
		contentType: "application/json",
	}
	assert.Equal(t, []recordHeader{post, put}, rec.headers())
}

func TestUpdateZone(t *testing.T) {
	srv, rec := capture(t, http.StatusNoContent, "")
	c := NewClient(srv.URL, "k", srv.Client())

	err := c.UpdateZone(context.Background(), "example org..Native",
		Zone{Name: "example.org.", Kind: "Native"})
	require.NoError(t, err, "UpdateZone")

	wantHeader := recordHeader{
		method:      http.MethodPut,
		path:        "/api/v1/servers/localhost/zones/example%20org..Native",
		apiKey:      "k",
		contentType: "application/json",
	}
	assert.Equal(t, []recordHeader{wantHeader}, rec.headers())
	require.Len(t, rec.records, 1)
	got := asZone(t, rec.records[0].body)
	assert.Equal(t, "Native", got.Kind)
}

func TestUpdateZoneWithNameserversSendsPutAndPatch(t *testing.T) {
	srv, rec := capture(t, http.StatusNoContent, "")

	c := NewClient(srv.URL, "k", srv.Client())
	err := c.UpdateZone(context.Background(), "example.org.", Zone{
		Name:        "example.org.",
		Kind:        "Native",
		Nameservers: []string{"ns1.example.org.", "ns2.example.org."},
	})
	require.NoError(t, err, "UpdateZone")

	wantPath := "/api/v1/servers/localhost/zones/example.org."
	put := recordHeader{
		method: http.MethodPut, path: wantPath, apiKey: "k", contentType: "application/json",
	}
	patch := recordHeader{
		method: http.MethodPatch, path: wantPath, apiKey: "k", contentType: "application/json",
	}
	assert.Equal(t, []recordHeader{put, patch}, rec.headers())

	require.Len(t, rec.records, 2)
	got := asRRSets(t, rec.records[1].body)
	require.Len(t, got.RRsets, 1)
	rr := got.RRsets[0]
	wantRR := RRset{
		Name:       "example.org.",
		Type:       "NS",
		TTL:        defaultNSTTL,
		ChangeType: "REPLACE",
		Records: []Record{
			{Content: "ns1.example.org."},
			{Content: "ns2.example.org."},
		},
	}
	assert.Equal(t, wantRR, rr)
}

func TestUpdateZoneEmptyNameserversSendsReplaceWithNoRecords(t *testing.T) {
	srv, rec := capture(t, http.StatusNoContent, "")

	c := NewClient(srv.URL, "k", srv.Client())
	require.NoError(t, c.UpdateZone(context.Background(), "example.org.",
	Zone{Kind: "Native", Nameservers: []string{}}), "UpdateZone")

	// UpdateZone first PUTs the zone, then PATCHes the NS rrset.
	require.Len(t, rec.records, 2)
	got := asRRSets(t, rec.records[1].body)
	require.Len(t, got.RRsets, 1)
	wantRR := RRset{
		Name:       "example.org.",
		Type:       "NS",
		TTL:        defaultNSTTL,
		ChangeType: "REPLACE",
		Records:    []Record{},
	}
	assert.Equal(t, wantRR, got.RRsets[0])
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
	require.NoError(t, err, "ReplaceRRset")

	wantHeader := recordHeader{
		method:      http.MethodPatch,
		path:        "/api/v1/servers/localhost/zones/example.org.",
		apiKey:      "k",
		contentType: "application/json",
	}
	assert.Equal(t, []recordHeader{wantHeader}, rec.headers())
	require.Len(t, rec.records, 1)
	got := asRRSets(t, rec.records[0].body)
	require.Len(t, got.RRsets, 1)
	wantRR := RRset{
		Name:       "www.example.org.",
		Type:       "A",
		TTL:        300,
		ChangeType: "REPLACE",
		Records: []Record{
			{Content: "192.0.2.1"},
			{Content: "192.0.2.2", Disabled: true},
		},
	}
	assert.Equal(t, wantRR, got.RRsets[0])
}

func TestReplaceRRsetOverwritesChangeType(t *testing.T) {
	srv, rec := capture(t, http.StatusNoContent, "")

	c := NewClient(srv.URL, "k", srv.Client())
	err := c.ReplaceRRset(context.Background(), "example.org.", RRset{
		Name:       "www.example.org.",
		Type:       "A",
		ChangeType: "DELETE",
	})
	require.NoError(t, err, "ReplaceRRset")

	require.Len(t, rec.records, 1)
	got := asRRSets(t, rec.records[0].body)
	require.Len(t, got.RRsets, 1)
	assert.Equal(t, "REPLACE", got.RRsets[0].ChangeType)
}

func TestDeleteRRset(t *testing.T) {
	srv, rec := capture(t, http.StatusNoContent, "")
	c := NewClient(srv.URL, "k", srv.Client())

	err := c.DeleteRRset(context.Background(), "example.org.", "www.example.org.", "A")
	require.NoError(t, err, "DeleteRRset")

	wantHeader := recordHeader{
		method:      http.MethodPatch,
		path:        "/api/v1/servers/localhost/zones/example.org.",
		apiKey:      "k",
		contentType: "application/json",
	}
	assert.Equal(t, []recordHeader{wantHeader}, rec.headers())
	require.Len(t, rec.records, 1)
	got := asRRSets(t, rec.records[0].body)
	require.Len(t, got.RRsets, 1)
	wantRR := RRset{
		Name:       "www.example.org.",
		Type:       "A",
		ChangeType: "DELETE",
		Records:    []Record{},
	}
	assert.Equal(t, wantRR, got.RRsets[0])
}

func TestDeleteZone(t *testing.T) {
	srv, rec := capture(t, http.StatusNoContent, "")
	c := NewClient(srv.URL, "k", srv.Client())

	err := c.DeleteZone(context.Background(), "example.org.")
	require.NoError(t, err, "DeleteZone")

	wantHeader := recordHeader{
		method: http.MethodDelete,
		path:   "/api/v1/servers/localhost/zones/example.org.",
		apiKey: "k",
	}
	assert.Equal(t, []recordHeader{wantHeader}, rec.headers())
}

func TestDeleteZoneNotFoundIsIdempotent(t *testing.T) {
	srv, _ := capture(t, http.StatusNotFound, `{"error": "not found"}`)
	c := NewClient(srv.URL, "k", srv.Client())

	require.NoError(t, c.DeleteZone(context.Background(), "example.org."), "DeleteZone on 404")
}

func TestNon2xxErrorIncludesStatusAndBody(t *testing.T) {
	srv, _ := capture(
		t, http.StatusBadRequest, `{"error": "Domain 'example.org.' is not a valid DNS name"}`)
	c := NewClient(srv.URL, "k", srv.Client())

	err := c.CreateZone(context.Background(), Zone{Name: "example.org.", Kind: "Native"})
	require.Error(t, err, "expected error for 400 response")
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "not a valid DNS name")
}

func TestErrorBodyIsCapped(t *testing.T) {
	srv, _ := newRecordingServer(t,
		staticResponse(http.StatusInternalServerError, strings.Repeat("\x00", 1<<20)))
	c := NewClient(srv.URL, "k", srv.Client())

	err := c.DeleteZone(context.Background(), "example.org.")
	require.Error(t, err, "expected error for 500 response")
	// Body is capped at 4 KiB; allow room for the error message prefix.
	assert.LessOrEqual(t, len(err.Error()), 4200)
	var apiErr *apiError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
}

func TestDoClosesBodyOnceOnError(t *testing.T) {
	var closes int
	srv, _ := newRecordingServer(t, staticResponse(http.StatusForbidden, ""))

	c := NewClient(srv.URL, "k", srv.Client())
	// Wrap the transport so we can count how many times the response body
	// of a non-2xx response is closed.
	c.httpClient = &http.Client{
		Transport: closeCountTransport{next: srv.Client().Transport, closes: &closes},
	}

	_, err := c.do(context.Background(), http.MethodGet, srv.URL, nil)
	require.Error(t, err, "expected error for 403 response")
	assert.Equal(t, 1, closes)
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

// response is the status and body a recorded request should be answered with.
type response struct {
	status int
	body   string
}

// staticResponse returns a responder that always replies with the given
// status and body.
func staticResponse(status int, body string) func(*record) response {
	return func(*record) response {
		return response{status: status, body: body}
	}
}

// recorder records every request received by its server as an ordered slice
// of records.
type recorder struct {
	records []record
}

// headers returns the ordered record headers of all recorded requests.
func (r *recorder) headers() []recordHeader {
	headers := make([]recordHeader, len(r.records))
	for i, rec := range r.records {
		headers[i] = rec.recordHeader
	}
	return headers
}

// newRecordingServer starts an httptest server that records every request and
// answers it with the response chosen by respond, which receives the record
// of the request being handled. Requests are answered sequentially.
func newRecordingServer(t *testing.T, respond func(rec *record) response) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.records = append(rec.records, record{
			recordHeader: recordHeader{
				method:      r.Method,
				path:        r.URL.EscapedPath(),
				apiKey:      r.Header.Get("X-API-Key"),
				contentType: r.Header.Get("Content-Type"),
			},
			body: b,
		})
		resp := respond(&rec.records[len(rec.records)-1])
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// capture is a recording server that replies with a fixed status and body.
func capture(t *testing.T, wantStatus int, wantBody string) (*httptest.Server, *recorder) {
	t.Helper()
	return newRecordingServer(t, staticResponse(wantStatus, wantBody))
}

func asZone(t *testing.T, body []byte) Zone {
	t.Helper()

	var z Zone
	err := json.Unmarshal(body, &z)
	require.NoError(t, err, "decode request body")
	return z
}

func asRRSets(t *testing.T, body []byte) rrsetsPayload {
	t.Helper()

	var rs rrsetsPayload
	err := json.Unmarshal(body, &rs)
	require.NoError(t, err, "decode request body")
	return rs
}

type recordHeader struct {
	method      string
	path        string
	apiKey      string
	contentType string
}

type record struct {
	recordHeader
	body []byte
}
