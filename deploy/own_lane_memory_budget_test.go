// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_lane_memory_budget_test.go — ПРОФИЛЬ ЗОНТА, ОБЪЯВИВШИЙ ЧИСЛА БЮДЖЕТА
// ПОЛОСЫ ВХОДА, ОБЯЗАН ОБЪЯВИТЬ И ПРЕДЕЛ ПАМЯТИ КОНТЕЙНЕРА — ПО АРИФМЕТИКЕ
// СТРАЖА, А НЕ «ПОБОЛЬШЕ» (kacho#2709, Ф3-42, ID-PW-1 PWV-15.7).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Полоса входа паролем под `own` сверяет свой бюджет с пределом СРЕДЫ (cgroup
// контейнера), а не с настройкой, и отказывает в старте, когда предел меньше
// бюджета либо не наложен вовсе. Бюджет = ёмкость × память одной проверки на
// потолке формата + резерв.
//
// Признак на день заведения (kacho `origin/main` @ `ac33f733`):
//
//	git show origin/main:deploy/helm/umbrella/charts/kaname/values.yaml \
//	  | grep -A5 '^resources:'                      → limits.memory: 512Mi
//	… values.prod.yaml | python3 -c '…["kaname"].get("resources")'   → None
//	grep -n 'verifierCapacity\|memoryReserveBytes' values.prod.yaml  → 8 · 268435456
//
// То есть боевой профиль объявлял ДВА числа бюджета из трёх, а предел приходил
// умолчанием подчарта — 512 МиБ против бюджета 1280 МиБ. Под, переведённый на
// `own`, прошёл бы рендер и ВСЕХ стражей посадки и лёг бы стражем полосы: профиль
// обещал посадку, которой не будет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПОТОЛОК ЧИТАЕТСЯ У ПИНА, А НЕ ВЫПИСАН ЗДЕСЬ
//
// Своя копия арифметики разошлась бы со стражем молча — на первой же смене
// потолка формата записи пароля. Проба чарта самой службы (kaname#251,
// `deploy/own_lane_memory_ceiling_test.go`) зовёт `ValidateMemoryBudget` прямо;
// отсюда это НЕВОЗМОЖНО: пакет службы лежит под `internal/`, и модуль платформы
// его не импортирует by construction.
//
// Поэтому величина не копируется, а ЧИТАЕТСЯ — разбором таблицы форматов у
// пиненного модуля, по тому же правилу, что применяет сам страж: худший формат
// перечня, память на его потолке. Разбор СТРУКТУРНЫЙ (go/ast), а не по образцу:
// у записи есть и `Ceiling`, и `Floor`, оба несут тот же параметр, и текстовый
// поиск взял бы не ту карту. Сменится форма таблицы — разбор не найдёт ничего и
// ОТКАЖЕТ; молча устареть он не может, и это единственное, что отличает чтение
// источника истины от его копии.
//
// ─────────────────────────────────────────────────────────────────────────────
// ДВА ВОПРОСА, И ВТОРОЙ НЕ ВАКУУМЕН СЕГОДНЯ
//
//	Р1  стенд на посадке `own` — предел обязан быть объявлен и обязан покрывать
//	    бюджет: там это условие СТАРТА;
//	Р2  стенд, объявивший ёмкость и резерв, — предел обязан покрывать бюджет,
//	    даже если стенд стоит на `external`. Два числа бюджета из трёх есть
//	    обещание, которое стенд не сможет сдержать в тот момент, когда его
//	    переведут; а переносить их в профиль заранее — решение приёмки Ф3 (Р3,
//	    Р11), а не случайность.
//
// Профилей на `own` сегодня может быть ноль — перепись печатает это числом,
// поэтому «находок нет» отличимо от «судить было нечего». Р2 предметен уже
// сейчас: ёмкость и резерв объявляет боевой профиль.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОБА НЕ УТВЕРЖДАЕТ
//
// Она не утверждает, что предел ДОСТАТОЧЕН процессу под нагрузкой: это свойство
// прогона, и его судит замер. Она утверждает, что страж старта примет этот вход,
// — то есть что профиль и страж говорят об одном числе.
package deploy_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// laneBudgetFacts — числа бюджета одного стенда, прочитанные из его цепочки.
type laneBudgetFacts struct {
	Stack    string
	Posture  string
	Capacity int64  // authn.login.verifierCapacity
	Reserve  uint64 // authn.login.memoryReserveBytes
	Limit    uint64 // resources.limits.memory, в байтах
	Declared bool   // объявлены ли ёмкость И резерв
	HasLimit bool   // объявлен ли предел памяти
}

// laneBudgetCensus — объём осмотренного: три величины, потому что «находок нет»
// обязано быть отличимо и от «стендов ноль», и от «на own ноль».
type laneBudgetCensus struct {
	Stacks        int
	Own           int
	DeclaringLane int
	PerCheckBytes uint64
}

func (c laneBudgetCensus) String() string {
	return fmt.Sprintf("стендов осмотрено %d · из них на own %d · объявляют числа полосы %d · "+
		"память одной проверки на потолке %d байт", c.Stacks, c.Own, c.DeclaringLane, c.PerCheckBytes)
}

// judgeLaneMemoryBudget — НАХОДКИ по перечню стендов.
//
// Чистая функция: инъекция подаёт ей синтетический вход и не трогает ни дерева,
// ни кэша модулей.
func judgeLaneMemoryBudget(facts []laneBudgetFacts, perCheck uint64) ([]string, laneBudgetCensus) {
	var (
		findings []string
		census   = laneBudgetCensus{PerCheckBytes: perCheck}
	)
	for _, f := range facts {
		census.Stacks++
		own := f.Posture == "own"
		if own {
			census.Own++
		}
		if f.Declared {
			census.DeclaringLane++
		}
		// Стенд, не объявивший чисел полосы и стоящий не на `own`, предметом не
		// является: законный близнец, проверяется инъекцией.
		if !own && !f.Declared {
			continue
		}
		if own && !f.Declared {
			findings = append(findings, fmt.Sprintf(
				"стенд %s на посадке own: ёмкость и резерв полосы не объявлены — страж не с чем "+
					"сверять бюджет, и служба откажет в старте", f.Stack))
			continue
		}
		if !f.HasLimit {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: `kaname.resources.limits.memory` не объявлен, а ёмкость (%d) и резерв (%d) "+
					"объявлены. Предел приносит СРЕДА, и без него страж полосы отказывает в старте: "+
					"«предел памяти средой не наложен — сверять не с чем». Два числа бюджета из трёх "+
					"есть обещание, которого стенд не сдержит",
				f.Stack, f.Capacity, f.Reserve))
			continue
		}
		if f.Capacity <= 0 {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: ёмкость проверяющего объявлена непозитивной (%d) — страж отвергает такую "+
					"величину до всякой арифметики", f.Stack, f.Capacity))
			continue
		}
		need := uint64(f.Capacity)*perCheck + f.Reserve
		if need > f.Limit {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: бюджет полосы %d байт (ёмкость %d × %d байт на проверку + резерв %d) "+
					"ПРЕВЫШАЕТ предел памяти контейнера %d байт — под пройдёт рендер и всех стражей "+
					"посадки и ляжет стражем полосы (ID-PW-1 PWV-15.7). Поднимите предел до %d байт "+
					"либо уменьшите ёмкость",
				f.Stack, need, f.Capacity, perCheck, f.Reserve, f.Limit, need))
		}
	}
	return findings, census
}

// memoryPerVerificationAtCeiling — память ОДНОЙ проверки на потолке самого
// дорогого формата перечня, прочитанная у пиненного модуля.
//
// Повторяет правило стража дословно: по каждой записи берётся потолок памяти
// argon2, а у записи без него — постоянная bcrypt; из полученных выбирается
// худшее.
func memoryPerVerificationAtCeiling(t *testing.T, moduleDir string) uint64 {
	t.Helper()
	path := filepath.Join(moduleDir, "internal", "domain", "password_hash_format.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("таблица форматов пиненного модуля не разбирается (%s): %v.\n"+
			"Это «не выполнилось», а не «бюджет сходится»: непрочитанное утверждением не является", path, err)
	}

	var bcryptBytes uint64
	var worst uint64
	records := 0

	ast.Inspect(file, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if name.Name == "bcryptMemoryPerVerificationBytes" && i < len(vs.Values) {
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok {
					if v, err := strconv.ParseUint(lit.Value, 10, 64); err == nil {
						bcryptBytes = v
					}
				}
			}
			if name.Name != "passwordHashFormats" || i >= len(vs.Values) {
				continue
			}
			lit, ok := vs.Values[i].(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, elt := range lit.Elts {
				rec, ok := elt.(*ast.CompositeLit)
				if !ok {
					continue
				}
				records++
				// Берётся ИМЕННО `Ceiling`: у записи есть и `Floor` с тем же
				// параметром, и текстовый поиск взял бы не ту карту.
				if m, ok := argon2MemoryOf(rec, "Ceiling"); ok {
					if b := m * 1024; b > worst {
						worst = b
					}
				} else if bcryptBytes > worst {
					worst = bcryptBytes
				}
			}
		}
		return true
	})

	if records == 0 || worst == 0 {
		t.Fatalf("в таблице форматов пиненного модуля не разобрано НИ ОДНОЙ записи с потолком "+
			"(%s: записей %d, худшее %d) — форма таблицы сменилась, и арифметику стража выводить "+
			"больше нечем. Почини разбор, а не выписывай число рядом: выписанное разойдётся "+
			"со стражем молча", path, records, worst)
	}
	return worst
}

// argon2MemoryOf — потолок памяти argon2 из названной карты записи.
func argon2MemoryOf(rec *ast.CompositeLit, field string) (uint64, bool) {
	for _, e := range rec.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != field {
			continue
		}
		m, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			return 0, false
		}
		for _, me := range m.Elts {
			mkv, ok := me.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			mk, ok := mkv.Key.(*ast.Ident)
			if !ok || mk.Name != "CostParamArgon2Memory" {
				continue
			}
			lit, ok := mkv.Value.(*ast.BasicLit)
			if !ok {
				return 0, false
			}
			v, err := strconv.ParseUint(lit.Value, 10, 64)
			if err != nil {
				return 0, false
			}
			return v, true
		}
		return 0, false
	}
	return 0, false
}

// TestOwnLaneMemoryBudget_ProfilesDeclareALimitThatTheGuardAccepts — сверка по
// дереву: перечень стендов из единственной таблицы, потолок формата из пина.
func TestOwnLaneMemoryBudget_ProfilesDeclareALimitThatTheGuardAccepts(t *testing.T) {
	perCheck := memoryPerVerificationAtCeiling(t, kanameModuleDir(t, ".."))
	facts := readLaneBudgetFacts(t)
	if len(facts) == 0 {
		t.Fatal("таблица стендов пуста: обход беспредметен, и «находок нет» здесь означало бы " +
			"«сверять было не с чем»")
	}

	findings, census := judgeLaneMemoryBudget(facts, perCheck)
	t.Logf("перепись: %s", census)
	for _, f := range facts {
		if !f.Declared && f.Posture != "own" {
			continue
		}
		t.Logf("  %s: posture=%s ёмкость=%d резерв=%d предел=%d бюджет=%d",
			f.Stack, f.Posture, f.Capacity, f.Reserve, f.Limit,
			uint64(maxInt64(f.Capacity, 0))*perCheck+f.Reserve)
	}
	if census.DeclaringLane == 0 && census.Own == 0 {
		t.Fatal("ни один стенд не объявил ни посадки own, ни чисел полосы — вердикт беспредметен")
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// readLaneBudgetFacts — числа из дерева: цепочка каждого стенда накладывается
// слева направо поверх базовых значений подчарта, ровно как её получает helm.
func readLaneBudgetFacts(t *testing.T) []laneBudgetFacts {
	t.Helper()
	stacksTbl := deployStacks(t)

	names := make([]string, 0, len(stacksTbl))
	for n := range stacksTbl {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]laneBudgetFacts, 0, len(names))
	for _, name := range names {
		// Базовые значения подчарта читаются ЗАНОВО на каждый стенд: `mergeValues`
		// правит карту НА МЕСТЕ, и одна общая карта умолчаний протекала бы из
		// стенда в стенд — ровно так этот предикат сперва и соврал, приписав
		// профилю `prorobotech` числа полосы, объявленные боевым.
		declared := map[string]any{"kaname": readYAML(t, filepath.Join(kanameSubchart(t), "values.yaml"))}
		for _, p := range stacksTbl[name] {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		f := laneBudgetFacts{Stack: name}
		if p, ok := lookup(declared, "kaname", "config", "authn", "identityProvider"); ok {
			f.Posture = fmt.Sprint(p)
		}
		cap, okCap := lookup(declared, "kaname", "config", "authn", "login", "verifierCapacity")
		res, okRes := lookup(declared, "kaname", "config", "authn", "login", "memoryReserveBytes")
		if okCap && okRes {
			f.Declared = true
			f.Capacity = asInt64(t, name, "verifierCapacity", cap)
			f.Reserve = uint64(maxInt64(asInt64(t, name, "memoryReserveBytes", res), 0))
		}
		if lim, ok := lookup(declared, "kaname", "resources", "limits", "memory"); ok {
			// Величина читается КОЛИЧЕСТВОМ Kubernetes, а не строкой: «1280Mi»,
			// «1342177280» и «1.28G» — одно и то же для среды и разное для глаза.
			bytes, err := parseMemoryQuantity(fmt.Sprint(lim))
			if err != nil {
				t.Fatalf("стенд %s: `kaname.resources.limits.memory` = %v не читается количеством "+
					"Kubernetes (%v) — такой профиль отвергнет apiserver, а не только эта проба",
					name, lim, err)
			}
			f.HasLimit = true
			f.Limit = bytes
		}
		out = append(out, f)
	}
	return out
}

func asInt64(t *testing.T, stack, key string, v any) int64 {
	t.Helper()
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		if err != nil {
			t.Fatalf("стенд %s: `%s` = %q не число (%v)", stack, key, n, err)
		}
		return parsed
	default:
		t.Fatalf("стенд %s: `%s` = %v непонятного вида %T", stack, key, v, v)
		return 0
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// parseMemoryQuantity — количество памяти Kubernetes в байтах.
//
// Разбор ФОРМАТА, а не арифметика стража: суффиксы — часть контракта Kubernetes,
// а не решение службы, и меняться вместе с потолком формата записи пароля они не
// могут. Сама арифметика по-прежнему читается у пина (memoryPerVerificationAtCeiling),
// и вот её копировать было бы нельзя.
//
// Библиотека Kubernetes сюда не тянется ради двадцати строк: `k8s.io/apimachinery`
// модулем платформы не требуется, и заводить ребро сборки ради разбора суффикса
// значило бы платить за это каждой сборке дерева.
func parseMemoryQuantity(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("пустое количество")
	}
	suffixes := []struct {
		suffix string
		mult   uint64
	}{
		{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40},
		{"k", 1e3}, {"M", 1e6}, {"G", 1e9}, {"T", 1e12},
	}
	mult := uint64(1)
	num := s
	for _, sf := range suffixes {
		if strings.HasSuffix(s, sf.suffix) {
			mult = sf.mult
			num = strings.TrimSuffix(s, sf.suffix)
			break
		}
	}
	n, err := strconv.ParseUint(num, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("количество %q: %w", s, err)
	}
	return n * mult, nil
}
