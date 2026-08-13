// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// executor_profile_test.go — профиль возможностей исполнителя датаплейна
// ОБЪЯВЛЕН чартом и доезжает до процесса.
//
// ЧТО ОХРАНЯЕТСЯ. Управляющий контур vpc принимает от арендатора адресные
// диапазоны, правила со ссылкой на именованный набор, ограничение полосы и
// прочее, что реализовать обязан исполнитель датаплейна. Управляющий контур его
// возможностей не знает и знать не может — их ОБЪЯВЛЯЕТ посадка. Пока объявления
// нет, «принято» и «реализуемо» неотличимы: запрос проходит проверку контура и
// не может быть исполнен, а узнать об этом можно только по последствиям в чужой
// отладке.
//
// Несущий признак — пересечение адресов между арендаторами. Уникальность
// диапазонов у vpc ограничена сетью по построению (`EXCLUDE USING gist
// (network_id WITH =, …)` в миграциях подсетей), то есть два арендатора вправе
// держать один и тот же диапазон и держат его штатно. Исполнитель, который не
// изолирует одинаковые адреса разных арендаторов, сводит их трафик вместе.
// Поэтому боевая посадка, не объявившая этот признак, НЕ ПОДНИМАЕТСЯ
// (config.ValidateExecutorProfile), а базовый профиль чарта обязан объявлять его
// положительно — молчание профиля означает «отрендерится», а не «безопасно».
//
// ПОЧЕМУ ЧИТАЮТСЯ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР. Тот же довод, что у соседней пробы
// (list_filter_default_test.go) и у gateway (deploy/token_shape_test.go): helm в
// этой среде недоступен, а проба, требующая внешнего инструмента, пропускается
// там, где его нет — и тогда измерение «ключа нет вовсе» бесполезно по
// построению. Отсутствие ключа — падение, а не пропуск.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// executorProfileKey — одна возможность исполнителя: ключ чарта (camelCase),
// путь значения, который обязан быть прочитан шаблоном, и зачем он нужен.
//
// Перечень закрытый и полный НАМЕРЕННО: он и есть то, что арендатор вправе
// попросить у контура. Добавили возможность — добавили строку сюда, иначе
// «объявлено» перестанет означать «объявлено полностью».
type executorProfileKey struct {
	name string // ключ внутри dataplane.executor в values.yaml
	why  string
}

var executorProfileKeys = []executorProfileKey{
	{"overlappingTenantAddresses", "пересечение адресов между арендаторами: диапазоны у vpc уникальны только в пределах сети, поэтому одинаковые адреса разных арендаторов — штатный случай"},
	{"stateTrackingFamilies", "отслеживание состояния по семействам (v4/v6): правило принимает и v4-, и v6-диапазоны, а обратный трафик разрешается состоянием"},
	{"namedSetReferenceInRule", "ссылка на именованный набор в правиле: цель правила может быть задана идентификатором группы, а не литеральным диапазоном"},
	{"guaranteedPayloadBytes", "гарантированный полезный размер кадра"},
	{"guaranteedBandwidthPerInterfaceMbps", "гарантированная полоса на интерфейс"},
	{"connectionLimitPerInterface", "предел соединений на интерфейс"},
	{"tenantSettableBandwidthLimit", "ограничение полосы, задаваемое арендатором"},
}

// executorProfileFromValues — объявление профиля в values-файле чарта. Возвращает
// само дерево, а не «нашлось/нет»: ненайденный блок — отдельный исход, и
// вызывающий обязан его различать.
func executorProfileFromValues(t *testing.T, path string) (map[string]any, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("не прочитан %s: %v", path, err)
	}
	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		t.Fatalf("%s не разбирается как YAML: %v", path, err)
	}
	dp, ok := root["dataplane"].(map[string]any)
	if !ok {
		return nil, false
	}
	ex, ok := dp["executor"].(map[string]any)
	return ex, ok
}

// Базовый профиль чарта ОБЯЗАН объявлять все возможности исполнителя.
//
// Перепись объявленных ключей — отдельное утверждение: «нарушений нет» обязано
// быть отличимо от «ничего не прочитано».
func TestChartDeclaresTheExecutorProfile(t *testing.T) {
	profile, found := executorProfileFromValues(t, vpcValues)
	if !found {
		t.Fatalf("%s не объявляет dataplane.executor — управляющий контур принимает "+
			"адресные диапазоны, правила и ограничения полосы, ничего не зная о том, что "+
			"исполнитель умеет; боевая посадка на этом не поднимается", vpcValues)
	}

	declared := 0
	for _, k := range executorProfileKeys {
		v, ok := profile[k.name]
		if !ok {
			t.Errorf("%s: dataplane.executor.%s не объявлен — %s", vpcValues, k.name, k.why)
			continue
		}
		if v == nil {
			t.Errorf("%s: dataplane.executor.%s объявлен пустым значением — это не объявление, "+
				"а его вид (%s)", vpcValues, k.name, k.why)
			continue
		}
		declared++
	}
	t.Logf("перепись: возможностей в перечне %d, объявлено в %s — %d",
		len(executorProfileKeys), vpcValues, declared)
}

// Несущее предусловие: пересечение адресов объявлено ПОДДЕРЖАННЫМ.
//
// Требуется именно положительное объявление, а не отсутствие `false`: базовый
// профиль чарта объявляет `authn.mode: production`, поэтому стоковая посадка
// обязана подниматься — а страж старта её не поднимет, пока признака нет.
func TestChartDeclaresOverlappingTenantAddressesSupported(t *testing.T) {
	profile, found := executorProfileFromValues(t, vpcValues)
	if !found {
		t.Fatalf("%s не объявляет dataplane.executor — предусловию пересечения адресов "+
			"нечего утверждать", vpcValues)
	}
	got, ok := profile["overlappingTenantAddresses"].(bool)
	if !ok {
		t.Fatalf("%s: dataplane.executor.overlappingTenantAddresses не объявлен признаком "+
			"(получено %#v) — признак, заданный не признаком, страж прочитает как «не объявлено»",
			vpcValues, profile["overlappingTenantAddresses"])
	}
	if !got {
		t.Fatalf("%s объявляет dataplane.executor.overlappingTenantAddresses: false — "+
			"стоковая посадка (authn.mode: production) с этим значением НЕ СТАРТУЕТ. Пока "+
			"исполнитель не изолирует одинаковые адреса разных арендаторов, включать "+
			"пересечение нельзя: диапазоны у vpc уникальны лишь в пределах сети", vpcValues)
	}
}

// Каждый объявленный ключ обязан иметь ЧИТАТЕЛЯ в шаблоне.
//
// Значение из профиля доходит до процесса ровно одним путём: шаблон подставляет
// его в файл настроек. Ключ, на который шаблон не ссылается, не покидает профиль
// никогда — оператор при этом уверен, что распоряжается посадкой. Тот же класс,
// что держит гейт дерева на ключах профилей развёртывания.
func TestChartTemplateReadsEveryExecutorProfileKey(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(vpcConfigMap))
	if err != nil {
		t.Fatalf("не прочитан %s: %v", vpcConfigMap, err)
	}
	body := string(raw)

	read := 0
	for _, k := range executorProfileKeys {
		ref := ".Values.dataplane.executor." + k.name
		if !strings.Contains(body, ref) {
			t.Errorf("%s не читает %s — ключ объявлен в values.yaml и до процесса не доедет "+
				"(%s)", vpcConfigMap, ref, k.why)
			continue
		}
		read++
	}
	t.Logf("перепись: ключей в перечне %d, прочитано шаблоном %d", len(executorProfileKeys), read)
}

// Шаблон не вправе подставлять умолчание за несущий признак.
//
// `default false` на этой строке объявил бы небезопасное умолчание ВТОРЫМ местом:
// удали ключ из values.yaml — и рендер всё равно дал бы «пересечение не
// поддержано», то есть правка одного values.yaml не имела бы силы. Ровно этот
// дефект уже был у соседнего фильтра видимости.
func TestChartTemplate_DoesNotDefaultTheOverlapPrecondition(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(vpcConfigMap))
	if err != nil {
		t.Fatalf("не прочитан %s: %v", vpcConfigMap, err)
	}
	for _, ln := range strings.Split(string(raw), "\n") {
		if !strings.Contains(ln, "dataplane.executor.overlappingTenantAddresses") {
			continue
		}
		if strings.Contains(ln, "default ") {
			t.Fatalf("%s подставляет умолчание за несущий признак: %q — правка values.yaml "+
				"на этой строке не имеет силы", vpcConfigMap, strings.TrimSpace(ln))
		}
		return
	}
	t.Fatalf("%s не рендерит dataplane.executor.overlappingTenantAddresses вовсе — "+
		"признак не доезжает до процесса", vpcConfigMap)
}
