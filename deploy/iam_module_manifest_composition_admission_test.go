// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_module_manifest_composition_admission_test.go — СБОРКА модели прав
// установки из доставленных манифестов объявляется вместе со своим ДОПУСКОМ,
// либо не объявляется вовсе (задача #1971).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Пока манифест модуля только доставляется, правка карты настроек кластера ни на
// что не влияет. Как только доставленное УЧАСТВУЕТ в модели прав установки,
// правка той же карты начинает эту модель ОПРЕДЕЛЯТЬ — то есть посадка получает
// новую поверхность. Производителем доверия к собранной модели служит допуск
// (`manifests.admission`); он и делает сборку выразимой.
//
// Состояния «сборка без допуска» не существует, и держит это страж старта службы
// (`ManifestsConfig.validateComposition` — ОТКАЗ В ПУСКЕ). Это верное место для
// отказа и САМОЕ ДОРОГОЕ для его обнаружения: величина, уехавшая в профиль,
// доживает до kubelet и проявляется отказом старта на поднимаемой площадке.
//
// Здесь то же судится ПО ОБЪЯВЛЕНИЮ — до всякого рендера и до всякого кластера.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПО ОБЪЯВЛЕНИЮ, А НЕ ПО РЕНДЕРУ
//
// Довод соседей дословно (`iam_module_manifest_pair_agreement_test.go`,
// `token_shape_test.go`): рендер требует helm, а его в харнессе нет, — проверка,
// которой нужен инструмент, УМЕЕТ ПРОПУСТИТЬСЯ, и тогда «ноль находок»
// неотличимо от «не запускались». Сверх того рендер собирается из упакованных
// зависимостей, и скопированный из соседнего клона `.tgz` дал бы вердикт о ЧУЖОМ
// дереве. Объявление профиля — то, что правит человек, и то, что уезжает в
// поставку.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПРИВЯЗКА СТРУКТУРНАЯ, А НЕ ПОДСТРОКОЙ И НЕ РЕГУЛЯРКОЙ
//
// Подстрока не участвует вовсе: сверяется КЛЮЧ КАРТЫ, а не текст. Поэтому
// `composeModelXX` не может быть принят за `composeModel` by construction, а
// ключ, лежащий НЕ В ТОМ УЗЛЕ, не находится — тогда как регулярка с границей
// слова закрыла бы приписку справа и осталась бы слепа ко второму. Промах по узлу
// возвращается ПРИЗНАКОМ ОБЪЯВЛЕННОСТИ отдельно от значения (`nestedString`), и
// «ключ не объявлен» не читается как «объявлен пустым».
//
// ─────────────────────────────────────────────────────────────────────────────
// СЛОВАРЯ ДВА, И ЭТО НЕ ОПЕЧАТКА
//
//	значения чарта   — camelCase: `manifests.composeModel`, `manifests.admission`
//	конфигурация     — kebab:     `manifests.compose-model`, `manifests.admission`
//
// Расхождение между ними НЕ РОНЯЕТ НИ ОДНОЙ СБОРКИ: разбор настроек молча
// отбрасывает незнакомую секцию, и посадка, объявившая сборку, поднялась бы
// БЕЗ неё — то есть страж старта промолчал бы там, где обязан отказать. Оба
// написания сходятся ровно в одном месте — мосте `templates/configmap.yaml`, — и
// поэтому именно он судится ниже.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТА ПРОВЕРКА НЕ СУДИТ — названо, чтобы заявление не было шире сделанного
//
//   - Она НЕ требует сборки ни от одного стенда. «Сборка не заведена» — решение
//     посадки, и оно законно; сегодня его приняли ВСЕ, и проверка обязана это
//     состояние проходить, объявляя переписью «сборку объявляют 0». Проба,
//     краснеющая на достижении собственной цели, подталкивает держать объявление
//     ради зелёного.
//   - Она НЕ судит ЗНАЧЕНИЕ допуска против закрытого набора. Набор объявлен одним
//     местом (`config.AdmissionByContent`), пакет которого лежит под `internal`
//     службы и отсюда неимпортируем; второй литерал того же написания разошёлся
//     бы с первым МОЛЧА — ровно тот класс, против которого набор и сведён в одно
//     место. Значение против набора судит страж старта, и его отказ печатает
//     перечень допустимых.
//   - Она НЕ судит «сборка объявлена, а доставка не объявлена». Это тоже отказ
//     старта, но предмет здесь другой, и смешение двух предметов сделало бы
//     находку непрослеживаемой.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ─────────────────────────────────────────────────────────────────────────────
// СУДЬЯ ПОСАДКИ
// ─────────────────────────────────────────────────────────────────────────────

// composeAdmissionDecl — то, что цепочка профилей сказала о сборке и её допуске.
//
// Признак объявленности хранится ОТДЕЛЬНО от значения: «ключ не объявлен» и
// «объявлен пустым» — разные утверждения, и второе есть решение посадки.
type composeAdmissionDecl struct {
	Compose       bool
	ComposeRaw    string
	ComposeSeen   bool
	ComposeSane   bool
	Admission     string
	AdmissionSeen bool
}

// mergeComposeAdmissionDecl читает цепочку профилей ТЕМ ЖЕ порядком, каким её
// сливает helm: последнее объявление ключа выигрывает.
//
// Профили передаются телами, а не путями, чтобы проверку можно было прогнать на
// синтетике: доказательство способности упасть не вправе зависеть от того, что
// сегодня лежит в дереве, — иначе оно исчезнет вместе с починкой дерева.
//
// Булева величина читается ТЕМ ЖЕ читателем, что и строковая: `nestedString`
// приводит нестроковое через `fmt.Sprint`, поэтому второго кодека здесь не
// заводится.
func mergeComposeAdmissionDecl(bodies [][]byte) (composeAdmissionDecl, error) {
	var out composeAdmissionDecl
	for _, raw := range bodies {
		var tree map[string]any
		if err := yaml.Unmarshal(raw, &tree); err != nil {
			return out, fmt.Errorf("профиль не разобран: %w", err)
		}
		if v, ok := nestedString(tree, "kaname", "manifests", "composeModel"); ok {
			out.ComposeRaw, out.ComposeSeen = v, true
			if trimmed := strings.TrimSpace(v); trimmed == "" {
				// Ключ объявлен без значения. Рендер донесёт до процесса пустое,
				// разбор прочитает его как «выключено» — и проверка обязана
				// читать так же: краснеть на том, что посадка исполняет как
				// «сборки нет», значило бы ловить форму, а не существо.
				out.Compose, out.ComposeSane = false, true
			} else {
				b, err := strconv.ParseBool(trimmed)
				out.Compose, out.ComposeSane = b, err == nil
			}
		}
		if v, ok := nestedString(tree, "kaname", "manifests", "admission"); ok {
			out.Admission, out.AdmissionSeen = v, true
		}
	}
	return out, nil
}

// composeAdmissionFinding — находка, названная стендом и текстом.
type composeAdmissionFinding struct {
	Stack string
	Text  string
}

// auditComposedModelAdmission — судья посадки. Отдельная функция, а не тело
// пробы: её зовут и обход дерева, и инъекция синтетикой, и проверка базовых
// слоёв значений.
//
// Умолчание чарта (сборка выключена, допуск пуст) согласовано, поэтому цепочка,
// не объявившая ни одной половины, находкой не является: она наследует
// согласованную пару целиком.
func auditComposedModelAdmission(stacks map[string][][]byte) ([]composeAdmissionFinding, string, error) {
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var findings []composeAdmissionFinding
	composing, profiles := 0, 0
	for _, stack := range names {
		profiles += len(stacks[stack])
		decl, err := mergeComposeAdmissionDecl(stacks[stack])
		if err != nil {
			return nil, "", fmt.Errorf("стенд %s: %w", stack, err)
		}

		// Величина, которую разбор настроек булевой не признает, — не «выключено»:
		// процесс откажет в старте на разборе, а проверка, прочитавшая её как
		// `false`, промолчала бы ровно там, где посадка сломана.
		if decl.ComposeSeen && !decl.ComposeSane {
			findings = append(findings, composeAdmissionFinding{Stack: stack, Text: fmt.Sprintf(
				"стенд %s: `manifests.composeModel` объявлен значением %q — оно не булево, "+
					"и «выключено» из него не следует: рендер донесёт эту строку до "+
					"`manifests.compose-model` конфигурации, а разбор настроек её отвергнет. "+
					"Объявите true либо false (kacho#1971)", stack, decl.ComposeRaw)})
			continue
		}
		if !decl.Compose {
			continue
		}
		composing++
		if strings.TrimSpace(decl.Admission) == "" {
			findings = append(findings, composeAdmissionFinding{Stack: stack, Text: fmt.Sprintf(
				"стенд %s: `manifests.composeModel` включён, а `manifests.admission` %s — "+
					"посадка собирает модель прав установки из доставленных манифестов и не "+
					"сказала, чем эта модель судится. Состояния «сборка без допуска» не "+
					"существует, и умолчания здесь нет намеренно: подставленный допуск был бы "+
					"непуст ВСЕГДА, и собранная модель выглядела бы суждённой на посадке, "+
					"которая допуска не объявляла. Страж старта откажет в пуске на kubelet "+
					"(kacho#1971)",
				stack, admissionAbsenceWord(decl))})
		}
	}
	census := fmt.Sprintf("осмотрено: стендов %d, профилей в цепочках %d, "+
		"сборку объявляют %d, находок %d", len(names), profiles, composing, len(findings))
	return findings, census, nil
}

// admissionAbsenceWord различает две причины пустого допуска. Различие не
// косметическое: «не объявлен» чинится добавлением ключа, «объявлен пустым» —
// тем, что оператор уже решил и написал, но написал не то.
func admissionAbsenceWord(d composeAdmissionDecl) string {
	if d.AdmissionSeen {
		return "объявлен пустым"
	}
	return "не объявлен вовсе"
}

// ─────────────────────────────────────────────────────────────────────────────
// ОБХОД ДЕРЕВА
// ─────────────────────────────────────────────────────────────────────────────

// treeComposeAdmissionStacks — цепочки стендов ТЕЛАМИ профилей, прочитанными из
// дерева. Популяция ВЫВОДИТСЯ из таблицы стеков: выписанный список разошёлся бы
// с ней молча, и стенд, заведённый завтра, приходил бы под проверку только
// правкой этого файла.
func treeComposeAdmissionStacks(t *testing.T) map[string][][]byte {
	t.Helper()
	out := map[string][][]byte{}
	for stack, chain := range deployStacks(t) {
		bodies := make([][]byte, 0, len(chain))
		for _, p := range chainPaths(chain) {
			// #nosec G304 -- путь собран из константы umbrellaDir и цепочки,
			// прочитанной из таблицы стеков.
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("профиль %s не прочитан: %v", p, err)
			}
			bodies = append(bodies, raw)
		}
		out[stack] = bodies
	}
	return out
}

// TestNoStackComposesTheModelWithoutAdmission — ни один стенд дерева не включает
// сборку модели прав, не объявив допуска.
//
// СЕГОДНЯ СБОРКУ НЕ ВКЛЮЧАЕТ НИ ОДИН СТЕНД, и это законное состояние: проверка
// обязана его ПРОХОДИТЬ, объявляя переписью «сборку объявляют 0». Способность
// упасть держит инъекция ниже, а не краснота на дереве.
func TestNoStackComposesTheModelWithoutAdmission(t *testing.T) {
	stacks := treeComposeAdmissionStacks(t)
	findings, census, err := auditComposedModelAdmission(stacks)
	if err != nil {
		t.Fatalf("обход не состоялся: %v", err)
	}
	t.Log(census)
	if len(stacks) == 0 {
		t.Fatal("стендов ноль — «сборки без допуска нет» здесь означало бы «сверять было нечего»")
	}
	for _, f := range findings {
		t.Error(f.Text)
	}
}

// TestUmbrellaBaseDoesNotEnableComposition — базовый слой значений умбреллы
// судится ТЕМ ЖЕ судьёй.
//
// Он не стоит ни в одной цепочке `-f` и потому обходом выше не виден, а helm
// применяет его ВСЕГДА и раньше всех профилей. Сборка, включённая здесь,
// досталась бы каждому стенду, и обход цепочек остался бы зелёным — то есть без
// этой пробы у судьи была бы слепая зона размером во все шесть стендов сразу.
func TestUmbrellaBaseDoesNotEnableComposition(t *testing.T) {
	p := filepath.Join(umbrellaDir, "values.yaml")
	// #nosec G304 -- путь собран из константы umbrellaDir.
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("базовый слой значений %s не прочитан: %v — предпосылка проверки исчезла, "+
			"а не дерево стало чистым", p, err)
	}
	findings, census, err := auditComposedModelAdmission(
		map[string][][]byte{"базовый слой умбреллы (" + p + ")": {raw}})
	if err != nil {
		t.Fatalf("обход не состоялся: %v", err)
	}
	t.Log(census)
	for _, f := range findings {
		t.Error(f.Text)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДПОСЫЛКА СУДЬИ: У ЕГО ВХОДА ЕСТЬ ПРОИЗВОДИТЕЛЬ
// ─────────────────────────────────────────────────────────────────────────────

// compositionOfferDecls — объявления, из которых складывается ВОЗМОЖНОСТЬ
// посадки объявить сборку. Тексты, а не пути: разбор отделён от чтения, чтобы
// инъекция подавала ему изменённые объявления, не трогая дерево.
type compositionOfferDecls struct {
	values    string
	configmap string
}

// auditCompositionOffer — находки по объявлениям чарта. Пусто = ручка сборки
// предложена посадке и доезжает до процесса.
//
// ЗАЧЕМ ЭТО ОТДЕЛЬНОЕ УТВЕРЖДЕНИЕ. Судья выше проверяет, что ни один стенд не
// включил сборку без допуска. У этого вопроса есть предпосылка: сборку ВООБЩЕ
// должно быть чем включить. Если чарт ручки не предлагает, судья зелен на всяком
// дереве не потому, что посадки исправны, а потому, что его вход НЕ ПРОИЗВОДИТСЯ
// ничем, — и заметить это по его зелени нельзя.
//
// Вторая половина того же: ручка, объявленная значениями и не доехавшая до
// процесса, есть возможность объявленная и неисполнимая. Оператор пишет
// `composeModel: true`, рендер проходит, страж старта молчит — потому что до
// него величина не добралась. Поэтому мост судится вместе с объявлением.
func auditCompositionOffer(d compositionOfferDecls) ([]string, string) {
	// Комментарии снимаются ДО поиска: об этих ключах в тех же файлах написана
	// проза, и она называет их дословно. Предикат по слову зачёл бы собственное
	// объяснение проверяемого за исполнение.
	configmap := stripDeclarationComments(d.configmap)

	var findings []string

	var tree map[string]any
	if err := yaml.Unmarshal([]byte(d.values), &tree); err != nil {
		findings = append(findings, iamChartDir+"/values.yaml: не разбирается: "+err.Error())
	}

	// (1) ЗНАЧЕНИЯ предлагают обе ручки, и предлагают их ОБЪЯВЛЕННЫМИ.
	//
	// Отсутствие ключа и объявленное «выключено» — разные утверждения. «Сборки
	// нет» есть решение посадки, и оно обязано быть написано, а не выведено из
	// молчания: тот же довод, по которому соседний `configMapName` объявлен
	// пустой строкой, а не опущен.
	composeRaw, composeSeen := nestedString(tree, "manifests", "composeModel")
	admissionRaw, admissionSeen := nestedString(tree, "manifests", "admission")
	if !composeSeen {
		findings = append(findings, iamChartDir+
			"/values.yaml: `manifests.composeModel` не объявлен — посадке нечем включить "+
			"сборку модели прав установки, и ручка, которую читает страж старта службы, "+
			"ей не предложена вовсе. Значение false законно (сборка не заведена), "+
			"отсутствие ключа — нет (kacho#1971)")
	}
	if !admissionSeen {
		findings = append(findings, iamChartDir+
			"/values.yaml: `manifests.admission` не объявлен — допуск, которым судится "+
			"собранная модель, посадке не предложен; пустое значение законно (сборка "+
			"не заведена), отсутствие ключа — нет (kacho#1971)")
	}

	// (2) Умолчание чарта — СОГЛАСОВАННАЯ пара «сборки нет». Иначе стенд,
	// не объявивший ничего, унаследовал бы сборку без допуска, а судья цепочек
	// об этом бы не узнал: он читает профили, а не умолчание подчарта.
	if composeSeen {
		if b, err := strconv.ParseBool(strings.TrimSpace(composeRaw)); err != nil || b {
			findings = append(findings, fmt.Sprintf("%s/values.yaml: `manifests.composeModel` "+
				"умолчанием равен %q — умолчание чарта достаётся всякой посадке, ничего "+
				"не объявившей, и сборка включилась бы там, где её никто не заводил. "+
				"Умолчание — false (kacho#1971)", iamChartDir, composeRaw))
		}
	}
	if admissionSeen && strings.TrimSpace(admissionRaw) != "" {
		findings = append(findings, fmt.Sprintf("%s/values.yaml: `manifests.admission` "+
			"умолчанием равен %q — допуск, названный умолчанием, выглядел бы объявленным "+
			"на посадке, которая его не выбирала. Умолчание — пусто (kacho#1971)",
			iamChartDir, admissionRaw))
	}

	// (3) МОСТ. Оба написания сходятся здесь и только здесь: ключ конфигурации —
	// kebab, ключ значений — camelCase. Ищутся ОБА, потому что каждое по
	// отдельности защитимо, а ломается их стык: секция, названная не тем
	// написанием, молча отбрасывается разбором настроек.
	for _, req := range []struct{ key, why string }{
		{"compose-model:",
			"ключа `manifests.compose-model` нет в конфигурации процесса — величина сборки " +
				"до службы не доезжает, страж старта молчит, и объявленная посадкой сборка " +
				"остаётся невидимой процессу"},
		{".Values.manifests.composeModel",
			"величина сборки не выводится из значений — она либо запечена, либо не " +
				"доезжает; ключ значений и ключ конфигурации становятся ДВУМЯ объявлениями " +
				"одного предмета и разойдутся молча"},
		{"admission:",
			"ключа `manifests.admission` нет в конфигурации процесса — допуск до службы " +
				"не доезжает, и страж старта не увидит его ни при какой посадке"},
		{".Values.manifests.admission",
			"допуск не выводится из значений — оператор не может его выбрать"},
	} {
		if !strings.Contains(configmap, req.key) {
			findings = append(findings, fmt.Sprintf("%s/templates/configmap.yaml: `%s` "+
				"не читается: %s (kacho#1971)", iamChartDir, req.key, req.why))
		}
	}

	census := fmt.Sprintf("осмотрено: файлов 2, байт прочитано %d, байт судимо %d "+
		"(после снятия комментариев), обязательных ключей 4, находок %d",
		len(d.values)+len(d.configmap), len(d.values)+len(configmap), len(findings))
	return findings, census
}

// readCompositionOfferDecls читает два объявления из дерева.
func readCompositionOfferDecls(t *testing.T) compositionOfferDecls {
	t.Helper()
	read := func(rel string) string {
		p := filepath.Join(iamChartDir, rel)
		// #nosec G304 -- путь собран из констант ЭТОГО файла (iamChartDir и
		// перечень rel), подставить посторонний файл извне нечем.
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("объявление %s не прочитано: %v — предпосылка проверки исчезла, "+
				"а не дерево стало чистым", p, err)
		}
		if len(raw) == 0 {
			t.Fatalf("объявление %s пусто — вердикт беспредметен", p)
		}
		return string(raw)
	}
	return compositionOfferDecls{
		values:    read("values.yaml"),
		configmap: read("templates/configmap.yaml"),
	}
}

// TestChartOffersTheCompositionPair — предпосылка судьи: ручку сборки и ручку
// допуска чарт ПРЕДЛАГАЕТ посадке, и обе доезжают до процесса.
func TestChartOffersTheCompositionPair(t *testing.T) {
	findings, census := auditCompositionOffer(readCompositionOfferDecls(t))
	t.Log(census)
	for _, f := range findings {
		t.Error(f)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ИНЪЕКЦИЯ — В ОБЕ СТОРОНЫ
// ─────────────────────────────────────────────────────────────────────────────

// TestComposedModelAdmissionAuditFindsCompositionWithoutAdmission — ИНЪЕКЦИЯ:
// сборка без допуска краснеет, и краснеет С ИМЕНЕМ СТЕНДА, а законные формы
// молчат.
//
// Вход синтетический намеренно: доказательство способности упасть не вправе
// зависеть от того, что сегодня лежит в дереве, — иначе оно исчезнет вместе с
// починкой дерева.
func TestComposedModelAdmissionAuditFindsCompositionWithoutAdmission(t *testing.T) {
	// body собирает тело профиля. Значение "-" означает «ключ не объявлен» —
	// это не то же самое, что объявленный пустым, и различие проверяется ниже.
	body := func(compose, admission string) []byte {
		var b strings.Builder
		b.WriteString("kaname:\n  manifests:\n")
		if compose != "-" {
			fmt.Fprintf(&b, "    composeModel: %s\n", compose)
		}
		if admission != "-" {
			fmt.Fprintf(&b, "    admission: %q\n", admission)
		}
		return []byte(b.String())
	}

	t.Run("сборка включена, допуск не объявлен — находка", func(t *testing.T) {
		findings, census, err := auditComposedModelAdmission(map[string][][]byte{
			"проба": {body("true", "-")},
		})
		if err != nil {
			t.Fatalf("обход не состоялся: %v", err)
		}
		if len(findings) != 1 {
			t.Fatalf("находок %d, ожидалась одна (%s)", len(findings), census)
		}
		if findings[0].Stack != "проба" {
			t.Fatalf("находка не называет стенд: %+v", findings[0])
		}
		for _, want := range []string{"проба", "composeModel", "admission", "не объявлен вовсе"} {
			if !strings.Contains(findings[0].Text, want) {
				t.Fatalf("находка не называет %q — читателю нечем её чинить: %s", want, findings[0].Text)
			}
		}
		if !strings.Contains(census, "сборку объявляют 1") {
			t.Fatalf("перепись не назвала объявившего сборку: %s", census)
		}
	})

	t.Run("сборка включена, допуск объявлен пустым — находка", func(t *testing.T) {
		findings, _, err := auditComposedModelAdmission(map[string][][]byte{
			"проба": {body("true", "")},
		})
		if err != nil {
			t.Fatalf("обход не состоялся: %v", err)
		}
		if len(findings) != 1 || !strings.Contains(findings[0].Text, "объявлен пустым") {
			t.Fatalf("объявленный пустым допуск не найден либо не отличён от неназванного: %+v",
				findings)
		}
	})

	t.Run("последний профиль цепочки ВКЛЮЧАЕТ сборку — находка", func(t *testing.T) {
		// Без этого случая судья, читающий только ПЕРВЫЙ профиль, был бы зелёным
		// на цепочке, где сборку включает накладка.
		findings, _, err := auditComposedModelAdmission(map[string][][]byte{
			"проба": {body("false", "-"), body("true", "-")},
		})
		if err != nil {
			t.Fatalf("обход не состоялся: %v", err)
		}
		if len(findings) != 1 {
			t.Fatalf("накладка, включившая сборку, не найдена: %+v", findings)
		}
	})

	t.Run("сборка объявлена не булевой величиной — находка", func(t *testing.T) {
		findings, _, err := auditComposedModelAdmission(map[string][][]byte{
			"проба": {body("yes", "-")},
		})
		if err != nil {
			t.Fatalf("обход не состоялся: %v", err)
		}
		if len(findings) != 1 || !strings.Contains(findings[0].Text, "не булево") {
			t.Fatalf("небулева величина прочитана как «выключено»: %+v", findings)
		}
	})

	// ЗАКОННЫЕ БЛИЗНЕЦЫ. Без них краснота выше неотличима от судьи, отвергающего
	// всякий вход.
	twins := []struct {
		name  string
		chain [][]byte
	}{
		{"сборка не заведена", [][]byte{body("false", "")}},
		{"сборка не заведена, ключей нет вовсе", [][]byte{[]byte("kaname: {}\n")}},
		{"сборка объявлена целиком", [][]byte{body("true", "content")}},
		{"последнее объявление цепочки достраивает пару", [][]byte{
			body("true", "-"), body("-", "content"),
		}},
		{"последняя накладка ВЫКЛЮЧАЕТ сборку", [][]byte{body("true", "content"), body("false", "")}},
		{"узел manifests объявлен не картой", [][]byte{[]byte("kaname:\n  manifests: \"опечатка\"\n")}},
		{"ключ с припиской справа сборкой не является", [][]byte{
			[]byte("kaname:\n  manifests:\n    composeModelXX: true\n")}},
		{"ключ лежит не в том узле", [][]byte{
			[]byte("kaname:\n  kacho:\n    composeModel: true\n")}},
		// Ключ объявлен без значения. Посадка исполняет это как «сборки нет»
		// (рендер донесёт пустое, разбор прочитает выключено), и проверка обязана
		// читать так же — иначе она ловит форму записи, а не существо.
		{"сборка объявлена без значения", [][]byte{
			[]byte("kaname:\n  manifests:\n    composeModel:\n")}},
		// Тот же случай рядом с НАЗВАННЫМ допуском: чтение пустой сборки не
		// вправе съесть второй ключ ТОГО ЖЕ тела.
		{"сборка без значения не съедает допуск того же профиля", [][]byte{
			[]byte("kaname:\n  manifests:\n    composeModel:\n    admission: \"content\"\n"),
			[]byte("kaname:\n  manifests:\n    composeModel: true\n"),
		}},
	}
	for _, tw := range twins {
		t.Run("близнец: "+tw.name, func(t *testing.T) {
			findings, census, err := auditComposedModelAdmission(map[string][][]byte{"проба": tw.chain})
			if err != nil {
				t.Fatalf("обход не состоялся: %v", err)
			}
			if len(findings) != 0 {
				t.Fatalf("законный близнец покраснел — судья ловит форму, а не существо: %v (%s)",
					findings, census)
			}
		})
	}
}

// TestCompositionOfferAuditFindsEveryMissingHalf — ИНЪЕКЦИЯ предпосылки: каждая
// снятая половина краснеет ПООДИНОЧКЕ и называет свою координату, а целое
// объявление молчит.
//
// Инъекция снимает НОВОЕ свойство у объявления, чьё старое на месте: тогда
// красное приходит от проверяемого, а не от соседа.
func TestCompositionOfferAuditFindsEveryMissingHalf(t *testing.T) {
	whole := compositionOfferDecls{
		values: "manifests:\n  configMapName: \"\"\n  mountPath: /etc/kaname/manifests\n" +
			"  required: false\n  composeModel: false\n  admission: \"\"\n",
		configmap: "data:\n  config.yaml: |\n    manifests:\n" +
			"      dir: \"\"\n      required: {{ .Values.manifests.required }}\n" +
			"      compose-model: {{ .Values.manifests.composeModel }}\n" +
			"      admission: {{ .Values.manifests.admission | quote }}\n",
	}

	t.Run("контроль: целое объявление молчит", func(t *testing.T) {
		findings, census := auditCompositionOffer(whole)
		if len(findings) != 0 {
			t.Fatalf("целое объявление покраснело — судья ловит форму, а не существо: %v (%s)",
				findings, census)
		}
		if !strings.Contains(census, "обязательных ключей 4") {
			t.Fatalf("перепись не назвала объём: %s", census)
		}
	})

	cases := []struct {
		name string
		mut  func(compositionOfferDecls) compositionOfferDecls
		want string
	}{
		{"ручка сборки не предложена значениями",
			func(d compositionOfferDecls) compositionOfferDecls {
				d.values = strings.ReplaceAll(d.values, "  composeModel: false\n", "")
				return d
			}, "`manifests.composeModel` не объявлен"},
		{"ручка допуска не предложена значениями",
			func(d compositionOfferDecls) compositionOfferDecls {
				d.values = strings.ReplaceAll(d.values, "  admission: \"\"\n", "")
				return d
			}, "`manifests.admission` не объявлен"},
		{"умолчание чарта ВКЛЮЧАЕТ сборку",
			func(d compositionOfferDecls) compositionOfferDecls {
				d.values = strings.ReplaceAll(d.values, "composeModel: false", "composeModel: true")
				return d
			}, "умолчанием равен"},
		{"мост не доносит сборку до процесса",
			func(d compositionOfferDecls) compositionOfferDecls {
				d.configmap = strings.ReplaceAll(d.configmap,
					"      compose-model: {{ .Values.manifests.composeModel }}\n", "")
				return d
			}, "compose-model:"},
		{"мост не доносит допуск до процесса",
			func(d compositionOfferDecls) compositionOfferDecls {
				d.configmap = strings.ReplaceAll(d.configmap,
					"      admission: {{ .Values.manifests.admission | quote }}\n", "")
				return d
			}, "admission:"},
		{"мост запекает сборку вместо чтения значений",
			func(d compositionOfferDecls) compositionOfferDecls {
				d.configmap = strings.ReplaceAll(d.configmap,
					"{{ .Values.manifests.composeModel }}", "false")
				return d
			}, ".Values.manifests.composeModel"},
		// Ключ, оставленный ТОЛЬКО в комментарии, обязан читаться как
		// отсутствующий: иначе проверка зачла бы собственное объяснение
		// проверяемого за исполнение.
		{"мост называет ключ только в комментарии",
			func(d compositionOfferDecls) compositionOfferDecls {
				d.configmap = strings.ReplaceAll(d.configmap,
					"      compose-model: {{ .Values.manifests.composeModel }}\n",
					"      # compose-model: {{ .Values.manifests.composeModel }}\n")
				return d
			}, "compose-model:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			findings, census := auditCompositionOffer(c.mut(whole))
			if len(findings) == 0 {
				t.Fatalf("снятая половина не найдена — проверка вакуумна (%s)", census)
			}
			hit := false
			for _, f := range findings {
				if strings.Contains(f, c.want) {
					hit = true
				}
				if !strings.Contains(f, iamChartDir) {
					t.Fatalf("находка не называет координату: %s", f)
				}
			}
			if !hit {
				t.Fatalf("находка не называет предмет %q: %v", c.want, findings)
			}
		})
	}
}
