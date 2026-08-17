// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSharedRestPair_CannotChangeAnAccessDecision — пара (метод, путь), которую
// объявляют ДВА разных RPC, обязана быть неразличима для решения о доступе.
//
// # Откуда взялась общая пара
//
// Ресурс имеет ОДИН канонический адрес. Когда административная поверхность
// публикуется рядом с внутренней (окно расширения ADM-1 S1→S3), публичный глагол
// встаёт на ТОТ ЖЕ путь: второго адреса у предмета быть не должно. На время окна
// одну пару объявляют два FQN.
//
// # Почему это опасно и что здесь стережётся
//
// Маршрутизатор края отображает пару в FQN, чтобы найти запись каталога прав, и
// возвращает ПЕРВОЕ совпадение. При двух кандидатах выбор — свойство порядка
// таблицы, а не решение, которое кто-нибудь принимал. Пока обе записи требуют
// одного и того же, выбор ничего не меняет; разойдись они хоть в одном поле — и
// действующее требование к вызывающему начнёт зависеть от порядка строк
// сгенерированного файла. Это ровно тот класс, который мы ловим в другом месте:
// защита, чьё поведение определяется не решением, а совпадением.
//
// Поэтому здесь утверждается не «пар нет» (они законны и временны), а
// «различить их решением о доступе НЕЛЬЗЯ»: отношение, извлечение области и
// требование к подтверждению личности совпадают дословно.
//
// # Проба ИСТЕКАЕТ САМА
//
// Стадия S3 снимает внутренний глагол, общих пар не остаётся, и проба проходит,
// объявив перепись «пар 0». Падать на достижении собственной цели она не вправе
// — пустая ведомость и есть то, ради чего ведомость заведена. Её способность
// упасть держится не наличием пар, а отдельной пробой ниже, которая эту
// способность доказывает на синтетике.
func TestSharedRestPair_CannotChangeAnAccessDecision(t *testing.T) {
	c, err := LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	shared := sharedPairs()

	// Перепись — отдельное утверждение: «расхождений 0» обязано быть отличимо от
	// «ничего не прочитано». Без неё проба, потерявшая доступ к таблице, читалась
	// бы как доказательство.
	t.Logf("перепись: маршрутов в таблице %d, пар с двумя и более FQN — %d",
		len(generatedRestRoutes), len(shared))
	require.NotZero(t, len(generatedRestRoutes),
		"таблица маршрутов пуста — проба ничего не осмотрела, а значит ничего не доказала")

	for pair, fqns := range shared {
		sort.Strings(fqns)
		first, ok := c.Lookup(fqns[0])
		require.Truef(t, ok, "общая пара %s: FQN %s отсутствует в каталоге прав — "+
			"край не сможет найти запись и ответит отказом в рантайме", pair, fqns[0])

		for _, other := range fqns[1:] {
			e, ok := c.Lookup(other)
			require.Truef(t, ok, "общая пара %s: FQN %s отсутствует в каталоге прав", pair, other)

			if diff, same := accessDecisionDiffers(first, e); !same {
				t.Errorf("общая пара %s: записи %s и %s требуют РАЗНОГО (%s). Какое из двух "+
					"подействует, решает порядок строк сгенерированной таблицы, а не чьё-либо решение",
					pair, fqns[0], other, diff)
			}
		}
	}
}

// accessDecisionDiffers — ЕДИНСТВЕННОЕ место, где сравниваются два требования к
// вызывающему. Названо функцией, а не расписано по месту, ровно затем, чтобы
// проба ниже исполняла ТО ЖЕ сравнение: инъекция, проверяющая свою копию
// предиката, доказывает лишь то, что копия работает.
func accessDecisionDiffers(a, b CatalogEntry) (string, bool) {
	switch {
	case a.RequiredRelation != b.RequiredRelation:
		return fmt.Sprintf("отношение %q против %q", a.RequiredRelation, b.RequiredRelation), false
	case a.RequiredACRMin != b.RequiredACRMin:
		return fmt.Sprintf("подтверждение личности %q против %q", a.RequiredACRMin, b.RequiredACRMin), false
	case scopeOf(a) != scopeOf(b):
		return fmt.Sprintf("извлечение области %s против %s", scopeOf(a), scopeOf(b)), false
	}
	return "", true
}

// TestSharedRestPair_ProbeSeesADivergence — доказательство способности пробы
// выше упасть, инъекцией в ОБЕ стороны и ТЕМ ЖЕ предикатом.
//
// Существует отдельно, потому что предмет основной пробы временный: стадия S3
// снимает внутренние глаголы, общих пар не остаётся, и основная проба начинает
// проходить вхолостую. Без этой проверки «зелено» стало бы неотличимо от
// «сравнивать было нечем».
func TestSharedRestPair_ProbeSeesADivergence(t *testing.T) {
	base := CatalogEntry{
		RequiredRelation: "system_admin",
		RequiredACRMin:   "1",
		ScopeExtractor:   ScopeExtractor{ObjectType: "cluster", FromRequestField: "*"},
	}

	// Законный близнец: две записи, требующие одного и того же, обязаны читаться
	// как неразличимые — иначе предикат ловил бы форму, а не существо, и первый
	// же ложный срабат его отключил бы.
	if diff, same := accessDecisionDiffers(base, base); !same {
		t.Fatalf("положительный контроль провален: совпадающие записи объявлены разными (%s)", diff)
	}

	// По одному расхождению на каждое поле решения — иначе «предикат различает»
	// доказывалось бы для одного поля, а молчало бы для двух остальных.
	for name, other := range map[string]CatalogEntry{
		"отношение": {RequiredRelation: "viewer", RequiredACRMin: "1",
			ScopeExtractor: ScopeExtractor{ObjectType: "cluster", FromRequestField: "*"}},
		"подтверждение личности": {RequiredRelation: "system_admin", RequiredACRMin: "2",
			ScopeExtractor: ScopeExtractor{ObjectType: "cluster", FromRequestField: "*"}},
		"извлечение области": {RequiredRelation: "system_admin", RequiredACRMin: "1",
			ScopeExtractor: ScopeExtractor{ObjectType: "project", FromRequestField: "project_id"}},
	} {
		if _, same := accessDecisionDiffers(base, other); same {
			t.Errorf("предикат НЕ различает расхождение по полю %q — основная проба зелена "+
				"при расхождении именно этого рода", name)
		}
	}
}

// sharedPairs собирает пары (метод, шаблон), объявленные более чем одним FQN.
func sharedPairs() map[string][]string {
	byPair := map[string][]string{}
	for _, r := range generatedRestRoutes {
		key := r.Method + " " + r.Template
		byPair[key] = append(byPair[key], r.FQN)
	}
	for k, v := range byPair {
		if len(v) < 2 {
			delete(byPair, k)
		}
	}
	return byPair
}

// scopeOf — извлечение области в сравнимой форме.
func scopeOf(e CatalogEntry) string {
	return fmt.Sprintf("%s/%s", e.ScopeExtractor.ObjectType,
		strings.TrimSpace(e.ScopeExtractor.FromRequestField))
}
