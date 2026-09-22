// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// knob_producer_parity_test.go — ДВЕ КОЛОНКИ ОБ ОДНОЙ РУЧКЕ: что объявляет
// ПРОЦЕСС и что эмитирует ЧАРТ.
//
// # Чего не хватало, пока этого файла не было
//
// Ведомость переименования ручек (`gateway/internal/config`, читает её
// `f1d_issuer_address_is_never_derived_test.go`) объявляет предикат ДВОЙНЫМ:
// старого имени не объявляет ни одно поле, новое объявлено ровно одним. Обе
// половины судят ОДНУ сторону — сторону ЧИТАТЕЛЯ. Сторону ПРОИЗВОДИТЕЛЯ не
// судила ни одна проверка в дереве, и переименование, доведённое до объявления
// процесса и не доведённое до шаблона, оставалось зелёным.
//
// Соседний гейт (`internal/repohygiene`, TestDeclaredKnobHasAReader) спрашивает
// обратное — «у ключа профиля есть читатель в шаблоне?» — и на этом дефекте
// честно печатает ноль: у ключа профиля, адресующего секрет связки, читатель в
// шаблоне ЕСТЬ, шаблон просто эмитит ПРЕЖНЕЕ имя переменной. Класс «процесс
// объявил имя, которого не эмитит ни один шаблон» держателя не имел вовсе.
//
// # Почему это не косметика
//
// По хопу за ключами верификации едет материал, которым край проверяет ПОДПИСЬ
// каждого предъявителя. Профиль пинит якорь, том монтируется, файл на диске
// лежит — а процесс читает ДРУГОЕ имя и получает пусто. Старт не отказывает:
// пустое значение законно по замыслу («якоря нет, транспорт по умолчанию»).
// Дальше либо зеркало набора ключей отдано под внутренним сертификатом — и
// рукопожатие не проходит, а слой интроспекции считает это постоянной
// неверной настройкой и отвергает КАЖДОГО предъявителя; либо зеркало на
// публичном сертификате — и закрепление якоря снято молча, а подменивший набор
// ключей подменяет решение о доступе.
//
// # Что судится и чем ограничено
//
//	(1) НАЗАД: имя, которое эмитит шаблон чарта КРАЯ, обязано быть объявлено
//	    полем настроек края. Иначе оператор заполняет значение, видит его в
//	    поде и получает поведение по умолчанию;
//	(2) ВПЕРЁД: для КАЖДОЙ записи ведомости переименования — новое имя обязано
//	    эмитироваться хотя бы одним шаблоном ДЕРЕВА, а старое — ни одним.
//
// Вперёд судится ведомость, а не все 95 объявленных имён: ручка с рабочим
// умолчанием, которую ни один профиль не задаёт, — законное состояние, и
// требование «эмитируй каждую» объявило бы находкой полтора десятка живых
// умолчаний. Ведомость же несёт ровно те имена, про которые СКАЗАНО, что
// предмет жив и переехал, — и половина производителя проверяема без ведомости
// исключений.
//
// # Читает ОБЪЯВЛЕНИЯ, а не рендер
//
// Тот же приём, что у соседей по каталогу: разбирается исходник шаблона, а не
// вывод helm. Проба, умеющая пропуститься из-за ненайденного helm, гейтом не
// является. Исполняемая часть от комментария отделяется — шаблон полон прозы,
// объясняющей в том числе СНЯТЫЕ переменные, и предикат по сырому тексту
// объявил бы находкой ровно ту запись, которая сообщает, что находки больше нет.
package deploy_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kelseyhightower/envconfig"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// knobEnvDecl — ОБЪЯВЛЕНИЕ переменной в шаблоне: элемент списка `env:` вида
// `- name: KACHO_…`. Строка, начинающаяся с решётки, отбрасывается: комментарий
// не исполняется.
var knobEnvDecl = regexp.MustCompile(`(?m)^[^#\n]*-\s*name:\s*([A-Z][A-Z0-9_]+)`)

// knobProductPrefixes — приставки имён окружения ЭТОГО продукта.
//
// Нужны затем, что envconfig консультирует для вложенного поля ДВА имени:
// выведенное из иерархии (`KACHO_API_GATEWAY_ADMISSION_PUBLIC_IN_FLIGHT`) и
// абсолютное из тега вложенного поля (`IN_FLIGHT`). Второе — законный вход
// процесса, но ручкой продукта не является и в чарте не эмитируется никогда.
var knobProductPrefixes = []string{"KACHO_", "KANAME_"}

func knobIsProductName(name string) bool {
	for _, p := range knobProductPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// knobEmitterDebtEntry — эмиссия, у которой нет читателя и которая ещё не
// закрыта фиксом.
//
// # Почему перечень БЕРЁТСЯ У ПРОЦЕССА, а не выписан здесь
//
// Четыре сегодняшние записи — производители ручек, СНЯТЫХ ЭТОЙ ЖЕ ПОЛОСОЙ со
// стороны читателя. Выписать их здесь вторым литералом значило бы завести
// третий словарь об одном предмете: он разошёлся бы с ведомостью процесса
// молча — и разошёлся бы тот, где дефект ещё не нашли. Поэтому известной
// считается эмиссия имени, которое СОБСТВЕННАЯ ведомость процесса
// (`config.RetiredKnobs()`) называет снятым.
//
// # Это не маска, и вот чем
//
// Перечень ЗАКРЫТ и живёт у процесса: имя, которого в нём нет, остаётся свежей
// находкой — включая опечатку и ручку, которую не объявлял никто. Обоснование
// у каждой записи там же, рядом с именем.
//
// # Предикат снятия — наблюдаемый, и записи НЕЧЕМ пережить свой предмет
//
// Ведомость ВЫВОДИТСЯ пересечением: снятая ручка × имя, которое чарт края ещё
// эмитит. Перестал эмитить — запись исчезает сама, потому что её никто не
// писал; исключению нечего пережить by construction. Перепись печатает ОБА
// числа: снятых ручек в ведомости процесса и сколько из них ещё эмитируется —
// одно число скрыло бы ровно тот случай, ради которого ведомость заведена.
//
// Снять сами эмиссии заодно нельзя: за ними стоят три живых гейта
// развёртывания и ключи боевых профилей, которые эти гейты судят, — отдельный
// предмет со своим прогоном.
type knobEmitterDebtEntry struct {
	Knob string
	Why  string
}

// knobEmitterDebt — ведомость, ВЫВЕДЕННАЯ пересечением: снятая ручка процесса ×
// имя, которое чарт ещё эмитит.
func knobEmitterDebt(retired map[string]string, emitted map[string][]string) []knobEmitterDebtEntry {
	out := make([]knobEmitterDebtEntry, 0, len(retired))
	for knob, why := range retired {
		if _, still := emitted[knob]; !still {
			continue
		}
		out = append(out, knobEmitterDebtEntry{Knob: knob, Why: why})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Knob < out[j].Knob })
	return out
}

func knobEmitterDebtDefects(entries []knobEmitterDebtEntry) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range entries {
		if strings.TrimSpace(d.Knob) == "" {
			out = append(out, fmt.Sprintf("запись без координаты: %+v", d))
			continue
		}
		if seen[d.Knob] {
			out = append(out, "дубль в ведомости: "+d.Knob)
		}
		seen[d.Knob] = true
		if strings.TrimSpace(d.Why) == "" {
			out = append(out, d.Knob+" — запись без письменного обоснования: неотличима от "+
				"упущения, и снять её потом будет не по чему")
		}
	}
	sort.Strings(out)
	return out
}

// knobColumns — ДВЕ КОЛОНКИ и объём осмотренного.
type knobColumns struct {
	// Declared — имена, которые консультирует загрузчик настроек края.
	Declared map[string]bool
	// EmittedByEdge — имя → координаты в шаблонах чарта КРАЯ.
	EmittedByEdge map[string][]string
	// EmittedInTree — имя → координаты в шаблонах ЛЮБОГО чарта дерева.
	EmittedInTree map[string][]string
	// TemplateFiles — сколько файлов шаблонов прочитано по дереву.
	TemplateFiles int
	// EdgeTemplateFiles — из них у чарта края.
	EdgeTemplateFiles int
}

func (c knobColumns) String() string {
	return fmt.Sprintf(
		"перепись: ОБЪЯВЛЕНО процессом края %d · ЭМИТИРУЕТ чарт края %d · "+
			"эмитируется по дереву %d; прочитано файлов шаблонов %d (из них у края %d)",
		len(c.Declared), len(c.EmittedByEdge), len(c.EmittedInTree),
		c.TemplateFiles, c.EdgeTemplateFiles)
}

// declaredEdgeKnobs — имена, которые РЕАЛЬНО консультирует загрузчик.
//
// Берётся из САМОГО загрузчика (envconfig по структуре Config с тем же
// префиксом, что и в Load), а не из списка в пробе: список разъехался бы с
// кодом ровно так же, как разъехался чарт. Учитываются ОБА имени, которые
// envconfig принимает для поля, — выведенное из иерархии (Key) и абсолютное из
// тега (Alt), — иначе живая переменная была бы объявлена мёртвой.
func declaredEdgeKnobs(t *testing.T) map[string]bool {
	t.Helper()
	var buf bytes.Buffer
	if err := envconfig.Usagef("", &config.Config{}, &buf,
		"{{range .}}{{.Key}}\n{{.Alt}}\n{{end}}"); err != nil {
		t.Fatalf("перечисление имён настроек не выполнено: %v", err)
	}
	out := map[string]bool{}
	for _, line := range strings.Split(buf.String(), "\n") {
		name := strings.TrimSpace(line)
		if name != "" && knobIsProductName(name) {
			out[name] = true
		}
	}
	return out
}

// emittedKnobs обходит шаблоны чартов дерева и собирает эмитируемые имена.
func emittedKnobs(t *testing.T, root string) (inTree map[string][]string, files int) {
	t.Helper()
	inTree = map[string][]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		// Ведущая косая добавляется намеренно: обход может начинаться С САМОГО
		// каталога шаблонов, и тогда путь относителен ему — без неё предикат
		// объявил бы «шаблонов не найдено» там, где их прочитано сто.
		if !strings.Contains("/"+filepath.ToSlash(path), "/templates/") {
			return nil
		}
		switch filepath.Ext(path) {
		case ".yaml", ".yml", ".tpl":
		default:
			return nil
		}
		raw, readErr := os.ReadFile(path) // #nosec G304 -- путь получен обходом дерева
		if readErr != nil {
			return readErr
		}
		files++
		for lineNo, line := range strings.Split(string(raw), "\n") {
			for _, m := range knobEnvDecl.FindAllStringSubmatch(line, -1) {
				if !knobIsProductName(m[1]) {
					continue
				}
				inTree[m[1]] = append(inTree[m[1]],
					fmt.Sprintf("%s:%d", filepath.ToSlash(path), lineNo+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход шаблонов не выполнен (%v) — посылка проверки исчезла, "+
			"а это НЕ то же самое, что «находок ноль»", err)
	}
	return inTree, files
}

// readKnobColumns собирает обе колонки.
func readKnobColumns(t *testing.T) knobColumns {
	t.Helper()
	cols := knobColumns{Declared: declaredEdgeKnobs(t)}
	cols.EmittedInTree, cols.TemplateFiles = emittedKnobs(t, filepath.Join("..", ".."))
	cols.EmittedByEdge, cols.EdgeTemplateFiles = emittedKnobs(t, "templates")
	return cols
}

// judgeEmittedWithoutReader — НАЗАД: чарт края эмитит имя, которого не
// объявляет ни одно поле настроек.
//
// Вынесено отдельной функцией, чтобы инъекция звала ТО ЖЕ, что исполняется на
// дереве: своя копия предиката в инъекции разошлась бы с настоящей пробой молча.
func judgeEmittedWithoutReader(cols knobColumns, debt []knobEmitterDebtEntry) (fresh, known []string) {
	inDebt := map[string]bool{}
	for _, d := range debt {
		inDebt[d.Knob] = true
	}
	for name, where := range cols.EmittedByEdge {
		if cols.Declared[name] {
			continue
		}
		sort.Strings(where)
		line := fmt.Sprintf("%s — эмитируется чартом края (%s), а поле настроек его не "+
			"объявляет: оператор заполняет значение, видит его в поде и получает поведение "+
			"по умолчанию", name, strings.Join(where, ", "))
		if inDebt[name] {
			known = append(known, line)
			continue
		}
		fresh = append(fresh, line)
	}
	sort.Strings(fresh)
	sort.Strings(known)
	return fresh, known
}

// judgeRenamedKnobProducers — ВПЕРЁД: у каждой записи ведомости переименования
// новое имя эмитируется хотя бы одним шаблоном дерева, старое — ни одним.
func judgeRenamedKnobProducers(renamed map[string]string, cols knobColumns) []string {
	var out []string
	for retired, live := range renamed {
		if where, still := cols.EmittedInTree[retired]; still {
			sort.Strings(where)
			out = append(out, fmt.Sprintf(
				"%s — СНЯТОЕ имя всё ещё эмитируется шаблоном (%s); процесс читает %s и "+
					"получает пусто, а пустое значение у этой ручки законно по замыслу, "+
					"поэтому старт НЕ отказывает и защита снимается молча",
				retired, strings.Join(where, ", "), live))
		}
		if _, emitted := cols.EmittedInTree[live]; !emitted {
			out = append(out, fmt.Sprintf(
				"%s — предмет объявлен ЖИВЫМ под этим именем, но его не эмитит НИ ОДИН "+
					"шаблон дерева: читатель переехал, производитель остался на прежнем "+
					"имени %s, и ведомость переименования судит только сторону читателя",
				live, retired))
		}
	}
	sort.Strings(out)
	return out
}

// TestEdgeChartEmitsNoKnobTheProcessNeverReads — НАЗАД.
func TestEdgeChartEmitsNoKnobTheProcessNeverReads(t *testing.T) {
	t.Parallel()
	cols := readKnobColumns(t)
	t.Log(cols.String())
	retired := config.RetiredKnobs()
	debt := knobEmitterDebt(retired, cols.EmittedByEdge)
	t.Logf("перепись ведомости: снятых ручек объявлено процессом %d · из них чарт края "+
		"ещё эмитит %d", len(retired), len(debt))

	if len(cols.Declared) == 0 {
		t.Fatal("процесс края не объявляет ни одного имени с приставкой продукта — " +
			"перечисление ничего не прочитало, и его молчание не является утверждением")
	}
	if cols.EdgeTemplateFiles == 0 {
		t.Fatal("прочитано ноль файлов шаблонов чарта края — эмиссию искать было негде")
	}
	if len(cols.EmittedByEdge) == 0 {
		t.Fatal("чарт края не эмитит ни одного имени с приставкой продукта — обход " +
			"прочитал не то, и «находок ноль» означало бы «прочитано ноль»")
	}

	for _, d := range knobEmitterDebtDefects(debt) {
		t.Error("ведомость: " + d)
	}

	fresh, known := judgeEmittedWithoutReader(cols, debt)
	for _, k := range known {
		t.Logf("  ведомость: %s", k)
	}
	for _, f := range fresh {
		t.Error(f)
	}

	// ПРЕДПОСЫЛКА ведомости: ведомость снятых ручек у процесса непуста. Пустая
	// означала бы, что известной может стать ЛЮБАЯ эмиссия — и ведомость
	// перестала бы отличать снятое от неизвестного.
	if len(retired) == 0 {
		t.Fatal("ведомость снятых ручек процесса пуста — известной не может стать ни " +
			"одна эмиссия, и разделение на свежие и известные ничего не значит")
	}
}

// TestRenamedKnobHasItsProducerMoved — ВПЕРЁД.
//
// На ПУСТОЙ ведомости переименования проба проходит, объявляя перепись: проба
// не имеет права падать на достижении своей цели.
func TestRenamedKnobHasItsProducerMoved(t *testing.T) {
	t.Parallel()
	renamed := config.RenamedKnobs()
	cols := readKnobColumns(t)
	t.Log(cols.String())
	t.Logf("перепись: записей ведомости переименования %d", len(renamed))

	if cols.TemplateFiles == 0 {
		t.Fatal("прочитано ноль файлов шаблонов по дереву — «ни один шаблон не эмитит» " +
			"было бы сказано обо всём дереве сразу")
	}
	if len(cols.EmittedInTree) == 0 {
		t.Fatal("по дереву не найдено ни одного эмитируемого имени продукта — обход " +
			"прочитал не то")
	}

	for _, f := range judgeRenamedKnobProducers(renamed, cols) {
		t.Error(f)
	}
}
