// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_image_has_a_producer_test.go — образ, который просит стенд kind,
// кто-то СОБИРАЕТ и кладёт в узлы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ — ДРУГОЙ, ЧЕМ У СОСЕДНЕЙ ПРОВЕРКИ, И РАЗЛИЧИЕ НЕСУЩЕЕ
//
// Рядом живёт image_name_agrees_with_the_source_of_names_test.go. Он отвечает на
// вопрос «КАКОЕ У ОБРАЗА ИМЯ»: всякая ссылка в профиле обязана быть каноническим
// именем части продукта. Здесь вопрос другой — «ЕСТЬ ЛИ У ОБРАЗА ТОТ, КТО ЕГО
// СОБИРАЕТ». Соседняя проверка на него не отвечает ничем: имя может быть
// каноническим у части, которую рецепт стенда не строит вовсе.
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
// ОТКУДА БЕРЁТСЯ КАЖДАЯ ИЗ ДВУХ СТОРОН — И ПОЧЕМУ ВЕДОМОСТИ ИМЁН ЗДЕСЬ НЕТ
//
// СПРОШЕННОЕ — из сложенных профилей стендов (`stacks.txt` + значения умбреллы).
//
// ПРОИЗВЕДЁННОЕ — из ДВУХ разных объявлений, и ни одно из них не является вторым
// словарём имён:
//
//	ЧТО строит рецепт   deploy/Makefile, `SERVICES` — перечень КАТАЛОГОВ служб.
//	                    Это собственное объявление производителя: ровно эти
//	                    каталоги обходит `build-services`, собирая и загружая
//	                    образ каждого. Каталог — не имя образа;
//	КАК это назовётся   productnaming.ChartName — ЕДИНСТВЕННЫЙ источник имён
//	                    (задача #2076), тот же, у которого спрашивает сам рецепт
//	                    через deploy/scripts/lib/product-names.sh.
//
// Здесь стояла ведомость, выписывавшая имя образа каждой службы отдельным полем
// (её имя не пишется координатой: файла в дереве нет, а путь в обратных кавычках
// читается проверкой свежести как живое утверждение). Она чинила ТОТ ЖЕ дефект,
// что и источник имён, но
// вторым объявлением — и разошлась бы с первым МОЛЧА: никакой предикат их не
// связывал, а имя, разошедшееся с каноном, эта проба принимала бы за истину и
// требовала бы его от профилей. Предмет ведомости — «есть ли производитель» —
// сохранён целиком; выписанные имена из неё убраны, потому что имя объявлено в
// другом месте и уже держится своим гейтом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ СВЕРКА ИДЁТ ПО ИМЕНАМ ОБРАЗОВ, А НЕ ПО КЛЮЧАМ КОМПОНЕНТОВ
//
// Предмет — ОБРАЗ: существует ли он в узлах кластера. Ключ компонента умбреллы
// (`vpc`, `kacho-nlb`, `kaname`) — координата для текста находки, а не предмет.
// Сверять по нему значило бы завести мост «компонент → служба», а он в этом
// дереве не выводится ни из имени, ни из приставки — то есть был бы ТРЕТЬИМ
// словарём об одном предмете. Соседняя проверка отказалась его заводить по той
// же причине, и здесь он не нужен: разойтись «спрошенное» и «произведённое»
// могут только как множества имён, и обе половины расхождения называются
// отдельно, каждая со своей координатой.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТА ПРОБА ЧИТАЕТ И ЧЕГО НЕ ЧИТАЕТ
//
// Только ОБЪЯВЛЕНИЯ — файлы значений, `SERVICES` рецепта и `IMAGE` собственных
// Makefile служб. Ни чартов, ни сети, ни кластера, ни `make`: пропуститься она
// не умеет.
//
// ПРЕДМЕТ — образы, названные СЛОЖЕННЫМ профилем стенда. Образы модулей консоли
// объявлены ВНУТРИ своего подчарта, профиль умбреллы их не называет, и в этот
// обход они не попадают: их производитель выводится из дерева тем же обходом,
// что и перечень модулей. Граница названа, чтобы «ноль находок» не читалось
// шире, чем осмотрено.
//
// Существование каталога службы здесь НЕ проверяется отдельно, и проверять его
// незачем: служба, названная в `SERVICES` по опечатке, даёт образ, которого не
// просит ни один стенд, — и это ловит вторая сторона сверки ниже.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// standRecipeServicesRe — объявление перечня служб в рецепте стенда.
//
// Привязка к началу строки несущая: слово `SERVICES` встречается в этом файле и
// в объяснениях, а комментарий рецепта начинается с `#`. Гейт, читающий текст, а
// не объявление, краснел бы на собственном разборе (`testing.md` §«Гейт на
// класс», п. 4).
var standRecipeServicesRe = regexp.MustCompile(`(?m)^SERVICES[ \t]*:=[ \t]*(.*)$`)

// standRecipeServices — каталоги служб, которые СОБИРАЕТ рецепт стенда.
//
// Читается собственное объявление производителя, а не дерево: служба, лежащая в
// дереве и не названная здесь, рецептом НЕ собирается — и ровно это расхождение
// проба и обязана видеть. Вывод перечня из `services/*` дал бы «производитель
// есть» там, где его нет.
func standRecipeServices(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("deploy/Makefile не прочитан (%v) — производителя образов стенда назвать нечем, "+
			"и тогда «у каждого спрошенного образа есть производитель» означает «не читали»", err)
	}
	m := standRecipeServicesRe.FindAllStringSubmatch(string(raw), -1)
	switch len(m) {
	case 1: // ожидаемое
	case 0:
		t.Fatalf("deploy/Makefile не объявляет SERVICES — предпосылка проверки исчезла: " +
			"перечень собираемых служб переехал, и вердикт беспредметен, а не зелен")
	default:
		t.Fatalf("deploy/Makefile объявляет SERVICES %d раза — какое объявление действует, "+
			"решает порядок, и сверять надо не с догадкой о нём", len(m))
	}
	svcs := strings.Fields(m[0][1])
	if len(svcs) == 0 {
		t.Fatalf("SERVICES объявлен пустым — рецепт не собирает ни одного образа, " +
			"и «спрошенное покрыто произведённым» стало бы утверждением ни о чём")
	}
	seen := map[string]bool{}
	for _, s := range svcs {
		if seen[s] {
			t.Fatalf("SERVICES называет службу %q дважды — перечень производителя перестал быть перечнем", s)
		}
		seen[s] = true
	}
	sort.Strings(svcs)
	return svcs
}

// recipeProducedImages — образ → служба, его производящая.
//
// Имя спрашивается у источника имён, а не выводится приставкой: у части со своим
// именем продукта приставка его не связывает (`iam` → `kaname`).
func recipeProducedImages(t *testing.T, svcs []string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(svcs))
	for _, svc := range svcs {
		img := productnaming.ChartName(svc)
		if prev, dup := out[img]; dup {
			t.Fatalf("службы %q и %q дают одно имя образа %q — рецепт соберёт одну поверх другой, "+
				"и в узлах окажется та, что собралась последней", prev, svc, img)
		}
		out[img] = svc
	}
	return out
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
//
// Стороны ДВЕ, и односторонняя проба поймала бы ровно один исход переименования,
// а какой именно — решал бы случай.
func imageDisagreements(asked standAsked, produced map[string]string) []string {
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
			if _, ok := produced[img]; ok {
				continue
			}
			// Диагностика зовёт ИСТОЧНИК ИМЁН, а не второй словарь: если имя
			// узнано как отставное имя части продукта, читателю называется
			// канон — то есть ровно то, что чинит находку. Если не узнано вовсе
			// — это говорится прямо, потому что такое имя выпадает и из-под всех
			// прочих проверок, ключующихся на именах продукта.
			hint := "источник имён такого имени не узнаёт вовсе"
			if dir, ok := productnaming.ServiceDir(img); ok {
				if canon := productnaming.ChartName(dir); canon != img {
					hint = fmt.Sprintf("это ОТСТАВНОЕ имя части %q, чьё имя продукта %q", dir, canon)
				} else {
					hint = fmt.Sprintf("это каноническое имя части %q, но рецепт стенда её не строит", dir)
				}
			}
			out = append(out, "стенд "+stand+": компонент "+comp+" просит локальный образ "+img+
				", а производителя у него нет — рецепт стенда (deploy/Makefile, SERVICES) такого "+
				"образа не собирает и в узлы не кладёт ("+hint+"). Образ, которого никто не собирает, "+
				"в реестре не существует: под уйдёт в ImagePullBackOff уже ПОСЛЕ успешного "+
				"«helm upgrade», и ни рендер, ни lint этого не увидят")
		}
	}

	imgs := make([]string, 0, len(produced))
	for img := range produced {
		imgs = append(imgs, img)
	}
	sort.Strings(imgs)
	for _, img := range imgs {
		if !seen[img] {
			out = append(out, "рецепт стенда собирает образ "+img+" для службы "+produced[img]+
				", а не просит его ни один стенд — вторая половина расхождения: так выглядит "+
				"переименование, доехавшее до сборки и не доехавшее до профиля, и так же выглядит "+
				"служба, названная в SERVICES по опечатке")
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
func TestEveryStandLocalImageHasAProducer(t *testing.T) {
	svcs := standRecipeServices(t)
	produced := recipeProducedImages(t, svcs)
	asked, standsSeen, componentsSeen := collectAskedLocalImages(t)

	for _, f := range imageDisagreements(asked, produced) {
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

	t.Logf("осмотрено: стендов %d, компонентов с образом %d; локальных образов спрошено %d "+
		"(по стендам: %s); рецепт собирает служб %d, имён образов %d — имена спрошены у "+
		"internal/productnaming, второй ведомости имён нет",
		standsSeen, componentsSeen, len(localAsked), strings.Join(perStand, " "),
		len(svcs), len(produced))
}

// ─────────────────────────────────────────────────────────────────────────────
// ТРЕТЬЯ СТОРОНА ТОГО ЖЕ ИМЕНИ: собственный Makefile службы.
//
// Профиль стенда ПРОСИТ, рецепт стенда СОБИРАЕТ, а `IMAGE` в Makefile службы
// собирает то же самое руками — целью для разработчика. Это ЛИТЕРАЛ, и на
// сегодняшнем дереве их восемь: единственные выписанные имена образов, которые
// в дереве остались.
//
// Перечень Makefile'ов ВЫВОДИТСЯ обходом дерева, а не собирается по каталогу
// службы: вывод каталога — правило рецепта (`api-gateway` → `gateway`), и второе
// его выражение здесь стало бы третьим местом об одном предмете. Обход же и
// правило вместе дают то, что нужно: множество объявленных имён сверяется с
// множеством производимых, в обе стороны.

// serviceMakefileImageRe — объявление имени образа в собственном Makefile службы.
var serviceMakefileImageRe = regexp.MustCompile(`(?m)^IMAGE[ \t]*:=[ \t]*(\S+)[ \t]*$`)

// serviceMakefiles — Makefile'ы частей продукта, ВЫВЕДЕННЫЕ обходом.
func serviceMakefiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, pattern := range []string{
		filepath.Join("..", "services", "*", "Makefile"),
		filepath.Join("..", "gateway", "Makefile"),
	} {
		m, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("обход %s: %v", pattern, err)
		}
		out = append(out, m...)
	}
	sort.Strings(out)
	return out
}

// makefileImageDisagreements — решение третьей оси, чистой функцией (тот же
// довод, что и выше: доказательство падучести подаёт сюда вход, а не подделывает
// дерево).
//
// `declared` — файл → значение `IMAGE` (пустое значение означает «объявления
// нет»); `produced` — образ → служба, от рецепта стенда.
func makefileImageDisagreements(declared map[string]string, produced map[string]string) []string {
	var out []string

	want := map[string]string{} // «имя:dev» → служба
	for img, svc := range produced {
		want[img+":dev"] = svc
	}

	files := make([]string, 0, len(declared))
	for f := range declared {
		files = append(files, f)
	}
	sort.Strings(files)

	covered := map[string]bool{}
	for _, f := range files {
		got := declared[f]
		if got == "" {
			out = append(out, f+" не объявляет IMAGE — проверить нечего, а объявление у имени "+
				"образа обязано быть одно на все три стороны (профиль стенда · рецепт стенда · "+
				"сборка службы)")
			continue
		}
		if svc, ok := want[got]; ok {
			covered[got] = true
			_ = svc
			continue
		}
		out = append(out, f+" собирает "+got+", а рецепт стенда образа с таким именем и тегом "+
			"не собирает. Разработчик, собравший службу её же целью, получит образ под ДРУГИМ "+
			"именем: локальная перекатка молча не доедет до пода")
	}

	tags := make([]string, 0, len(want))
	for tag := range want {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	for _, tag := range tags {
		if !covered[tag] {
			out = append(out, "рецепт стенда собирает "+tag+" (служба "+want[tag]+"), а собственного "+
				"Makefile, собирающего то же имя, в дереве нет — вторая половина того же "+
				"расхождения: цель разработчика и цель стенда разошлись")
		}
	}
	return out
}

// TestServiceMakefileImageAgreesWithTheStand — третья сторона того же имени.
func TestServiceMakefileImageAgreesWithTheStand(t *testing.T) {
	produced := recipeProducedImages(t, standRecipeServices(t))

	files := serviceMakefiles(t)
	declared := map[string]string{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s не прочитан (%v) — обход дерева разошёлся с деревом, и тогда "+
				"«расхождений нет» означает «не читали»", f, err)
		}
		if m := serviceMakefileImageRe.FindStringSubmatch(string(raw)); m != nil {
			declared[f] = m[1]
		} else {
			declared[f] = ""
		}
	}

	for _, f := range makefileImageDisagreements(declared, produced) {
		t.Error(f)
	}

	withImage := 0
	for _, v := range declared {
		if v != "" {
			withImage++
		}
	}
	if len(files) == 0 || withImage == 0 {
		t.Fatalf("обход ничего не прочитал: Makefile'ов найдено %d, объявлений IMAGE %d — "+
			"предикат перестал узнавать дерево", len(files), withImage)
	}
	t.Logf("осмотрено: Makefile'ов частей продукта %d, объявлений IMAGE %d; "+
		"рецепт стенда производит имён %d", len(files), withImage, len(produced))
}
