// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// transportmessagereach.go — анализатор «транспортное сообщение, которого не
// касается ни один глагол».
//
// # Что он ищет
//
// Имя, оканчивающееся на `Request`, `Response` или `Metadata`, существует в этом
// дереве единственно ради RPC: вход глагола, его выход, либо тело опции
// `kacho.cloud.api.operation`. Такое сообщение, которого не называет ни один
// глагол и ни одно поле, вызвать нельзя НИ ПРИ КАКОМ ВХОДЕ — и при этом оно
// компилируется, попадает в стабы и публикуется в дескрипторе наравне с живым.
//
// Это класс «неисполнимая возможность» (`api-conventions.md`) на уровне
// сообщения, а не поля: контракт объявляет то, чем нельзя воспользоваться.
//
// # Почему гейт, а не внимательность на обзоре
//
// Симптома у такого сообщения нет по построению. Оно ничего не ломает, ничего не
// замедляет и не приходит ни в один прогон: вызывающего у него не существует.
// Заметить его можно только переписью, а перепись, сделанная один раз, стареет
// к следующему снятию глагола.
//
// Наблюдалось: снятие восьми методов vpc (`7675925f`) убрало глаголы, хендлеры,
// строки каталога прав, маршруты края, кейсы и пробы — и оставило ДВЕНАДЦАТЬ
// своих сообщений. Три из них потом были заведены отдельной задачей
// (PRO-Robotech/kacho#499) как «глаголы требуют идентификатор, которого нет в
// контракте»: читатель, нашедший осиротевшее сообщение, естественно принял его
// за объявление живой возможности. Именно это надгробие и вводит в заблуждение —
// не отсутствие глагола, а присутствие его формы.
//
// # Чем это отличается от соседних гейтов
//
//   - `catalogreachability.go` спрашивает «у объявленного метода есть ли
//     листенер» — он про МЕТОД и молчит, когда метода нет вовсе;
//   - `retiredrpcsurface.go` стережёт возвращение СНЯТОГО ИМЕНИ метода — он
//     молчит на имени, которое никогда не было снято, потому что и не было
//     объявлено глаголом;
//   - здесь предмет — СООБЩЕНИЕ, пережившее свой глагол либо не получившее его
//     никогда. Ни один из двух соседей этого не видит: у обоих единица счёта —
//     метод, а метода тут нет.
//
// # Почему только верхний уровень
//
// Учитываются сообщения, объявленные на верхнем уровне файла. Транспортное имя
// вложенным в этом дереве не бывает, и перепись это УТВЕРЖДАЕТ (`NestedTransport`):
// если такое появится, счётчик перестанет быть нулём и об этом будет сказано, а
// не умолчано. Так «ноль находок» остаётся отличимым от «ноль рассмотренного».
//
// # Послабления
//
// Послабление живёт, ПОКА У НЕГО ЕСТЬ ПРЕДМЕТ. Запись, которой больше нечего
// исключать, — сама находка (`stale-allowance`): иначе она унаследует следующее
// осиротевшее сообщение того же имени и сделает его невидимым. Каждое послабление
// обязано называть задачу — по записи без разрешимой ссылки нельзя установить,
// чего она ждёт.
package repohygiene

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// TransportMessageAllowance — одно послабление: сообщение, которому глагола пока
// нет по названной причине.
type TransportMessageAllowance struct {
	// Message — имя сообщения, как оно объявлено в контракте.
	Message string
	// Issue — РАЗРЕШИМАЯ ссылка на задачу (`kacho#580`). Послабление без задачи
	// не может истечь даже в принципе: нечему закрыться.
	Issue string
	// Reason — почему глагола нет сейчас.
	Reason string
}

// TransportMessageOptions — вход анализатора.
type TransportMessageOptions struct {
	// Root — корень репозитория.
	Root string
	// ProtoRoot — путь (относительно Root) к дереву исходного контракта.
	ProtoRoot string
	// Allow — перепись послаблений.
	Allow []TransportMessageAllowance
}

// TransportMessageCensus — то, что анализатор прочитал.
type TransportMessageCensus struct {
	ProtoFiles      int
	Messages        int
	TransportMsgs   int
	NestedTransport int
	TouchedNames    int
	Allowances      int
}

// TransportMessageFinding — одна находка.
type TransportMessageFinding struct {
	Kind    string // "untouched" | "stale-allowance"
	Message string
	Where   string
	Reason  string
}

func (f TransportMessageFinding) String() string {
	return f.Kind + " " + f.Message + " (" + f.Where + "): " + f.Reason
}

// Регулярные выражения НЕ привязаны к началу строки и допускают перевод строки
// внутри объявления. Обе привязки уже стоили ложных находок на этом дереве:
// объявление глагола переносится на вторую строку, а
// потоковый ответ несёт слово `stream` внутри скобок (`returns (stream Event)`),
// из-за чего живые сообщения читались как осиротевшие.
var (
	tmMessageRe = regexp.MustCompile(`\bmessage\s+(\w+)`)
	tmRPCRe     = regexp.MustCompile(
		`(?s)\brpc\s+\w+\s*\(\s*(?:stream\s+)?([\w.]+)\s*\)\s*returns\s*\(\s*(?:stream\s+)?([\w.]+)\s*\)`)
	tmOperandRe = regexp.MustCompile(`\b(?:metadata|response)\s*:\s*"([\w.]+)"`)
	// Тип поля начинается с заглавной: имена сообщений в этом дереве такие, а
	// скалярные типы (`string`, `int32`) сообщениями не бывают. Два идентификатора
	// перед `=` отсекают значения перечислений (`FOO = 1`).
	tmFieldRe   = regexp.MustCompile(`\b([A-Z][\w.]*)\s+\w+\s*=\s*\d+`)
	tmCommentRe = regexp.MustCompile(`(?m)//.*$`)
	tmBlockRe   = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

func tmIsTransport(name string) bool {
	return strings.HasSuffix(name, "Request") ||
		strings.HasSuffix(name, "Response") ||
		strings.HasSuffix(name, "Metadata")
}

// leaf возвращает последний сегмент точечного имени: в контракте одно и то же
// сообщение называют то коротко, то с пакетом.
func tmLeaf(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

// AuditTransportMessageReach читает дерево контракта и возвращает сообщения, до
// которых не дотягивается ни один глагол.
//
// Возвращает ошибку, если читать оказалось нечего: «ноль находок» на нулевом
// входе — это не вердикт, а несработавший гейт.
func AuditTransportMessageReach(
	opts TransportMessageOptions, log io.Writer,
) ([]TransportMessageFinding, TransportMessageCensus, error) {
	var census TransportMessageCensus

	protoRoot := filepath.Join(opts.Root, opts.ProtoRoot)
	var files []string
	err := filepath.WalkDir(protoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".proto") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, census, fmt.Errorf("обход дерева контракта %s: %w", protoRoot, err)
	}
	sort.Strings(files)
	census.ProtoFiles = len(files)

	declaredAt := map[string]string{} // имя сообщения верхнего уровня -> файл
	touched := map[string]bool{}      // имена, которых касается глагол или поле

	for _, path := range files {
		raw, readErr := os.ReadFile(path) // #nosec G304 -- путь получен обходом дерева контракта, не извне
		if readErr != nil {
			return nil, census, fmt.Errorf("чтение %s: %w", path, readErr)
		}
		rel, relErr := filepath.Rel(opts.Root, path)
		if relErr != nil {
			rel = path
		}
		// Комментарии снимаются ДО всего: скобка в прозе сместила бы уровень
		// вложенности на весь остаток файла, а имя, упомянутое в комментарии,
		// засчиталось бы за ссылку и скрыло бы настоящую находку.
		clean := tmBlockRe.ReplaceAllString(string(raw), "")
		clean = tmCommentRe.ReplaceAllString(clean, "")

		for _, m := range tmRPCRe.FindAllStringSubmatch(clean, -1) {
			touched[tmLeaf(m[1])] = true
			touched[tmLeaf(m[2])] = true
		}
		for _, m := range tmOperandRe.FindAllStringSubmatch(clean, -1) {
			touched[tmLeaf(m[1])] = true
		}
		for _, m := range tmFieldRe.FindAllStringSubmatch(clean, -1) {
			touched[tmLeaf(m[1])] = true
		}

		// Объявления сообщений — с учётом вложенности. Глубина считается по
		// уже вычищенному тексту, поэтому проза на неё не влияет.
		for _, loc := range tmMessageRe.FindAllStringSubmatchIndex(clean, -1) {
			name := clean[loc[2]:loc[3]]
			depth := strings.Count(clean[:loc[0]], "{") - strings.Count(clean[:loc[0]], "}")
			census.Messages++
			switch {
			case depth == 0:
				declaredAt[name] = rel
			case tmIsTransport(name):
				census.NestedTransport++
			}
		}
	}

	if census.ProtoFiles == 0 || census.Messages == 0 {
		return nil, census, fmt.Errorf(
			"читать нечего: файлов контракта %d, сообщений %d — гейт не может отличить "+
				"«ноль находок» от «ноль прочитанного»", census.ProtoFiles, census.Messages)
	}
	census.TouchedNames = len(touched)

	allow := map[string]TransportMessageAllowance{}
	for _, a := range opts.Allow {
		allow[a.Message] = a
	}
	census.Allowances = len(allow)

	var untouched []string
	for name := range declaredAt {
		if !tmIsTransport(name) {
			continue
		}
		census.TransportMsgs++
		if !touched[name] {
			untouched = append(untouched, name)
		}
	}
	sort.Strings(untouched)

	var findings []TransportMessageFinding
	excused := map[string]bool{}
	for _, name := range untouched {
		if a, ok := allow[name]; ok {
			excused[name] = true
			if log != nil {
				_, _ = fmt.Fprintf(log, "  послабление: %s — %s (%s)\n", name, a.Reason, a.Issue)
			}
			continue
		}
		findings = append(findings, TransportMessageFinding{
			Kind:    "untouched",
			Message: name,
			Where:   declaredAt[name],
			Reason: "объявлено, но не является ни входом глагола, ни его выходом, ни телом " +
				"опции operation, ни типом поля — вызвать нельзя ни при каком входе",
		})
	}

	// Послабление, которому больше нечего исключать, — находка: иначе оно
	// унаследует следующее осиротевшее сообщение того же имени.
	var stale []string
	for name := range allow {
		if !excused[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	for _, name := range stale {
		where := declaredAt[name]
		if where == "" {
			where = "сообщения нет в дереве"
		}
		findings = append(findings, TransportMessageFinding{
			Kind:    "stale-allowance",
			Message: name,
			Where:   where,
			Reason: "послаблению больше нечего исключать: глагол у сообщения появился либо само " +
				"сообщение снято — запись обязана уйти вместе со своим предметом (" + allow[name].Issue + ")",
		})
	}

	if log != nil {
		_, _ = fmt.Fprintf(log, "осмотрено: файлов контракта %d; сообщений %d (вложенных транспортных %d); "+
			"транспортных верхнего уровня %d; имён, которых касается глагол или поле, %d; послаблений %d\n",
			census.ProtoFiles, census.Messages, census.NestedTransport,
			census.TransportMsgs, census.TouchedNames, census.Allowances)
	}
	return findings, census, nil
}
