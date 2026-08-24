// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenpolicyquantity_test.go — каждая величина политики токенов объявлена
// ЧИСЛОМ ровно в одном месте (приёмка F2, сценарий F2-22).
//
// # Предмет
//
// Допуск расхождения часов и потолок длительности утверждения — величины, у
// которых цена расхождения названа приёмкой прямо. Потолок участвует в ДВУХ
// расчётах сразу: он ограничивает само утверждение и он же задаёт верхнюю
// границу жизни строки погашения. Разойдись они — строка станет либо короче
// утверждения (повтор законен), либо длиннее (хранилище растёт без границы, и
// темп роста выбирает предъявитель). Допуск задаёт границу «принять/отвергнуть»
// по времени, и двух границ у неё быть не может.
//
// # Область гейта — политика и те, кто ею пользуется
//
// Величина принадлежит ПОЛИТИКЕ токенов, поэтому опасен её повтор там, где обе
// живут в одном расчёте: у владельца политики и у всякого, кто её читает.
// Файл, о политике не знающий, объявляет СВОЮ величину о своём предмете —
// измерено, а не предположено: допуск на отметку времени доклада исполнителя
// (`services/compute/…/realization.go`, пять минут) к проверке токена
// отношения не имеет, и гейт, судящий всё дерево по имени, объявил бы его
// находкой.
//
// Область при этом РАСТЁТ САМА: поверхность, перешедшая на политику, входит в
// неё в тот же момент, и её собственное число становится находкой без правки
// этого файла. Тот же приём, что у словаря производителей проверяющего в
// `tokencheckcomposition_test.go`, где край объявлен вне области с названным
// предикатом входа.
//
// # Что здесь считается деревом
//
// Индекс git — то же множество, которое увидит свежий клон и CI.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
)

const (
	// tokenPolicyImportPath — единственный дом политики.
	tokenPolicyImportPath = "github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
	// tokenPolicyOwnerDir — каталог владельца политики.
	tokenPolicyOwnerDir = "pkg/tokenpolicy/"
	// tokenPolicyCensusFloor — порог переписи ПО ВСЕМУ дереву: ниже него
	// «область пуста» означало бы «ноль прочитанного».
	tokenPolicyCensusFloor = 1000
)

// tokenPolicyQuantity — величина политики, чьё число обязано быть одно.
type tokenPolicyQuantity struct {
	// Name — как величина называется в отказе.
	Name string
	// Concept — чем идентификатор выдаёт в себе эту величину.
	//
	// Признак по ИМЕНИ, и это названо прямо: числом величина неотличима от
	// чужой одноимённой по значению (шестьдесят секунд встречаются везде), а
	// типом — от любой другой длительности. Имя — единственное, чем величина
	// себя объявляет. Цена признака: переименование выводит объявление
	// из-под гейта, и поэтому рядом стоит проверка «величина опознана ровно
	// один раз», которая на исчезнувшем имени краснеет, а не молчит.
	Concept *regexp.Regexp
	// Want — значение, объявленное политикой. Гейт читает его из САМОЙ
	// политики: вторая копия числа внутри проверки числа была бы тем самым
	// дефектом, который проверка ищет.
	Want time.Duration
	// Not — имена, которые под Concept подходят, а величиной ЭТОЙ не являются.
	//
	// Введено с задачи #1124, где рядом с потолком длительности утверждения
	// появился ВТОРОЙ, федеративный: величины разные (у полос разные подписанты
	// и разные сроки), а имя второй подходит под образец первой. Без
	// исключающего образца гейт объявил бы две разные величины одной — то есть
	// краснел бы на исправном дереве и был бы снят первым же обходом.
	//
	// Отрицающий образец, а не порядок перебора: порядок был бы неявным
	// свойством перечня, а исключение обязано быть ВИДНО у той величины,
	// которой оно принадлежит. RE2 не умеет заглядывать вперёд, поэтому
	// отдельным полем.
	Not *regexp.Regexp
}

// Matches отвечает, называет ли идентификатор эту величину.
func (q tokenPolicyQuantity) Matches(name string) bool {
	if q.Not != nil && q.Not.MatchString(name) {
		return false
	}
	return q.Concept.MatchString(name)
}

// tokenPolicyQuantities — стережённые величины.
//
// Перечень закрыт: величина, которой здесь нет, гейтом не стережётся, и её
// добавление обязано быть решением, а не умолчанием.
var tokenPolicyQuantities = []tokenPolicyQuantity{
	{
		Name:    "допуск расхождения часов",
		Concept: regexp.MustCompile(`(?i)(clock)?skew`),
		Want:    tokenpolicy.ClockSkew,
	},
	{
		Name:    "потолок длительности утверждения",
		Concept: regexp.MustCompile(`(?i)assertion.*(lifetime|ttl|maxage)|(max|ceiling).*assertion`),
		// Федеративный потолок — ДРУГАЯ величина, и стережётся он записью ниже.
		Not:  regexp.MustCompile(`(?i)federated`),
		Want: tokenpolicy.MaxAssertionLifetime,
	},
	{
		Name:    "федеративный потолок длительности утверждения",
		Concept: regexp.MustCompile(`(?i)federated.*(lifetime|ttl|maxage)|(max|ceiling).*federated`),
		Want:    tokenpolicy.MaxFederatedAssertionLifetime,
	},
	// Задача #1142, приёмка BAT-1 §7: величина, которой в перечне нет, не
	// стережётся ничем. Обе внесены решением, а не умолчанием.
	{
		Name:    "потолок срока базового секрета",
		Concept: regexp.MustCompile(`(?i)secret.*(credential)?.*ttl.*ceiling|ceiling.*secret.*ttl`),
		Want:    tokenpolicy.SecretCredentialTTLCeiling,
	},
	{
		Name:    "умолчание срока базового секрета",
		Concept: regexp.MustCompile(`(?i)secret.*(credential)?.*ttl.*default|default.*secret.*ttl`),
		Want:    tokenpolicy.SecretCredentialTTLDefault,
	},
}

// TestTokenPolicyQuantityIsDeclaredOnce — сам гейт.
func TestTokenPolicyQuantityIsDeclaredOnce(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	// (1) Предпосылка: величины политики заданы и положительны. Ноль означал бы
	// «не задано», и стеречь было бы нечего.
	for _, q := range tokenPolicyQuantities {
		if q.Want <= 0 {
			t.Fatalf("величина «%s» объявлена политикой как %v — не положительна, то есть "+
				"не задана. Гейт беспредметен: он молчал бы и тогда, когда предмет исчез.",
				q.Name, q.Want)
		}
	}

	var all []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if skipPath(rel) {
			continue
		}
		all = append(all, rel)
	}
	sort.Strings(all)

	// (2) Область: владелец политики и всякий, кто её читает.
	var (
		scope     []string
		scanned   int
		outside   int
		specs     int
		durations int
		unresolv  int
		found     []DurationDeclaration
	)
	for _, rel := range all {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		scanned++
		inScope := strings.HasPrefix(rel, tokenPolicyOwnerDir) ||
			strings.Contains(string(src), `"`+tokenPolicyImportPath+`"`)
		if !inScope {
			outside++
			continue
		}
		scope = append(scope, rel)
		decls, census, err := ScanDurationDeclarations(rel, src)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		specs += census.ValueSpecs
		durations += census.Durations
		unresolv += census.Unresolved
		found = append(found, decls...)
	}

	t.Logf("перепись: не-тестовых файлов Go в дереве %d, из них в области политики %d "+
		"(вне области %d), объявлений const/var прочитано %d, из них длительностей %d, "+
		"из них не сведённых к числу %d",
		scanned, len(scope), outside, specs, durations, unresolv)

	if scanned < tokenPolicyCensusFloor {
		t.Fatalf("перепись обвалилась: прочитано %d файлов при пороге %d — на таком объёме "+
			"«ноль находок» означало бы «ноль прочитанного»", scanned, tokenPolicyCensusFloor)
	}
	// (3) Предпосылка области: читатели политики в дереве есть. Пустая область
	// оставила бы гейт зелёным навсегда и при любом дефекте.
	if len(scope) == 0 {
		t.Fatalf("в области политики НОЛЬ файлов: ни одного читателя %s в дереве не найдено. "+
			"Гейт молчал бы при любом числе объявлений — область, в которой нечего смотреть, "+
			"утверждением не является.", tokenPolicyImportPath)
	}
	if durations == 0 {
		t.Fatalf("в области политики (%d файлов) объявлений длительности НОЛЬ — разбор "+
			"перестал видеть предмет, и его молчание сказано ни о чём", len(scope))
	}

	// (4) Находка: величина объявлена числом больше одного раза либо ни разу.
	var problems []string
	for _, q := range tokenPolicyQuantities {
		var hits []DurationDeclaration
		for _, d := range found {
			if q.Matches(d.Name) {
				hits = append(hits, d)
			}
		}
		sort.Slice(hits, func(i, j int) bool {
			if hits[i].File != hits[j].File {
				return hits[i].File < hits[j].File
			}
			return hits[i].Line < hits[j].Line
		})

		switch {
		case len(hits) == 0:
			problems = append(problems, fmt.Sprintf(
				"«%s» не объявлена ЧИСЛОМ ни в одном месте области. Величина, которую "+
					"никто не объявляет, приезжает умолчанием того, кто её читает, — а "+
					"умолчаний столько же, сколько читателей", q.Name))
		case len(hits) > 1:
			var where []string
			for _, h := range hits {
				where = append(where, fmt.Sprintf("%s:%d  %s = %s", h.File, h.Line, h.Name, h.Expr))
			}
			problems = append(problems, fmt.Sprintf(
				"«%s» объявлена числом в %d местах:\n    %s",
				q.Name, len(hits), strings.Join(where, "\n    ")))
		default:
			h := hits[0]
			if !h.Resolved {
				problems = append(problems, fmt.Sprintf(
					"«%s» (%s:%d) объявлена выражением %q, а не числом: величина не выбрана, "+
						"а выведена, и проверить её значением нечем", q.Name, h.File, h.Line, h.Expr))
				continue
			}
			if h.Nanos != int64(q.Want) {
				problems = append(problems, fmt.Sprintf(
					"«%s» (%s:%d) объявлена как %v, а политика отдаёт %v — разбор читает "+
						"не то объявление, и стережёт он не ту величину",
					q.Name, h.File, h.Line, time.Duration(h.Nanos), q.Want))
			}
		}
	}

	if len(problems) > 0 {
		t.Fatalf("величины политики токенов объявлены не по одному разу:\n  %s\n\n"+
			"Второе объявление одной величины не расходится с первым сразу — оно расходится "+
			"при первой же правке одной стороны, и расходится там, где расхождение не видно: "+
			"обе величины по отдельности выглядят разумными. Потолок длительности "+
			"утверждения участвует В ДВУХ расчётах: он ограничивает само утверждение и он же "+
			"задаёт верхнюю границу жизни строки погашения. Разойдясь, они делают строку либо "+
			"короче утверждения (повтор становится законным), либо длиннее (хранилище растёт "+
			"без границы).\n"+
			"Снятие: читать величину из %s, а не объявлять своё число.",
			strings.Join(problems, "\n  "), tokenPolicyImportPath)
	}

	for _, q := range tokenPolicyQuantities {
		for _, d := range found {
			if q.Matches(d.Name) {
				t.Logf("«%s»: единственное объявление %s:%d — %s = %s (%v)",
					q.Name, d.File, d.Line, d.Name, d.Expr, time.Duration(d.Nanos))
			}
		}
	}
	// Законный близнец В ДЕРЕВЕ: величины политики, стережёнными не являющиеся,
	// объявлены каждая по одному разу и находкой не становятся. Без этой строки
	// молчание гейта было бы верно и для гейта, считающего длительности.
	var others []string
	for _, d := range found {
		watched := false
		for _, q := range tokenPolicyQuantities {
			if q.Matches(d.Name) {
				watched = true
			}
		}
		if !watched {
			others = append(others, fmt.Sprintf("%s:%d %s", d.File, d.Line, d.Name))
		}
	}
	sort.Strings(others)
	t.Logf("прочие длительности области, находкой не являющиеся (%d): %s",
		len(others), strings.Join(others, ", "))
}
