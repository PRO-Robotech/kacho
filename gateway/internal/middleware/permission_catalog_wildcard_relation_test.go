// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// permission_catalog_wildcard_relation_test.go — a relation that a wildcard tuple
// satisfies narrows NOTHING, and every catalog row standing on one has to say why.
//
// The authorization model lets a relation be granted to `user:*`. The cluster
// bootstrap uses that deliberately: it writes `cluster:<root>#viewer@user:*` so the
// GLOBAL REFERENCE CATALOGUE — the regions, zones and disk types a tenant must be
// able to read before it can place anything — is readable by anyone
// authenticated. For that data it is the correct design.
//
// The consequence is that a catalog row asking for `viewer` on `cluster` means
// "authenticated", not "authorized". It costs a round-trip, reads in the catalog
// exactly like a row that narrows, and admits every tenant. Rows for tenant data
// carrying that shape returned other tenants' data to anyone with a token.
//
// So the gate is not "no wildcard-satisfiable relations". It is: ENUMERATE every
// row standing on one, and require each to be named here with a reason. The named
// list is the whole point — an unnamed row fails the build, and a named row that
// no longer qualifies fails it too, so the list cannot rot into a comment.
//
// The set of wildcard-satisfiable relations is COMPUTED FROM THE MODEL, not
// listed: the day someone adds `user:*` to another relation, every catalog row
// resting on it appears here without anyone remembering to update a literal.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// referenceCatalogueRPCs — the RPCs whose response IS the global reference
// catalogue, and which therefore MAY stand on a wildcard-satisfiable relation.
//
// The justification each one has to meet: the response is admin-curated platform
// topology or platform inventory, identical for every tenant, containing no
// tenant-owned row and no infrastructure detail. A tenant cannot place a machine
// or a volume without reading it, and it holds nothing that one tenant learning
// would tell them anything about another.
//
// Anything else — bindings, attachments, interfaces, authorization answers — is
// tenant data, however read-only it looks.
func referenceCatalogueRPCs() map[string]string {
	// НЕ ПУСТО — И ЭТО РЕШЕНИЕ, А НЕ ПРОБЕЛ (#893/#895).
	//
	// Прежняя редакция объявляла пустоту целью и направляла глобальный справочник
	// в полосу `<exempt>`. Довод был верен ровно наполовину: отношение, на котором
	// справочник стоял, действительно не производил никто, и проверка отвечала
	// отказом всем. Но `<exempt>` лечит это ценой, которую владелец назвал
	// неприемлемой: доступ, выданный освобождением, НЕ ВИДЕН перечислением выдач и
	// НЕ ОТЗЫВАЕТСЯ ничем, кроме выкатки, — «ничего не выдано» и «выдано в обход»
	// становятся неотличимы.
	//
	// Теперь у отношения есть производитель, и он первоклассный: СИСТЕМНАЯ ВЫДАЧА
	// с подстановочным субъектом, заведённая посевом, видимая перечислением выдач
	// и отзываемая штатно. Поэтому строка каталога честна дважды: она признаёт,
	// что отношение выполняется подстановкой (то есть означает
	// «аутентифицирован»), и указывает на объект, снятие которого этот доступ
	// закрывает.
	//
	// Обоснование, которому обязан отвечать каждый пункт, не изменилось: ответ —
	// администрируемая платформой топология либо инвентарь, одинаковый для всех
	// арендаторов, без единой арендаторской строки и без инфраструктурных
	// подробностей.
	const why = "глобальный справочник платформы: одинаков для всех арендаторов, " +
		"арендаторских строк не содержит; отношение выполняется подстановочным " +
		"субъектом СИСТЕМНОЙ ВЫДАЧИ, которая видна перечислением выдач и отзывается штатно"
	return map[string]string{
		"kacho.cloud.geo.v1.RegionService/Get":                              why + " (каталог размещения: регионы)",
		"kacho.cloud.geo.v1.RegionService/List":                             why + " (каталог размещения: регионы)",
		"kacho.cloud.geo.v1.ZoneService/Get":                                why + " (каталог размещения: зоны)",
		"kacho.cloud.geo.v1.ZoneService/List":                               why + " (каталог размещения: зоны)",
		"kacho.cloud.compute.v1.MachineTypeService/Get":                     why + " (инвентарь: типы машин)",
		"kacho.cloud.compute.v1.MachineTypeService/List":                    why + " (инвентарь: типы машин)",
		"kacho.cloud.storage.v1.DiskTypeService/Get":                        why + " (инвентарь: типы дисков)",
		"kacho.cloud.storage.v1.DiskTypeService/List":                       why + " (инвентарь: типы дисков)",
		"kaname.cloud.iam.v1.PermissionCatalogService/ListPermissionCatalog": why + " (словарь прав платформы)",
	}
}

// typeRelation is one (object type, relation) pair of the authorization model.
type typeRelation struct{ Type, Relation string }

// TestCatalog_WildcardSatisfiableRelations_AreNamedReferenceCatalogues is the gate.
func TestCatalog_WildcardSatisfiableRelations_AreNamedReferenceCatalogues(t *testing.T) {
	wildcard := wildcardSatisfiableRelations(t)

	// Anti-vacuum. If the model parse silently produced nothing, every assertion
	// below would pass while checking nothing at all — the exact failure mode this
	// gate exists to catch elsewhere.
	require.NotEmpty(t, wildcard, "no wildcard-satisfiable relation found — the model parse is broken, not the model")
	require.Contains(t, wildcard, typeRelation{"cluster", "viewer"},
		"cluster#viewer is granted to user:* by the cluster bootstrap; a parse that misses it misses everything")
	require.Contains(t, wildcard, typeRelation{"registry_repository", "v_get"},
		"registry_repository#v_get carries the anonymous public-read wildcard; a parse that misses it is "+
			"mishandling the userset syntax (`group#member` is not a comment)")

	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	standing := map[string]typeRelation{}
	for _, fqn := range c.FQNs() {
		e, ok := c.Lookup(fqn)
		require.True(t, ok)
		tr := typeRelation{e.ScopeExtractor.ObjectType, e.RequiredRelation}
		if _, isWildcard := wildcard[tr]; isWildcard {
			standing[fqn] = tr
		}
	}

	named := referenceCatalogueRPCs()

	var unnamed []string
	for fqn, tr := range standing {
		if _, ok := named[fqn]; !ok {
			unnamed = append(unnamed, fqn+" ("+tr.Relation+" on "+tr.Type+")")
		}
	}
	sort.Strings(unnamed)
	assert.Empty(t, unnamed,
		"these catalog rows gate on a relation a wildcard tuple satisfies, so they narrow NOTHING and mean "+
			"only \"authenticated\". Either the response really is the global reference catalogue — then name "+
			"it in referenceCatalogueRPCs() with the reason — or the row is declaring an authorization it does "+
			"not perform and must be brought to the lane it really implements:\n  "+
			strings.Join(unnamed, "\n  "))

	var stale []string
	for fqn := range named {
		if _, ok := standing[fqn]; !ok {
			stale = append(stale, fqn)
		}
	}
	sort.Strings(stale)
	assert.Empty(t, stale,
		"these RPCs are named as reference catalogues but no longer stand on a wildcard-satisfiable relation; "+
			"drop them from referenceCatalogueRPCs() so the list keeps meaning what it says:\n  "+
			strings.Join(stale, "\n  "))

	assert.Len(t, standing, len(named),
		"the enumerated set and the named set must coincide exactly")
}

var (
	// A DSL comment is a `#` at the start of a line or after whitespace. A `#`
	// glued to a word is a userset (`group#member`) and is part of the expression.
	fgaCommentRe  = regexp.MustCompile(`(^|\s)#.*$`)
	fgaTypeRe     = regexp.MustCompile(`^type\s+(\w+)$`)
	fgaDefineRe   = regexp.MustCompile(`^define\s+([\w]+)\s*:\s*(.+)$`)
	fgaBracketRe  = regexp.MustCompile(`\[([^\]]*)\]`)
	fgaWildcardRe = regexp.MustCompile(`\w+:\*`)
	fgaFromRe     = regexp.MustCompile(`^(\w+)\s+from\s+(\w+)$`)
	fgaIdentRe    = regexp.MustCompile(`^\w+$`)
)

// wildcardSatisfiableRelations reads the canonical authorization model and returns
// every (type, relation) a `<subject>:*` tuple can satisfy — directly, or through
// a computed userset / tuple-to-userset that reaches one.
//
// Over-approximation is the safe direction here: a pair listed that turns out not
// to be reachable only forces a catalog row to be named and justified; a pair
// MISSED lets an unnarrowed row through unnoticed.
func wildcardSatisfiableRelations(t *testing.T) map[typeRelation]struct{} {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(repoRootForWildcardGate(t), "proto/kaname/cloud/iam/v1/fga_model.fga"))
	require.NoError(t, err, "the canonical authorization model must be readable — it is what defines the class")

	// type -> relation -> expression
	types := map[string]map[string]string{}
	// type -> relation -> assignable types (the bracket list), for `X from Y`.
	assignable := map[string]map[string][]string{}

	var cur string
	for _, line := range strings.Split(string(raw), "\n") {
		s := strings.TrimSpace(fgaCommentRe.ReplaceAllString(line, ""))
		if s == "" {
			continue
		}
		if m := fgaTypeRe.FindStringSubmatch(s); m != nil {
			cur = m[1]
			types[cur] = map[string]string{}
			assignable[cur] = map[string][]string{}
			continue
		}
		if cur == "" {
			continue
		}
		m := fgaDefineRe.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		rel, expr := m[1], strings.TrimSpace(m[2])
		types[cur][rel] = expr
		if br := fgaBracketRe.FindStringSubmatch(expr); br != nil {
			for _, tok := range strings.Split(br[1], ",") {
				// "group#member" -> "group"; "user with mfa_fresh" -> "user".
				tok = strings.TrimSpace(tok)
				tok = strings.SplitN(tok, "#", 2)[0]
				tok = strings.Fields(tok)[0]
				if tok != "" {
					assignable[cur][rel] = append(assignable[cur][rel], strings.TrimSuffix(tok, ":*"))
				}
			}
		}
	}
	require.NotEmpty(t, types, "the model parsed to zero types — the gate would be vacuous")

	sat := map[typeRelation]struct{}{}
	for typ, rels := range types {
		for rel, expr := range rels {
			if br := fgaBracketRe.FindStringSubmatch(expr); br != nil && fgaWildcardRe.MatchString(br[1]) {
				sat[typeRelation{typ, rel}] = struct{}{}
			}
		}
	}

	// Closure over `or <rel>` (same object) and `or <rel> from <pointer>` (the
	// relation on whatever object the pointer holds).
	for changed := true; changed; {
		changed = false
		for typ, rels := range types {
			for rel, expr := range rels {
				if _, done := sat[typeRelation{typ, rel}]; done {
					continue
				}
				body := fgaBracketRe.ReplaceAllString(expr, "")
				for _, part := range strings.Split(body, " or ") {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					if fm := fgaFromRe.FindStringSubmatch(part); fm != nil {
						for _, parentType := range assignable[typ][fm[2]] {
							if _, ok := sat[typeRelation{parentType, fm[1]}]; ok {
								sat[typeRelation{typ, rel}] = struct{}{}
								changed = true
							}
						}
						continue
					}
					if fgaIdentRe.MatchString(part) {
						if _, ok := sat[typeRelation{typ, part}]; ok {
							sat[typeRelation{typ, rel}] = struct{}{}
							changed = true
						}
					}
				}
			}
		}
	}
	return sat
}

func repoRootForWildcardGate(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	// .../gateway/internal/middleware/<file> -> repo root
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}
