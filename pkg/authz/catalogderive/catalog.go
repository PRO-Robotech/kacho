// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// catalog.go — чтение сгенерированного каталога прав.
//
// Каталог — ВЫХОД генератора, а карта сервиса теперь выводится из его ВХОДА
// (аннотаций). Читать его здесь нужно ровно для одного: чтобы сверить выход со
// входом. Пока обе стороны сходятся, «каталог, который читает оператор» и
// «правило, которое исполняет сервис» — одно и то же утверждение.

package catalogderive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CatalogPath is the repo-relative location of the generated catalog embedded
// into the api-gateway binary. It is the artefact the gateway actually enforces.
const CatalogPath = "gateway/internal/middleware/embed/permission_catalog.json"

// Entry is the subset of a catalog row this comparison needs.
type Entry struct {
	FQN              string `json:"fqn"`
	Permission       string `json:"permission"`
	RequiredRelation string `json:"required_relation"`
	ScopeExtractor   struct {
		ObjectType                 string `json:"object_type"`
		FromRequestField           string `json:"from_request_field"`
		ObjectTypeFromRequestField string `json:"object_type_from_request_field"`
	} `json:"scope_extractor"`
	// ScopeFiltered — the catalog's own declaration that the OWNING SERVICE
	// authorizes this call over the data it answers with, so the edge
	// authenticates and runs no per-RPC Check. It is the catalog-side counterpart
	// of authz.RPCEntry.ScopeFiltered, and Compare requires the two to agree.
	ScopeFiltered bool `json:"scope_filtered"`
	// HideExistence — the catalog's explicit mark that a deny on this method is
	// answered with the owning service's NotFound. Mirrors
	// authz.RPCEntry.HideExistence; see catalogHidesExistence for the derived
	// (unmarked) majority.
	HideExistence bool `json:"hide_existence"`
}

// LoadCatalog reads the generated catalog, keyed by gRPC full method
// ("/kacho.cloud.storage.v1.VolumeService/Get") so it joins directly against
// authz.RPCMap keys. The catalog stores FQNs without the leading slash.
//
// dir is any directory inside the module; the repo root is located by walking up
// to the go.mod that declares the module.
func LoadCatalog(dir string) (map[string]Entry, error) {
	root, err := moduleRoot(dir)
	if err != nil {
		return nil, err
	}
	// The read target is not caller-chosen: CatalogPath is a constant in this
	// package and root is this module's own directory, located by walking up to
	// the go.mod that declares it. `dir` only says where to start walking, so no
	// value of it can name a different file. The package has no non-test
	// importer either — every caller is a *_test.go parity check, so this never
	// runs while a request is being served.
	raw, err := os.ReadFile(filepath.Join(root, CatalogPath)) // #nosec G304 -- constant path under this module's own root; `dir` only picks the walk-up start
	if err != nil {
		return nil, fmt.Errorf("read permission catalog: %w", err)
	}
	var rows []Entry
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode permission catalog: %w", err)
	}
	out := make(map[string]Entry, len(rows))
	for _, r := range rows {
		out["/"+r.FQN] = r
	}
	return out, nil
}

// moduleRoot walks up from dir until it finds the go.mod of this module.
func moduleRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		abs = parent
	}
}
