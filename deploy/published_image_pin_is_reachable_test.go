// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// published_image_pin_is_reachable_test.go — пин опубликованного образа продукта
// ТЯНЕТСЯ. Не «назван канонически» и не «выведен из записи», а именно тянется:
// манифест с таким тегом в реестре ЕСТЬ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ НОВАЯ ПРОВЕРКА, А НЕ РАСШИРЕНИЕ СОСЕДНЕЙ — ИХ ШЕСТЬ, И НИ ОДНА НЕ МОЖЕТ
//
// Единица перечня — проверка каталога `deploy`, чья СТРОКА-ЗАГОЛОВОК называет
// образ (инъекционные доказательства исключены: их предмет — падучесть соседа).
// Перечень обязан быть ПОЛНЫМ: довод «ни одна не может» проверяем ровно настолько,
// насколько перечислены все. Прежняя редакция называла три из шести — то есть
// довод держался на неполном обходе. Предикат:
//
//	for f in deploy/*_test.go; do case $f in *injection*) continue;; esac
//	  grep -m1 -iE "^// $(basename $f) — .*образ" "$f"; done
//
// Он даёт СЕМЬ строк: шесть соседей плюс эта проверка. `-i` здесь несущий, а не
// косметика: один заголовок пишет «ИМЯ ОБРАЗА» заглавными, и предикат без `-i`
// теряет его молча — то есть недобирает ровно тем способом, против которого
// перечень и написан.
//
//	stand_image_has_a_producer_test.go     есть ли у образа ТОТ, КТО ЕГО СОБИРАЕТ.
//	                                       Её граница ОБЪЯВЛЕНА в шапке: «только
//	                                       локальные образы: опубликованные
//	                                       приходят из реестра, и производитель
//	                                       им не нужен». Расширять её — ломать
//	                                       объявленную границу;
//	image_overlay_coverage_test.go         ЗАМЕНЁН ли локальный образ публичным у
//	                                       каждого компонента. О содержимом
//	                                       реестра не спрашивает вовсе;
//	managed_cluster_profile_test.go        РАВЕН ли тег выводу из записанного
//	                                       коммита. Сверяет тег с записью — и
//	                                       зелен, когда оба согласны и образа с
//	                                       таким тегом не существует;
//	image_name_agrees_with_the_source…     согласуется ли ИМЯ образа с единственным
//	                                       источником имён. Судит имя, а не
//	                                       существование: `kacho-vpc:main-latest`
//	                                       названо канонически и не тянется;
//	stand_provenance_declaration_test.go   несёт ли образ метку ревизии дерева.
//	                                       Читает объявление СБОРКИ, а не реестр,
//	                                       и о неопубликованном теге молчит;
//	pg_image_pinned_test.go                припинен ли и одинаков ли образ
//	                                       Postgres. Предмет — СТОРОННИЙ образ,
//	                                       выведенный отсюда второй границей ниже.
//
// Последнее — не теория. Замер 2026-09-12 по шести стендам: профиль управляемого
// кластера нёс СЕМНАДЦАТЬ пинов, выведенных из одной записи о коммите, и все они
// были СОГЛАСНЫ с записью; манифеста не было у одного из них — у образа службы
// доступа, потому что её тег называет коммит ЕЁ ствола, а запись выводила коммит
// ЭТОГО дерева. Проверка вывода этого не видит by construction: она сверяет две
// строки друг с другом, а не с реестром.
//
// Предикат на это число — одна команда, и она читает объявление, а не память:
//
//	git show fb562c4d20:deploy/helm/umbrella/values.a8f60d.yaml \
//	  | grep -cE '^\s*(image:\s*docker\.io|repository:\s*docker\.io)'
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТО СТОИЛО, ПОКА ПРОВЕРКИ НЕ БЫЛО — ПЕРЕПИСЬЮ ПО КОМПОНЕНТАМ, А НЕ ОДНИМ
// ЧИСЛОМ
//
// Здесь стояло «одиннадцать у семи компонентов» — и компонентов было восьми, а
// девяти ссылкам единицы гейта отвечает шести. То есть число ссылок было верно
// для одной единицы счёта, число компонентов — ни для какой, и разошлись они
// молча: у итога, написанного прозой, нет производителя. Поэтому цена называется
// СТРОКОЙ НА КОМПОНЕНТ, и итог есть сумма строк — тогда прибавившийся компонент
// виден, а не спрятан за одним числом.
//
// ЕДИНИЦ СЧЁТА ЗДЕСЬ ДВЕ, И ОНИ НАЗВАНЫ. Первая — ссылки, объявленные СЛОЯМИ
// УМБРЕЛЛЫ: ровно то, что судит этот гейт (см. «ПЕРВАЯ ГРАНИЦА» ниже). Вторая —
// всё, что стенд ТЯНЕТ: та же таблица плюс две ссылки, приезжавшие умолчанием
// братского подчарта, то есть мимо границы гейта. Второй единицей измеряется
// цена дефекта, первой — работа проверки; путать их нельзя, и именно эта путаница
// дала прежнее число.
//
//	компонент          ссылок  дофиксовый тег     чем объявлялся
//	────────────────────────────────────────────────────────────────────────────
//	kaname                  4  main-7bb0a6e1      слой умбреллы
//	                           main-b622b388      слой умбреллы (два стенда)
//	                           main-a1b28f4c      слой умбреллы
//	                           main-be5a45a2      слой умбреллы (базовый)
//	kacho-vpc               1  main-3b48f99b      слой умбреллы
//	kacho-compute           1  main-1ecb2817      слой умбреллы
//	kacho-api-gateway       1  main-fa9a8422      слой умбреллы
//	kacho-geo               1  main-latest        слой умбреллы
//	kacho-storage           1  main-latest        слой умбреллы
//	────────────────────────────────────────────────────────────────────────────
//	единица гейта           9 ссылок у 6 компонентов
//	kacho-registry          1  main-latest        УМОЛЧАНИЕ ПОДЧАРТА
//	kacho-nlb               1  0.2.0 (AppVersion) УМОЛЧАНИЕ ПОДЧАРТА
//	────────────────────────────────────────────────────────────────────────────
//	тянет стенд            11 ссылок у 8 компонентов
//
// Затронуты ВСЕ ШЕСТЬ стендов таблицы: образ службы доступа просил каждый.
//
// ЛЮБАЯ СТРОКА ПРОВЕРЯЕТСЯ ОДНИМ ЗАПРОСОМ, пока реестр ещё отвечает так же:
//
//	repo=prorobotech/kaname; tag=main-be5a45a2
//	t=$(curl -s "https://auth.docker.io/token?service=registry.docker.io\
//	&scope=repository:$repo:pull" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
//	curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $t" \
//	  "https://registry-1.docker.io/v2/$repo/tags/list?n=1"   # 200 — репозиторий читается
//	curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $t" \
//	  "https://registry-1.docker.io/v2/$repo/manifests/$tag"  # 404 — манифеста нет
//
// Две команды, а не одна, намеренно: первая — тот же управляющий вопрос, которым
// гейт отделяет находку от несостоявшегося измерения (см. ниже). Без него `404`
// не доказывает ничего.
//
// ТЕКУЩИЕ ЖЕ ЧИСЛА В ПРОЗЕ НЕ СТОЯТ ВОВСЕ — их печатает перепись гейта строкой
// «различных ссылок N у M компонентов» и строкой на компонент. Дофиксовые остались
// прозой потому, что производителя у них быть не может: дерево их уже не несёт.
//
// Отказ приходит на кластере, минутами позже «helm upgrade прошёл»:
//
//	Failed to pull image "docker.io/prorobotech/kaname:main-be5a45a2":
//	  ... manifest unknown  →  Init:ImagePullBackOff
//
// Под не стартует ни разу, край держит /readyz=503, `helm --wait` истекает. Ни
// рендер, ни `helm lint`, ни любая из трёх проверок выше этого не видят:
// манифест собирается, образ в нём назван, он просто недостижим.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЕДИНИЦА СЧЁТА И ГРАНИЦА — ОБЪЯВЛЕНЫ, А НЕ ПОДРАЗУМЕВАЮТСЯ
//
// ЕДИНИЦА — РАЗЛИЧНАЯ ССЫЛКА сложенного стенда: цепочки берутся из
// deploy/stacks.txt и складываются так же, как их складывает helm. Одна ссылка,
// названная тремя стендами, спрашивается у реестра ОДИН раз, а в находке
// называются все три.
//
// ПЕРВАЯ ГРАНИЦА: судятся ссылки, объявленные СЛОЯМИ УМБРЕЛЛЫ. Ссылка,
// приезжающая умолчанием братского подчарта, здесь не судится — её тег бывает
// вычисляемым (`kacho-nlb.image` в services/nlb/deploy/templates/_helpers.tpl
// подставляет `.Chart.AppVersion`, когда тег пуст), и читатель файлов значений
// судил бы строку, которой кластер не видит. Слепой зоной граница не становится:
// вторая половина этого файла требует, чтобы умбрелла пинила КАЖДЫЙ образ,
// который дерево собирает, — тогда «объявлено умбреллой» и «тянется стендом»
// совпадают.
//
// ВТОРАЯ ГРАНИЦА: судятся образы ПРОДУКТА. Сторонние (`bitnamilegacy/postgresql`,
// `oryd/kratos-selfservice-ui-node`, `nginxinc/nginx-unprivileged`,
// `axllent/mailpit`) пинятся умолчаниями чужих чартов, и их удержание в реестре —
// чужая политика; сделав вердикт её функцией, мы получили бы гейт, который
// отключат после первой ложной находки. Граница проверена замером, а не вкусом:
// на день заведения манифест был у КАЖДОЙ сторонней ссылки дерева — и ни у одной
// из одиннадцати продуктовых, недостижимых по второй единице счёта выше (той, что
// «тянет стенд»; по единице гейта их девять, и обе перечислены строкой на
// компонент).
//
// СОСТОЯНИЕ `enabled` НЕ УЧИТЫВАЕТСЯ НАМЕРЕННО. Состав стенда флипается ключом
// (`--set <компонент>.enabled=…`) и шардами сквозного прогона, поэтому
// «выключен сегодня» — свойство прогона, а не пина. Мёртвый пин выключенного
// компонента лжёт ровно так же, и включат его раньше, чем перечитают.
//
// ─────────────────────────────────────────────────────────────────────────────
// ИСХОДОВ ЧЕТЫРЕ, И ТРЕТИЙ НЕ ВЫДАЁТСЯ ЗА ВТОРОЙ
//
//	манифест есть      — ссылка тянется;
//	манифеста НЕТ      — НАХОДКА: тег в реестре отсутствует;
//	не выполнилось     — спросить не удалось (сети нет, реестр отвечает 5xx,
//	                     нет прав). Не вычитается из вердикта и находкой НЕ
//	                     становится: молчание источника не читается как «нет»;
//	измерение выключено — ручка не поднята. Печатается «сверено 0 из N».
//
// ТРЕТИЙ ОТДЕЛЁН ОТ ВТОРОГО КОНТРОЛЕМ, А НЕ ТЕКСТОМ ОТКАЗА. Прежде главного
// вопроса задаётся управляющий: виден ли РЕПОЗИТОРИЙ вообще (перечень его тегов).
// Управляющий прошёл — реестр достижим и репозиторий читается, значит отсутствие
// ОДНОГО тега есть находка. Управляющий упал — спросить не удалось. Классификация
// по фразе ответа этого не даёт: `404` приходит и на снятый тег, и на закрытый
// репозиторий, и на опечатку в имени.
//
// ─────────────────────────────────────────────────────────────────────────────
// СЕТЬ — ПО РУЧКЕ, И ПЕРЕПИСЬ НАЗЫВАЕТ ЕЁ СОСТОЯНИЕ ВСЕГДА
//
// Вердикт проверки не вправе быть функцией доступности реестра, а `go test ./...`
// не вправе ходить в сеть. Измерение включается ручкой
// `KACHO_IMAGE_REGISTRY_CHECK=1`; перепись печатает «сверено N из M» на КАЖДОМ
// прогоне — «сверено 0» никогда не выглядит как «сверено». Тот же приём, что у
// сверки номера задачи в пробах консоли и у scripts/release/assert-pin-agrees.sh;
// второго уклада не заводится.
//
// ─────────────────────────────────────────────────────────────────────────────
// СПОСОБНОСТЬ УПАСТЬ
//
// Доказана инъекцией в обе стороны —
// published_image_pin_is_reachable_injection_test.go: решение вынесено чистой
// функцией, ей подаётся настоящий вход дерева, и каждая ось проверяется отдельно
// вместе с законным близнецом.
package deploy_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// registryCheckKnob — ручка, включающая сетевое измерение.
const registryCheckKnob = "KACHO_IMAGE_REGISTRY_CHECK"

// ─────────────────────────────────────────────────────────────────────────────
// Чтение дерева. Ничего не решает.

// pinnedImage — ссылка на образ, объявленная слоями умбреллы.
type pinnedImage struct {
	Stand     string // стенд таблицы стеков
	Component string // путь ключа в дереве значений — координата для находки
	Repo      string // репозиторий целиком, как объявлен
	Tag       string // тег, либо пусто если объявлен только репозиторий
	Digest    string // digest, либо пусто
}

// Ref — ссылка, как её получит kubelet.
func (p pinnedImage) Ref() string {
	if p.Digest != "" {
		return p.Repo + "@" + p.Digest
	}
	return p.Repo + ":" + p.Tag
}

// Image — короткое имя образа: то, чем он называется в перечнях.
func (p pinnedImage) Image() string {
	return p.Repo[strings.LastIndex(p.Repo, "/")+1:]
}

// pinCensus — объём осмотренного. Считается независимо от находок: без него
// молчание гейта не отличить от того, что он ничего не прочитал.
type pinCensus struct {
	Stands       int // стендов сложено
	Declared     int // объявлений образа встречено (стенд × координата)
	Product      int // из них образов продукта, тянущихся из реестра
	WithoutTag   int // из них без объявленного тега — судить нечего
	DistinctRefs int // различных ссылок к сверке
	Components   int // различных ОБРАЗОВ среди них — вторая единица счёта, см. ниже
	Repos        int // различных РЕПОЗИТОРИЕВ — третья: по ней помнится маркер и
	//                  ответ управляющего вопроса, поэтому именно она множитель в
	//                  числе обращений к реестру (см. registryProbe)
	Checked    int // сверено с реестром
	Absent     int // манифеста нет
	Unresolved int // спросить не удалось
	NetworkOn  bool
}

// componentTally — перепись ОДНОГО компонента: сколько у него различных ссылок и
// что реестр про них ответил.
//
// ЗАЧЕМ ОТДЕЛЬНАЯ ЕДИНИЦА, А НЕ ОДНО ЧИСЛО. «Ссылок 47» и «компонентов 17» —
// РАЗНЫЕ величины об одном дереве: один образ бывает запинен несколькими стендами
// разными тегами. Печатая только первую, перепись скрывает ровно то, что меняется
// чаще всего: КАКОЙ компонент прибавился или выпал. Цена измерена на этом же
// файле — шапка называла «одиннадцать у семи компонентов» там, где компонентов
// восьми, и число компонентов не сходилось ни при какой единице счёта, потому что
// у него не было производителя. Теперь его печатает проверка.
type componentTally struct {
	Image      string
	Refs       int
	Checked    int
	Absent     int
	Unresolved int
}

// distinctRepositories — ТРЕТЬЯ единица счёта: репозитории. Вынесена функцией не
// ради краткости, а ради доказуемости: на сегодняшнем дереве образов и
// репозиториев поровну (все под одним владельцем), поэтому подмена единицы здесь
// НЕ МЕНЯЕТ НИ ОДНОГО ЧИСЛА и осталась бы незаметной. Доказательство подаёт вход,
// на котором единицы расходятся, — но подать его можно только отдельной функции.
//
// Единица важна потому, что именно репозиторий — множитель числа обращений к
// реестру: маркер и управляющий вопрос `registryProbe` помнит ПО РЕПОЗИТОРИЮ.
func distinctRepositories(pins []pinnedImage) int {
	repos := map[string]bool{}
	for _, p := range pins {
		repos[p.Repo] = true
	}
	return len(repos)
}

// tallyByComponent — перепись по компонентам из тех же ответов, по которым
// вынесен вердикт. Второго прохода по дереву здесь нет намеренно: перепись,
// считающая своим обходом, разошлась бы с вердиктом молча.
func tallyByComponent(where map[string][]pinnedImage, answers []pinAnswer) []componentTally {
	answerOf := make(map[string]registryAnswer, len(answers))
	for _, a := range answers {
		answerOf[a.Ref] = a.Answer
	}
	byImage := map[string]*componentTally{}
	for ref, pins := range where {
		if len(pins) == 0 {
			continue
		}
		img := pins[0].Image()
		row, ok := byImage[img]
		if !ok {
			row = &componentTally{Image: img}
			byImage[img] = row
		}
		row.Refs++
		switch a, asked := answerOf[ref]; {
		case !asked: // измерение выключено ручкой — ни сверено, ни не выполнилось
		case a == answerAbsent:
			row.Checked++
			row.Absent++
		case a == answerPresent:
			row.Checked++
		default:
			row.Unresolved++
		}
	}
	out := make([]componentTally, 0, len(byImage))
	for _, row := range byImage {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Image < out[j].Image })
	return out
}

// collectUmbrellaImagePins — обход РАЗОБРАННОГО дерева значений, а не текста.
// Форма объявления образа у подчартов разная (плоская строка либо карта), обе
// законны, и гейт, знающий одну из двух, молчит там, где выглядит работающим.
func collectUmbrellaImagePins(t *testing.T) ([]pinnedImage, pinCensus) {
	t.Helper()
	chains := deployStacks(t)
	base := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))

	var (
		out    []pinnedImage
		census pinCensus
	)
	census.Stands = len(chains)
	for _, stand := range sortedStackNames(chains) {
		folded := mergeValues(map[string]any{}, base)
		for _, p := range chains[stand] {
			folded = mergeValues(folded, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		for _, decl := range walkImageDeclarations(folded, nil) {
			census.Declared++
			if !productnaming.IsProductImageRepo(decl.Repo) || !pulledFromARegistry(decl.Repo) {
				continue
			}
			census.Product++
			if decl.Tag == "" && decl.Digest == "" {
				census.WithoutTag++
				continue
			}
			decl.Stand = stand
			out = append(out, decl)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ref() != out[j].Ref() {
			return out[i].Ref() < out[j].Ref()
		}
		if out[i].Stand != out[j].Stand {
			return out[i].Stand < out[j].Stand
		}
		return out[i].Component < out[j].Component
	})
	return out, census
}

// walkImageDeclarations — рекурсивный обход: образы лежат и на верхнем уровне
// (`vpc.image`), и внутри компонента консоли (`uif.dashboard.image`).
func walkImageDeclarations(node any, path []string) []pinnedImage {
	var out []pinnedImage
	tree, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range sortedKeys(tree) {
		here := append(append([]string{}, path...), key)
		if key == "image" {
			switch img := tree[key].(type) {
			case string:
				if ref := strings.TrimSpace(img); ref != "" {
					// Соседний ключ `imageDigest` — форма вендоренных подчартов
					// (charts/kacho-geo): digest приписывается к репозиторию, а
					// хвостовой тег отбрасывается.
					digest, _ := tree["imageDigest"].(string)
					out = append(out, splitFlatImageRef(strings.Join(here, "."), ref, strings.TrimSpace(digest)))
				}
				continue
			case map[string]any:
				repo, _ := img["repository"].(string)
				if strings.TrimSpace(repo) != "" {
					tag, _ := img["tag"].(string)
					digest, _ := img["digest"].(string)
					out = append(out, pinnedImage{
						Component: strings.Join(here, "."),
						Repo:      strings.TrimSpace(repo),
						Tag:       strings.TrimSpace(tag),
						Digest:    strings.TrimSpace(digest),
					})
				}
				continue
			}
		}
		out = append(out, walkImageDeclarations(tree[key], here)...)
	}
	return out
}

// splitFlatImageRef — разбор плоской формы `<репозиторий>[:<тег>]`.
func splitFlatImageRef(component, ref, digest string) pinnedImage {
	repo, tag := ref, ""
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		repo, tag = ref[:i], ref[i+1:]
	}
	return pinnedImage{Component: component, Repo: repo, Tag: tag, Digest: digest}
}

// ─────────────────────────────────────────────────────────────────────────────
// РЕШЕНИЕ — чистой функцией. Вынесено затем, чтобы доказательство падучести
// подавало сюда настоящий вход, а не подделывало дерево и не ходило в сеть.

// registryAnswer — что ответил реестр про одну ссылку.
type registryAnswer int

const (
	answerPresent    registryAnswer = iota // манифест есть
	answerAbsent                           // манифеста нет — находка
	answerUnresolved                       // спросить не удалось — НЕ находка
)

// pinAnswer — ответ про ссылку вместе с причиной, как её напечатает реестр.
type pinAnswer struct {
	Ref    string
	Answer registryAnswer
	Why    string
}

// unreachablePinFindings — РЕШЕНИЕ гейта. Находкой становится только
// `answerAbsent`; `answerUnresolved` попадает в перепись отдельной величиной и
// вердикта не меняет.
func unreachablePinFindings(answers []pinAnswer, where map[string][]pinnedImage) []string {
	var out []string
	for _, a := range answers {
		if a.Answer != answerAbsent {
			continue
		}
		stands := map[string]bool{}
		var coords []string
		for _, p := range where[a.Ref] {
			stands[p.Stand] = true
			coords = append(coords, p.Stand+":"+p.Component)
		}
		names := make([]string, 0, len(stands))
		for s := range stands {
			names = append(names, s)
		}
		sort.Strings(names)
		sort.Strings(coords)
		out = append(out, fmt.Sprintf(
			"ссылка %s недостижима: репозиторий реестром отдаётся, а манифеста с таким тегом в нём "+
				"НЕТ (%s). Её просят стенды: %s (координаты: %s). Такой образ не «ещё не собран» — "+
				"его не тянет kubelet: под уйдёт в ImagePullBackOff уже ПОСЛЕ успешного «helm "+
				"upgrade», и ни рендер, ни lint, ни сверка тега с записью о коммите этого не видят",
			a.Ref, a.Why, strings.Join(names, " "), strings.Join(coords, " ")))
	}
	sort.Strings(out)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Сетевая половина. Спрашивает, ничего не решает.

// dockerHubRegistry — единственный реестр, который проба умеет спрашивать.
// Всё прочее — «не выполнилось», а не «манифеста нет»: непонятый адрес не
// доказывает отсутствия.
const dockerHubRegistry = "docker.io/"

// registryEndpoints — адреса, у которых проба спрашивает. Выделены затем, чтобы
// доказательство падучести подавало СВОЙ сервер и проверяло разбор ответов, не
// завися от сети: гейт, чьи ответы нельзя подать, доказать нечем.
type registryEndpoints struct {
	Token string // выдача маркера на чтение репозитория
	API   string // корень v2-интерфейса реестра, со слэшем на конце
}

// dockerHubEndpoints — адреса Docker Hub.
var dockerHubEndpoints = registryEndpoints{
	Token: "https://auth.docker.io/token",
	API:   "https://registry-1.docker.io/v2/",
}

// registryProbe — один проход по ссылкам. Маркер и ответ управляющего вопроса
// помнятся ПО РЕПОЗИТОРИЮ, а не спрашиваются на каждую ссылку.
//
// Причина измерена, а не предположена: анонимный предел Docker Hub. Прежняя
// редакция задавала три запроса на ссылку (маркер · перечень тегов · манифест), и
// на сорока семи ссылках реестр начинал отвечать `429` — гейт честно относил это
// к «не выполнилось» и не краснел, но сверял единицы ссылок из сорока семи.
// Проверка, чьё измерение обычно не состоится, перестаёт читаться, а перестав
// читаться, перестаёт работать.
//
// СКОЛЬКО ИМЕННО ОСТАЛОСЬ НЕСВЕРЕННЫМ — НАБЛЮДЕНИЕ ОДНОГО ПРОГОНА, А НЕ ЗАМЕР, и
// потому числом здесь не стоит: порог предела зависит от того, сколько обращений
// к Docker Hub эта машина сделала до прогона. Два прогона подряд на одном дереве
// дали «сверено 47 из 47» и «сверено 5 из 47». Величину печатает перепись гейта
// на КАЖДОМ прогоне — брать её оттуда, а не отсюда.
//
// ЧТО ЗАМЕР ДАЁТ ДЕТЕРМИНИРОВАННО — ЧИСЛО ОБРАЩЕНИЙ, и оно здесь прозой НЕ
// названо: его печатает перепись гейта целиком, вместе с обоими множителями —
// «обращений к реестру: 2×<репозиториев>+<ссылок>=<итог>, без памяти было бы
// 3×<ссылок>=<итог>». Формула та же, что исполняет `askRegistry`: маркер и
// управляющий вопрос по разу на РЕПОЗИТОРИЙ, манифест — на каждую ссылку.
//
// Прежняя редакция называла здесь «141 запрос к 65». Первое число верно (3×47),
// второе — ни при какой единице счёта: 65 получается из девяти репозиториев, а их
// семнадцать. Оно не измерялось, а подсчитывалось в уме, и потому разошлось молча
// — тот же класс, что и «одиннадцать у семи компонентов» выше. Замер, которым это
// опровергнуто: счётчик обращений, обёрнутый вокруг переноса HTTP, дал 17 маркеров
// + 17 перечней тегов + 47 манифестов = 81 — ровно то, что печатает перепись.
//
// Ответ управляющего вопроса памятью НЕ ослабляется: он задаётся по разу на
// репозиторий, а различает он состояние РЕПОЗИТОРИЯ, а не тега.
type registryProbe struct {
	client *http.Client
	ep     registryEndpoints
	tokens map[string]string // репозиторий → маркер
	gate   map[string]string // репозиторий → причина, по которой его нельзя судить ("" = можно)
}

func newRegistryProbe(client *http.Client, ep registryEndpoints) *registryProbe {
	return &registryProbe{
		client: client,
		ep:     ep,
		tokens: map[string]string{},
		gate:   map[string]string{},
	}
}

// askRegistry — есть ли манифест. КОНТРОЛЬ ПЕРЕД ГЛАВНЫМ ВОПРОСОМ: сперва
// спрашивается перечень тегов репозитория. Прошёл — реестр достижим и
// репозиторий читается, значит отсутствие одного тега есть факт. Упал — спросить
// не удалось, и «не знаю» не выдаётся за «нет».
func (p *registryProbe) askRegistry(ref string) (registryAnswer, string) {
	client, ep := p.client, p.ep
	repo, reference := ref, ""
	if i := strings.Index(ref, "@"); i >= 0 {
		repo, reference = ref[:i], ref[i+1:]
	} else if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		repo, reference = ref[:i], ref[i+1:]
	}
	if !strings.HasPrefix(repo, dockerHubRegistry) {
		return answerUnresolved, "реестр " + repo + " проба спрашивать не умеет"
	}
	path := strings.TrimPrefix(repo, dockerHubRegistry)

	token, seen := p.tokens[path]
	if !seen {
		var err error
		if token, err = dockerHubToken(client, ep, path); err != nil {
			p.gate[path] = "токен на чтение " + path + " не получен: " + err.Error()
		}
		p.tokens[path] = token
		// Управляющий вопрос — по разу на репозиторий: он различает состояние
		// РЕПОЗИТОРИЯ, а не тега.
		if _, blocked := p.gate[path]; !blocked {
			if code, err := registryGet(client, ep, token, path, "tags/list?n=1"); err != nil || code != http.StatusOK {
				p.gate[path] = fmt.Sprintf("репозиторий %s не читается (перечень тегов: код %d, %v) — "+
					"отсутствие отдельного тега этим не доказывается", path, code, err)
			} else {
				p.gate[path] = ""
			}
		}
	}
	if why := p.gate[path]; why != "" {
		return answerUnresolved, why
	}
	code, err := registryGet(client, ep, token, path, "manifests/"+reference)
	switch {
	case err != nil:
		return answerUnresolved, "манифест " + reference + " не спрошен: " + err.Error()
	case code == http.StatusOK:
		return answerPresent, ""
	case code == http.StatusNotFound:
		return answerAbsent, fmt.Sprintf("перечень тегов %s читается, манифест %s → код %d", path, reference, code)
	default:
		return answerUnresolved, fmt.Sprintf("манифест %s → код %d: не «нет», а «не спросили»", reference, code)
	}
}

// registryCredentialEnv — откуда берётся удостоверение к реестру. Те же имена,
// которыми пользуется сборка образов, — второго уклада не заводится.
//
// АНОНИМНЫЙ ПРЕДЕЛ — НЕ ТЕОРИЯ, А ЗАМЕР. Без удостоверения Docker Hub начинает
// отвечать `429` после нескольких десятков обращений: гейт честно относит это к
// «не выполнилось» и не краснеет, но сверяет часть ссылок вместо всех. Поэтому
// задание, поднимающее ручку, обязано передать и эти две величины; сверено
// сколько и было ли удостоверение — печатает перепись.
const (
	registryUserEnv  = "DOCKERHUB_USERNAME"
	registryTokenEnv = "DOCKERHUB_TOKEN" //nolint:gosec // имя переменной, не величина
)

// registryCredentialPresent — обе половины удостоверения заданы. Половина хуже
// отсутствия обеих: она выглядит настроенной.
func registryCredentialPresent() bool {
	return os.Getenv(registryUserEnv) != "" && os.Getenv(registryTokenEnv) != ""
}

// dockerHubToken — маркер на чтение одного репозитория.
func dockerHubToken(client *http.Client, ep registryEndpoints, path string) (string, error) {
	url := ep.Token + "?service=registry.docker.io&scope=repository:" + path + ":pull"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	if registryCredentialPresent() {
		req.SetBasicAuth(os.Getenv(registryUserEnv), os.Getenv(registryTokenEnv))
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("код %d", resp.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.Token == "" {
		return "", fmt.Errorf("ответ без маркера")
	}
	return body.Token, nil
}

// registryGet — один запрос к реестру; возвращает код ответа.
func registryGet(client *http.Client, ep registryEndpoints, token, path, tail string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, ep.API+path+"/"+tail, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for _, accept := range []string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	} {
		req.Header.Add("Accept", accept)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Гейт.

// TestPublishedProductImagePinIsReachable — пин опубликованного образа продукта
// тянется из реестра.
func TestPublishedProductImagePinIsReachable(t *testing.T) {
	pins, census := collectUmbrellaImagePins(t)

	where := map[string][]pinnedImage{}
	for _, p := range pins {
		where[p.Ref()] = append(where[p.Ref()], p)
	}
	refs := make([]string, 0, len(where))
	for r := range where {
		refs = append(refs, r)
	}
	sort.Strings(refs)
	census.DistinctRefs = len(refs)
	census.NetworkOn = os.Getenv(registryCheckKnob) == "1"

	var answers []pinAnswer
	if census.NetworkOn {
		probe := newRegistryProbe(&http.Client{Timeout: 20 * time.Second}, dockerHubEndpoints)
		for _, ref := range refs {
			answer, why := probe.askRegistry(ref)
			answers = append(answers, pinAnswer{Ref: ref, Answer: answer, Why: why})
			switch answer {
			case answerAbsent:
				census.Checked++
				census.Absent++
			case answerPresent:
				census.Checked++
			case answerUnresolved:
				census.Unresolved++
				t.Logf("НЕ ВЫПОЛНИЛОСЬ для %s: %s", ref, why)
			}
		}
	}

	for _, f := range unreachablePinFindings(answers, where) {
		t.Error(f)
	}

	tally := tallyByComponent(where, answers)
	census.Components = len(tally)
	census.Repos = distinctRepositories(pins)

	// Проверка СВОЕЙ предпосылки. Ослепнуть этот гейт может тремя способами:
	// перестать видеть стенды, перестать видеть объявления образа и перестать
	// узнавать образ продукта. Во всех трёх молчание неотличимо от того, что
	// каждый пин тянется.
	if census.Stands == 0 || census.Declared == 0 {
		t.Fatalf("обход ничего не прочитал: стендов=%d, объявлений образа=%d — предикат "+
			"перестал узнавать дерево, а не дерево стало чистым", census.Stands, census.Declared)
	}
	if census.DistinctRefs == 0 || census.Components == 0 {
		t.Fatalf("ни одной ссылки на опубликованный образ продукта не найдено (стендов %d, "+
			"объявлений образа %d, различных ссылок %d, компонентов %d) — предикат разошёлся с "+
			"деревом, и тогда «все пины тянутся» означает «сверять было нечего»",
			census.Stands, census.Declared, census.DistinctRefs, census.Components)
	}

	knobState := "ВЫКЛЮЧЕНО (ручка " + registryCheckKnob + "=1 не поднята)"
	if census.NetworkOn {
		knobState = "включено ручкой " + registryCheckKnob + ", удостоверение реестра " +
			map[bool]string{true: "задано", false: "НЕ задано"}[registryCredentialPresent()]
	}
	if census.Unresolved > 0 && !registryCredentialPresent() {
		t.Logf("подсказка: %d ссылок не сверено, а удостоверение реестра (%s/%s) не задано — "+
			"анонимный предел Docker Hub отвечает 429 после нескольких десятков обращений. "+
			"Это НЕ находка и не вычитается из вердикта, но и сверкой не является",
			census.Unresolved, registryUserEnv, registryTokenEnv)
	}
	t.Logf("осмотрено: стендов %d, объявлений образа %d, из них образов продукта из реестра %d "+
		"(без объявленного тега %d — судить нечего); различных ссылок %d у %d компонентов в %d "+
		"репозиториях (обращений к реестру: 2×%d+%d=%d, без памяти было бы 3×%d=%d); измерение %s; "+
		"сверено %d из %d, манифеста нет у %d, не выполнилось для %d",
		census.Stands, census.Declared, census.Product, census.WithoutTag,
		census.DistinctRefs, census.Components, census.Repos,
		census.Repos, census.DistinctRefs, 2*census.Repos+census.DistinctRefs,
		census.DistinctRefs, 3*census.DistinctRefs,
		knobState, census.Checked, census.DistinctRefs,
		census.Absent, census.Unresolved)

	// ПЕРЕПИСЬ ПО КОМПОНЕНТАМ — печатается ВСЕГДА, а не только на находке. Одно
	// число («ссылок 47») скрывает, какой компонент прибавился; строка на
	// компонент этого скрыть не умеет, и всякое число о компонентах, стоящее в
	// прозе этого файла, сверяется с НЕЙ, а не с памятью пишущего.
	for _, row := range tally {
		t.Logf("  компонент %-26s различных ссылок %d, сверено %d, манифеста нет %d, не выполнилось %d",
			row.Image, row.Refs, row.Checked, row.Absent, row.Unresolved)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ВТОРАЯ ПОЛОВИНА: граница гейта выше не должна быть слепой зоной.

// imagesTheUmbrellaLeavesToItsSubcharts — РЕШЕНИЕ второй половины, чистой
// функцией: образы, которые дерево собирает, а базовый слой умбреллы не пинит.
//
// `produced` — образ → служба (от рецепта стенда); `pinned` — образ → координата
// объявления в умбрелле.
func imagesTheUmbrellaLeavesToItsSubcharts(produced map[string]string, pinned map[string]string) []string {
	images := make([]string, 0, len(produced))
	for img := range produced {
		images = append(images, img)
	}
	sort.Strings(images)

	var out []string
	for _, img := range images {
		if _, ok := pinned[img]; ok {
			continue
		}
		out = append(out, "рецепт стенда собирает образ "+img+" (служба "+produced[img]+"), а "+
			"базовый слой умбреллы его не пинит — ссылка приедет умолчанием подчарта, и тогда "+
			"она уходит из-под проверки досягаемости: та судит объявления УМБРЕЛЛЫ, потому что "+
			"тег подчарта бывает вычисляемым. Объяви `image` здесь, рядом с остальными")
	}
	return out
}

// TestUmbrellaPinsEveryImageTheTreeBuilds — умбрелла объявляет образ КАЖДОЙ
// части, которую собирает рецепт стенда.
//
// ПРЕДМЕТ. Гейт выше судит ссылки, объявленные слоями умбреллы, и не судит те,
// что приезжают умолчанием братского подчарта (причина — в шапке: тег там бывает
// вычисляемым). Пока умбрелла пинит все свои образы, эти два множества
// совпадают. Перестанет — граница молча станет слепой зоной, и «все пины
// тянутся» будет означать «часть пинов не читалась».
//
// Замер, ради которого проверка заведена: `registry` и `kacho-nlb` умбрелла не
// пинила вовсе, и их ссылка приезжала умолчанием подчарта — у первого мёртвый
// `main-latest`, у второго пустой тег, который подчарт подставляет из
// `.Chart.AppVersion`. Второе особенно тихо: тега в файле значений НЕТ ВОВСЕ.
//
// ЧИТАЕТСЯ ТОЛЬКО ОБЪЯВЛЕНИЕ: `SERVICES` рецепта (собственное объявление
// производителя) и базовый слой умбреллы. Ни чартов, ни сети — пропуститься она
// не умеет. Имя образа спрашивается у единственного владельца имён
// (`internal/productnaming`), второй ведомости здесь нет.
func TestUmbrellaPinsEveryImageTheTreeBuilds(t *testing.T) {
	produced := recipeProducedImages(t, standRecipeServices(t))

	base := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))
	pinned := map[string]string{} // имя образа → координата объявления
	for _, decl := range walkImageDeclarations(base, nil) {
		if !productnaming.IsProductImageRepo(decl.Repo) {
			continue
		}
		pinned[decl.Image()] = decl.Component
	}

	for _, f := range imagesTheUmbrellaLeavesToItsSubcharts(produced, pinned) {
		t.Error(f)
	}

	if len(produced) == 0 || len(pinned) == 0 {
		t.Fatalf("обход ничего не прочитал: рецепт производит образов=%d, умбрелла пинит=%d — "+
			"предикат перестал узнавать дерево", len(produced), len(pinned))
	}
	t.Logf("осмотрено: рецепт собирает образов %d, базовый слой умбреллы пинит образов продукта %d",
		len(produced), len(pinned))
}
