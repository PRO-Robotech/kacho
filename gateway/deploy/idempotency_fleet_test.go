// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// idempotency_fleet_test.go — хранилище однократности и объявленный размер флота
// обязаны быть ОДНИМ объявлением (#694).
//
// # Предмет
//
// Край обещает по `Idempotency-Key` однократность. Домен параллелизма этой
// гарантии — весь флот подов, а не один процесс: повтор, попавший в соседнюю
// реплику, записи в чужой памяти не находит и уходит к downstream. Отказа при
// этом не происходит, наблюдаемого признака нет.
//
// Условие, при котором обещание верно, было НАЗВАНО — комментарием рядом с
// автомасштабированием — и не было связано ни с чем: в том же файле стояло
// «до десяти реплик». Два объявления об одном предмете, из которых верно одно.
//
// # Что проверяет этот файл — и почему ОБЪЯВЛЕНИЕ, а не рендер
//
// Рендер требует helm, которого в этом харнессе нет; проверка, которую нельзя
// исполнить, не проверяет ничего. Поэтому здесь читаются ОБЪЯВЛЕНИЯ: значения
// чарта, значения каждого профиля умбреллы и текст шаблона развёртывания.
// Утверждается три вещи:
//
//  1. пара «вид хранилища ↔ объявленный флот» исполнима в чарте и в КАЖДОМ
//     профиле, который эти значения трогает;
//  2. размер флота в шаблоне ВЫВОДИТСЯ из значения автомасштабирования, а не
//     выписывается вторым литералом (иначе они разойдутся молча);
//  3. шаблон падает на неисполнимой паре, то есть неверная посадка не
//     доезжает до кластера.
//
// Перепись печатается всегда: «ноль находок» обязано быть отличимо от «ноль
// прочитанного», а пустой обход — отказ.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// gatewayChartValues читает объявления самого чарта края.
func gatewayChartValues(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("values.yaml")
	if err != nil {
		t.Fatalf("прочитать gateway/deploy/values.yaml: %v", err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("разобрать gateway/deploy/values.yaml: %v", err)
	}
	return tree
}

// fleetPairing — то, что профиль объявляет об однократности.
type fleetPairing struct {
	store string
	fleet int
	// how — из чего выведен размер флота, для внятного текста отказа.
	how string
}

// asInt приводит значение yaml к числу; nil означает «не объявлено».
func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}

// pairingOf складывает объявление чарта с накладкой профиля (профили умбреллы
// НЕ наслаиваются друг на друга — каждый рендерится поверх значений сабчарта).
func pairingOf(chart, override map[string]any) fleetPairing {
	get := func(path ...string) any {
		for _, src := range []map[string]any{override, chart} {
			if src == nil {
				continue
			}
			var cur any = src
			ok := true
			for _, k := range path {
				m, isMap := cur.(map[string]any)
				if !isMap {
					ok = false
					break
				}
				cur, ok = m[k]
				if !ok {
					break
				}
			}
			if ok && cur != nil {
				return cur
			}
		}
		return nil
	}
	p := fleetPairing{store: "memory", fleet: 1, how: "replicas"}
	if s, ok := get("idempotency", "store").(string); ok && s != "" {
		p.store = s
	}
	if n, ok := asInt(get("replicas")); ok {
		p.fleet, p.how = n, "replicas"
	}
	if enabled, ok := get("autoscaling", "enabled").(bool); ok && enabled {
		if n, ok := asInt(get("autoscaling", "maxReplicas")); ok {
			p.fleet, p.how = n, "autoscaling.maxReplicas"
		}
	}
	return p
}

// satisfiable — пара исполнима: либо флот из одной реплики, либо общее хранилище.
func (p fleetPairing) satisfiable() bool { return p.store != "memory" || p.fleet <= 1 }

// TestChartPairsTheIdempotencyStoreWithTheFleetItDeclares — объявление самого
// чарта исполнимо.
func TestChartPairsTheIdempotencyStoreWithTheFleetItDeclares(t *testing.T) {
	chart := gatewayChartValues(t)
	if _, ok := chart["idempotency"]; !ok {
		t.Fatal("gateway/deploy/values.yaml не объявляет idempotency.store — тогда вид " +
			"хранилища выбирает умолчание сборки, и профиль о нём ничего не говорит")
	}
	p := pairingOf(chart, nil)
	if !p.satisfiable() {
		t.Fatalf("чарт объявляет хранилище %q и флот %d (из %s): повтор, попавший в "+
			"соседнюю реплику, записи не найдёт, и мутация исполнится второй раз.\n"+
			"Исходов два: объявить одну реплику ЛИБО общее хранилище (idempotency.store=postgres).",
			p.store, p.fleet, p.how)
	}
	t.Logf("перепись: чарт края — хранилище %q, флот %d (выведен из %s)", p.store, p.fleet, p.how)
}

// TestEveryUmbrellaProfilePairsTheStoreWithItsFleet — то же по КАЖДОМУ профилю.
//
// Профиль вправе поднять потолок реплик; тогда он обязан в том же месте объявить
// общее хранилище. Именно здесь ловится расхождение, которое было предметом
// задачи: потолок объявлял один файл, а условие его допустимости — другой.
func TestEveryUmbrellaProfilePairsTheStoreWithItsFleet(t *testing.T) {
	chart := gatewayChartValues(t)

	dir := filepath.Join("..", "..", "deploy", "helm", "umbrella")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("прочитать каталог профилей: %v", err)
	}
	var profiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "values") && strings.HasSuffix(e.Name(), ".yaml") {
			profiles = append(profiles, e.Name())
		}
	}
	sort.Strings(profiles)
	if len(profiles) == 0 {
		t.Fatal("профилей умбреллы не найдено — обход пуст, и это отказ, а не пустой успех")
	}

	var (
		touching []string
		failures []string
	)
	for _, name := range profiles {
		tree := umbrellaValues(t, name)
		override, _ := tree["api-gateway"].(map[string]any)
		if override == nil {
			continue
		}
		_, hasReplicas := override["replicas"]
		_, hasAuto := override["autoscaling"]
		_, hasIdem := override["idempotency"]
		if !hasReplicas && !hasAuto && !hasIdem {
			continue
		}
		touching = append(touching, name)
		if p := pairingOf(chart, override); !p.satisfiable() {
			failures = append(failures, fmt.Sprintf(
				"%s: хранилище %q при флоте %d (из %s)", name, p.store, p.fleet, p.how))
		}
	}

	t.Logf("перепись: профилей прочитано %d, из них трогают посадку края %d (%s)",
		len(profiles), len(touching), strings.Join(touching, ", "))
	if len(failures) > 0 {
		t.Fatalf("профили объявляют неисполнимую пару «хранилище ↔ флот»:\n  %s\n"+
			"Хранилище в памяти процесса законно ровно для одной реплики; для большего "+
			"нужен idempotency.store=postgres с адресом.", strings.Join(failures, "\n  "))
	}
}

// TestDeploymentTemplateDerivesTheFleetSizeFromAutoscaling — размер флота, который
// получает процесс, ВЫВЕДЕН из значения автомасштабирования, а не выписан вторым
// литералом, и шаблон падает на неисполнимой паре.
//
// Без первого утверждения два объявления вернулись бы под другим именем: одно в
// `autoscaling.maxReplicas`, другое — в env. Без второго неверная посадка молча
// доезжала бы до кластера и падала только при старте процесса.
func TestDeploymentTemplateDerivesTheFleetSizeFromAutoscaling(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("прочитать шаблон развёртывания: %v", err)
	}
	tpl := string(raw)

	if !strings.Contains(tpl, "KACHO_GATEWAY_FLEET_SIZE") {
		t.Fatal("шаблон не передаёт процессу размер флота (KACHO_GATEWAY_FLEET_SIZE): " +
			"тогда отказ в старте судит по умолчанию сборки, а не по тому, что объявил профиль")
	}
	if !strings.Contains(tpl, ".Values.autoscaling.maxReplicas") {
		t.Fatal("размер флота не выведен из .Values.autoscaling.maxReplicas — значит он " +
			"выписан вторым литералом, и с потолком автомасштабирования он разойдётся молча")
	}
	if !strings.Contains(tpl, "KACHO_IDEMPOTENCY_STORE") {
		t.Fatal("шаблон не передаёт процессу вид хранилища (KACHO_IDEMPOTENCY_STORE)")
	}
	if !strings.Contains(tpl, "KACHO_IDEMPOTENCY_DSN") {
		t.Fatal("шаблон не рендерит адрес общего хранилища (KACHO_IDEMPOTENCY_DSN) ни в одной " +
			"ветке — тогда общее хранилище нельзя включить ни одним профилем, и оно мертво")
	}
	fails := strings.Count(tpl, "{{- fail")
	if fails < 2 {
		t.Fatalf("в шаблоне %d отказов рендера; ожидались как минимум два по этому предмету: "+
			"неисполнимая пара «память + флот больше одной реплики» и общее хранилище без адреса",
			fails)
	}
	t.Logf("перепись: шаблон прочитан (%d байт), отказов рендера — %d", len(raw), fails)
}
