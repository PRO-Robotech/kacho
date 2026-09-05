// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// membershipreadrelation.go — разбор для гейта «чем гейтится чтение членства»
// (IAM-ID-2-06).
//
// # ПРЕДМЕТ, И ОН НЕ В ПРИСУТСТВИИ ЗАПИСИ
//
// Запись каталога у чтения членства может быть полной, согласованной и при этом
// НЕВЕРНОЙ: глагольное отношение объекта аккаунта (`v_get`/`v_list`) выглядит
// уместным на глагольном чтении, проходит все прочие проверки каталога — и
// оставляет ДЕЛЕГИРОВАННОГО распорядителя аккаунта без чтения членств в своём
// же аккаунте. То есть ломает ровно того, ради кого чтение заведено.
//
// Причина в модели: глагольные отношения объекта аккаунта намеренно НЕ читают
// ярус распорядителя — аккаунт есть ГРАНИЦА его области, а не вещь внутри неё.
// Ярусное `viewer` читает: оно выводится через `editor` и `admin`.
//
// Поэтому гейт утверждает ВЫБОР — сравнением ДВУХ объявлений модели, а не
// присутствием одного. Присутствие ловит опечатку; сравнение ловит ошибку.
//
// # ГРАНИЦА ПРЕДМЕТА НАЗВАНА
//
// Судятся ТОЛЬКО чтения членства. Прочие аккаунт-скоупные записи каталога
// (их в дереве больше десятка) под правило «ярус обязателен» не подводятся, и
// это не послабление, а разные предметы: `AccountService/Get` читает САМ объект
// аккаунта, и глагольное отношение там выбрано осознанно. Правило, растянутое
// на всю область, объявило бы находкой чужой намеренный выбор — и было бы
// снято первым же ложным срабатом.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

const (
	mrrCatalog   = "gateway/internal/middleware/embed/permission_catalog.json"
	mrrModel     = "proto/kacho/cloud/iam/v1/fga_model.fga"
	mrrScopeType = "account"
	// mrrTierAnchor — ярус распорядителя, который отношение обязано читать.
	mrrTierAnchor = "admin"
	// mrrWildcard — подстановочный субъект. Отношение, чей набор его принимает,
	// означает «аутентифицирован», а не «авторизован».
	mrrWildcard = "user:*"
)

// mrrSubjects — RPC, которые этот гейт судит. Перечень УЗКИЙ намеренно: граница
// предмета названа в шапке.
var mrrSubjects = []string{
	"kacho.cloud.iam.v1.MembershipService/Get",
	"kacho.cloud.iam.v1.MembershipService/List",
}

// mrrVerbPrefix — приставка, по которой отношение опознаётся как ГЛАГОЛЬНОЕ.
//
// Вторая сторона сравнения ВЫВОДИТСЯ ИЗ МОДЕЛИ, а не перечисляется здесь. Это
// не стилистика: перечень, лежащий в одном пакете с гейтом, гейт вправе
// импортировать — и тогда ожидаемое значение бралось бы из литерала, а не из
// предмета. Гарантия «стороны недостижимы» держалась бы добросовестностью;
// выведенная сторона не требует её вовсе.
//
// Следствие, ради которого это и сделано: заведут у типа третий глагол — он
// попадёт в сравнение САМ, а не будет ждать, пока кто-нибудь допишет его сюда.
const mrrVerbPrefix = "v_"

// mrrVerbRelationsOf — глагольные отношения типа, выведенные из его объявления.
func mrrVerbRelationsOf(defs map[string]string) []string {
	var out []string
	for rel := range defs {
		if strings.HasPrefix(rel, mrrVerbPrefix) {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// MRREntry — запись каталога, как её читает край.
type MRREntry struct {
	FQN              string `json:"fqn"`
	Permission       string `json:"permission"`
	RequiredRelation string `json:"required_relation"`
	ScopeFiltered    bool   `json:"scope_filtered"`
	ScopeExtractor   struct {
		ObjectType       string `json:"object_type"`
		FromRequestField string `json:"from_request_field"`
	} `json:"scope_extractor"`
}

// MRRCensus — исход. Объём осмотренного входит в исход.
type MRRCensus struct {
	CatalogRows int
	Relations   int
	Subjects    []MRREntry
	// TierReaders — какие из рассмотренных отношений читают ярус распорядителя.
	TierReaders map[string]bool
	// WildcardSat — какие выполнимы подстановочным субъектом.
	WildcardSat map[string]bool
	// VerbRelations — вторая сторона сравнения, ВЫВЕДЕННАЯ из модели.
	VerbRelations []string
	Findings      []string
}

var (
	mrrTypeRe = regexp.MustCompile(`(?m)^type\s+(\w+)\s*$`)
	mrrDefRe  = regexp.MustCompile(`(?m)^\s*define\s+(\w+)\s*:\s*(.*)$`)
)

// SurveyMembershipReadRelation сводит запись каталога с моделью прав.
func SurveyMembershipReadRelation(tree *treecorpus.Tree) (MRRCensus, error) {
	var c MRRCensus
	c.TierReaders = map[string]bool{}
	c.WildcardSat = map[string]bool{}

	raw, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(mrrCatalog)))
	if err != nil {
		return c, fmt.Errorf("каталог прав не читается: %w", err)
	}
	var rows []MRREntry
	if err := json.Unmarshal(raw, &rows); err != nil {
		return c, fmt.Errorf("каталог прав не разбирается: %w", err)
	}
	c.CatalogRows = len(rows)

	byFQN := map[string]MRREntry{}
	for _, r := range rows {
		byFQN[r.FQN] = r
	}

	defs, err := mrrRelationDefs(tree)
	if err != nil {
		return c, err
	}
	c.Relations = len(defs)

	verbs := mrrVerbRelationsOf(defs)
	c.VerbRelations = verbs
	want := map[string]bool{}
	for _, rel := range verbs {
		want[rel] = true
	}

	for _, fqn := range mrrSubjects {
		e, ok := byFQN[fqn]
		if !ok {
			c.Findings = append(c.Findings, fqn+
				": записи в каталоге прав НЕТ — край отвечает fail-closed отказом "+
				"в рантайме, а не «работает по умолчанию»")
			continue
		}
		c.Subjects = append(c.Subjects, e)
		want[e.RequiredRelation] = true

		if e.ScopeFiltered {
			c.Findings = append(c.Findings, fqn+
				": объявлена полоса сужения на данных. У этого чтения есть ОДИН объект, "+
				"про который край задаёт ОДИН вопрос — аккаунт из пути; сужение сняло бы "+
				"пообъектную проверку края и заменило её кодом, который можно забыть")
		}
		if e.RequiredRelation == "" {
			c.Findings = append(c.Findings, fqn+": отношение не названо — гейтить нечем")
		}
		if e.ScopeExtractor.ObjectType != mrrScopeType || e.ScopeExtractor.FromRequestField != "account_id" {
			c.Findings = append(c.Findings, fmt.Sprintf(
				"%s: область — {%q, %q}, а обязана быть {%q, %q}: объект вопроса есть "+
					"аккаунт ИЗ ПУТИ, и другого у этого чтения нет",
				fqn, e.ScopeExtractor.ObjectType, e.ScopeExtractor.FromRequestField,
				mrrScopeType, "account_id"))
		}
	}

	for rel := range want {
		if rel == "" {
			continue
		}
		c.TierReaders[rel] = mrrDerivesFrom(defs, rel, mrrTierAnchor, map[string]bool{})
		c.WildcardSat[rel] = mrrAcceptsWildcard(defs, rel, map[string]bool{})
	}

	for _, e := range c.Subjects {
		rel := e.RequiredRelation
		if rel == "" {
			continue
		}
		if c.WildcardSat[rel] {
			c.Findings = append(c.Findings, fmt.Sprintf(
				"%s: отношение %q выполнимо подстановочным кортежем %s — такая проверка "+
					"отвечает «да» каждому аутентифицированному субъекту, то есть означает "+
					"«аутентифицирован», а не «авторизован»", e.FQN, rel, mrrWildcard))
		}
		if !c.TierReaders[rel] {
			c.Findings = append(c.Findings, fmt.Sprintf(
				"%s: отношение %q НЕ читает ярус распорядителя (%q). Делегированный "+
					"распорядитель аккаунта остался бы без чтения членств в СВОЁМ аккаунте — "+
					"то есть сломан адресат, ради которого чтение и заведено",
				e.FQN, rel, mrrTierAnchor))
		}
	}
	sort.Strings(c.Findings)
	return c, nil
}

// mrrRelationDefs — определения отношений типа `account` из модели прав.
func mrrRelationDefs(tree *treecorpus.Tree) (map[string]string, error) {
	body, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(mrrModel)))
	if err != nil {
		return nil, fmt.Errorf("модель прав не читается: %w", err)
	}
	s := string(body)
	loc := mrrTypeRe.FindAllStringSubmatchIndex(s, -1)
	block := ""
	for i, m := range loc {
		if s[m[2]:m[3]] != mrrScopeType {
			continue
		}
		end := len(s)
		if i+1 < len(loc) {
			end = loc[i+1][0]
		}
		block = s[m[1]:end]
	}
	defs := map[string]string{}
	for _, m := range mrrDefRe.FindAllStringSubmatch(block, -1) {
		defs[m[1]] = m[2]
	}
	return defs, nil
}

// mrrDerivesFrom — выводится ли `rel` из `anchor` (транзитивно по дизъюнктам
// того же типа).
func mrrDerivesFrom(defs map[string]string, rel, anchor string, seen map[string]bool) bool {
	if rel == anchor {
		return true
	}
	if seen[rel] {
		return false
	}
	seen[rel] = true
	for _, d := range mrrDisjuncts(defs[rel]) {
		if _, ok := defs[d]; ok && mrrDerivesFrom(defs, d, anchor, seen) {
			return true
		}
	}
	return false
}

// mrrAcceptsWildcard — принимает ли набор отношения подстановочный субъект,
// транзитивно.
func mrrAcceptsWildcard(defs map[string]string, rel string, seen map[string]bool) bool {
	if seen[rel] {
		return false
	}
	seen[rel] = true
	def, ok := defs[rel]
	if !ok {
		return false
	}
	if i := strings.Index(def, "["); i >= 0 {
		if j := strings.Index(def[i:], "]"); j >= 0 && strings.Contains(def[i:i+j], mrrWildcard) {
			return true
		}
	}
	for _, d := range mrrDisjuncts(def) {
		if _, ok := defs[d]; ok && mrrAcceptsWildcard(defs, d, seen) {
			return true
		}
	}
	return false
}

// mrrDisjuncts — имена отношений, стоящие дизъюнктами определения (прямой набор
// в скобках отбрасывается: там субъекты, а не отношения).
func mrrDisjuncts(def string) []string {
	if i := strings.Index(def, "["); i >= 0 {
		if j := strings.Index(def[i:], "]"); j >= 0 {
			def = def[:i] + def[i+j+1:]
		}
	}
	var out []string
	for _, tok := range strings.FieldsFunc(def, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '#' || r == ','
	}) {
		if tok == "or" || tok == "and" || tok == "but" || tok == "not" || tok == "from" || tok == "" {
			continue
		}
		out = append(out, tok)
	}
	return out
}
