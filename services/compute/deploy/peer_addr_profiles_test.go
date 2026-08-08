// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// peer_addr_profiles_test.go — профиль развёртывания не вправе обнулить адрес
// соседа: пустая строка не выключает ребро, а подменяет его заглушкой.
//
// Каждый адрес соседа в конфиге компьюта читается по одному правилу: непустое
// значение → живой типизированный клиент; пустое → no-op-клиент. Разница между
// «ребро выключено» и «ребро подменено» здесь не косметическая:
//
//   - мутации (привязка интерфейса, привязка тома) отвечают Unavailable — это
//     видно вызывающему и это честно;
//   - а ЧТЕНИЯ, на которых стоят шаги высвобождения в саге удаления машины
//     (перечисление привязок), возвращают ПУСТОЙ СПИСОК и ошибки не дают. Сага
//     проходит успешно и не снимает ничего. Наблюдаемо это неотличимо от «нечего
//     было снимать», а на деле привязки остаются у владельца висеть.
//
// Поэтому обнуление такого ключа — не настройка профиля, а тихая деградация
// целого шага саги, и профилю оно недоступно. Нужно поднять стенд без соседа —
// выключается САБЧАРТ соседа, а не адрес у консумера: тогда отказ виден.
//
// Реальный случай, из которого выведено: `compute.vpcInternalAddr: ""` в
// профиле разработки. Обоснованием служила недоступность ребра compute→vpc
// :9091, но недоступность создавал сам чарт компьюта — он не выставлял группу
// клиентских кред, которую читает конфиг (см. peer_mtls_producer_test.go рядом).
// То есть пустая строка была обходом собственного дефекта, а стоила — молчащего
// шага высвобождения интерфейса на КАЖДОМ удалении машины.
//
// Проверка ДЕКЛАРАТИВНА: читает файлы значений, а не отрендеренный манифест,
// поэтому не зависит ни от helm, ни от порядка наложения профилей и не может
// пропуститься. Перечень ключей ВЫВОДИТСЯ из умолчаний самого чарта, а не
// выписывается: чарт объявляет адрес непустым умолчанием ⇒ он считает его
// обязательным, и новый адрес попадает под правило сам.
package deploy_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	computeChartValues = "values.yaml"
	umbrellaValuesGlob = "../../../deploy/helm/umbrella/values*.yaml"
)

// requiredPeerAddrKeys — верхнеуровневые ключи чарта компьюта, чьё имя
// оканчивается на "Addr" и чьё умолчание — непустая строка. Вложенные адреса
// (напр. authzIam.grpcAddr) сюда намеренно не входят: у них своя форма
// переопределения, и правило про них здесь не утверждается.
func requiredPeerAddrKeys(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(computeChartValues))
	if err != nil {
		t.Fatalf("прочитать умолчания чарта %s: %v", computeChartValues, err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("разобрать %s: %v", computeChartValues, err)
	}
	var keys []string
	for k, v := range tree {
		if !strings.HasSuffix(k, "Addr") {
			continue
		}
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestNoUmbrellaProfileBlanksAComputePeerAddress — ни один профиль зонтика не
// обнуляет адрес соседа, объявленный чартом обязательным.
func TestNoUmbrellaProfileBlanksAComputePeerAddress(t *testing.T) {
	keys := requiredPeerAddrKeys(t)
	if len(keys) == 0 {
		t.Fatalf("%s: не найдено ни одного ключа вида *Addr с непустым умолчанием — "+
			"предпосылка проверки исчезла (переехали ключи чарта?), а не «нарушений нет»",
			computeChartValues)
	}

	profiles, err := filepath.Glob(filepath.FromSlash(umbrellaValuesGlob))
	if err != nil {
		t.Fatalf("перечислить профили %s: %v", umbrellaValuesGlob, err)
	}
	if len(profiles) == 0 {
		t.Fatalf("по образцу %s не найдено ни одного профиля — "+
			"предпосылка проверки исчезла (переехал зонтик?)", umbrellaValuesGlob)
	}
	sort.Strings(profiles)

	overrides := 0
	for _, path := range profiles {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("прочитать профиль %s: %v", path, rerr)
		}
		var tree map[string]any
		if uerr := yaml.Unmarshal(raw, &tree); uerr != nil {
			t.Fatalf("разобрать профиль %s: %v", path, uerr)
		}
		compute, ok := tree["compute"].(map[string]any)
		if !ok {
			continue
		}
		for _, k := range keys {
			v, present := compute[k]
			if !present {
				continue
			}
			overrides++
			s, isStr := v.(string)
			if !isStr || strings.TrimSpace(s) == "" {
				t.Errorf("%s: compute.%s обнулён (%#v) — пустое значение не выключает ребро, "+
					"а подменяет его заглушкой: мутации отвечают Unavailable, а шаг высвобождения "+
					"в саге удаления машины молча находит пустой список и не снимает ничего. "+
					"Нужен стенд без соседа — выключай сабчарт соседа, а не адрес у консумера",
					filepath.Base(path), k, v)
			}
		}
	}

	// Объём осмотренного печатается всегда: «ноль находок» обязано быть отличимо
	// от «ноль прочитанного».
	t.Logf("осмотрено: %d профилей зонтика, %d обязательных адресных ключей (%s), %d переопределений",
		len(profiles), len(keys), strings.Join(keys, ", "), overrides)
}
