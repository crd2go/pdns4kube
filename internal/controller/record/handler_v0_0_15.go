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
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/runtime"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	constate "github.com/crd2go/constate"
	crapi "github.com/crd2go/crapi"
	bases "github.com/crd2go/pdns4kube/config/crd/bases"
	"github.com/crd2go/pdns4kube/internal/pdns"
	v1 "github.com/crd2go/pdns4kube/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// crdVersion is the CRD version used for the DNSRecord resource.
	crdVersion = "v1"
	// majorVersion is the pinned SDK major version handled by this handler.
	majorVersion = "v0_0_15"
	// defaultTTL is the TTL applied to RRsets when the spec entry has no
	// explicit TTL. The CRD TTL field is optional, so a documented default
	// is used.
	defaultTTL = 3600
)

// NewV000015Handler builds the crapi translator from the embedded CRD base
// and the generated v1 types registered in a runtime scheme, then returns a
// handler using it.
func NewV000015Handler(pdnsClient PDNSRecordClient) (*V000015Handler, error) {
	sch := runtime.NewScheme()
	if err := v1.AddToScheme(sch); err != nil {
		return nil, fmt.Errorf("register v1 types in scheme: %w", err)
	}
	crd, err := dnsRecordCRD()
	if err != nil {
		return nil, fmt.Errorf("parse embedded CRD: %w", err)
	}
	tr, err := crapi.NewTranslator(sch, crd, crdVersion, majorVersion)
	if err != nil {
		return nil, fmt.Errorf("create translator: %w", err)
	}
	return &V000015Handler{pdns: pdnsClient, translator: tr}, nil
}

// dnsRecordCRD parses the embedded CRD base and returns the DNSRecord CRD.
// The embedded YAML is a multi-document file whose first document is the
// DNSZone CRD, so the documents are scanned for the DNSRecord kind.
func dnsRecordCRD() (*apiextensionsv1.CustomResourceDefinition, error) {
	reader := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(bases.PDNSExampleIOYAML)))
	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("dnsrecords CRD not found in embedded bases")
		}
		if err != nil {
			return nil, fmt.Errorf("read embedded CRD document: %w", err)
		}
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := yaml.Unmarshal(doc, crd); err != nil {
			return nil, fmt.Errorf("parse embedded CRD document: %w", err)
		}
		if crd.Spec.Names.Kind == "DNSRecord" {
			return crd, nil
		}
	}
}

type V000015Handler struct {
	constate.FallbackHandler[v1.DNSRecord]
	// pdns performs rrset mutations against the PowerDNS API.
	pdns PDNSRecordClient
	// translator converts CRs into pdns API objects.
	translator crapi.Translator
}

func (h *V000015Handler) For() (client.Object, builder.Predicates) {
	return nil, builder.Predicates{}
}

func (h *V000015Handler) HandleInitial(ctx context.Context, obj *v1.DNSRecord) (constate.Result, error) {
	if _, err := h.rrsetFromEntry(obj); err != nil {
		return constate.ErrorState(constate.StateInitial, err)
	}
	return h.upsert(ctx, obj)
}

func (h *V000015Handler) HandleCreated(ctx context.Context, obj *v1.DNSRecord) (constate.Result, error) {
	return h.upsert(ctx, obj)
}

func (h *V000015Handler) HandleUpdated(ctx context.Context, obj *v1.DNSRecord) (constate.Result, error) {
	return h.upsert(ctx, obj)
}

func (h *V000015Handler) HandleDeletionRequested(ctx context.Context, obj *v1.DNSRecord) (constate.Result, error) {
	rrset, err := h.rrsetFromEntry(obj)
	if err != nil {
		return constate.ErrorState(constate.StateDeletionRequested, err)
	}
	if err := h.pdns.DeleteRRset(ctx, rrset.zoneID, rrset.rrset.Name, rrset.rrset.Type); err != nil {
		return constate.ErrorState(constate.StateDeletionRequested, fmt.Errorf("delete rrset: %w", err))
	}
	return constate.NextState(constate.StateDeleted, "rrset deleted from pdns")
}

func (h *V000015Handler) upsert(ctx context.Context, obj *v1.DNSRecord) (constate.Result, error) {
	rrset, err := h.rrsetFromEntry(obj)
	if err != nil {
		return constate.ErrorState(constate.StateCreated, err)
	}
	if err := h.pdns.ReplaceRRset(ctx, rrset.zoneID, rrset.rrset); err != nil {
		return constate.ErrorState(constate.StateCreated, fmt.Errorf("replace rrset: %w", err))
	}
	return constate.NextState(constate.StateUpdated, "rrset replaced in pdns")
}

// translatedRRset pairs the PowerDNS RRset built from a spec entry with the
// zone ID the RRset belongs to.
type translatedRRset struct {
	zoneID string
	rrset  pdns.RRset
}

// rrsetFromEntry converts the v0_0_15 spec entry into a translatedRRset.
// The crapi translator projects the RRset-shaped entry fields (name, type,
// ttl, records) into the PowerDNS RRset; the zone ID is extracted explicitly
// because it is not part of the RRset payload, and the default TTL is applied
// only when the entry omits one.
func (h *V000015Handler) rrsetFromEntry(obj *v1.DNSRecord) (translatedRRset, error) {
	if obj.Spec.V0_0_15 == nil || obj.Spec.V0_0_15.Entry == nil {
		return translatedRRset{}, fmt.Errorf("v0_0_15 spec entry is required")
	}
	entry := obj.Spec.V0_0_15.Entry
	if entry.Records == nil {
		return translatedRRset{}, fmt.Errorf("v0_0_15 spec entry records is required")
	}
	var rrset pdns.RRset
	if err := h.translator.ToAPI(&rrset, obj); err != nil {
		return translatedRRset{}, fmt.Errorf("translate entry to rrset: %w", err)
	}
	if entry.Ttl == nil {
		rrset.TTL = defaultTTL
	}
	return translatedRRset{zoneID: entry.Zone, rrset: rrset}, nil
}
