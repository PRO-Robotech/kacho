// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
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
// Обе границы сужают НАХОДКИ и не трогают ОХВАТ: число осмотренных файлов от
// них не меняется (7130 до и после) — значит сняты ложные, а не полоса дерева.
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

type familyReaderCensus struct {
	scanned    int
	skippedDoc int
	named      int
	reaching   int
	findings   int
}

// auditValidationFamilyReaders — ядро гейта, отделённое от обхода дерева, чтобы
// инъекция исполнялась на синтетике и не писала в индекс репозитория.
func auditValidationFamilyReaders(corpus map[string]string) ([]string, familyReaderCensus) {
	var c familyReaderCensus
	var findings []string
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
		c.findings++
		findings = append(findings, path)
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
	corpus := map[string]string{}

	for _, dir := range []string{"internal", "services", "gateway", "pkg", "tools", "ui-future", "deploy", "terraform", "scripts"} {
		files, err := treecorpus.Under(filepath.Join(root, dir))
		if err != nil {
			// Каталога может не быть в индексе — это не молчание гейта, а
			// отсутствие предмета, и оно называется вслух.
			t.Logf("каталог %s вне охвата: %v", dir, err)
			continue
		}
		for _, f := range files {
			rel, rerr := filepath.Rel(root, f)
			if rerr != nil {
				t.Fatalf("относительный путь %s: %v", f, rerr)
			}
			rel = filepath.ToSlash(rel)
			// Стабы — производная контрактов, а не проверка о них.
			if strings.HasPrefix(rel, "pkg/api/") {
				continue
			}
			body, berr := os.ReadFile(f)
			if berr != nil {
				continue
			}
			corpus[rel] = string(body)
		}
	}

	findings, c := auditValidationFamilyReaders(corpus)

	if c.scanned == 0 {
		t.Fatal("осмотрено НОЛЬ файлов — «находок ноль» здесь означало бы «ноль прочитанного»")
	}
	t.Logf("перепись: файлов осмотрено %d (документации пропущено %d); называют опцию семейства "+
		"в исполняемой части %d; из них обращаются к контрактам %d; находок %d",
		c.scanned, c.skippedDoc, c.named, c.reaching, c.findings)

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
