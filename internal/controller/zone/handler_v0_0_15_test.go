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

package zone

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

	t.Run("HandleInitial creates zone", func(t *testing.T) {
		pdnsClient := &fakePDNSZoneClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleInitial(ctx, testDNSZone())
		require.NoError(t, err)
		assert.Equal(t, constate.StateCreated, res.NextState)
		require.Len(t, pdnsClient.created, 1)
		want := pdns.Zone{
			Name:        "example.org.",
			Kind:        "Native",
			Nameservers: []string{"ns1.example.org.", "ns2.example.org."},
		}
		assert.Equal(t, want, pdnsClient.created[0])
	})

	t.Run("HandleInitial missing entry", func(t *testing.T) {
		pdnsClient := &fakePDNSZoneClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleInitial(ctx, &v1.DNSZone{})
		require.ErrorContains(t, err, "v0_0_15 spec entry is required")
		assert.Equal(t, constate.StateInitial, res.NextState)
		assert.Empty(t, pdnsClient.created)
	})

	t.Run("HandleInitial create error", func(t *testing.T) {
		pdnsClient := &fakePDNSZoneClient{createErr: errors.New("pdns failure")}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleInitial(ctx, testDNSZone())
		require.ErrorContains(t, err, "create zone: pdns failure")
		assert.Equal(t, constate.StateInitial, res.NextState)
	})

	t.Run("HandleCreated updates zone", func(t *testing.T) {
		pdnsClient := &fakePDNSZoneClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleCreated(ctx, testDNSZone())
		require.NoError(t, err)
		assert.Equal(t, constate.StateUpdated, res.NextState)
		require.Len(t, pdnsClient.updated, 1)
		assert.Equal(t, "example.org.", pdnsClient.updated[0].id)
		want := pdns.Zone{
			Name:        "example.org.",
			Kind:        "Native",
			Nameservers: []string{"ns1.example.org.", "ns2.example.org."},
		}
		assert.Equal(t, want, pdnsClient.updated[0].zone)
	})

	t.Run("HandleUpdated updates zone", func(t *testing.T) {
		pdnsClient := &fakePDNSZoneClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleUpdated(ctx, testDNSZone())
		require.NoError(t, err)
		assert.Equal(t, constate.StateUpdated, res.NextState)
		require.Len(t, pdnsClient.updated, 1)
		assert.Equal(t, "example.org.", pdnsClient.updated[0].id)
		want := pdns.Zone{
			Name:        "example.org.",
			Kind:        "Native",
			Nameservers: []string{"ns1.example.org.", "ns2.example.org."},
		}
		assert.Equal(t, want, pdnsClient.updated[0].zone)
	})

	t.Run("HandleCreated missing entry", func(t *testing.T) {
		pdnsClient := &fakePDNSZoneClient{}
		h := newTestHandler(t, pdnsClient)

		_, err := h.HandleCreated(ctx, &v1.DNSZone{})
		require.ErrorContains(t, err, "v0_0_15 spec entry is required")
	})

	t.Run("HandleUpdated missing entry", func(t *testing.T) {
		pdnsClient := &fakePDNSZoneClient{}
		h := newTestHandler(t, pdnsClient)

		_, err := h.HandleUpdated(ctx, &v1.DNSZone{})
		require.ErrorContains(t, err, "v0_0_15 spec entry is required")
	})

	t.Run("HandleCreated update error", func(t *testing.T) {
		pdnsClient := &fakePDNSZoneClient{updateErr: errors.New("pdns failure")}
		h := newTestHandler(t, pdnsClient)

		_, err := h.HandleCreated(ctx, testDNSZone())
		require.ErrorContains(t, err, "update zone: pdns failure")
	})

	t.Run("HandleUpdated update error", func(t *testing.T) {
		pdnsClient := &fakePDNSZoneClient{updateErr: errors.New("pdns failure")}
		h := newTestHandler(t, pdnsClient)

		_, err := h.HandleUpdated(ctx, testDNSZone())
		require.ErrorContains(t, err, "update zone: pdns failure")
	})

	t.Run("HandleDeletionRequested deletes zone", func(t *testing.T) {
		pdnsClient := &fakePDNSZoneClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleDeletionRequested(ctx, testDNSZone())
		require.NoError(t, err)
		assert.Equal(t, constate.StateDeleted, res.NextState)
		assert.Equal(t, []string{"example.org."}, pdnsClient.deleted)
	})

	t.Run("HandleDeletionRequested missing entry", func(t *testing.T) {
		pdnsClient := &fakePDNSZoneClient{}
		h := newTestHandler(t, pdnsClient)

		res, err := h.HandleDeletionRequested(ctx, &v1.DNSZone{})
		require.ErrorContains(t, err, "v0_0_15 spec entry is required")
		assert.Equal(t, constate.StateDeletionRequested, res.NextState)
		assert.Empty(t, pdnsClient.deleted)
	})

	t.Run("HandleDeletionRequested delete error", func(t *testing.T) {
		pdnsClient := &fakePDNSZoneClient{deleteErr: errors.New("pdns failure")}
		h := newTestHandler(t, pdnsClient)

		_, err := h.HandleDeletionRequested(ctx, testDNSZone())
		require.ErrorContains(t, err, "delete zone: pdns failure")
	})
}

func TestZoneFromEntry(t *testing.T) {
	h := newTestHandler(t, &fakePDNSZoneClient{})

	t.Run("translates valid CR fields", func(t *testing.T) {
		obj := testDNSZone()

		got, err := h.zoneFromEntry(obj)
		require.NoError(t, err)
		want := pdns.Zone{
			Name:        "example.org.",
			Kind:        "Native",
			Nameservers: []string{"ns1.example.org.", "ns2.example.org."},
		}
		assert.Equal(t, want, got)
	})

	t.Run("absent optional nameservers", func(t *testing.T) {
		obj := testDNSZone()
		obj.Spec.V0_0_15.Entry.Nameservers = nil

		got, err := h.zoneFromEntry(obj)
		require.NoError(t, err)
		assert.Equal(t, "example.org.", got.Name)
		assert.Equal(t, "Native", got.Kind)
		assert.Empty(t, got.Nameservers)
	})

	t.Run("missing versioned entry", func(t *testing.T) {
		obj := &v1.DNSZone{}

		_, err := h.zoneFromEntry(obj)
		require.ErrorContains(t, err, "v0_0_15 spec entry is required")
	})

	t.Run("malformed versioned entry", func(t *testing.T) {
		obj := &v1.DNSZone{
			Spec: v1.DNSZoneSpec{
				V0_0_15: &v1.DNSZoneSpecV0_0_15{},
			},
		}

		_, err := h.zoneFromEntry(obj)
		require.ErrorContains(t, err, "v0_0_15 spec entry is required")
	})

	t.Run("translation error for mismatched GVK", func(t *testing.T) {
		obj := testDNSZone()
		obj.APIVersion = "other.example.io/v1"
		obj.Kind = "DNSZone"

		_, err := h.zoneFromEntry(obj)
		require.ErrorContains(t, err, "translate v0_0_15 entry")
	})
}

// newTestHandler builds a V000015Handler through the constructor.
func newTestHandler(t *testing.T, pdnsClient PDNSZoneClient) *V000015Handler {
	t.Helper()
	h, err := NewV000015Handler(pdnsClient)
	require.NoError(t, err)
	return h
}

// fakePDNSZoneClient records PDNS calls made by the handler.
type fakePDNSZoneClient struct {
	created   []pdns.Zone
	updated   []updateCall
	deleted   []string
	createErr error
	updateErr error
	deleteErr error
}

type updateCall struct {
	id   string
	zone pdns.Zone
}

func (f *fakePDNSZoneClient) CreateZone(ctx context.Context, z pdns.Zone) error {
	f.created = append(f.created, z)
	return f.createErr
}

func (f *fakePDNSZoneClient) UpdateZone(ctx context.Context, zoneID string, z pdns.Zone) error {
	f.updated = append(f.updated, updateCall{id: zoneID, zone: z})
	return f.updateErr
}

func (f *fakePDNSZoneClient) DeleteZone(ctx context.Context, zoneID string) error {
	f.deleted = append(f.deleted, zoneID)
	return f.deleteErr
}

// testDNSZone returns a DNSZone with a v0_0_15 entry for handler tests.
func testDNSZone() *v1.DNSZone {
	return &v1.DNSZone{
		Spec: v1.DNSZoneSpec{
			V0_0_15: &v1.DNSZoneSpecV0_0_15{
				Entry: &v1.Entry{
					Name:        "example.org.",
					Kind:        "Native",
					Nameservers: &[]string{"ns1.example.org.", "ns2.example.org."},
				},
			},
		},
	}
}
