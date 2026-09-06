// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// image_name_agrees_with_the_source_of_names_test.go — ИМЯ ОБРАЗА, КОТОРОЕ
// ПРОСИТ ПРОФИЛЬ, СВЕРЯЕТСЯ С ЕДИНСТВЕННЫМ ИСТОЧНИКОМ ИМЁН.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Инвариант, ради которого заводилась работа с именами частей продукта, звучит
// так: СТЕНД СОБИРАЕТ ТО, ЧТО ПРОСИТ ЧАРТ.
//
// Сторона РЕЦЕПТА закрыта: ссылка на образ в оснастке стенда обязана быть
// величиной, полученной у объявленного источника имён. Сторона ЧАРТА была
// свободна: имя, которое просит профиль, не сверялось ни с чем. Сходились они
// совпадением, а не построением.
//
// Замер, воспроизводимый одной правкой: подменить в профиле стенда имя
// запрашиваемого образа на выведенное приставкой платформы —
//
//	deploy/helm/umbrella/values.dev.yaml   kaname.image.repository: kaname → kacho-iam
//
// — и прогнать проверки дерева. `go test ./deploy/` и
// `go test ./internal/repohygiene/` отвечают `ok`. Расхождение увидел бы только
// живой кластер: отказом загрузки образа, которого никто не собирал.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПРИСТАВКА ЕЁ НЕ ЛОВИТ, А ИСТОЧНИК ИМЁН ЛОВИТ
//
// `kacho-iam` — имя ЗАКОННОЕ по форме: источник имён узнаёт его как имя части
// `iam`, потому что приставка платформы связывает те части, у которых своего
// имени продукта нет. Но у ЭТОЙ части своё имя есть — `kaname` (решение
// владельца, задача #2076), — и `productnaming.ChartName("iam")` возвращает
// именно его.
//
// Отсюда предикат, который не требует ни одного нового словаря:
//
//	ChartName(ServiceDir(последний сегмент)) == последний сегмент
//
// Круг замыкается на каноническом имени и НЕ замыкается на отставном: `ServiceDir`
// узнаёт обе формы (в том и её работа — распознать нашу часть), `ChartName`
// возвращает одну. Всякое имя, узнанное как часть продукта, но не равное её
// каноническому имени, называет образ, которого никто не собирает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ДВЕ ОСИ, И НИ ОДНА НЕ ПОГЛОЩАЕТ ДРУГУЮ
//
//	ось А — КАНОНИЧНОСТЬ. Тотальна: судит КАЖДУЮ ссылку на образ во всех
//	        профилях, чей последний сегмент источник имён узнаёт. Ловит
//	        отставное имя где угодно.
//	ось Б — СОГЛАСИЕ С МЕСТОМ. Судит ссылку, стоящую на рабочем месте образа
//	        секции (`<секция>.image[.repository]` в профиле умбреллы; корневой
//	        `image[.repository]` в значениях чарта). Ловит КАНОНИЧЕСКОЕ имя
//	        ЧУЖОЙ части — то, что ось А пропускает by construction, потому что
//	        круг у такого имени замкнут.
//
// Ось Б ловит и третий случай: на рабочем месте образа стоит имя, которого
// источник имён не узнаёт ВОВСЕ. Ось А такое молча пропускает — и это не
// придирка, а тот самый способ ослепнуть, о котором предупреждает шапка самого
// источника имён: распознаватель, ключующийся на приставке, чужое имя не
// отвергает, он его НЕ ВИДИТ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЕ, А НЕ РЕНДЕР
//
// Рендер требует `helm` и скачанных зависимостей, то есть УМЕЕТ ПРОПУСТИТЬСЯ, а
// пропустившаяся проверка неотличима от прошедшей. Проба читает файлы значений
// и потому пропуститься не умеет. Прецедент в дереве — проба формы токена у
// края (gateway/deploy/token_shape_test.go).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ ДЕЛАЕТ — сказано прямо, чтобы «зелено» не читалось шире
//
//   - она НЕ судит ТЕГ и НЕ судит СВЕЖЕСТЬ пина: у этого предмета свой владелец
//     (managed_cluster_profile_test.go). Здесь судится только ИМЯ части;
//   - она НЕ судит имя развёртывания и имя базы. Это ТРИ РАЗНЫЕ ОСИ, и
//     унификация по самой широкой семантике дала бы объявление, отвечающее на
//     вопросы, которых ему не задают. У имени базы свой гейт —
//     iam_database_named_for_its_product_test.go;
//   - осью Б НЕ судятся секции, чей ключ источник имён не узнаёт (`vpc`,
//     `compute`, `api-gateway`, `storage`, `registry`, `uif`): имя их ЧАРТА и
//     имя их ОБРАЗА в этом дереве разные слова, и мост между ними — второй
//     словарь, заводить который здесь запрещено. Их ссылки судит ось А, тотально.
//     Число нерассуженных секций печатается ПЕРЕПИСЬЮ, а не подразумевается;
//   - она НЕ судит образы, источником имён не узнанные, вне рабочего места
//     (`kaname.opaSidecar.image`, `pg-*.image`, `kratos`, `hydra`): это чужие
//     образы, и предмета у проверки на них нет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ДОКАЗАНА СПОСОБНОСТЬ УПАСТЬ
//
// Разбор вынесен в чистую функцию auditProfileImageNames, принимающую
// ОБЪЯВЛЕНИЯ, а не файлы: настоящее дерево и синтетический вход инъекции
// проходят одну и ту же функцию. Инъекция —
// image_name_agrees_with_the_source_of_names_injection_test.go, по одной оси на
// каждую форму отказа плюс ЗАКОННЫЙ БЛИЗНЕЦ на каждую.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// imageNameDecl — ОДНА ссылка на образ, как её объявляет профиль.
//
// Поля называют ровно то, что нужно обеим осям, и ничего сверх: координату для
// текста находки, секцию (пусто — место не рабочее) и саму ссылку.
type imageNameDecl struct {
	where   string // координата: файл (или стек) и путь узла в дереве значений
	section string // ключ секции, чей РАБОЧИЙ образ здесь стоит; "" — место не рабочее
	ref     string // ссылка на образ, как она объявлена
	// effective — объявление ДЕЙСТВУЮЩЕЕ, то есть полученное наложением цепочки
	// профилей стека поверх умолчаний подчарта.
	//
	// Различие несущее. Отдельный ФАЙЛ вправе не называть образа: профиль,
	// накладывающий один тег, репозиторий наследует у слоя под собой, и
	// требовать имя от каждого файла значило бы краснеть на верном дереве —
	// проверено, на этом дереве таких файлов четыре. Отсутствие имени у
	// ДЕЙСТВУЮЩЕГО значения — другое дело: стенду нечего загружать.
	effective bool
}

// imageNameCensus — объём осмотренного. Печатается ВСЕГДА, включая зелёный
// прогон: «ноль находок» обязано быть отличимо от «ноль прочитанного».
type imageNameCensus struct {
	profiles      int // файлов значений прочитано
	stacks        int // стеков таблицы стендов рассмотрено
	refs          int // ссылок на образ найдено
	productRefs   int // из них узнано источником имён как часть продукта
	workloadPos   int // рабочих мест образа найдено
	sectionParts  int // из них часть определена источником имён
	canonicalHits int // сверок, давших согласие
}

// imageRepoLastSegment — последний сегмент репозитория ссылки на образ.
//
// Тег и пин по содержимому отбрасываются: они предмет соседней проверки. Второй
// возврат — «ссылкой на образ является»: пустая строка означает «профиль образ
// не объявил», и это ОТЛИЧАЕТСЯ от «объявил неверно».
func imageRepoLastSegment(ref string) (string, bool) {
	s := strings.TrimSpace(ref)
	if s == "" {
		return "", false
	}
	// Пин по содержимому: `repo@sha256:…`.
	if at := strings.IndexByte(s, '@'); at >= 0 {
		s = s[:at]
	}
	// Тег: двоеточие ПОСЛЕ последнего слэша (иначе съедим порт реестра).
	if colon := strings.LastIndexByte(s, ':'); colon > strings.LastIndexByte(s, '/') {
		s = s[:colon]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	return s[strings.LastIndex(s, "/")+1:], true
}

// auditProfileImageNames судит ОБЪЯВЛЕНИЯ и возвращает находки с переписью.
//
// Функция чистая: тем же входом её кормит инъекция, поэтому её вердикт о
// синтетике есть вердикт о механизме.
func auditProfileImageNames(decls []imageNameDecl) ([]string, imageNameCensus) {
	var (
		findings []string
		census   imageNameCensus
	)

	for _, d := range decls {
		census.refs++

		// Часть, которую настраивает МЕСТО. Спрашивается у источника имён, а не
		// выводится приставкой: ключ секции — это имя чарта, и для части со
		// своим именем продукта приставка его не связывает.
		sectionDir, sectionKnown := "", false
		if d.section != "" {
			census.workloadPos++
			sectionDir, sectionKnown = productnaming.ServiceDir(d.section)
			if sectionKnown {
				census.sectionParts++
			}
		}

		seg, isRef := imageRepoLastSegment(d.ref)
		if !isRef {
			if sectionKnown && d.effective {
				findings = append(findings, fmt.Sprintf(
					"%s: рабочее место образа части %q не называет образа вовсе — "+
						"стенд не узнает, что собирать; канон источника имён — %q",
					d.where, sectionDir, productnaming.ChartName(sectionDir)))
			}
			continue
		}

		valueDir, valueKnown := productnaming.ServiceDir(seg)
		if !valueKnown {
			// Чужой образ. На рабочем месте части продукта это находка: имя,
			// которого источник имён не узнаёт, выпадает из-под КАЖДОЙ проверки,
			// ключующейся на именах продукта, — молча.
			if sectionKnown {
				findings = append(findings, fmt.Sprintf(
					"%s: рабочее место образа части %q просит %q — источник имён такого "+
						"имени не узнаёт вовсе, значит образ выпадает из-под всех проверок, "+
						"ключующихся на именах продукта; канон — %q",
					d.where, sectionDir, seg, productnaming.ChartName(sectionDir)))
			}
			continue
		}
		census.productRefs++

		// ─── ось А: каноничность. Круг «имя → часть → имя» обязан замкнуться.
		want := productnaming.ChartName(valueDir)
		if seg != want {
			findings = append(findings, fmt.Sprintf(
				"%s: профиль просит образ %q — это ОТСТАВНОЕ имя части %q, чьё имя "+
					"продукта %q. Образа с таким именем не собирает никто, и отказ придёт "+
					"загрузкой образа уже в кластере",
				d.where, seg, valueDir, want))
		} else {
			census.canonicalHits++
		}

		// ─── ось Б: согласие с местом. Каноническое имя ЧУЖОЙ части круг
		// замыкает, поэтому оси А оно невидимо by construction.
		if sectionKnown && valueDir != sectionDir {
			findings = append(findings, fmt.Sprintf(
				"%s: рабочее место образа части %q занято образом части %q (%q) — "+
					"секция получит чужой процесс; канон этой секции — %q",
				d.where, sectionDir, valueDir, seg, productnaming.ChartName(sectionDir)))
		}
	}

	sort.Strings(findings)
	return findings, census
}

// ─────────────────────────────────────────────────────────────────────────────
// Сбор объявлений из дерева. Перечень файлов ВЫВОДИТСЯ обходом, а не
// выписывается: выписанный разошёлся бы с деревом молча, и разошёлся бы именно
// там, где завели новый профиль.

// collectImageNameDecls обходит дерево значений и собирает ссылки на образы
// вместе с координатой узла.
//
// Рабочим местом образа секции считается ровно `image` и `image.repository` на
// объявленной глубине — не глубже. Глубже лежат чужие образы (боковой
// контейнер, база, поставщик личности), и требовать от них имени части продукта
// значило бы краснеть на верном дереве.
func collectImageNameDecls(node any, path []string, file, section string, workloadDepth int, out *[]imageNameDecl) {
	switch v := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := append(append([]string{}, path...), k)
			if k == "image" {
				sec := ""
				if len(path) == workloadDepth {
					sec = section
					if workloadDepth == 1 {
						sec = path[0] // профиль умбреллы: секция — ключ верхнего уровня
					}
				}
				switch img := v[k].(type) {
				case string:
					*out = append(*out, imageNameDecl{
						where:   file + ":" + strings.Join(child, "."),
						section: sec,
						ref:     img,
					})
					continue
				case map[string]any:
					repo, _ := img["repository"].(string)
					*out = append(*out, imageNameDecl{
						where:   file + ":" + strings.Join(append(child, "repository"), "."),
						section: sec,
						ref:     repo,
					})
					// Внутри карты образа могут лежать и другие узлы; своих
					// ссылок на образ они не несут, обходить их незачем.
					continue
				}
			}
			collectImageNameDecls(v[k], child, file, section, workloadDepth, out)
		}
	case []any:
		for i, item := range v {
			collectImageNameDecls(item, append(append([]string{}, path...), fmt.Sprintf("[%d]", i)), file, section, workloadDepth, out)
		}
	}
}

// imageNameProfile — файл значений и то, чем в нём определяется секция.
type imageNameProfile struct {
	path          string
	section       string // имя чарта: для файла значений ЧАРТА; пусто для профиля умбреллы
	workloadDepth int    // глубина рабочего места: 1 у профиля умбреллы, 0 у значений чарта
}

// imageNameProfiles — все профили развёртывания дерева.
//
// Три вида, и у каждого своя связь «место → часть»:
//
//	профиль умбреллы     helm/umbrella/values*.yaml         секция — ключ верхнего уровня
//	значения подчарта    helm/umbrella/charts/*/values.yaml секция — имя чарта из Chart.yaml
//	значения чарта части */deploy/values*.yaml              секция — имя чарта из Chart.yaml
//
// Имя чарта берётся из его Chart.yaml, а не из имени каталога: каталог и имя
// чарта в этом дереве расходятся (`services/iam/deploy` ↔ `kaname`), и вывод по
// каталогу был бы вторым словарём.
func imageNameProfiles(t *testing.T) []imageNameProfile {
	t.Helper()

	var out []imageNameProfile

	umbrella, err := filepath.Glob(filepath.Join(umbrellaDir, "values*.yaml"))
	if err != nil {
		t.Fatalf("обход профилей умбреллы: %v", err)
	}
	sort.Strings(umbrella)
	for _, p := range umbrella {
		out = append(out, imageNameProfile{path: p, workloadDepth: 1})
	}

	chartValues := []string{}
	sub, err := filepath.Glob(filepath.Join(umbrellaDir, "charts", "*", "values.yaml"))
	if err != nil {
		t.Fatalf("обход значений подчартов: %v", err)
	}
	chartValues = append(chartValues, sub...)
	for _, pattern := range []string{
		filepath.Join("..", "services", "*", "deploy", "values*.yaml"),
		filepath.Join("..", "services", "*", "docs", "deploy", "values*.yaml"),
		filepath.Join("..", "gateway", "deploy", "values*.yaml"),
		filepath.Join("..", "gateway", "docs", "deploy", "values*.yaml"),
		filepath.Join("..", "ui-future", "deploy", "values*.yaml"),
	} {
		m, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("обход %s: %v", pattern, err)
		}
		chartValues = append(chartValues, m...)
	}
	sort.Strings(chartValues)
	for _, p := range chartValues {
		chartFile := filepath.Join(filepath.Dir(p), "Chart.yaml")
		if _, err := os.Stat(chartFile); err != nil {
			t.Fatalf("рядом с %s нет Chart.yaml (%v) — секцию определить нечем, "+
				"а молча пропустить профиль значило бы завести слепую зону", p, err)
		}
		name, _ := readYAML(t, chartFile)["name"].(string)
		if strings.TrimSpace(name) == "" {
			t.Fatalf("%s не объявляет имени чарта — предпосылка проверки исчезла", chartFile)
		}
		out = append(out, imageNameProfile{path: p, section: name, workloadDepth: 0})
	}
	return out
}

// readImageNameStackDecls читает ДЕЙСТВУЮЩИЕ объявления по каждому стеку
// таблицы стендов.
//
// Единица счёта здесь — стек, а не файл: судится то, что стенд получает НА
// САМОМ ДЕЛЕ — умолчания подчартов, поверх них `values.yaml` умбреллы, поверх
// них цепочка профилей слева направо, ровно так их сливает helm. Проверка,
// читающая только профили, объявила бы «имя не названо» там, где профиль
// накладывает один тег, а репозиторий приходит слоем ниже.
//
// Состав подчартов ВЫВОДИТСЯ (subchartDirs): зависимости `file://` из Chart.yaml
// умбреллы плюс каталоги, физически лежащие в charts/. Выписанный перечень
// разошёлся бы с деревом молча.
func readImageNameStackDecls(t *testing.T) ([]imageNameDecl, int) {
	t.Helper()

	dirs := subchartDirs(t)
	defaults := map[string]map[string]any{}
	for key, dir := range dirs {
		vals := filepath.Join(dir, "values.yaml")
		if _, err := os.Stat(vals); err != nil {
			continue // подчарт без своих значений — накладывать нечего
		}
		defaults[key] = readYAML(t, vals)
	}
	if len(defaults) == 0 {
		t.Fatalf("ни у одного подчарта не прочитано значений — предпосылка проверки исчезла, " +
			"а не дерево стало чистым")
	}

	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)

	var decls []imageNameDecl
	for _, name := range names {
		chain := stacks[name]
		tree := effectiveValues(t, chain)
		for key, def := range defaults {
			merged := mergeValues(map[string]any{}, def)
			if cur, ok := tree[key].(map[string]any); ok {
				merged = mergeValues(merged, cur)
			}
			tree[key] = merged
		}

		var found []imageNameDecl
		collectImageNameDecls(tree, nil, fmt.Sprintf("стек %q (%s)", name, strings.Join(chain, ",")), "", 1, &found)
		for i := range found {
			found[i].effective = true
		}
		decls = append(decls, found...)
	}
	return decls, len(names)
}

// readImageNameDecls читает объявления из всех профилей дерева.
func readImageNameDecls(t *testing.T) ([]imageNameDecl, int) {
	t.Helper()

	var decls []imageNameDecl
	profiles := imageNameProfiles(t)
	for _, p := range profiles {
		collectImageNameDecls(readYAML(t, p.path), nil, p.path, p.section, p.workloadDepth, &decls)
	}
	return decls, len(profiles)
}

// TestProfileImageNameAgreesWithTheSourceOfNames — гейт класса.
func TestProfileImageNameAgreesWithTheSourceOfNames(t *testing.T) {
	decls, profiles := readImageNameDecls(t)
	stackDecls, stacks := readImageNameStackDecls(t)
	findings, census := auditProfileImageNames(append(decls, stackDecls...))
	census.profiles = profiles
	census.stacks = stacks

	t.Logf("перепись: профилей прочитано %d · стеков рассмотрено %d · "+
		"ссылок на образ найдено %d · узнано частями продукта %d · рабочих мест образа %d · "+
		"из них часть определена %d · сверок с каноном сошлось %d · находок %d",
		census.profiles, census.stacks, census.refs, census.productRefs, census.workloadPos,
		census.sectionParts, census.canonicalHits, len(findings))

	// Пустой обход — не «дерево чисто», а «читать было нечего». Три оси сразу:
	// профиль без ссылок, ссылки без узнанных частей и рабочее место без
	// определённой части дали бы вакуумный зелёный по каждой из двух осей.
	if census.profiles == 0 || census.stacks == 0 || census.refs == 0 ||
		census.productRefs == 0 || census.sectionParts == 0 {
		t.Fatalf("обход пуст — вердикт беспредметен: профилей %d, стеков %d, ссылок %d, "+
			"узнанных частей %d, рабочих мест с определённой частью %d",
			census.profiles, census.stacks, census.refs, census.productRefs, census.sectionParts)
	}

	if len(findings) > 0 {
		t.Fatalf("имя образа расходится с источником имён (%d):\n%s",
			len(findings), strings.Join(findings, "\n"))
	}
}
