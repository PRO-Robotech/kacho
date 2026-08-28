// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// clientpage_test.go — гейт «клиентская страница называет ТО ЖЕ, что принимает
// ручка».
//
// # Предмет
//
// Всё, что нужно клиенту для вызова, до задачи #1390 лежало ВНЕ клиентской
// документации: адрес — в инженерном документе, который в сайт не входит;
// единственный пример запроса — в пробе, которую прогонщик не исполняет (её
// каталог выведен из набора). За актуальность формы запроса не отвечал НИКТО, и
// расхождение наступало бы молча: страница осталась бы прежней, а ручка приняла
// бы другой набор.
//
// Здесь предмет ровно один — СОГЛАСИЕ ДВУХ ОБЪЯВЛЕНИЙ. Гейт не судит, верна ли
// проза страницы: у «понятно написано» машинного предиката нет. Он судит то, у
// чего предикат есть: набор имён.
//
// # Почему гейт живёт ЗДЕСЬ, а не среди гейтов дерева
//
// Подлинные величины — неэкспортируемые константы этого пакета (`knownParams`,
// значения `start`, имена событий, заголовок позиции). Гейт дерева живёт под
// `internal/repohygiene`, то есть вне поддерева `gateway/`, и импортировать
// `gateway/internal/...` не может — это свойство языка. Читать их текстом
// исходника было бы вторым объявлением того же предмета: имена стоят и в
// комментариях этого файла, и сверка по подстроке краснела бы на собственном
// объяснении.
//
// Проба того же пакета берёт величины НАПРЯМУЮ. Разойдись они со страницей —
// краснеет здесь, с обоими перечнями в находке.
//
// # Что он держит В ОБЕ СТОРОНЫ
//
// Параметр, заведённый в коде и не описанный страницей, — возможность, о которой
// клиент не узнает. Параметр, описанный страницей и снятый из кода, — обещание,
// которого нет: запрос с ним получит `400`, а страница будет его советовать.
// Оба исхода находка, поэтому сверяются МНОЖЕСТВА, а не вхождение.
//
// # Проверка своей предпосылки
//
// Гейт стоит на двух фактах: страница существует и её таблица параметров
// разбирается. Исчезнет первое — гейту нечего охранять; перестанет разбираться
// вторая — он зелен всегда. Оба заявляются переписью, и пустой разбор — отказ.

// clientPageRel — путь клиентской страницы от корня дерева.
const clientPageRel = "gateway/docs/content/api/subscription.mdx"

// paramsHeading — заголовок раздела, чья таблица объявляет параметры. Гейт судит
// ИМЕННО его таблицу, а не всю страницу: имена параметров встречаются на ней и в
// примерах, и в прозе, и сверка по всей странице зеленела бы на упоминании.
const paramsHeading = "### Параметры"

// paramRowRe — первая ячейка строки таблицы параметров.
//
// Форма таблиц этого сайта — HTML в MDX, поэтому имя параметра стоит первой
// ячейкой строки: `<td><code>owner</code></td>`. Отступы и переводы строк
// допускаются — их ставит вёрстка, а не автор.
var paramRowRe = regexp.MustCompile(`<tr>\s*\n?\s*<td><code>([A-Za-z]+)</code></td>`)

// readClientPage отдаёт текст клиентской страницы.
func readClientPage(t *testing.T) string {
	t.Helper()
	// Гейт живёт в gateway/internal/subscriptionstream — до корня четыре шага.
	path := filepath.Join("..", "..", "..", clientPageRel)
	raw, err := os.ReadFile(path) // #nosec G304 -- обход собственного дерева
	if err != nil {
		t.Fatalf("клиентская страница %s не читается (%v) — гейту нечего сверять, "+
			"и его молчание не было бы утверждением о согласии величин", clientPageRel, err)
	}
	return string(raw)
}

// paramsDeclaredByPage разбирает таблицу параметров страницы.
func paramsDeclaredByPage(t *testing.T, page string) map[string]bool {
	t.Helper()
	idx := strings.Index(page, paramsHeading)
	if idx < 0 {
		t.Fatalf("на странице %s нет раздела %q — таблица параметров не найдена, "+
			"и всякий вывод гейта был бы сделан о непрочитанном", clientPageRel, paramsHeading)
	}
	section := page[idx:]
	if end := strings.Index(section, "</table>"); end >= 0 {
		section = section[:end]
	}
	out := map[string]bool{}
	for _, m := range paramRowRe.FindAllStringSubmatch(section, -1) {
		out[m[1]] = true
	}
	return out
}

// compareParamSets сводит два объявления набора параметров.
//
// Вынесена отдельно затем, чтобы доказательство способности гейта упасть
// прогоняло ТУ ЖЕ функцию, а не её пересказ: инъекция, воспроизводящая логику
// своей копией, доказывает свойство копии.
func compareParamSets(documented, accepted map[string]bool) (missing, invented []string) {
	missing = make([]string, 0, len(accepted))
	for name := range accepted {
		if !documented[name] {
			missing = append(missing, name)
		}
	}
	invented = make([]string, 0, len(documented))
	for name := range documented {
		if !accepted[name] {
			invented = append(invented, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(invented)
	return missing, invented
}

// sortedNames отдаёт имена набора в устойчивом порядке — для переписи и находок.
func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestClientPageNamesExactlyTheParametersTheHandleAccepts — согласие двух
// объявлений набора параметров.
func TestClientPageNamesExactlyTheParametersTheHandleAccepts(t *testing.T) {
	page := readClientPage(t)
	documented := paramsDeclaredByPage(t, page)
	missing, invented := compareParamSets(documented, knownParams)
	accepted := sortedNames(knownParams)

	t.Logf("перепись: ручка принимает %d %v · страница объявляет %d · не описано %d %v · описано лишнего %d %v",
		len(knownParams), accepted, len(documented), len(missing), missing, len(invented), invented)

	if len(documented) == 0 {
		t.Fatal("из таблицы параметров не разобрано ни одного имени — прочитано ноль, " +
			"и зелёное здесь было бы пустым обходом, а не согласием")
	}
	for _, name := range missing {
		t.Errorf("параметр %q ручка принимает, а клиентская страница его не описывает: "+
			"возможность есть и о ней неоткуда узнать — ровно тот исход, ради которого "+
			"страница и заведена", name)
	}
	for _, name := range invented {
		t.Errorf("клиентская страница описывает параметр %q, которого ручка НЕ принимает: "+
			"набор имён закрыт, и такой запрос получит `400` с этим именем — страница "+
			"советует то, что отвергается", name)
	}
}

// wireValue — величина, которую клиент видит на проводе и не выведет ниоткуда.
type wireValue struct {
	what string
	text string
}

// wireValues — перечень таких величин, собранный из ПОДЛИННЫХ констант ручки.
func wireValues() []wireValue {
	return []wireValue{
		{"адрес ручки", Path},
		{"значение начала «с начала журнала»", startBeginning},
		{"значение начала «с текущего конца»", startCurrentEnd},
		{"заголовок позиции", headerLastEventID},
		{"имя события открытия", fmt.Sprintf("event: %s", eventOpened)},
		{"имя события изменения", fmt.Sprintf("event: %s", eventEvent)},
	}
}

// missingWireValues отдаёт величины, которых на странице нет.
func missingWireValues(page string) []wireValue {
	out := make([]wireValue, 0, 2)
	for _, v := range wireValues() {
		if !strings.Contains(page, v.text) {
			out = append(out, v)
		}
	}
	return out
}

// TestClientPageNamesTheValuesTheHandleProduces — величины, которые клиент видит
// на проводе, обязаны быть названы страницей дословно.
//
// Они не выводимы ниоткуда: значения начала, имена событий потока и заголовок
// позиции клиент либо прочтёт здесь, либо не узнает вовсе. Сверяется вхождение, а
// не множество: страница вправе называть их и в прозе, и в примерах, а вот
// ОТСУТСТВИЕ означает, что величину неоткуда взять.
func TestClientPageNamesTheValuesTheHandleProduces(t *testing.T) {
	page := readClientPage(t)

	required := wireValues()

	missing := missingWireValues(page)
	named := len(required) - len(missing)
	for _, v := range missing {
		t.Errorf("клиентская страница не называет %s (%q): величину неоткуда взять — "+
			"ни контракт, ни перечень параметров её не выдают", v.what, v.text)
	}

	t.Logf("перепись: величин на проводе %d · названо страницей %d · длина страницы %d байт",
		len(required), named, len(page))

	if len(page) == 0 {
		t.Fatal("страница пуста — прочитано ноль")
	}
}
