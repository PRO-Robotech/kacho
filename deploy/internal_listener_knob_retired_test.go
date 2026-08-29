// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// internal_listener_knob_retired_test.go — у края нет ВНУТРЕННЕГО листенера,
// значит ни один профиль не вправе делать вид, что настраивает его.
//
// ПРЕДМЕТ. Задача #1024 сняла у края единственную службу его cluster-internal
// gRPC-листенера (`InternalAuthzCacheService`), а с ней и сам листенер. Ручка
// `api-gateway.internalListener`, которой этот листенер настраивали, вместе с
// ним снята из чарта.
//
// Значение профиля, называющее СНЯТУЮ ручку, инертно — и именно этим опасно: оно
// читается как принятое решение о посадке, ничего при этом не делая. Оператор,
// увидев `internalListener.mtls.enable: true` в боевом профиле, заключит, что
// внутренний периметр края защищён; проверить это он сможет только чтением
// шаблона, которого там уже нет.
//
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР. Та же причина, что у соседнего
// posture_parity_test.go: контракт — то, что профиль ОБЪЯВЛЯЕТ. Проверке не
// нужны ни helm, ни vendored-зависимости чартов, поэтому она не умеет
// пропуститься. Рендерную сторону («ни одной переменной внутреннего листенера в
// PodSpec») держит gateway/deploy/render_internal_listener_retired_test.go.
//
// ЭТО НЕ ВЕДОМОСТЬ ПРОЩЁННЫХ, А ЕЁ ПРОТИВОПОЛОЖНОСТЬ. Проверка не перечисляет
// разрешённое: она требует, чтобы у СНЯТОЙ ручки не осталось объявлений. В день,
// когда у края снова появится внутренний листенер, ручка вернётся в values.yaml
// чарта — и предпосылка ниже (её там НЕТ) покраснеет первой, потребовав снять эту
// проверку вместе с её предметом.
package deploy_test

import (
	"path/filepath"
	"testing"
)

// retiredEdgeKnob — ключ значений, снятый вместе с внутренним листенером края.
const retiredEdgeKnob = "internalListener"

// edgeSubchartKey — ключ подчарта края в значениях умбреллы.
const edgeSubchartKey = "api-gateway"

// TestEdgeInternalListenerKnobHasNoDeclarationsLeft — ни чарт, ни один профиль
// умбреллы не объявляет снятую ручку.
func TestEdgeInternalListenerKnobHasNoDeclarationsLeft(t *testing.T) {
	dirs := subchartDirs(t)
	edgeDir, ok := dirs[edgeSubchartKey]
	if !ok {
		t.Fatalf("подчарт %q не выведен из Chart.yaml — состав подчартов сменился, "+
			"проверь вывод: %v", edgeSubchartKey, dirs)
	}

	// ПРЕДПОСЫЛКА, а не украшение: values.yaml края обязан читаться и быть
	// непустым. Без этого «ключа нет» означало бы «файл не прочитан», и проверка
	// объявляла бы дерево чистым, ничего не осмотрев.
	chartValues := readYAML(t, filepath.Join(edgeDir, "values.yaml"))
	if len(chartValues) == 0 {
		t.Fatalf("values.yaml края (%s) прочитан пустым — обход ничего не осмотрел", edgeDir)
	}
	if _, present := chartValues[retiredEdgeKnob]; present {
		t.Fatalf("чарт края (%s/values.yaml) всё ещё объявляет ручку %q, "+
			"хотя внутреннего листенера у края нет (#1024): ручка настраивает "+
			"поверхность, которой не существует", edgeDir, retiredEdgeKnob)
	}

	profiles := profileFiles(t)
	if len(profiles) == 0 {
		t.Fatalf("профилей умбреллы не найдено — обход пуст, а не дерево чисто")
	}
	for _, p := range profiles {
		tree := readYAML(t, p)
		edge, ok := tree[edgeSubchartKey].(map[string]any)
		if !ok {
			continue // профиль про край ничего не говорит — законно
		}
		if _, present := edge[retiredEdgeKnob]; present {
			t.Errorf("%s: блок %s.%s называет ручку, снятую вместе с внутренним "+
				"листенером края (#1024). Значение инертно и потому читается как "+
				"принятое решение о защите периметра, ничего при этом не делая — "+
				"снять запись, а не оставить «на будущее»",
				filepath.Base(p), edgeSubchartKey, retiredEdgeKnob)
		}
	}
	t.Logf("осмотрено: профилей=%d, ключей верхнего уровня в values.yaml края=%d",
		len(profiles), len(chartValues))
}
