// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Block-storage retire gate.
//
// kacho-storage owns Volume / Snapshot / Image / DiskType. compute used to serve a
// SECOND, independent copy of the same four resource types: its own tables, its own
// gRPC services, its own REST routes, its own catalog entries. That duplication is
// retired — this gate is what keeps it retired.
//
// It asserts the two halves that together mean "retired", because either half alone
// leaves the product worse off than not having started:
//
//	reachability — no surface promises the resource. No REST route, no permission
//	catalog entry (in EITHER embedded copy), no gateway allowlist entry, no proto
//	service, no generated stub, and nothing left in compute that names the retired
//	authorization object types.
//
//	storage      — no live path reaches the dropped tables. No non-test compute
//	source issues SQL against `disks` / `images` / `snapshots` / `disk_types`.
//
// A surface with no table behind it answers callers with a promise the product
// cannot keep. A table with no surface is data nobody can reach and nobody
// maintains. Both halves are needed for the claim "this resource is gone" to be
// true, so both are checked here rather than argued in a review comment.
//
// The migration history is deliberately EXEMPT from the storage half. An applied
// migration is never edited, so the `CREATE TABLE` in the 0001 baseline and the
// `DROP TABLE` that retires it must both stay on the record; a gate that forbade
// naming the table in migrations would forbid dropping it.

// retiredResource — one retired compute duplicate: the table that was dropped, and
// the gRPC services that used to serve it.
type retiredResource struct {
	table    string
	services []string
}

// retiredBlockStorage — the four resource types compute no longer owns. Each entry
// is only added once that type is retired WHOLE (proto, routes, both catalog copies,
// service layer, repository, drop migration, black boxes, docs), so the list doubles
// as the record of what has been taken off the contract.
var retiredBlockStorage = []retiredResource{
	{table: "disks", services: []string{"DiskService"}},
	{table: "images", services: []string{"ImageService"}},
	{table: "snapshots", services: []string{"SnapshotService"}},
	{table: "disk_types", services: []string{"DiskTypeService", "InternalDiskTypeService"}},
}

// retiredAuthzObjects — the authorization object types the retired resources used to
// register owner-tuples for.
//
// WHAT THIS GATE PROVES, EXACTLY: that COMPUTE cannot address them — it emits no
// tuple, runs no check, filters no list on these types, and no catalog entry scopes
// one of its RPCs to them. That is the whole of the claim, and it is deliberately
// narrower than "the types do not exist": whether they are declared at all is an
// iam-side fact, and this gate reads only the compute tree.
//
// The iam side is now done too, and the order it went in is worth keeping, because
// the obvious order is the broken one. Removing a declared type while a producer
// still emits onto it turns every such write into a rejection the store never stops
// returning — the failure mode recorded above `type storage_volume` in the model,
// arrived at from the opposite direction. So the producers were retracted FIRST:
// migration 0074 removed the nine `compute.{disk,image,snapshot}.*` system roles,
// the selector rows that expanded onto the three dotted names, the mirror rows, the
// members and the queued tuples; then iam's four vocabularies (grantable catalog,
// per-verb emission guard, label-selectable/materializable feed, AccessBinding
// target whitelist) dropped them; and only then did `type compute_disk` /
// `compute_image` / `compute_snapshot` leave the canonical authorization model and
// the embedded copy the service compiles its decision plan from.
//
// That half is held by services/iam/internal/check/retired_block_storage_test.go
// (artefacts in the tree) and
// services/iam/internal/repo/kacho/pg/retired_block_storage_integration_test.go
// (effective rows in a migrated schema). This gate stays as it is: it answers for
// compute, and a future re-introduction here would be caught here.
var retiredAuthzObjects = []string{"compute_disk", "compute_image", "compute_snapshot"}

// retiredFQNs returns the fully-qualified gRPC service names taken off the contract.
func retiredFQNs() []string {
	var out []string
	for _, r := range retiredBlockStorage {
		for _, s := range r.services {
			out = append(out, "kacho.cloud.compute.v1."+s)
		}
	}
	return out
}

// moduleRootFrom walks up from dir until it finds the go.mod of this module.
func moduleRootFrom(t *testing.T, dir string) string {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("abs %q: %v", dir, err)
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			t.Fatalf("go.mod not found above %s", dir)
		}
		abs = parent
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- fixed in-repo paths under this module's own root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// TestRetiredBlockStorageIsUnreachable — the reachability half.
//
// Every artefact a caller could arrive through is checked by name, so that a partial
// regeneration (route table refreshed but a catalog copy left stale, say) fails here
// instead of at runtime, where the symptom is a route that authorizes against an
// entry for a service that no longer exists.
func TestRetiredBlockStorageIsUnreachable(t *testing.T) {
	root := moduleRootFrom(t, ".")
	fqns := retiredFQNs()
	if len(fqns) == 0 {
		t.Fatal("no retired service names — this gate would assert nothing")
	}

	// Surfaces a request can be routed or authorized through. Both embedded copies
	// of the permission catalog are listed: they are required to be byte-identical,
	// and checking only one would let the other drift while the gate stayed green.
	surfaces := []string{
		filepath.Join(root, "gateway", "internal", "middleware", "rest_route_table_gen.go"),
		filepath.Join(root, "gateway", "internal", "middleware", "embed", "permission_catalog.json"),
		filepath.Join(root, "services", "iam", "internal", "apps", "kaname", "seed", "embedded", "permission_catalog.json"),
		filepath.Join(root, "gateway", "internal", "allowlist", "list.go"),
	}
	for _, path := range surfaces {
		body := readFile(t, path)
		for _, fqn := range fqns {
			if strings.Contains(body, fqn) {
				t.Errorf("%s still promises retired service %s — kacho-storage owns this resource; a surface with no implementation behind it is a promise the product cannot keep",
					mustRel(t, root, path), fqn)
			}
		}
	}

	// The proto tree must not define the services, and the generated tree must not
	// carry stubs for them: either one left behind lets the surface be re-registered
	// by a single line elsewhere.
	for _, dir := range []string{
		filepath.Join(root, "proto", "kacho", "cloud", "compute", "v1"),
		filepath.Join(root, "pkg", "api", "kacho", "cloud", "compute", "v1"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			body := readFile(t, filepath.Join(dir, e.Name()))
			for _, r := range retiredBlockStorage {
				for _, s := range r.services {
					// `service DiskService {` in proto, `type DiskServiceClient` /
					// `RegisterDiskServiceServer` in generated Go.
					for _, decl := range []string{"service " + s + " ", "type " + s + "Client ", "func Register" + s + "Server"} {
						if strings.Contains(body, decl) {
							t.Errorf("%s still declares retired service %s (%q)",
								mustRel(t, root, filepath.Join(dir, e.Name())), s, decl)
						}
					}
				}
			}
		}
	}

	// No catalog entry may scope an RPC to a retired authorization object type: that
	// is what would make the leftover type declarations reachable again.
	catalogPath := filepath.Join(root, "gateway", "internal", "middleware", "embed", "permission_catalog.json")
	var rows []struct {
		FQN            string `json:"fqn"`
		ScopeExtractor *struct {
			ObjectType string `json:"object_type"`
		} `json:"scope_extractor"`
	}
	if err := json.Unmarshal([]byte(readFile(t, catalogPath)), &rows); err != nil {
		t.Fatalf("decode permission catalog: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("permission catalog decoded to zero entries — this gate would assert nothing")
	}
	for _, row := range rows {
		if row.ScopeExtractor == nil {
			continue
		}
		for _, obj := range retiredAuthzObjects {
			if row.ScopeExtractor.ObjectType == obj {
				t.Errorf("permission catalog still scopes %s to retired object type %q — the retired resource is reachable for authorization",
					row.FQN, obj)
			}
		}
	}

	// compute itself must not name the retired object types any more: no owner-tuple
	// emission, no list filtering, no permission map entry.
	forEachComputeSource(t, root, func(rel, body string) {
		for _, obj := range retiredAuthzObjects {
			if strings.Contains(body, obj) {
				t.Errorf("%s still names retired authorization object type %q — compute must neither register nor check tuples for a resource it no longer owns", rel, obj)
			}
		}
	})
}

// TestNoLivePathReachesRetiredTables — the storage half.
//
// The subject is a STATEMENT that would hit the table, not a mention of its name:
// prose may still explain what was dropped and why, and should. Anything that reads,
// writes or alters the table is a live path by definition.
func TestNoLivePathReachesRetiredTables(t *testing.T) {
	root := moduleRootFrom(t, ".")

	type probe struct {
		table string
		re    *regexp.Regexp
	}
	var probes []probe
	for _, r := range retiredBlockStorage {
		probes = append(probes, probe{
			table: r.table,
			re:    regexp.MustCompile(`(?i)\b(from|join|into|update|table)\s+` + r.table + `\b`),
		})
	}
	if len(probes) == 0 {
		t.Fatal("no retired tables — this gate would assert nothing")
	}

	forEachComputeSource(t, root, func(rel, body string) {
		for _, p := range probes {
			if m := p.re.FindString(body); m != "" {
				t.Errorf("%s issues SQL against dropped table %q (%q) — kacho-storage owns this data; the table does not exist after the retire migration",
					rel, p.table, strings.Join(strings.Fields(m), " "))
			}
		}
	})
}

// forEachComputeSource visits every non-test compute source file that can run in
// production, and hands the callback its repo-relative path and contents.
//
// Excluded: _test.go (a test naming a retired table in a fixture is not a live
// path), and the migration directories (an applied migration is never edited, so the
// baseline CREATE and the retiring DROP must both remain readable).
func forEachComputeSource(t *testing.T, root string, visit func(rel, body string)) {
	t.Helper()
	computeDir := filepath.Join(root, "services", "compute")
	skipDirs := map[string]bool{
		filepath.Join(computeDir, "internal", "migrations"): true,
		filepath.Join(computeDir, "migrations"):             true,
	}

	visited := 0
	err := filepath.WalkDir(computeDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[path] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, ".sql") {
			return nil
		}
		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		visited++
		visit(mustRel(t, root, path), readFile(t, path))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", computeDir, err)
	}
	if visited == 0 {
		t.Fatal("no compute source files were scanned — this gate would assert nothing")
	}
}

func mustRel(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
