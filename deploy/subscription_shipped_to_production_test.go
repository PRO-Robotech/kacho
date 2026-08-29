// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// subscription_shipped_to_production_test.go — возможность, включённая ХОТЬ ГДЕ,
// включена и в БОЕВОЙ посадке.
//
// # Предмет — поставка, а не код
//
// Ручка потока изменений, её пять величин посадки и владельцы журналов были
// сделаны все до одного, а перечень владельцев не назывался НИ ОДНИМ профилем и
// нёс пустое значение. Пустое означает «владелец не объявлен»: ручка отвечает
// `501`. То есть возможность была объявлена, задокументирована, покрыта типами —
// и не работала ни на одном стенде, включая боевой (kacho#1388).
//
// Класс тот же, что корпус ловит в контрактах («принято и проигнорировано»), но
// на уровне поставки, и потому невидимый обычным пробам: код верен, чарт
// рендерится, страж старта доволен, а клиент получает отказ.
//
// # Что именно судится — ДЕЙСТВУЮЩЕЕ значение, а не строка в файле
//
// Значение приходит наложением: умолчание чарта края, затем профили стека слева
// направо. Требовать строки именно в боевом профиле значило бы требовать ВТОРОГО
// МЕСТА об одном предмете — оно разошлось бы с умолчанием молча, потому что оба
// непусты и оба выглядят действующими. Поэтому здесь вычисляется то, что
// получит helm.
//
// # Почему сравнение стеков, а не порог
//
// «Сколько владельцев полагается» — свойство фазы, а не текста, и предиката у
// него нет. А вот вопрос «решал ли кто-нибудь, что на боевом их меньше» ответ
// имеет всегда: стенд, знающий возможность, которой не знает бой, — находка.

// edgeChartValuesRel — умолчание чарта края от каталога deploy.
const edgeChartValuesRel = "../gateway/deploy/values.yaml"

// umbrellaProfileDir — где лежат профили умбреллы.
const umbrellaProfileDir = "helm/umbrella"

// productionBaseProfile — профиль, которым задаётся БОЕВАЯ посадка.
//
// Выведен из таблицы стеков, а не из имени стека: боевым считается всякий стек,
// чья цепочка НАЧИНАЕТСЯ с этого профиля (сегодня их два — `prod` и `fe3455`).
// Судить по имени стека значило бы завести словарь имён, который разойдётся с
// таблицей молча.
const productionBaseProfile = "values.prod.yaml"

// ownersDeclaredBy читает объявление владельцев в одном файле значений.
//
// Три исхода, и они РАЗНЫЕ: ключа нет (профиль о нём не высказывался) · ключ есть
// и пуст (профиль ВЫКЛЮЧАЕТ возможность) · ключ есть и непуст. Схлопывание
// первых двух сделало бы выключение неотличимым от умолчания.
func ownersDeclaredBy(t *testing.T, path string, nested bool) (owners string, declared bool) {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- обход собственного дерева
	if err != nil {
		t.Fatalf("файл значений %s не читается (%v) — предпосылка гейта исчезла, "+
			"а не поставка стала полной", path, err)
	}
	type streamBlock struct {
		SubscriptionStream *struct {
			Owners *string `yaml:"owners"`
		} `yaml:"subscriptionStream"`
	}
	if nested {
		var doc struct {
			Gateway *streamBlock `yaml:"api-gateway"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("разбор %s: %v", path, err)
		}
		if doc.Gateway == nil || doc.Gateway.SubscriptionStream == nil || doc.Gateway.SubscriptionStream.Owners == nil {
			return "", false
		}
		return *doc.Gateway.SubscriptionStream.Owners, true
	}
	var doc streamBlock
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("разбор %s: %v", path, err)
	}
	if doc.SubscriptionStream == nil || doc.SubscriptionStream.Owners == nil {
		return "", false
	}
	return *doc.SubscriptionStream.Owners, true
}

// effectiveOwners сводит умолчание и цепочку профилей так же, как их сводит helm:
// последнее объявление выигрывает, необъявившийся профиль ничего не меняет.
func effectiveOwners(base string, chain []string, declaredBy func(profile string) (string, bool)) string {
	value := base
	for _, profile := range chain {
		if owners, declared := declaredBy(profile); declared {
			value = owners
		}
	}
	return value
}

// countOwners — сколько имён несёт значение. Разбирает так же, как край.
func countOwners(raw string) int {
	n := 0
	for _, part := range strings.Split(raw, ",") {
		if strings.TrimSpace(part) != "" {
			n++
		}
	}
	return n
}

// shippingCensus — то, что гейт вычисляет по таблице стеков: какие стеки
// возможность включили, какие из них боевые и какие боевые остались без неё.
type shippingCensus struct {
	// stacks — осмотренные стеки по возрастанию имени. Отдельным полем, чтобы
	// «ноль находок» было отличимо от «ноль прочитанного».
	stacks []string
	// ownersByStack — сколько владельцев получит каждый стек после наложения.
	ownersByStack map[string]int
	// enabled — стеки, где возможность включена (владельцев больше нуля).
	enabled []string
	// production — стеки боевой посадки.
	production []string
	// dark — боевые стеки без единого владельца.
	dark []string
}

// takeShippingCensus — ЕДИНСТВЕННОЕ место, где выносится суждение «этот стек
// боевой» и «этот боевой стек затемнён».
//
// # Почему отдельной функцией
//
// Доказательство способности гейта упасть обязано прогонять ЭТУ функцию, а не её
// пересказ. Прежняя редакция инъекции воспроизводила цикл суждения своей копией
// и оставалась зелёной, когда судящую ветку настоящего гейта снимали
// (`if owners == 0` → `if owners < 0`), — то есть доказывала свойство копии, а не
// гейта. Найдено приёмкой 2026-08-29; опыт повторяется дословно и обязан теперь
// давать красное.
//
// Боевым стек делается ПЕРВЫМ профилем цепочки, а не именем: словарь имён
// разошёлся бы с таблицей стеков молча.
func takeShippingCensus(
	stacks map[string][]string,
	base string,
	declaredBy func(profile string) (string, bool),
) shippingCensus {
	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)

	census := shippingCensus{
		stacks:        names,
		ownersByStack: make(map[string]int, len(names)),
		enabled:       make([]string, 0, len(names)),
		production:    make([]string, 0, 2),
		dark:          make([]string, 0, 2),
	}

	for _, name := range names {
		chain := stacks[name]
		owners := countOwners(effectiveOwners(base, chain, declaredBy))
		census.ownersByStack[name] = owners

		if owners > 0 {
			census.enabled = append(census.enabled, name)
		}
		if len(chain) == 0 || chain[0] != productionBaseProfile {
			continue
		}
		census.production = append(census.production, name)
		if owners == 0 {
			census.dark = append(census.dark, name)
		}
	}
	return census
}

// shippingCensusOfTree — перепись по НАСТОЯЩЕМУ дереву: таблица стеков, умолчание
// чарта края и профили умбреллы.
func shippingCensusOfTree(t *testing.T) shippingCensus {
	t.Helper()
	base, _ := ownersDeclaredBy(t, edgeChartValuesRel, false)
	return takeShippingCensus(deployStacks(t), base, func(profile string) (string, bool) {
		return ownersDeclaredBy(t, filepath.Join(umbrellaProfileDir, profile), true)
	})
}

// TestSubscriptionOwnersReachProductionAndNotOnlyAStand — если возможность
// включена хоть в одном стеке, она включена и в боевом.
func TestSubscriptionOwnersReachProductionAndNotOnlyAStand(t *testing.T) {
	census := shippingCensusOfTree(t)
	base, _ := ownersDeclaredBy(t, edgeChartValuesRel, false)

	for _, name := range census.stacks {
		t.Logf("перепись: стек %s → владельцев %d", name, census.ownersByStack[name])
	}
	t.Logf("перепись: стеков осмотрено %d · возможность включена в %d %v · боевых %d %v · "+
		"боевых без возможности %d %v · умолчание чарта края несёт %d владельцев",
		len(census.stacks), len(census.enabled), census.enabled,
		len(census.production), census.production, len(census.dark), census.dark, countOwners(base))

	if len(census.production) == 0 {
		t.Fatalf("ни один стек не начинается с %s — боевой посадки гейт не нашёл, и его "+
			"молчание не было бы утверждением о поставке", productionBaseProfile)
	}
	if len(census.enabled) > 0 && len(census.dark) > 0 {
		t.Errorf("возможность включена в стеках %v, а в боевых %v перечень владельцев пуст: "+
			"ручка отвечает там 501, то есть возможность существует ТОЛЬКО НА СТЕНДЕ — "+
			"ни попробовать, ни купить", census.enabled, census.dark)
	}
}

// clientPromiseRel — клиентская страница, обещающая арендатору поток изменений.
//
// Она и есть ПРЕДМЕТ утверждения ниже: пока страница обещает ручку, боевая
// посадка обязана её отдавать. Снимут возможность — уйдёт страница, и требование
// истечёт вместе с ней, никем не тронутое.
const clientPromiseRel = "../gateway/docs/content/api/subscription.mdx"

// TestProductionKeepsThePromiseTheClientPageMakes — боевая посадка отдаёт то, что
// обещано клиенту.
//
// # Чем это отличается от утверждения выше
//
// То судит СОГЛАСИЕ стеков: «решал ли кто-нибудь, что на боевом возможность
// беднее». Оно молчит, когда возможность выключена ВЕЗДЕ, — а это ровно то
// состояние, в котором ручка прожила всю свою жизнь, отвечая `501` при том, что
// код, величины и владельцы были сделаны все до одного.
//
// Здесь судится вторая сторона: обещание клиенту. Страница называет адрес,
// параметры и словарь владельцев; пустой перечень делает её документированной
// ложью — вызывающий читает инструкцию, шлёт запрос и получает «такого здесь
// нет».
//
// # Почему требование привязано к странице, а не к фазе
//
// «Сколько владельцев полагается» — свойство фазы, и предиката у него нет.
// «Обещано ли арендатору» — факт дерева, и он самоистекает: решат снять
// возможность — снимут страницу, и утверждение уйдёт вместе с ней.
func TestProductionKeepsThePromiseTheClientPageMakes(t *testing.T) {
	if _, err := os.Stat(clientPromiseRel); err != nil {
		t.Skipf("клиентской страницы %s нет (%v) — обещания арендатору не существует, "+
			"и требовать его исполнения не от чего", clientPromiseRel, err)
	}

	census := shippingCensusOfTree(t)

	t.Logf("перепись: страница обещает поток · боевых стеков осмотрено %d %v · "+
		"без единого владельца %d %v", len(census.production), census.production,
		len(census.dark), census.dark)

	if len(census.production) == 0 {
		t.Fatalf("боевых стеков не найдено (профиль %s), — гейт ничего не проверял",
			productionBaseProfile)
	}
	for _, name := range census.dark {
		t.Errorf("боевой стек %s не называет НИ ОДНОГО владельца журнала, а страница %s "+
			"обещает арендатору поток изменений: ручка ответит там 501 — обещание "+
			"задокументировано и неисполнимо", name, clientPromiseRel)
	}
}
