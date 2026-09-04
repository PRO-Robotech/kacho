// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// validationfamilyreaders_test.go — ПРОВЕРКИ, ЧИТАЮЩЕЙ СНЯТОЕ СЕМЕЙСТВО, В ДЕРЕВЕ
// НЕТ (задача #1255, приёмка PROTO-1, полоса Л).
//
// # ПРЕДМЕТ — МОЛЧАНИЕ, А НЕ ПАДЕНИЕ
//
// Семейство `kacho.cloud.validation` снято с контрактов. Позитивная проверка
// («у объявленного обязан быть исполнитель») на исчезнувшем предмете КРАСНЕЕТ:
// её премиса «прочитано ноль объявлений» роняет прогон, и она зовёт к себе. Так
// и вышло — гейт кратности покраснел в том же прогоне и был переоснован.
//
// НЕГАТИВНАЯ проверка («этого в контракте быть не должно») ведёт себя
// противоположно: вход, на котором она находит нарушение, перестаёт быть
// представимым, ветвь остаётся в коде, счётчик её утверждений продолжает расти,
// а находка становится непроизводимой НИ ПРИ КАКОМ ВХОДЕ. Она не краснеет и не
// зовёт к себе — она замолкает, и молчание неотличимо от исправности.
//
// Такая проверка в дереве была ровно одна (анализатор формы подписки, прод-файл),
// и снятие семейства не покраснило её ничем. Этот гейт закрывает класс: следующая
// негативная проверка о снятом семействе будет названа поимённо.
//
// # ПОЧЕМУ ПРЕДИКАТ ИЩЕТ НЕ ПОДСТРОКУ
//
// Опция записывается в проверках ДВУМЯ формами, и предикат, знающий одну,
// объявил бы «ноль находок», ничего не прочитав:
//
//   - буквальной строкой — `MandatoryOption: "(required) = true"` (так она стояла
//     в конфигурации анализатора);
//   - РЕГУЛЯРНЫМ ВЫРАЖЕНИЕМ — `\(\s*required\s*\)\s*=\s*true` (так она стояла в
//     самом анализаторе). Буквальной подстроки `(required) = true` в том файле не
//     было НИ РАЗУ: `grep -c '(required) = true'` давал по нему ноль.
//
// Именно на втором сужении предикат первых редакций приёмки не видел ровно тот
// экземпляр, ради которого полоса написана. Здесь покрыты обе формы, и
// способность предиката видеть форму «опция выражением» доказана инъекцией
// (`validationfamilyreaders_injection_test.go`), а не объявлена.
//
// # ПОЧЕМУ ДОКУМЕНТАЦИЯ ВНЕ ОХВАТА
//
// Разбор снятия обязан называть снятое — иначе он непонятен, а непонятное правило
// снимают. Предмет гейта — ПРОВЕРКА, у входа которой нет производителя, а не
// упоминание. Поэтому осматривается исполняемое дерево, а `*.md`/`*.mdx`
// исключены явно, и число исключённого печатается.

// retiredFamilyOptionNames — имена снятых расширений.
var retiredFamilyOptionNames = []string{
	"required", "pattern", "value", "size", "length", "unique", "map_key", "bytes", "exactly_one",
}

// familyOptionInExecutableForm — опция семейства в ЛЮБОЙ из двух форм записи.
//
// `\\?` перед скобками — потому что в регулярном выражении скобка экранирована
// (`\(`), а в строковом литерале нет. Форма, о которой распознаватель не знает,
// не край и не редкость: всё записанное в ней оказывается вне наблюдения — не
// нарушением, а невидимостью.
//
// # ДВЕ ГРАНИЦЫ, И ОБЕ ИЗМЕРЕНЫ, А НЕ ПРЕДПОЛОЖЕНЫ
//
// Первая редакция выражения давала 8 находок, из которых 2 были ЛОЖНЫМИ — и обе
// одной формы: идентификатор `value` в позиции вызова или стрелки
// (`strings.TrimSpace(value) ==`, `.map((value) => …)`). Имя `value` — одно из
// девяти снятых расширений, и без границ выражение читало обычный код как
// объявление опции. Инструмент, у которого четверть находок ложные, перестают
// читать, а перестав читать, теряют и настоящие.
//
// Граница 1 — ЗНАК ПРИСВАИВАНИЯ ОДИНАРНЫЙ. В объявлении опции стоит `=`; `==` —
// сравнение, `=>` — стрелка. Обе ложные находки снимаются этой границей.
//
// Граница 2 — ПЕРЕД СКОБКОЙ НЕ ИМЯ. В объявлении опции скобке предшествует
// пробел, `[`, `,` или начало строки; в вызове — имя функции. RE2 не знает
// ретроспективных проверок, поэтому предшествующий знак захватывается явно.
//
// Обе границы сужают НАХОДКИ и не трогают ОХВАТ, и это свойство построения, а не
// совпадение замера: осмотренное считается ДО применения выражения, поэтому от
// его правки измениться не может. Число рядом — размер корпуса на день той
// правки (7130); сегодня он больше, потому что охват расширен с рукописного
// перечня каталогов на весь индекс, и это ДРУГАЯ ось — см. разбор у
// familySubjectPrefixes.
// Полнота перечня форм доказана инъекцией по КАЖДОЙ, включая обе ложные:
// validationfamilyreaders_injection_test.go.
var familyOptionInExecutableForm = regexp.MustCompile(
	`(?:^|[^\w$.])\\?\(\s*(?:\\s\*)?(?:` + strings.Join(retiredFamilyOptionNames, "|") +
		`)(?:\\s\*)?\s*\\?\)\s*(?:\\s\*)?=(?:[^=>]|$)`)

// contractReach — признак ОБРАЩЕНИЯ к контракту либо описателю. Опция, названная
// в файле, который контрактов не читает, проверкой о них не является.
var contractReach = regexp.MustCompile(
	`\.proto|protoreflect|GetExtension|MessageDescriptor|FileDescriptor|` +
		`readdirSync|readFileSync|filepath\.Walk|os\.ReadFile|ReadDir|glob|ProtoRoot|PROTO_ROOT`)

// familyReaderExemptions — ЕДИНСТВЕННОЕ послабление, и у него есть предмет.
//
// Инъекция, доказывающая способность этой переписи падать, обязана вносить
// снятую опцию — иначе она доказывала бы способность падать на чём-то другом.
// Поэтому файл инъекции называет опцию по построению и находкой не является.
//
// ПОСЛАБЛЕНИЕ САМОИСТЕКАЮЩЕЕ. Если названный файл перестанет нести опцию — либо
// он переписан, либо переехал, — запись теряет предмет, и это НАХОДКА, а не
// безобидный остаток: иначе она молча унаследует следующую слепую зону.
//
// Отдельно названо, чего послабление НЕ покрывает: снять находку, дописав сюда
// строку, нельзя — запись обязана нести причину, а причина здесь ровно одна и
// проверяема (файл есть инъекция ЭТОГО гейта).
var familyReaderExemptions = map[string]string{
	"internal/repohygiene/validationfamilyreaders_injection_test.go": "инъекция самой этой переписи: " +
		"вносит снятую опцию по построению, доказывая способность гейта падать",
}

// familySubjectPrefixes — ПРЕДМЕТ проверки, а не её слепая зона.
//
// `proto/` — сами контракты, `pkg/api/` — их производная. Ни то, ни другое
// проверкой о семействе не является, и у обоих есть СВОЙ судья: что семейства
// в контрактах нет, утверждает по ОПИСАТЕЛЮ
// TestValidationFamilyIsRetiredFromTheContracts. Этим исключение предмета
// отличается от исключения слепой зоны — у второго судьи нет никакого, поэтому
// число исключённого печатается рядом с осмотренным.
var familySubjectPrefixes = []string{"proto/", "pkg/api/"}

// corpusCoverage — три исхода, на которые разбивается состав индекса. Печатаются
// все три: «ноль находок» обязано быть отличимо от «ноль прочитанного».
type corpusCoverage struct {
	tracked         int
	subject         int
	unreadable      int
	unreadableNames []string
}

// partitionTreeCorpus — разбивает состав индекса на корпус · предмет · непрочтённое
// и ОТКАЗЫВАЕТСЯ отдавать корпус, из которого файлы потерялись молча.
//
// Вынесено из пробы отдельной функцией по одной причине: предпосылку охвата
// нельзя доказать, пока она стоит внутри обхода дерева. Здесь она принимает
// состав и читателя аргументами, поэтому инъекция подаёт ей потерю файла на
// синтетике и смотрит, назовёт ли она её.
func partitionTreeCorpus(
	root string,
	tracked []string,
	subjectPrefixes []string,
	read func(string) ([]byte, error),
) (map[string]string, corpusCoverage, error) {
	corpus := map[string]string{}
	cov := corpusCoverage{tracked: len(tracked)}
	for _, f := range tracked {
		rel, rerr := filepath.Rel(root, f)
		if rerr != nil {
			return nil, cov, fmt.Errorf("относительный путь %s: %w", f, rerr)
		}
		rel = filepath.ToSlash(rel)
		if slices.ContainsFunc(subjectPrefixes, func(p string) bool { return strings.HasPrefix(rel, p) }) {
			cov.subject++
			continue
		}
		body, berr := read(f)
		if berr != nil {
			// Отслеживаемый файл, которого не прочесть, — это НЕ «ноль находок»
			// по нему: он назван и посчитан, иначе пропажа была бы неотличима
			// от чистоты.
			cov.unreadable++
			cov.unreadableNames = append(cov.unreadableNames, rel+" — "+berr.Error())
			continue
		}
		corpus[rel] = string(body)
	}
	if got := len(corpus) + cov.subject + cov.unreadable; got != cov.tracked {
		return nil, cov, fmt.Errorf("не сходится с индексом: в корпусе %d + предмет %d + "+
			"не прочитано %d = %d, а отслеживается %d — значит часть дерева выпала "+
			"из наблюдения молча", len(corpus), cov.subject, cov.unreadable, got, cov.tracked)
	}
	return corpus, cov, nil
}

type familyReaderCensus struct {
	scanned    int
	skippedDoc int
	named      int
	reaching   int
	exempt     int
	findings   int
}

// auditValidationFamilyReaders — ядро гейта, отделённое от обхода дерева, чтобы
// инъекция исполнялась на синтетике и не писала в индекс репозитория.
func auditValidationFamilyReaders(corpus map[string]string) ([]string, familyReaderCensus) {
	var c familyReaderCensus
	var findings []string
	seenExempt := map[string]bool{}
	for path, body := range corpus {
		if strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".mdx") {
			c.skippedDoc++
			continue
		}
		c.scanned++
		stripped := stripLineComments(path, body)
		if !familyOptionInExecutableForm.MatchString(stripped) {
			continue
		}
		c.named++
		if !contractReach.MatchString(body) {
			continue
		}
		c.reaching++
		if _, ok := familyReaderExemptions[path]; ok {
			c.exempt++
			seenExempt[path] = true
			continue
		}
		c.findings++
		findings = append(findings, path)
	}

	// Запись, которой больше нечего исключать, — находка. Послабление живёт
	// ровно пока живёт его предмет.
	for path := range familyReaderExemptions {
		if _, inCorpus := corpus[path]; !inCorpus {
			continue // файла нет в этом корпусе — судить не о чем (инъекция)
		}
		if !seenExempt[path] {
			findings = append(findings, path+
				" — ПОСЛАБЛЕНИЕ ПОТЕРЯЛО ПРЕДМЕТ: файл больше не несёт снятой опции, "+
				"значит запись в familyReaderExemptions ничего не исключает и молча "+
				"унаследует следующую слепую зону. Снимите запись")
		}
	}
	sort.Strings(findings)
	return findings, c
}

// stripLineComments — снимает построчные комментарии, чтобы разбор снятия не
// читался как сам механизм. Двух языков достаточно: исполняемое дерево здесь —
// Go, TypeScript и оболочка, и во всех трёх комментарий строчный.
func stripLineComments(path, body string) string {
	marker := "//"
	if strings.HasSuffix(path, ".sh") || strings.HasSuffix(path, ".py") ||
		strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		marker = "#"
	}
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if i := strings.Index(l, marker); i >= 0 {
			l = l[:i]
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// TestNoCheckReadsTheRetiredValidationFamily — полоса Л-04.
func TestNoCheckReadsTheRetiredValidationFamily(t *testing.T) {
	root := repoRoot(t)

	// ОХВАТ ВЫВОДИТСЯ ИЗ ИНДЕКСА ЦЕЛИКОМ, а не выписывается перечнем каталогов.
	//
	// Здесь стоял рукописный список из девяти имён верхнего уровня. Он оставлял
	// вне охвата 89 отслеживаемых файлов — `tests/`, `.github/`, `docs/` и файлы
	// корня — и не говорил об этом ни строкой: по ним «находок ноль» означало
	// «ноль прочитанного», то есть ровно тот класс, который этот гейт и ловит,
	// этажом выше. Перечень ВКЛЮЧАЕМЫХ каталогов расходится с деревом молча:
	// первый же новый каталог верхнего уровня выпадает из наблюдения, и заметить
	// это нельзя ничем — гейт остаётся зелёным.
	//
	// Исключаются ДВА префикса, и оба — ПРЕДМЕТ проверки, а не слепая зона:
	// `proto/` — сами контракты, `pkg/api/` — их производная. Ни то, ни другое
	// проверкой о семействе не является; что семейства в контрактах нет,
	// утверждает по ОПИСАТЕЛЮ TestValidationFamilyIsRetiredFromTheContracts.
	// Разница между исключением предмета и исключением слепой зоны в том, что
	// первое имеет своего судью, а второе — никакого; поэтому число исключённого
	// печатается рядом с осмотренным.
	tracked, err := treecorpus.Under(root)
	if err != nil {
		t.Fatalf("состав дерева взять неоткуда: %v", err)
	}

	corpus, cov, cerr := partitionTreeCorpus(root, tracked, familySubjectPrefixes, os.ReadFile)
	for _, name := range cov.unreadableNames {
		t.Logf("не прочитан (в охват не вошёл): %s", name)
	}
	// ПРЕДПОСЫЛКА ОХВАТА: ни один отслеживаемый файл не потерян молча. Сумма трёх
	// исходов обязана сойтись с составом индекса — иначе сужение охвата (правкой
	// префиксов, ошибкой относительного пути) прошло бы незамеченным, а гейт
	// остался бы зелёным на меньшем дереве. Способность этой предпосылки упасть
	// доказана инъекцией, а не очевидностью арифметики:
	// TestTreeCorpusPartitionRefusesToLoseFilesSilently.
	if cerr != nil {
		t.Fatalf("охват: %v", cerr)
	}

	findings, c := auditValidationFamilyReaders(corpus)

	if c.scanned == 0 {
		t.Fatal("осмотрено НОЛЬ файлов — «находок ноль» здесь означало бы «ноль прочитанного»")
	}

	t.Logf("перепись: отслеживается %d, из них предмет проверки (%s) %d, не прочитано %d; "+
		"файлов осмотрено %d (документации пропущено %d); называют опцию семейства "+
		"в исполняемой части %d; из них обращаются к контрактам %d; освобождено %d (из %d записей); находок %d",
		cov.tracked, strings.Join(familySubjectPrefixes, ", "), cov.subject, cov.unreadable,
		c.scanned, c.skippedDoc, c.named, c.reaching, c.exempt, len(familyReaderExemptions), c.findings)

	if len(findings) > 0 {
		t.Errorf("проверок, читающих СНЯТОЕ семейство ограничений полей: %d\n  %s\n\n"+
			"У такой проверки нет производителя входа: семейство снято с контрактов "+
			"(kacho#1255), и объявить поле обязательным больше нечем. Если проверка "+
			"НЕГАТИВНА («этого быть не должно»), она не покраснеет никогда — она "+
			"замолчит, и молчание будет неотличимо от исправности.\n"+
			"Исходов два, третьего нет: снять утверждение вместе с предметом (все его "+
			"носители — поле конфигурации, образец, ветвь, фикстуры) либо перевести на "+
			"ПРОИЗВОДИМЫЙ признак и доказать способность падать инъекцией.",
			len(findings), strings.Join(findings, "\n  "))
	}
	_ = fmt.Sprint()
}
