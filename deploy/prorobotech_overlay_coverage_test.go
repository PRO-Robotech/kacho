// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// prorobotech_overlay_coverage_test.go — накладка публичных образов покрывает
// КАЖДЫЙ компонент, который разворачивает слой под ней.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// `values.prorobotech.yaml` существует ради одного: заменить локальные образы
// kind на опубликованные, чтобы продукт поднимался на управляемом кластере.
// Компонент, которого в ней нет, наследует объявление слоя под собой — то есть
// остаётся на `kacho-<svc>:dev`. Такого образа нет НИ В ОДНОМ реестре: под
// уходит в ImagePullBackOff, и происходит это не при рендере, а на кластере,
// через минуты после «helm upgrade прошёл».
//
// Замер на день заведения: слой объявлял восемь компонентов, накладка — шесть.
// Отсутствовали storage, geo и registry, хотя их образы CI публикует наравне с
// остальными (проверено: у всех трёх есть тег на том же коммите, что у прочих).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПРОПУСК НЕВИДИМ БЕЗ ЭТОЙ ПРОБЫ
//
// Накладка — файл про образы, и «компонента здесь нет» неотличимо от «компонент
// не разворачивается»: обе формы выглядят как молчание. Рендер её не ловит
// (манифест собирается, образ в нём есть — просто недостижимый), `helm lint`
// тоже. Первый наблюдатель — кластер.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ПРОБА ЧИТАЕТ
//
// Только ОБЪЯВЛЕНИЯ — оба файла значений, — поэтому она не требует ни чартов,
// ни сети, ни кластера и не умеет пропуститься. Форма объявления образа у
// подчартов разная (плоская строка либо карта), и проба принимает обе: предмет
// — покрытие компонента, а не написание ключа.
package deploy_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// prorobotechBaseProfile — слой, чьи компоненты накладка обязана покрыть.
// Именно этой парой файлов её и предписывает применять её собственная шапка.
const (
	prorobotechBaseProfile    = "values.dev.yaml"
	prorobotechOverlayProfile = "values.prorobotech.yaml"
)

// componentDeclaresImage — компонент объявляет образ в этом дереве значений.
//
// Принимаются обе формы: `image: "<строка>"` у братских подчартов и
// `image: {repository, tag}` у вендоренных. Различие — деталь их чартов, а не
// предмет этой пробы.
func componentDeclaresImage(tree map[string]any, name string) bool {
	return componentImageRef(tree, name) != ""
}

// componentImageRef — ссылка на образ компонента, либо пусто.
func componentImageRef(tree map[string]any, name string) string {
	comp, ok := tree[name].(map[string]any)
	if !ok {
		return ""
	}
	switch img := comp["image"].(type) {
	case string:
		return strings.TrimSpace(img)
	case map[string]any:
		repo, _ := img["repository"].(string)
		return strings.TrimSpace(repo)
	}
	return ""
}

// imageIsLocalToTheStand — образ ожидается уже загруженным в kind, а не
// подтягиваемым из реестра.
//
// Признак — ОТСУТСТВИЕ сегмента пространства имён: `kacho-vpc:dev` против
// `bitnamilegacy/postgresql` и `quay.io/minio/minio`. В этом дереве предикат
// точен, и это свойство дерева, а не догадка: КАЖДЫЙ сторонний образ здесь
// несёт пространство имён, а голым именем объявлены только собственные сборки
// продукта. Проверяется положительным контролем ниже — если сторонний образ
// однажды объявят голым именем официальной библиотеки (`postgres:16`), гейт
// потребует его назвать, и это правильный исход: тогда предикат перестанет быть
// точным молча.
func imageIsLocalToTheStand(ref string) bool {
	repo := ref
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	return repo != "" && !strings.Contains(repo, "/")
}

// TestProrobotechOverlayCoversEveryEnabledComponent — каждый компонент базового
// слоя, объявляющий образ, объявлен и в накладке.
func TestProrobotechOverlayCoversEveryEnabledComponent(t *testing.T) {
	base := readYAML(t, filepath.Join(umbrellaDir, prorobotechBaseProfile))
	overlay := readYAML(t, filepath.Join(umbrellaDir, prorobotechOverlayProfile))

	var (
		withImage []string
		local     []string
		missing   []string
	)
	for name := range base {
		ref := componentImageRef(base, name)
		if ref == "" {
			continue // не компонент с образом — не предмет
		}
		withImage = append(withImage, name)
		if !imageIsLocalToTheStand(ref) {
			continue // сторонний образ уже публичен — заменять нечего
		}
		local = append(local, name)
		if !componentDeclaresImage(overlay, name) {
			missing = append(missing, name)
		}
	}
	sort.Strings(withImage)
	sort.Strings(local)
	sort.Strings(missing)

	t.Logf("осмотрено: компонентов с образом в %s — %d; из них локальных для стенда — %d %v; "+
		"не покрыто накладкой — %d",
		prorobotechBaseProfile, len(withImage), len(local), local, len(missing))

	if len(withImage) == 0 {
		t.Fatal("в базовом слое не найдено ни одного компонента с образом — «покрыто всё» " +
			"здесь означало бы «нечего покрывать», а не покрытие")
	}
	if len(local) == 0 {
		t.Fatal("ни один образ базового слоя не опознан локальным — предикат разошёлся с " +
			"деревом, и проба обязана краснеть, а не молчать: молчание здесь неотличимо от " +
			"полного покрытия")
	}

	for _, name := range missing {
		t.Errorf("%s не объявлен в %s — на управляемом кластере он останется на локальном "+
			"образе kind, которого нет ни в одном реестре, и уйдёт в ImagePullBackOff уже ПОСЛЕ "+
			"успешного «helm upgrade». Ни рендер, ни lint этого не видят: манифест собирается, "+
			"образ в нём есть, он просто недостижим",
			name, prorobotechOverlayProfile)
	}
}

// TestProrobotechOverlayDeclaresOnlyPublishedRegistry — накладка объявляет
// ПУБЛИЧНЫЕ образы, а не локальные.
//
// Положительный контроль к пробе выше: без него «покрыт» удовлетворялось бы
// записью, повторяющей локальный образ, — покрытие формой без содержания.
func TestProrobotechOverlayDeclaresOnlyPublishedRegistry(t *testing.T) {
	overlay := readYAML(t, filepath.Join(umbrellaDir, prorobotechOverlayProfile))
	const wantPrefix = "docker.io/prorobotech/"

	checked := 0
	for name := range overlay {
		comp, ok := overlay[name].(map[string]any)
		if !ok {
			continue
		}
		var ref string
		switch img := comp["image"].(type) {
		case string:
			ref = img
		case map[string]any:
			ref, _ = img["repository"].(string)
		default:
			continue
		}
		if strings.TrimSpace(ref) == "" {
			continue
		}
		checked++
		if !strings.HasPrefix(ref, wantPrefix) {
			t.Errorf("%s: образ %q не из публичного реестра — накладка существует ровно затем, "+
				"чтобы заменить локальные образы kind на опубликованные", name, ref)
		}
	}

	t.Logf("осмотрено: объявлений образа в накладке — %d", checked)
	if checked == 0 {
		t.Fatal("в накладке нет ни одного объявления образа — предмет пробы исчез")
	}
}
