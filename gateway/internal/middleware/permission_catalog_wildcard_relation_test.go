// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// permission_catalog_wildcard_relation_test.go — a relation that a wildcard tuple
// satisfies narrows NOTHING, and every catalog row standing on one has to say why.
//
// Модель прав позволяет выдать отношение каждому: подстановкой `user:*` либо, с
// PRO-Robotech/kaname#829, системным классом `system_class:authenticated`. Бутстрап
// кластера делает это намеренно — пишет `cluster:<root>#viewer@system_class:authenticated`,
// чтобы ГЛОБАЛЬНЫЙ СПРАВОЧНИК (регионы, зоны, типы дисков, которые арендатор
// читает до любого размещения) читал всякий вошедший. Для этих данных так и задумано.
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
// listed: the day someone adds `user:*` or `system_class` to another relation, every catalog row
// resting on it appears here without anyone remembering to update a literal.
//
// Класс и подстановка при этом судятся по-разному (kacho#3251, REG-PUB-1):
// кортеж класса рождается только выдачей и лежит на объекте выдачи, поэтому
// строка, спрашивающая об объекте, названном запросом, сужает до объектов с
// явной выдачей классу. Правило и его инъекции — у
// TestCatalog_WildcardSatisfiableRelations_AreNamedReferenceCatalogues и в
// permission_catalog_wildcard_relation_injection_test.go.

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
	"github.com/PRO-Robotech/kacho/internal/contractsource"
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
		"kacho.cloud.geo.v1.RegionService/Get":                               why + " (каталог размещения: регионы)",
		"kacho.cloud.geo.v1.RegionService/List":                              why + " (каталог размещения: регионы)",
		"kacho.cloud.geo.v1.ZoneService/Get":                                 why + " (каталог размещения: зоны)",
		"kacho.cloud.geo.v1.ZoneService/List":                                why + " (каталог размещения: зоны)",
		"kacho.cloud.compute.v1.MachineTypeService/Get":                      why + " (инвентарь: типы машин)",
		"kacho.cloud.compute.v1.MachineTypeService/List":                     why + " (инвентарь: типы машин)",
		"kacho.cloud.storage.v1.DiskTypeService/Get":                         why + " (инвентарь: типы дисков)",
		"kacho.cloud.storage.v1.DiskTypeService/List":                        why + " (инвентарь: типы дисков)",
		"kaname.cloud.iam.v1.PermissionCatalogService/ListPermissionCatalog": why + " (словарь прав платформы)",
	}
}

// typeRelation is one (object type, relation) pair of the authorization model.
type typeRelation struct{ Type, Relation string }

// catalogRow — строка каталога в том виде, в каком её судит гейт: отношение на
// типе объекта и то, назван ли объект запросом (`concrete`, предикат края
// [middleware.IsConcreteResourceScope]).
type catalogRow struct {
	FQN      string
	TR       typeRelation
	Concrete bool
	Field    string
}

// catalogRows — все строки встроенного каталога.
func catalogRows(t *testing.T, c *middleware.PermissionCatalog) []catalogRow {
	t.Helper()
	var rows []catalogRow
	for _, fqn := range c.FQNs() {
		e, ok := c.Lookup(fqn)
		require.True(t, ok)
		rows = append(rows, catalogRow{
			FQN:      fqn,
			TR:       typeRelation{e.ScopeExtractor.ObjectType, e.RequiredRelation},
			Concrete: middleware.IsConcreteResourceScope(e),
			Field:    strings.TrimSpace(e.ScopeExtractor.FromRequestField),
		})
	}
	return rows
}

// fixedObjectTypes — типы, объект которых строка каталога берёт НЕ из запроса, а
// единственным (`from_request_field: "*"`, синглтон `cluster`). Кортеж «каждому»
// на таком объекте открывает всякий запрос, который до него доходит.
func fixedObjectTypes(rows []catalogRow) map[string]struct{} {
	out := map[string]struct{}{}
	for _, r := range rows {
		if r.Field == "*" && r.TR.Type != "" {
			out[r.TR.Type] = struct{}{}
		}
	}
	return out
}

// TestCatalog_WildcardSatisfiableRelations_AreNamedReferenceCatalogues is the gate.
//
// ЧТО СЧИТАЕТСЯ «НЕ СУЖАЕТ НИЧЕГО» (kacho#3251, REG-PUB-1). Субъектов «каждый» в
// модели два вида, и модель их различает:
//
//   - подстановка `<тип>:*` — кортеж, рождённый НЕ выдачей: перечислением выдач
//     он не виден и глаголом отзыва не снимается. Строка на таком отношении не
//     сужает ничего, где бы объект ни лежал;
//   - системный класс (`system_class`) — его выдают и отзывают ПОИМЁННО (шапка
//     `type system_class` модели). Кортеж класса лежит на объекте выдачи. Строка,
//     спрашивающая об объекте, НАЗВАННОМ запросом, сужает до объектов, на которых
//     класс выдан явно (публичный реестр — выдача роли классу с целью на реестр).
//     Не сужает ничего она тогда, когда объект строки один на всех
//     (`from_request_field: "*"`), либо когда отношение доходит до класса через
//     объект такого типа (каскад от синглтона: одна выдача на `cluster` — всем).
func TestCatalog_WildcardSatisfiableRelations_AreNamedReferenceCatalogues(t *testing.T) {
	m := loadEveryoneModel(t)

	// Anti-vacuum. If the model parse silently produced nothing, every assertion
	// below would pass while checking nothing at all — the exact failure mode this
	// gate exists to catch elsewhere.
	require.NotEmpty(t, m.sat, "no wildcard-satisfiable relation found — the model parse is broken, not the model")
	require.Contains(t, m.sat, typeRelation{"cluster", "viewer"},
		"cluster#viewer is granted to system_class:authenticated by the cluster bootstrap; a parse that misses it misses everything")
	require.Contains(t, m.sat, typeRelation{"registry_repository", "v_get"},
		"registry_repository#v_get carries the anonymous public-read class (system_class:anonymous); a parse "+
			"that misses it is mishandling the userset syntax (`group#member` is not a comment)")

	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)
	rows := catalogRows(t, c)
	fixed := fixedObjectTypes(rows)
	require.Contains(t, fixed, "cluster",
		"no catalog row addresses the cluster singleton as a fixed object — the fixed-object census is broken")

	standing, targeted := judgeRows(m, rows, fixed)
	t.Logf("перепись: строк каталога %d · на отношениях, выполнимых «каждым», %d · не сужают ничего %d · "+
		"сужены явной выдачей классу %d · типов с единственным объектом %d",
		len(rows), len(standing)+len(targeted), len(standing), len(targeted), len(fixed))

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

// judgeRows делит строки на отношениях, выполнимых «каждым», на две части:
// не сужающие ничего (`standing`) и суженные явной выдачей классу (`targeted`).
func judgeRows(m everyoneModel, rows []catalogRow, fixed map[string]struct{}) (standing, targeted map[string]typeRelation) {
	standing, targeted = map[string]typeRelation{}, map[string]typeRelation{}
	for _, r := range rows {
		if _, ok := m.sat[r.TR]; !ok {
			continue
		}
		if m.rowNarrowsNothing(r.TR, r.Concrete, fixed) {
			standing[r.FQN] = r.TR
		} else {
			targeted[r.FQN] = r.TR
		}
	}
	return standing, targeted
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

// fgaSystemClassType — тип системного класса субъектов в модели службы доступа
// (PRO-Robotech/kaname#829, приёмка anonymous-and-authenticated-are-subject-classes).
// Отношение, принимающее класс прямо, выполнимо «каждым вошедшим» так же, как
// прежде подстановкой — но на ТОМ объекте, где класс выдан. Распознаватель знает
// ОБЕ законные формы субъекта «каждый»: подстановку DSL допускает по-прежнему, и
// форма, которую он забыл, — молчание.
const fgaSystemClassType = "system_class"

// bracketHasWildcard — есть ли в перечне назначаемых типов подстановка `<тип>:*`.
func bracketHasWildcard(list string) bool { return fgaWildcardRe.MatchString(list) }

// bracketHasClass — назначаем ли системный класс.
func bracketHasClass(list string) bool {
	for _, tok := range strings.Split(list, ",") {
		f := strings.Fields(strings.SplitN(strings.TrimSpace(tok), "#", 2)[0])
		if len(f) > 0 && f[0] == fgaSystemClassType {
			return true
		}
	}
	return false
}

// everyoneModel — что модель говорит о субъектах «каждый».
type everyoneModel struct {
	// sat — (тип, отношение), выполнимые подстановкой либо классом: объединение.
	sat map[typeRelation]struct{}
	// wildcard — выполнимые подстановкой `<тип>:*` (прямо либо через вычисление).
	wildcard map[typeRelation]struct{}
	// classAnchors — для выполнимых классом: типы объектов, на которых лежит
	// кортеж класса, дающий это отношение.
	classAnchors map[typeRelation]map[string]struct{}
}

// rowNarrowsNothing — не сужает ли строка на отношении tr ничего: подстановка —
// всегда; класс — если объект строки не назван запросом либо кортеж класса лежит
// на объекте типа, который один на всех.
func (m everyoneModel) rowNarrowsNothing(tr typeRelation, concrete bool, fixed map[string]struct{}) bool {
	if _, ok := m.wildcard[tr]; ok {
		return true
	}
	anchors, ok := m.classAnchors[tr]
	if !ok {
		return false
	}
	if !concrete {
		return true
	}
	for a := range anchors {
		if _, isFixed := fixed[a]; isFixed {
			return true
		}
	}
	return false
}

// wildcardSatisfiableRelations — объединение выполнимых «каждым» (подстановкой
// либо классом), как его спрашивают соседние пробы пакета.
func wildcardSatisfiableRelations(t *testing.T) map[typeRelation]struct{} {
	t.Helper()
	return loadEveryoneModel(t).sat
}

// loadEveryoneModel reads the canonical authorization model and parses it.
//
// ГДЕ ЛЕЖИТ МОДЕЛЬ. Не под `proto/` этого дерева: решением владельца (kacho#2616,
// исход C, 2026-09-13) контракты службы доступа уехали в её репозиторий и
// приезжают сюда опубликованным модулем `github.com/PRO-Robotech/kaname`,
// каталогом `proto/kaname` внутри него. Координата разрешается
// `internal/contractsource` — один и тот же резолвер для корней этого дерева и
// для внешних, — а не собирается литералом: литерал после переезда не краснеет,
// он ОТКАЗЫВАЕТ либо МОЛЧИТ, и здесь молчание было бы зелёным вердиктом по
// непрочитанной модели.
func loadEveryoneModel(t *testing.T) everyoneModel {
	t.Helper()
	m := parseEveryoneModel(t, readCanonicalModel(t))
	require.NotEmpty(t, m.sat, "the model parsed to no everyone-satisfiable relation — the gate would be vacuous")
	return m
}

func readCanonicalModel(t *testing.T) string {
	t.Helper()
	model, err := contractsource.Path(repoRootForWildcardGate(t), "kaname/cloud/iam/v1/fga_model.fga")
	require.NoError(t, err, "the canonical authorization model must be resolvable — it is what defines the class")
	raw, err := os.ReadFile(model)
	require.NoError(t, err, "the canonical authorization model must be readable — it is what defines the class")
	return string(raw)
}

// parseEveryoneModel returns every (type, relation) a `<subject>:*` tuple or a
// system class tuple (`system_class:<имя>`) can satisfy — directly, or through a
// computed userset / tuple-to-userset that reaches one — and, for the class, on
// which object types the class tuple has to sit.
//
// Over-approximation is the safe direction here: a pair listed that turns out not
// to be reachable only forces a catalog row to be named and justified; a pair
// MISSED lets an unnarrowed row through unnoticed.
func parseEveryoneModel(t *testing.T, raw string) everyoneModel {
	t.Helper()

	// type -> relation -> expression
	types := map[string]map[string]string{}
	// type -> relation -> assignable types (the bracket list), for `X from Y`.
	assignable := map[string]map[string][]string{}

	var cur string
	for _, line := range strings.Split(raw, "\n") {
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

	m := everyoneModel{
		sat:          map[typeRelation]struct{}{},
		wildcard:     map[typeRelation]struct{}{},
		classAnchors: map[typeRelation]map[string]struct{}{},
	}
	addAnchor := func(tr typeRelation, anchor string) bool {
		if m.classAnchors[tr] == nil {
			m.classAnchors[tr] = map[string]struct{}{}
		}
		if _, had := m.classAnchors[tr][anchor]; had {
			return false
		}
		m.classAnchors[tr][anchor] = struct{}{}
		return true
	}
	for typ, rels := range types {
		for rel, expr := range rels {
			br := fgaBracketRe.FindStringSubmatch(expr)
			if br == nil {
				continue
			}
			tr := typeRelation{typ, rel}
			if bracketHasWildcard(br[1]) {
				m.wildcard[tr] = struct{}{}
			}
			if bracketHasClass(br[1]) {
				addAnchor(tr, typ)
			}
		}
	}

	// Closure over `or <rel>` (same object) and `or <rel> from <pointer>` (the
	// relation on whatever object the pointer holds). The class anchor travels
	// with the relation: through `from` it is the PARENT's object that carries it.
	for changed := true; changed; {
		changed = false
		for typ, rels := range types {
			for rel, expr := range rels {
				tr := typeRelation{typ, rel}
				body := fgaBracketRe.ReplaceAllString(expr, "")
				for _, part := range strings.Split(body, " or ") {
					part = strings.TrimSpace(part)
					var sources []typeRelation
					if fm := fgaFromRe.FindStringSubmatch(part); fm != nil {
						for _, parentType := range assignable[typ][fm[2]] {
							sources = append(sources, typeRelation{parentType, fm[1]})
						}
					} else if part != "" && fgaIdentRe.MatchString(part) {
						sources = append(sources, typeRelation{typ, part})
					}
					for _, src := range sources {
						if _, ok := m.wildcard[src]; ok {
							if _, had := m.wildcard[tr]; !had {
								m.wildcard[tr] = struct{}{}
								changed = true
							}
						}
						for anchor := range m.classAnchors[src] {
							if addAnchor(tr, anchor) {
								changed = true
							}
						}
					}
				}
			}
		}
	}
	for tr := range m.wildcard {
		m.sat[tr] = struct{}{}
	}
	for tr := range m.classAnchors {
		m.sat[tr] = struct{}{}
	}
	return m
}

func repoRootForWildcardGate(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	// .../gateway/internal/middleware/<file> -> repo root
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}
