// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// image_overlay_coverage_test.go — стенд, который тянет образы из реестра,
// тянет их для КАЖДОГО своего компонента.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Накладка публичных образов существует ради одного: заменить локальные образы
// kind на опубликованные, чтобы продукт поднимался на управляемом кластере.
// Компонент, которого в ней нет, наследует объявление слоя под собой — то есть
// остаётся на образе, которого нет ни в одном реестре. Под уходит в
// ImagePullBackOff, и происходит это не при рендере, а на кластере, через
// минуты после «helm upgrade прошёл».
//
// Замер на день заведения пробы: слой объявлял восемь компонентов, накладка —
// шесть; отсутствовали три, хотя их образы CI публикует наравне с остальными.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЕДИНИЦА ИЗМЕРЕНИЯ — СЛОЖЕННЫЙ РЕЛИЗ, А НЕ ОДНА НАКЛАДКА
//
// Первая редакция этой пробы была привязана КОНСТАНТОЙ к одной накладке из
// двух: вторая не осматривалась вовсе, и её «ноль находок» был неотличим от
// «ноль прочитанного». Свойство здесь принадлежит не файлу, а СОСТАВЛЕННОМУ
// стенду: накладка законно молчит о компоненте, если о нём уже сказал слой под
// ней, и законно молчит обо всём, если менять нечего. Судить надо результат
// наложения.
//
// Поэтому набор осматриваемых стендов ВЫВОДИТСЯ: цепочки берутся из таблицы
// стеков, складываются так же, как их складывает helm, и стенд признаётся
// «тянущим из реестра», если хотя бы один его слой ЗАМЕНИЛ локальный образ
// нижнего слоя на опубликованный. Новая площадка приходит под проверку без
// правки этого файла — и не приходит, пока менять ей нечего.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ПРОБА ЧИТАЕТ
//
// Только ОБЪЯВЛЕНИЯ — файлы значений, — поэтому она не требует ни чартов, ни
// сети, ни кластера и не умеет пропуститься. Форма объявления образа у
// подчартов разная (плоская строка либо карта), и проба принимает обе: предмет
// — покрытие компонента, а не написание ключа.
package deploy_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// componentImageRef — ссылка на образ компонента в дереве значений, либо пусто.
//
// Принимаются обе формы: `image: "<строка>"` у братских подчартов и
// `image: {repository, tag}` у вендоренных. Различие — деталь их чартов, а не
// предмет этой пробы.
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
// `bitnamilegacy/postgresql` и опубликованный образ сервиса. В этом дереве предикат
// точен, и это свойство дерева, а не догадка: КАЖДЫЙ сторонний образ здесь
// несёт пространство имён, а голым именем объявлены только собственные сборки
// продукта. Проверяется положительным контролем ниже — если сторонний образ
// однажды объявят голым именем официальной библиотеки (`postgres:16`), гейт
// потребует его назвать, и это правильный исход: тогда предикат перестанет
// быть точным молча.
func imageIsLocalToTheStand(ref string) bool {
	repo := ref
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	return repo != "" && !strings.Contains(repo, "/")
}

// chainImages — состояние образов сложенного стенда.
type chainImages struct {
	withImage []string // компоненты, у которых образ объявлен хоть где-то в цепочке
	replaced  []string // компоненты, чей локальный образ ЗАМЕНЁН публичным выше по цепочке
	local     []string // компоненты, чей ИТОГОВЫЙ образ остался локальным
}

// foldChainImages складывает цепочку слева направо и отвечает, что получил бы
// релиз. Чистая функция над деревьями значений — самопроверка ниже подаёт ей
// синтетический вход, а не подделывает дерево.
func foldChainImages(layers []map[string]any) chainImages {
	final := map[string]string{}  // компонент → итоговая ссылка
	wasLocal := map[string]bool{} // компонент был локальным на каком-то слое
	for _, layer := range layers {
		for name := range layer {
			ref := componentImageRef(layer, name)
			if ref == "" {
				continue
			}
			if imageIsLocalToTheStand(ref) {
				wasLocal[name] = true
			}
			final[name] = ref
		}
	}

	var out chainImages
	for name, ref := range final {
		out.withImage = append(out.withImage, name)
		switch {
		case imageIsLocalToTheStand(ref):
			out.local = append(out.local, name)
		case wasLocal[name]:
			out.replaced = append(out.replaced, name)
		}
	}
	sort.Strings(out.withImage)
	sort.Strings(out.replaced)
	sort.Strings(out.local)
	return out
}

// TestRegistryPulledStacksLeaveNoStandLocalImage — сам гейт.
func TestRegistryPulledStacksLeaveNoStandLocalImage(t *testing.T) {
	chains := deployStacks(t)
	base := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))

	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)

	pulling, standLocal, componentsSeen := 0, 0, 0
	for _, name := range names {
		layers := []map[string]any{base}
		for _, p := range chains[name] {
			layers = append(layers, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		got := foldChainImages(layers)
		componentsSeen += len(got.withImage)

		if len(got.replaced) == 0 {
			// Стенд ничего не заменял — он поднимается образами стенда либо
			// вовсе не объявляет своих. Это законно, и объём осмотренного
			// печатается, чтобы «не рассматривался» не читалось как «покрыт».
			t.Logf("%s: не тянет из реестра — замен локального образа 0 "+
				"(компонентов с образом %d, из них локальных на выходе %d)",
				name, len(got.withImage), len(got.local))
			standLocal += len(got.local)
			continue
		}
		pulling++
		t.Logf("%s: тянет из реестра — заменено %d (%s); компонентов с образом %d; "+
			"осталось локальных %d",
			name, len(got.replaced), strings.Join(got.replaced, " "),
			len(got.withImage), len(got.local))

		for _, comp := range got.local {
			t.Errorf("%s: компонент %q остался на образе стенда — цепочка заменяет локальные "+
				"образы публичными (%d уже заменено), но этот пропущен. На управляемом кластере "+
				"такого образа нет ни в одном реестре: под уйдёт в ImagePullBackOff уже ПОСЛЕ "+
				"успешного «helm upgrade». Ни рендер, ни lint этого не видят — манифест "+
				"собирается, образ в нём есть, он просто недостижим",
				name, comp, len(got.replaced))
		}
	}

	// Проверка СВОЕЙ предпосылки. «Ноль находок» обязано быть отличимо от «ноль
	// прочитанного» ТРЕМЯ разными способами, потому что ослепнуть эта проба
	// может тремя: перестать видеть стеки, перестать видеть образы и перестать
	// узнавать локальный образ (тогда заменять станет «нечего» — и молчание
	// будет полным).
	if len(chains) == 0 || componentsSeen == 0 {
		t.Fatalf("обход ничего не прочитал: стеков=%d, компонентов с образом=%d — "+
			"предикат перестал узнавать дерево, а не дерево стало чистым",
			len(chains), componentsSeen)
	}
	if pulling == 0 {
		t.Fatalf("ни один стек не опознан тянущим из реестра (стеков %d, компонентов с "+
			"образом %d) — либо предикат локального образа разошёлся с деревом, либо накладки "+
			"перестали заменять. Молчание здесь неотличимо от полного покрытия", len(chains), componentsSeen)
	}
	if standLocal == 0 {
		t.Fatalf("ни один стек не объявил локального образа вовсе — предикат перестал их " +
			"узнавать, и тогда «заменено всё» означает «заменять было нечего»")
	}
	t.Logf("осмотрено: стеков %d, из них тянущих из реестра %d; компонентов с образом %d; "+
		"локальных образов на стендах разработки %d", len(chains), pulling, componentsSeen, standLocal)
}

// TestRegistryPulledStacksDeclarePublishedImages — положительный контроль к
// пробе выше: замена обязана вести в реестр С ПРОСТРАНСТВОМ ИМЁН, иначе
// «покрыт» удовлетворялось бы записью, повторяющей локальный образ, — покрытие
// формой без содержания.
func TestRegistryPulledStacksDeclarePublishedImages(t *testing.T) {
	chains := deployStacks(t)
	base := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))

	checked := 0
	for name, chain := range chains {
		layers := []map[string]any{base}
		for _, p := range chain {
			layers = append(layers, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		got := foldChainImages(layers)
		if len(got.replaced) == 0 {
			continue
		}
		merged := map[string]any{}
		for _, l := range layers {
			merged = mergeValues(merged, l)
		}
		for _, comp := range got.replaced {
			ref := componentImageRef(merged, comp)
			checked++
			if !strings.Contains(ref, "/") {
				t.Errorf("%s: %s — замена ведёт на %q, где нет пространства имён; такой образ "+
					"тянется не из реестра, а ожидается уже загруженным в узел", name, comp, ref)
			}
		}
	}
	t.Logf("осмотрено: замен локального образа на публичный — %d", checked)
	if checked == 0 {
		t.Fatal("замен не найдено ни одной — предмет пробы исчез, и её молчание ничего не значит")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе той же формы.

func TestFoldChainImages_SelfTest(t *testing.T) {
	stand := map[string]any{
		"vpc":     map[string]any{"image": "kacho-vpc:dev"},
		"storage": map[string]any{"image": "kacho-storage:dev"},
		"pg":      map[string]any{"image": map[string]any{"repository": "bitnamilegacy/postgresql"}},
	}

	// (а) внесённый дефект — накладка заменила один компонент из двух.
	partial := foldChainImages([]map[string]any{stand, {
		"vpc": map[string]any{"image": "docker.io/prorobotech/kacho-vpc:main-1"},
	}})
	if len(partial.replaced) != 1 || len(partial.local) != 1 || partial.local[0] != "storage" {
		t.Fatalf("неполная накладка не опознана: %+v", partial)
	}

	// (б) законная конструкция ТОЙ ЖЕ формы — заменены оба. Молчит.
	full := foldChainImages([]map[string]any{stand, {
		"vpc":     map[string]any{"image": "docker.io/prorobotech/kacho-vpc:main-1"},
		"storage": map[string]any{"image": "docker.io/prorobotech/kacho-storage:main-1"},
	}})
	if len(full.local) != 0 || len(full.replaced) != 2 {
		t.Fatalf("полная накладка покрашена: %+v", full)
	}

	// (в) второй законный близнец — стенд разработки без накладки вовсе: замен
	//     нет, значит правило к нему не относится (а не «нарушено»).
	only := foldChainImages([]map[string]any{stand})
	if len(only.replaced) != 0 || len(only.local) != 2 {
		t.Fatalf("стенд без накладки разобран неверно: %+v", only)
	}

	// (г) третий законный близнец — сторонний образ уже публичен, заменять
	//     нечего, и он не обязан появляться в накладке.
	for _, comp := range full.local {
		if comp == "pg" {
			t.Fatalf("сторонний образ с пространством имён засчитан локальным: %+v", full)
		}
	}

	// (д) накладка, повторившая ЛОКАЛЬНЫЙ образ, покрытием не является:
	//     компонент остаётся в local, а не переезжает в replaced.
	echoed := foldChainImages([]map[string]any{stand, {
		"vpc": map[string]any{"image": "kacho-vpc:other"},
	}})
	if len(echoed.replaced) != 0 || len(echoed.local) != 2 {
		t.Fatalf("повтор локального образа засчитан заменой: %+v", echoed)
	}
}

// Предикат обязан узнавать НАСТОЯЩЕЕ дерево, а не только синтетику: иначе
// самопроверка выше зелёная, а обход читает ноль.
func TestImageOverlayPredicates_RecogniseTheRealTree(t *testing.T) {
	dev := readYAML(t, filepath.Join(umbrellaDir, "values.dev.yaml"))
	local := 0
	for name := range dev {
		if ref := componentImageRef(dev, name); ref != "" && imageIsLocalToTheStand(ref) {
			local++
		}
	}
	if local == 0 {
		t.Errorf("в базовом слое стенда не опознано ни одного локального образа — предикат " +
			"разошёлся с деревом; тогда «заменено всё» означает «заменять было нечего»")
	}
	// Отрицание — только в паре с положительным: предикат, признающий локальным
	// что угодно, зеленит проверку выше.
	for _, published := range []string{
		"docker.io/prorobotech/kacho-vpc:main-1", "bitnamilegacy/postgresql", "docker.io/prorobotech/kaname:main-1",
	} {
		if imageIsLocalToTheStand(published) {
			t.Errorf("%q опознан локальным для стенда — предикат стал слишком широким", published)
		}
	}
	t.Logf("осмотрено: локальных образов в базовом слое стенда — %d", local)
}
