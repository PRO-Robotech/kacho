// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// umbrella_invocation_test.go — каждый вызов helm по умбрелле несёт общий набор
// параметров, а не свой.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// cert-manager вынесен из умбреллы в собственный релиз, чтобы продукт
// поднимался ОДНИМ прогоном и сразу в боевой посадке (до этого первая фаза
// снимала mTLS у всех подчартов и включала аварийный обход авторизации реестра —
// посадку, которую ban #16 запрещает на поднятом кластере).
//
// Следствие: каждый вызов умбреллы обязан объявить `cert-manager.enabled=false`.
// Забывший вызов не рендерит лишнее и не падает на рендере — он падает НА
// ПРИМЕНЕНИИ, пытаясь присвоить чужие CRD: «invalid ownership metadata».
//
// Наблюдалось сразу: правя подъём, я поставил флаг на один вызов из трёх и
// получил отказ на третьем. Отказ был громкий и потому дешёвый — но следующий
// вызов забыл бы флаг так же, а заметить это можно было бы уже на чужом
// кластере, где половина стенда осталась бы в промежуточном состоянии.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ГЕЙТ ТРЕБУЕТ
//
// Не наличие подстроки `cert-manager.enabled=false` у каждого вызова — этого
// мало: две копии одного решения расходятся молча. Требуется, чтобы вызов
// подставлял ОБЩУЮ переменную `$(UMBRELLA_OPTS)`. Тогда решение живёт в одном
// месте, и следующий параметр, который обязан нести каждый вызов, добавляется
// туда же, а не в три места из четырёх.
package deploy_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// umbrellaInvocationRe — вызов helm по чарту умбреллы.
var umbrellaInvocationRe = regexp.MustCompile(`helm upgrade[^\n]*\./helm/umbrella\b`)

func TestEveryUmbrellaInvocationCarriesSharedOpts(t *testing.T) {
	// #nosec G304 -- путь фиксирован константой этого файла.
	raw, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("Makefile не читается (%v) — предпосылка гейта исчезла", err)
	}
	body := string(raw)

	if !strings.Contains(body, "UMBRELLA_OPTS :=") {
		t.Fatal("переменной UMBRELLA_OPTS в Makefile нет — гейт требует свойства, которого " +
			"дереву больше не от чего иметь; либо верните переменную, либо снимите гейт вместе " +
			"с её предметом")
	}

	lines := strings.Split(body, "\n")
	invocations := 0
	for i, line := range lines {
		if !umbrellaInvocationRe.MatchString(line) {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue // объяснение, а не вызов
		}
		invocations++
		// Вызов — многострочный (`\`-продолжения). Собираем его целиком.
		call := line
		for j := i; j < len(lines)-1 && strings.HasSuffix(strings.TrimSpace(lines[j]), `\`); j++ {
			call += "\n" + lines[j+1]
		}
		if !strings.Contains(call, "$(UMBRELLA_OPTS)") {
			t.Errorf("Makefile:%d — вызов helm по умбрелле не подставляет $(UMBRELLA_OPTS).\n"+
				"Общий набор параметров обязан приезжать ОДНОЙ переменной: cert-manager вынесен "+
				"в отдельный релиз, и вызов без `cert-manager.enabled=false` падает не на рендере, "+
				"а на применении — попыткой присвоить чужие CRD («invalid ownership metadata»), "+
				"оставив стенд в промежуточном состоянии", i+1)
		}
	}

	t.Logf("осмотрено: вызовов helm по умбрелле — %d", invocations)
	if invocations == 0 {
		t.Fatal("вызовов helm по умбрелле не найдено — «все несут набор» здесь означало бы " +
			"«ни одного не смотрели»")
	}
}
