// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_image_has_a_producer_test.go — образ, который просит стенд kind,
// кто-то собирает и кладёт в узлы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Локальный образ (`kaname:dev`, `kacho-vpc:dev`) в реестре не существует: его
// кладёт в узлы кластера сборка образов, а профиль стенда лишь НАЗЫВАЕТ. Стороны
// две, объявление у них раздельное, и расхождение читается только в кластере:
//
//	Failed to pull image "kaname:dev": failed to resolve reference
//	"docker.io/library/kaname:dev": pull access denied, repository does not
//	exist or may require authorization  →  Init:ImagePullBackOff
//
// Ни рендер, ни `helm lint`, ни проверки посадки этого не видят: манифест
// собирается, образ в нём назван, он просто недостижим. Отказ приходит через
// минуты после «helm upgrade прошёл» и стоит полного подъёма стенда.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПРЕЖНЕЕ ИМЯ НЕ НАХОДИЛОСЬ ПОИСКОМ
//
// Имя образа СТОРОНА СБОРКИ не писала, а ВЫВОДИЛА из имени каталога службы
// (`kacho-$svc:dev`). Поэтому вхождений прежнего имени в дереве было НОЛЬ —
// перепись по строке не находила ничего, и расхождение существовало ровно там,
// где искать было нечем. Для семи служб из восьми вывод случайно совпадал с тем,
// что просит профиль; восьмая (служба личности, получившая собственное имя
// продукта) разошлась молча.
//
// Отсюда форма починки: имя образа ОБЪЯВЛЕНО поимённо (`images.txt`), а не
// выведено. Умолчания у него нет намеренно — умолчание всегда непусто, поэтому
// новая служба читалась бы настроенной и отказывала бы уже в кластере.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТА ПРОБА ЧИТАЕТ И ЧЕГО НЕ ЧИТАЕТ
//
// Только ОБЪЯВЛЕНИЯ — файлы значений и `images.txt`. Ни чартов, ни сети, ни
// кластера, ни `make`: пропуститься она не умеет.
//
// ПРЕДМЕТ — образы, названные СЛОЖЕННЫМ профилем стенда. Образы модулей консоли
// объявлены ВНУТРИ своего подчарта, профиль их не называет, и в этот обход они
// не попадают: их производитель выводится из дерева тем же обходом, что и
// перечень модулей. Граница названа, чтобы «ноль находок» не читалось шире, чем
// осмотрено.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// imageProducers — перечень «служба → имя образа», объявленный в images.txt.
//
// Тот же формат и тот же довод, что у stacks.txt: перечень, выписанный дважды,
// расходится молча, поэтому объявление одно, а читателей несколько (Makefile
// собирает по нему образы, эта проба сверяет с профилями).
func imageProducers(t *testing.T) (byService map[string]string, byComponent map[string]string) {
	t.Helper()
	raw, err := os.ReadFile("images.txt")
	if err != nil {
		t.Fatalf("images.txt не прочитан (%v) — объявления «служба → имя образа» в дереве "+
			"нет, значит имя образа для сборки выводится, а не называется. Именно так "+
			"расхождение и становится невидимым: выведенное имя в дереве не встречается "+
			"ни разу, и перепись по строке находит ноль", err)
	}
	byService, byComponent = map[string]string{}, map[string]string{}
	for i, line := range strings.Split(string(raw), "\n") {
		if j := strings.Index(line, "#"); j >= 0 {
			line = line[:j]
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(strings.TrimSpace(line), ":")
		if len(f) != 3 || f[0] == "" || f[1] == "" || f[2] == "" {
			t.Fatalf("images.txt:%d: ожидались три непустых поля «служба:компонент:образ», получено %q", i+1, line)
		}
		byService[f[0]] = f[2]
		byComponent[f[1]] = f[2]
	}
	return byService, byComponent
}

// standAsked — что просит СЛОЖЕННЫЙ профиль: стенд → компонент → имя образа
// (без тега). Только локальные образы: опубликованные приходят из реестра, и
// производитель им не нужен.
type standAsked map[string]map[string]string

// imageDisagreements — РЕШЕНИЕ гейта, вынесенное чистой функцией.
//
// Вынесено затем, чтобы доказательство падучести подавало сюда настоящий вход, а
// не подделывало дерево. Проба, зовущая гейт целиком, доказала бы лишь, что он
// зелен на сегодняшнем дереве, — то есть ровно то, что и так видно.
func imageDisagreements(asked standAsked, byService, byComponent map[string]string) []string {
	var out []string
	seen := map[string]bool{}

	stands := make([]string, 0, len(asked))
	for s := range asked {
		stands = append(stands, s)
	}
	sort.Strings(stands)

	for _, stand := range stands {
		comps := make([]string, 0, len(asked[stand]))
		for c := range asked[stand] {
			comps = append(comps, c)
		}
		sort.Strings(comps)
		for _, comp := range comps {
			img := asked[stand][comp]
			seen[img] = true
			want, ok := byComponent[comp]
			switch {
			case !ok:
				out = append(out, "стенд "+stand+": компонент "+comp+" просит локальный образ "+img+
					", а производителя у него нет — images.txt о таком компоненте не говорит. Образ, "+
					"которого никто не собирает и не кладёт в узлы, в реестре не существует: под уйдёт "+
					"в ImagePullBackOff уже ПОСЛЕ успешного «helm upgrade», и ни рендер, ни lint этого "+
					"не увидят")
			case want != img:
				out = append(out, "стенд "+stand+": компонент "+comp+" просит образ "+img+
					", а собирается "+want+" — профиль и сборка называют РАЗНОЕ. Имя образа есть "+
					"координата, которую видит оператор, и разойтись эти два объявления могут только "+
					"молча: отказ приходит в кластере")
			}
		}
	}

	svcs := make([]string, 0, len(byService))
	for s := range byService {
		svcs = append(svcs, s)
	}
	sort.Strings(svcs)
	for _, svc := range svcs {
		if !seen[byService[svc]] {
			out = append(out, "images.txt объявляет сборку образа "+byService[svc]+" для службы "+svc+
				", а не просит его ни один стенд — вторая половина расхождения: так выглядит "+
				"переименование, доехавшее до сборки и не доехавшее до профиля")
		}
	}
	return out
}

// collectAskedLocalImages — чтение дерева: сложить каждый стенд и снять с него
// локальные образы. Ничего не решает.
func collectAskedLocalImages(t *testing.T) (asked standAsked, standsSeen, componentsSeen int) {
	t.Helper()
	chains := deployStacks(t)
	base := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))
	asked = standAsked{}

	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, stand := range names {
		layers := []map[string]any{base}
		for _, p := range chains[stand] {
			layers = append(layers, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		folded := foldChainImages(layers)
		componentsSeen += len(folded.withImage)
		asked[stand] = map[string]string{}
		for _, comp := range folded.local {
			ref := ""
			for i := len(layers) - 1; i >= 0 && ref == ""; i-- {
				ref = componentImageRef(layers[i], comp)
			}
			if k := strings.LastIndex(ref, ":"); k > strings.LastIndex(ref, "/") {
				ref = ref[:k]
			}
			asked[stand][comp] = ref
		}
	}
	return asked, len(chains), componentsSeen
}

// TestEveryStandLocalImageHasAProducer — сам гейт, в ОБЕ стороны.
//
// Спрошенный образ без производителя — ImagePullBackOff в кластере. Объявленный
// производитель, которого не спрашивает ни один стенд, — вторая половина того же
// расхождения: так выглядит переименование, доехавшее до сборки и не доехавшее
// до профиля. Односторонняя проба поймала бы ровно один из двух исходов
// переименования, и какой именно — решал бы случай.
func TestEveryStandLocalImageHasAProducer(t *testing.T) {
	byService, byComponent := imageProducers(t)
	asked, standsSeen, componentsSeen := collectAskedLocalImages(t)

	for _, f := range imageDisagreements(asked, byService, byComponent) {
		t.Error(f)
	}

	// Проверка СВОЕЙ предпосылки. Ослепнуть эта проба может тремя способами:
	// перестать видеть стенды, перестать видеть образы и перестать узнавать
	// локальный образ. Во всех трёх молчание неотличимо от полного покрытия.
	localAsked := map[string]bool{}
	perStand := make([]string, 0, len(asked))
	standNames := make([]string, 0, len(asked))
	for s := range asked {
		standNames = append(standNames, s)
	}
	sort.Strings(standNames)
	for _, s := range standNames {
		for _, img := range asked[s] {
			localAsked[img] = true
		}
		perStand = append(perStand, fmt.Sprintf("%s=%d", s, len(asked[s])))
	}

	if standsSeen == 0 || componentsSeen == 0 {
		t.Fatalf("обход ничего не прочитал: стендов=%d, компонентов с образом=%d — предикат "+
			"перестал узнавать дерево, а не дерево стало чистым", standsSeen, componentsSeen)
	}
	if len(localAsked) == 0 {
		t.Fatalf("ни один стенд не попросил локального образа (стендов %d, компонентов с "+
			"образом %d) — предикат локального образа разошёлся с деревом, и тогда «у всех "+
			"спрошенных есть производитель» означает «спрошенных нет»", standsSeen, componentsSeen)
	}
	if len(byService) == 0 {
		t.Fatalf("images.txt не объявил ни одного производителя — сверять не с чем")
	}

	t.Logf("осмотрено: стендов %d, компонентов с образом %d; локальных образов спрошено %d "+
		"(по стендам: %s); производителей объявлено %d",
		standsSeen, componentsSeen, len(localAsked), strings.Join(perStand, " "), len(byService))
}

// serviceMakefileImage — что объявляет собственный Makefile службы (цель `docker`
// для разработчика). ТРЕТЬЕ объявление того же имени: профиль стенда просит,
// deploy/Makefile собирает, Makefile службы собирает то же самое руками.
//
// Путь выводится ТАК ЖЕ, как его выводит сборка, и отсутствие файла — НАХОДКА, а
// не пропуск: иначе разошедшийся вывод пути дал бы молчание вместо проверки.
func serviceMakefilePath(svc string) string {
	if svc == "api-gateway" {
		return filepath.Join("..", "gateway", "Makefile")
	}
	return filepath.Join("..", "services", svc, "Makefile")
}

// makefileImageDisagreements — решение третьей оси, чистой функцией (тот же довод,
// что и выше: доказательство падучести подаёт сюда вход, а не подделывает дерево).
func makefileImageDisagreements(declared map[string]string, byService map[string]string) []string {
	var out []string
	svcs := make([]string, 0, len(byService))
	for s := range byService {
		svcs = append(svcs, s)
	}
	sort.Strings(svcs)
	for _, svc := range svcs {
		got, ok := declared[svc]
		if !ok {
			out = append(out, "служба "+svc+": собственный Makefile не объявляет IMAGE — "+
				"проверить нечего, а объявление у имени образа обязано быть одно на все три "+
				"стороны (профиль стенда · сборка стенда · сборка службы)")
			continue
		}
		if want := byService[svc] + ":dev"; got != want {
			out = append(out, "служба "+svc+": собственный Makefile собирает "+got+
				", а стенд — "+want+". Разработчик, собравший службу её же целью, получит образ "+
				"под ДРУГИМ именем: локальная перекатка молча не доедет до пода")
		}
	}
	return out
}

// TestServiceMakefileImageAgreesWithTheStand — третья сторона того же имени.
func TestServiceMakefileImageAgreesWithTheStand(t *testing.T) {
	byService, _ := imageProducers(t)
	declared := map[string]string{}
	read := 0
	for svc := range byService {
		raw, err := os.ReadFile(serviceMakefilePath(svc))
		if err != nil {
			t.Errorf("служба %s: Makefile не прочитан по выведенному пути %s (%v) — вывод пути "+
				"разошёлся с деревом, и тогда «расхождений нет» означает «не читали»",
				svc, serviceMakefilePath(svc), err)
			continue
		}
		read++
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(line, "IMAGE") {
				continue
			}
			f := strings.SplitN(line, ":=", 2)
			if len(f) != 2 || strings.TrimSpace(f[0]) != "IMAGE" {
				continue
			}
			declared[svc] = strings.TrimSpace(f[1])
			break
		}
	}
	for _, f := range makefileImageDisagreements(declared, byService) {
		t.Error(f)
	}
	if read == 0 || len(declared) == 0 {
		t.Fatalf("обход ничего не прочитал: Makefile'ов прочитано %d, объявлений IMAGE найдено %d — "+
			"предикат перестал узнавать дерево", read, len(declared))
	}
	t.Logf("осмотрено: служб %d, Makefile'ов прочитано %d, объявлений IMAGE найдено %d",
		len(byService), read, len(declared))
}
