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

package record

import (
	"context"
	"errors"
	"strings"
	"testing"

	constate "github.com/crd2go/constate"
	"github.com/josvazg/pdns4kube/internal/pdns"
	v1 "github.com/josvazg/pdns4kube/v1"
)

func TestV000015Handler(t *testing.T) {
	ctx := context.Background()

	t.Run("HandleInitial replaces rrset", func(t *testing.T) {
		pdnsClient := &fakePDNSRecordClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleInitial(ctx, testDNSRecord())
		if err != nil {
			t.Fatalf("HandleInitial() error = %v", err)
		}
		if res.NextState != constate.StateUpdated {
			t.Errorf("NextState = %v, want %v", res.NextState, constate.StateUpdated)
		}
		if len(pdnsClient.replaced) != 1 {
			t.Fatalf("replaced calls = %d, want 1", len(pdnsClient.replaced))
		}
		got := pdnsClient.replaced[0]
		if got.zoneID != "example.org." {
			t.Errorf("zone ID = %q, want %q", got.zoneID, "example.org.")
		}
		wantRRset := pdns.RRset{
			Name:    "www.example.org.",
			Type:    "A",
			TTL:     300,
			Records: []pdns.Record{{Content: "192.0.2.1"}, {Content: "192.0.2.2"}},
		}
		if got.rrset.Name != wantRRset.Name || got.rrset.Type != wantRRset.Type ||
			got.rrset.TTL != wantRRset.TTL ||
			!recordsEqual(got.rrset.Records, wantRRset.Records) {
			t.Errorf("replaced rrset = %+v, want %+v", got.rrset, wantRRset)
		}
	})

	t.Run("HandleInitial missing entry", func(t *testing.T) {
		pdnsClient := &fakePDNSRecordClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleInitial(ctx, &v1.DNSRecord{})
		if wantErr := "v0_0_15 spec entry is required"; err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("error = %v, want substring %q", err, wantErr)
		}
		if res.NextState != constate.StateInitial {
			t.Errorf("NextState = %v, want %v", res.NextState, constate.StateInitial)
		}
		if len(pdnsClient.replaced) != 0 {
			t.Errorf("replaced calls = %d, want 0", len(pdnsClient.replaced))
		}
	})

	t.Run("HandleInitial replace error", func(t *testing.T) {
		pdnsClient := &fakePDNSRecordClient{replaceErr: errors.New("pdns failure")}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleInitial(ctx, testDNSRecord())
		if wantErr := "replace rrset: pdns failure"; err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("error = %v, want substring %q", err, wantErr)
		}
		if res.NextState != constate.StateCreated {
			t.Errorf("NextState = %v, want %v", res.NextState, constate.StateCreated)
		}
	})

	t.Run("HandleCreated and HandleUpdated replace rrset", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			handle func(*V000015Handler, context.Context, *v1.DNSRecord) (constate.Result, error)
		}{
			{"HandleCreated", (*V000015Handler).HandleCreated},
			{"HandleUpdated", (*V000015Handler).HandleUpdated},
		} {
			t.Run(tc.name, func(t *testing.T) {
				pdnsClient := &fakePDNSRecordClient{}
				h := newTestHandler(t, pdnsClient)

				res, err := tc.handle(h, ctx, testDNSRecord())
				if err != nil {
					t.Fatalf("%s() error = %v", tc.name, err)
				}
				if res.NextState != constate.StateUpdated {
					t.Errorf("NextState = %v, want %v", res.NextState, constate.StateUpdated)
				}
				if len(pdnsClient.replaced) != 1 {
					t.Fatalf("replaced calls = %d, want 1", len(pdnsClient.replaced))
				}
				if got := pdnsClient.replaced[0].zoneID; got != "example.org." {
					t.Errorf("zone ID = %q, want %q", got, "example.org.")
				}
			})
		}
	})

	t.Run("HandleDeletionRequested deletes rrset", func(t *testing.T) {
		pdnsClient := &fakePDNSRecordClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleDeletionRequested(ctx, testDNSRecord())
		if err != nil {
			t.Fatalf("HandleDeletionRequested() error = %v", err)
		}
		if res.NextState != constate.StateDeleted {
			t.Errorf("NextState = %v, want %v", res.NextState, constate.StateDeleted)
		}
		want := deleteCall{zoneID: "example.org.", name: "www.example.org.", rtype: "A"}
		if got := pdnsClient.deleted; len(got) != 1 || *got[0] != want {
			t.Errorf("deleted = %v, want [%v]", got, want)
		}
	})

	t.Run("HandleDeletionRequested missing entry", func(t *testing.T) {
		pdnsClient := &fakePDNSRecordClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleDeletionRequested(ctx, &v1.DNSRecord{})
		if wantErr := "v0_0_15 spec entry is required"; err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("error = %v, want substring %q", err, wantErr)
		}
		if res.NextState != constate.StateDeletionRequested {
			t.Errorf("NextState = %v, want %v", res.NextState, constate.StateDeletionRequested)
		}
		if len(pdnsClient.deleted) != 0 {
			t.Errorf("deleted calls = %d, want 0", len(pdnsClient.deleted))
		}
	})

	t.Run("HandleDeletionRequested delete error", func(t *testing.T) {
		pdnsClient := &fakePDNSRecordClient{deleteErr: errors.New("pdns failure")}
		h := newTestHandler(t, pdnsClient)

		_, err := h.HandleDeletionRequested(ctx, testDNSRecord())
		if wantErr := "delete rrset: pdns failure"; err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("error = %v, want substring %q", err, wantErr)
		}
	})
}

func TestRRsetFromEntry(t *testing.T) {
	h := newTestHandler(t, &fakePDNSRecordClient{})

	t.Run("nil ttl uses default", func(t *testing.T) {
		obj := testDNSRecord()
		obj.Spec.V0_0_15.Entry.Ttl = nil

		got, err := h.rrsetFromEntry(obj)
		if err != nil {
			t.Fatalf("rrsetFromEntry() error = %v", err)
		}
		if got.rrset.TTL != defaultTTL {
			t.Errorf("TTL = %d, want %d", got.rrset.TTL, defaultTTL)
		}
	})

	t.Run("nil values", func(t *testing.T) {
		obj := testDNSRecord()
		obj.Spec.V0_0_15.Entry.Values = nil

		_, err := h.rrsetFromEntry(obj)
		if wantErr := "v0_0_15 spec entry values is required"; err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("error = %v, want substring %q", err, wantErr)
		}
	})

	t.Run("missing versioned entry", func(t *testing.T) {
		obj := &v1.DNSRecord{}

		_, err := h.rrsetFromEntry(obj)
		if wantErr := "v0_0_15 spec entry is required"; err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("error = %v, want substring %q", err, wantErr)
		}
	})

	t.Run("malformed versioned entry", func(t *testing.T) {
		obj := &v1.DNSRecord{
			Spec: v1.DNSRecordSpec{
				V0_0_15: &v1.DNSRecordSpecV0_0_15{},
			},
		}

		_, err := h.rrsetFromEntry(obj)
		if wantErr := "v0_0_15 spec entry is required"; err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("error = %v, want substring %q", err, wantErr)
		}
	})
}

// newTestHandler builds a V000015Handler through the constructor.
func newTestHandler(t *testing.T, pdnsClient PDNSRecordClient) *V000015Handler {
	t.Helper()
	h, err := NewV000015Handler(pdnsClient)
	if err != nil {
		t.Fatalf("NewV000015Handler() error = %v", err)
	}
	return h
}

// recordsEqual reports whether two record slices have equal contents.
func recordsEqual(a, b []pdns.Record) bool {
	if len(a) != len(b) {
		return false
	}
	amap := make(map[string]int, len(a))
	for _, r := range a {
		amap[r.Content]++
	}
	for _, r := range b {
		amap[r.Content]--
		if amap[r.Content] < 0 {
			return false
		}
	}
	return true
}

// fakePDNSRecordClient records PDNS calls made by the handler.
type fakePDNSRecordClient struct {
	replaced   []replaceCall
	deleted    []*deleteCall
	replaceErr error
	deleteErr  error
}

type replaceCall struct {
	zoneID string
	rrset  pdns.RRset
}

type deleteCall struct {
	zoneID string
	name   string
	rtype  string
}

func (f *fakePDNSRecordClient) ReplaceRRset(ctx context.Context, zoneID string, rrset pdns.RRset) error {
	f.replaced = append(f.replaced, replaceCall{zoneID: zoneID, rrset: rrset})
	return f.replaceErr
}

func (f *fakePDNSRecordClient) DeleteRRset(ctx context.Context, zoneID, name, rtype string) error {
	f.deleted = append(f.deleted, &deleteCall{zoneID: zoneID, name: name, rtype: rtype})
	return f.deleteErr
}

// testDNSRecord returns a DNSRecord with a v0_0_15 entry for handler tests.
func testDNSRecord() *v1.DNSRecord {
	return &v1.DNSRecord{
		Spec: v1.DNSRecordSpec{
			V0_0_15: &v1.DNSRecordSpecV0_0_15{
				Entry: &v1.V0_0_15Entry{
					Name:   "www.example.org.",
					Ttl:    &[]int{300}[0],
					Type:   "A",
					Values: &[]string{"192.0.2.1", "192.0.2.2"},
					Zone:   "example.org.",
				},
			},
		},
	}
}
