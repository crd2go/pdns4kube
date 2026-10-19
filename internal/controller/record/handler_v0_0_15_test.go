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
	"testing"

	constate "github.com/crd2go/constate"
	"github.com/crd2go/pdns4kube/internal/pdns"
	v1 "github.com/crd2go/pdns4kube/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV000015Handler(t *testing.T) {
	ctx := context.Background()

	t.Run("HandleInitial replaces rrset", func(t *testing.T) {
		pdnsClient := &fakePDNSRecordClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleInitial(ctx, testDNSRecord())
		require.NoError(t, err)
		assert.Equal(t, constate.StateUpdated, res.NextState)
		require.Len(t, pdnsClient.replaced, 1)
		got := pdnsClient.replaced[0]
		assert.Equal(t, "example.org.", got.zoneID)
		wantRRset := pdns.RRset{
			Name: "www.example.org.",
			Type: "A",
			TTL:  300,
			Records: []pdns.Record{
				{Content: "192.0.2.1"},
				{Content: "192.0.2.2", Disabled: true},
			},
		}
		assert.Equal(t, wantRRset, got.rrset)
	})

	t.Run("HandleInitial missing entry", func(t *testing.T) {
		pdnsClient := &fakePDNSRecordClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleInitial(ctx, &v1.DNSRecord{})
		require.ErrorContains(t, err, "v0_0_15 spec entry is required")
		assert.Equal(t, constate.StateInitial, res.NextState)
		assert.Empty(t, pdnsClient.replaced)
	})

	t.Run("HandleInitial replace error", func(t *testing.T) {
		pdnsClient := &fakePDNSRecordClient{replaceErr: errors.New("pdns failure")}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleInitial(ctx, testDNSRecord())
		require.ErrorContains(t, err, "replace rrset: pdns failure")
		assert.Equal(t, constate.StateCreated, res.NextState)
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
				require.NoError(t, err)
				assert.Equal(t, constate.StateUpdated, res.NextState)
				require.Len(t, pdnsClient.replaced, 1)
				assert.Equal(t, "example.org.", pdnsClient.replaced[0].zoneID)
			})
		}
	})

	t.Run("HandleDeletionRequested deletes rrset", func(t *testing.T) {
		pdnsClient := &fakePDNSRecordClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleDeletionRequested(ctx, testDNSRecord())
		require.NoError(t, err)
		assert.Equal(t, constate.StateDeleted, res.NextState)
		want := deleteCall{zoneID: "example.org.", name: "www.example.org.", rtype: "A"}
		require.Len(t, pdnsClient.deleted, 1)
		assert.Equal(t, want, *pdnsClient.deleted[0])
	})

	t.Run("HandleDeletionRequested missing entry", func(t *testing.T) {
		pdnsClient := &fakePDNSRecordClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleDeletionRequested(ctx, &v1.DNSRecord{})
		require.ErrorContains(t, err, "v0_0_15 spec entry is required")
		assert.Equal(t, constate.StateDeletionRequested, res.NextState)
		assert.Empty(t, pdnsClient.deleted)
	})

	t.Run("HandleDeletionRequested delete error", func(t *testing.T) {
		pdnsClient := &fakePDNSRecordClient{deleteErr: errors.New("pdns failure")}
		h := newTestHandler(t, pdnsClient)

		_, err := h.HandleDeletionRequested(ctx, testDNSRecord())
		require.ErrorContains(t, err, "delete rrset: pdns failure")
	})
}

func TestRRsetFromEntry(t *testing.T) {
	h := newTestHandler(t, &fakePDNSRecordClient{})

	t.Run("nil ttl uses default", func(t *testing.T) {
		obj := testDNSRecord()
		obj.Spec.V0_0_15.Entry.Ttl = nil

		got, err := h.rrsetFromEntry(obj)
		require.NoError(t, err)
		assert.Equal(t, defaultTTL, got.rrset.TTL)
	})

	t.Run("nil records", func(t *testing.T) {
		obj := testDNSRecord()
		obj.Spec.V0_0_15.Entry.Records = nil

		_, err := h.rrsetFromEntry(obj)
		require.ErrorContains(t, err, "v0_0_15 spec entry records is required")
	})

	t.Run("missing versioned entry", func(t *testing.T) {
		obj := &v1.DNSRecord{}

		_, err := h.rrsetFromEntry(obj)
		require.ErrorContains(t, err, "v0_0_15 spec entry is required")
	})

	t.Run("malformed versioned entry", func(t *testing.T) {
		obj := &v1.DNSRecord{
			Spec: v1.DNSRecordSpec{
				V0_0_15: &v1.DNSRecordSpecV0_0_15{},
			},
		}

		_, err := h.rrsetFromEntry(obj)
		require.ErrorContains(t, err, "v0_0_15 spec entry is required")
	})
}

// newTestHandler builds a V000015Handler through the constructor.
func newTestHandler(t *testing.T, pdnsClient PDNSRecordClient) *V000015Handler {
	t.Helper()
	h, err := NewV000015Handler(pdnsClient)
	require.NoError(t, err)
	return h
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
					Name: "www.example.org.",
					Records: &[]v1.Records{
						{Content: "192.0.2.1"},
						{Content: "192.0.2.2", Disabled: &[]bool{true}[0]},
					},
					Ttl:  &[]int{300}[0],
					Type: "A",
					Zone: "example.org.",
				},
			},
		},
	}
}
