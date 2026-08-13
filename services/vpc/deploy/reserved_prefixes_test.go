// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// reserved_prefixes_test.go — перечень адресных диапазонов, которые платформа
// держит за собой, ОБЪЯВЛЕН чартом и доезжает до процесса.
//
// ЧТО ОХРАНЯЕТСЯ. Часть адресного пространства обслуживает саму платформу:
// служебные адреса узлов, адреса служб внутри подсети, точка получения метаданных
// экземпляра. Подсеть арендатора, объявленная поверх такого диапазона, проходит
// все проверки контура и НЕ РАБОТАЕТ, причём симптом выглядит сетевым, а причина
// лежит в перекрытии.
//
// Какие диапазоны служебные — знает ПОСАДКА, а не продукт, поэтому перечень
// объявляется конфигурацией. Следствие: у настройки есть состояние «не задана», и
// оно не безобидно — пустой перечень означает «не сужаем», а не «нечего сужать»:
// проверка на пути запроса присутствует, исполняется на каждом создании подсети и
// не отвергает ничего. Поэтому боевая посадка на пустом перечне НЕ ПОДНИМАЕТСЯ
// (config.ValidateReservedPrefixes), а базовый профиль чарта обязан объявлять
// перечень непустым — молчание профиля означает «отрендерится», а не «безопасно».
//
// ПОЧЕМУ ЧИТАЮТСЯ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР. Тот же довод, что у соседних проб
// (list_filter_default_test.go, executor_profile_test.go): helm в этой среде
// недоступен, а проба, требующая внешнего инструмента, пропускается там, где его
// нет — и тогда измерение «ключа нет вовсе» бесполезно по построению. Отсутствие
// ключа — падение, а не пропуск.
package deploy_test

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// reservedPrefixesFromValues — объявление перечня в values-файле чарта.
// Возвращает само значение, а не «нашлось/нет»: ненайденный ключ — отдельный
// исход, и вызывающий обязан его различать.
func reservedPrefixesFromValues(t *testing.T, path string) ([]any, bool) {
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
	entries, ok := dp["reservedPrefixes"].([]any)
	return entries, ok
}

// Базовый профиль чарта ОБЯЗАН объявлять перечень, и объявлять непустым.
//
// Требуется именно положительное объявление, а не отсутствие пустого списка:
// базовый профиль объявляет `authn.mode: production`, поэтому стоковая посадка
// обязана подниматься — а страж старта её не поднимет, пока перечня нет.
//
// Перепись прочитанного — отдельное утверждение: «нарушений нет» обязано быть
// отличимо от «ничего не прочитано».
func TestChartDeclaresTheReservedPrefixes(t *testing.T) {
	entries, found := reservedPrefixesFromValues(t, vpcValues)
	if !found {
		t.Fatalf("%s не объявляет dataplane.reservedPrefixes — контур принимает диапазоны "+
			"арендатора, ничего не зная о том, какая часть адресного пространства обслуживает "+
			"саму платформу; стоковая боевая посадка на этом не поднимается", vpcValues)
	}
	if len(entries) == 0 {
		t.Fatalf("%s объявляет dataplane.reservedPrefixes пустым — это НЕ «нечего резервировать», "+
			"а «не сужаем»: проверка на пути запроса исполняется на каждом создании подсети и не "+
			"отвергает ничего. Стоковая посадка (authn.mode: production) с пустым перечнем НЕ СТАРТУЕТ",
			vpcValues)
	}
	t.Logf("перепись: объявлено записей в %s — %d", vpcValues, len(entries))
}

// Каждая объявленная запись — CIDR в канонической форме.
//
// Проба нужна не ради педантизма: страж старта отвергает негодную запись, поэтому
// опечатка в базовом профиле означает, что стоковая посадка НЕ ПОДНИМЕТСЯ. Пусть
// об этом скажет проба, а не первый оператор, поднимающий стенд.
//
// Форма проверяется тем же разбором, каким её читает продукт (`netip.ParsePrefix`
// + сверка с сетевым адресом), а не собственным подобием: второе написание
// разошлось бы с первым молча.
func TestChartReservedPrefixesAreCanonicalCIDRs(t *testing.T) {
	entries, found := reservedPrefixesFromValues(t, vpcValues)
	if !found {
		t.Fatalf("%s не объявляет dataplane.reservedPrefixes — форме нечего проверять", vpcValues)
	}

	checked := 0
	for _, raw := range entries {
		entry, ok := raw.(string)
		if !ok {
			t.Errorf("%s: запись %#v объявлена не строкой — загрузчик прочитает её не как CIDR",
				vpcValues, raw)
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(entry))
		if err != nil {
			t.Errorf("%s: запись %q не разбирается как CIDR (%v) — страж старта отвергнет её, "+
				"и стоковая посадка не поднимется", vpcValues, entry, err)
			continue
		}
		if prefix.Masked() != prefix {
			t.Errorf("%s: у записи %q непустые host-биты — напишите сетевой адрес %s",
				vpcValues, entry, prefix.Masked())
			continue
		}
		if prefix.Bits() == 0 {
			t.Errorf("%s: запись %q покрывает всё семейство — контуру не осталось бы ни одного "+
				"адреса для выдачи, и каждая подсеть этого семейства была бы отвергнута", vpcValues, entry)
			continue
		}
		if prefix.Addr().Is4In6() {
			t.Errorf("%s: запись %q написана в форме IPv4-в-IPv6 — это адрес семейства IPv6, "+
				"он не пересечётся ни с одним v4-блоком арендатора и не зарезервирует ничего",
				vpcValues, entry)
			continue
		}
		checked++
	}
	t.Logf("перепись: записей прочитано %d, признано каноничными %d", len(entries), checked)
}

// Ключ обязан иметь ЧИТАТЕЛЯ в шаблоне.
//
// Значение из профиля доходит до процесса ровно одним путём: шаблон подставляет
// его в файл настроек. Ключ, на который шаблон не ссылается, не покидает профиль
// никогда — оператор при этом уверен, что распоряжается посадкой.
func TestChartTemplateReadsTheReservedPrefixesKey(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(vpcConfigMap))
	if err != nil {
		t.Fatalf("не прочитан %s: %v", vpcConfigMap, err)
	}
	if !strings.Contains(string(raw), ".Values.dataplane.reservedPrefixes") {
		t.Fatalf("%s не читает .Values.dataplane.reservedPrefixes — перечень объявлен в "+
			"values.yaml и до процесса не доедет, а боевая посадка не поднимется", vpcConfigMap)
	}
}

// Шаблон не вправе подставлять умолчание за перечень.
//
// `default` на этой строке объявил бы значение ВТОРЫМ местом: удали ключ из
// values.yaml — и рендер всё равно дал бы что-то, то есть правка одного
// values.yaml не имела бы силы. Ровно этот дефект уже был у фильтра видимости и у
// несущего признака профиля исполнителя.
func TestChartTemplate_DoesNotDefaultTheReservedPrefixes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(vpcConfigMap))
	if err != nil {
		t.Fatalf("не прочитан %s: %v", vpcConfigMap, err)
	}
	for _, ln := range strings.Split(string(raw), "\n") {
		if !strings.Contains(ln, "dataplane.reservedPrefixes") {
			continue
		}
		if strings.Contains(ln, "default ") {
			t.Fatalf("%s подставляет умолчание за перечень служебных диапазонов: %q — правка "+
				"values.yaml на этой строке не имеет силы", vpcConfigMap, strings.TrimSpace(ln))
		}
		return
	}
	t.Fatalf("%s не рендерит dataplane.reservedPrefixes вовсе — перечень не доезжает до процесса",
		vpcConfigMap)
}
