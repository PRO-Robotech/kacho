// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// peer_transport_profiles_test.go — профиль, поднимающий сервис в боевом режиме,
// обязан взвести транспорт КАЖДОГО его исходящего ребра.
//
// Предмет. У storage и registry загрузочная стража теперь ОТКАЗЫВАЕТ В СТАРТЕ,
// пока ребро к владельцу прав (и соседние рёбра того же дозвона) поднимается без
// проверяемого транспорта. Это правильная посадка — и ровно поэтому у неё
// появилась вторая сторона: профиль, забывший ручку, роняет не «чуть менее
// строгий» под, а под, который не поднимется вовсе. Узнать об этом на файлах
// значений дешевле, чем на стенде; иначе честную стражу снимут как помеху.
//
// Почему проверка читает ОБЪЯВЛЕНИЯ, а не отрендеренный шаблон. Рендер умбреллы
// требует зависимостей (helm dep build ходит в сеть), поэтому проверка по рендеру
// в обычном прогоне тестов ПРОПУСКАЕТСЯ — а пропускающаяся проверка на измерении
// «значение не задано вовсе» бесполезна по построению: именно отсутствие ключа она
// и должна ловить. Тот же довод — в gateway/deploy/token_shape_test.go.
//
// Предпосылки гейта проверяются и объявляются: ключи, которые он читает, обязаны
// существовать в чарте сервиса; перечень профилей обязан быть непуст; объём
// осмотренного печатается, чтобы «ноль нарушений» было отличимо от «ноль
// прочитанного».
package umbrella_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// profileStack — набор файлов значений, применяемый ОДНОЙ командой, в порядке
// применения. Списки взяты из того, чем стенд поднимается на самом деле:
// deploy/Makefile (dev-up, dev-prod-up) и helm/umbrella/cutover-fe3455.sh.
type profileStack struct {
	name  string
	files []string
}

var deployedStacks = []profileStack{
	{"dev", []string{"values.dev.yaml"}},
	{"dev-prod", []string{"values.dev.yaml", "values.dev-prod.yaml"}},
	{"prod", []string{"values.prod.yaml"}},
	// values.fe3455-ory.yaml из стека опущен намеренно: он git-ignored (секреты
	// площадки) и рёбер не касается. Остальные три — те же файлы и тот же
	// порядок, что в cutover-fe3455.sh.
	{"fe3455", []string{"values.prod.yaml", "values.fe3455.yaml", "values.fe3455-prod.yaml"}},
}

// guardedService — сервис, чья загрузочная стража требует проверяемого транспорта
// на каждом ПОДНИМАЕМОМ исходящем ребре.
type guardedService struct {
	key       string   // ключ сервиса в значениях умбреллы
	chart     string   // путь к чарту сервиса относительно этого каталога
	guardSrc  string   // где живёт стража — называется в тексте отказа
	modePath  []string // путь к режиму внутри блока сервиса
	edgePaths [][]string
}

var guardedServices = []guardedService{
	{
		key:      "storage",
		chart:    "../../../services/storage/deploy",
		guardSrc: "services/storage/internal/config/validate.go",
		modePath: []string{"config", "authMode"},
		edgePaths: [][]string{
			{"mtls", "edges", "iam"},
			{"mtls", "edges", "geo"},
		},
	},
	{
		key:      "registry",
		chart:    "../../../services/registry/deploy",
		guardSrc: "services/registry/cmd/kacho-registry/serve.go",
		modePath: []string{"authMode"},
		edgePaths: [][]string{
			{"mtls", "edges", "iamAuthz"},
			{"mtls", "edges", "iamProject"},
			{"mtls", "edges", "geo"},
		},
	},
}

func loadTree(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return tree
}

// lookup — значение по пути ключей; второе значение сообщает, объявлен ли путь.
func lookup(tree map[string]any, path ...string) (any, bool) {
	var cur any = tree
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// effective — значение, которое реально доедет до чарта: последний слой,
// объявивший путь, выигрывает. Слои идут от умолчаний чарта к последнему
// профилю стека — тот же порядок, в котором их складывает helm.
func effective(layers []map[string]any, path ...string) (any, bool) {
	var val any
	var found bool
	for _, layer := range layers {
		if v, ok := lookup(layer, path...); ok {
			val, found = v, true
		}
	}
	return val, found
}

func isProductionMode(v any) bool {
	s, ok := v.(string)
	return ok && (s == "production" || s == "production-strict")
}

// TestProductionProfiles_ArmEveryDialledPeerEdge — ядро гейта.
func TestProductionProfiles_ArmEveryDialledPeerEdge(t *testing.T) {
	if len(deployedStacks) == 0 || len(guardedServices) == 0 {
		t.Fatal("the gate's own census is empty — a check that inspects nothing must fail, not pass")
	}
	base := loadTree(t, "values.yaml")

	var examined []string
	for _, svc := range guardedServices {
		chartDefaults := loadTree(t, filepath.Join(svc.chart, "values.yaml"))

		// Предпосылка: ключи, которые читает гейт, обязаны существовать у чарта.
		// Переезд ключа иначе превратил бы гейт в «ноль находок» навсегда.
		if _, ok := lookup(chartDefaults, svc.modePath...); !ok {
			t.Fatalf("%s: chart declares no %s — this gate reads a key that no longer exists; "+
				"fix the gate together with the chart", svc.key, strings.Join(svc.modePath, "."))
		}
		for _, edge := range svc.edgePaths {
			if _, ok := lookup(chartDefaults, edge...); !ok {
				t.Fatalf("%s: chart declares no %s — this gate reads a key that no longer exists; "+
					"fix the gate together with the chart", svc.key, strings.Join(edge, "."))
			}
		}

		for _, stack := range deployedStacks {
			layers := []map[string]any{chartDefaults}
			if sub, ok := lookup(base, svc.key); ok {
				if m, ok := sub.(map[string]any); ok {
					layers = append(layers, m)
				}
			}
			for _, f := range stack.files {
				if sub, ok := lookup(loadTree(t, f), svc.key); ok {
					if m, ok := sub.(map[string]any); ok {
						layers = append(layers, m)
					}
				}
			}

			// Сервис, выключенный в этом стеке, не поднимается — требовать с него
			// нечего. Отсутствие ключа означает «включён» (helm игнорирует условие
			// с отсутствующим значением), поэтому умолчание здесь именно true.
			if enabled, ok := effective(layers, "enabled"); ok {
				if on, isBool := enabled.(bool); isBool && !on {
					examined = append(examined, fmt.Sprintf("%s/%s: not deployed", stack.name, svc.key))
					continue
				}
			}

			mode, _ := effective(layers, svc.modePath...)
			if !isProductionMode(mode) {
				examined = append(examined, fmt.Sprintf("%s/%s: %v (guard inert)", stack.name, svc.key, mode))
				continue
			}
			examined = append(examined, fmt.Sprintf("%s/%s: %v", stack.name, svc.key, mode))

			if on, _ := effective(layers, "mtls", "enable"); on != true {
				t.Errorf("profile %s puts %s in %v mode with mtls.enable=%v: the whole mTLS block of the "+
					"chart hangs off that key, so no client credentials reach the pod and the boot guard "+
					"(%s) refuses to start — the release would never become ready",
					stack.name, svc.key, mode, on, svc.guardSrc)
				continue
			}
			for _, edge := range svc.edgePaths {
				if on, _ := effective(layers, edge...); on != true {
					t.Errorf("profile %s puts %s in %v mode with %s=%v: that edge is dialled with unarmed "+
						"client credentials, which degrade to cleartext silently, so the boot guard (%s) "+
						"refuses to start — arm the edge or stop dialling it",
						stack.name, svc.key, mode, strings.Join(edge, "."), on, svc.guardSrc)
				}
			}
		}
	}
	sort.Strings(examined)
	t.Logf("examined %d profile×service cell(s): %s", len(examined), strings.Join(examined, "; "))
}
