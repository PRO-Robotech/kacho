// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ct2_registry_provider_edge_denial.go — анализатор «провайдер не высказывается о
// крае неправду».
//
// # Предмет
//
// Описание атрибута и комментарий провайдера — клиентская поверхность: их читают в
// справочнике и в подсказке редактора. Утверждение о ЧУЖОЙ поверхности, записанное
// здесь прозой, переживает свой предмет молча — ни сборка, ни линтер, ни пробы
// провайдера о крае ничего не знают.
//
// Наблюдалось (#1646): описание имени репозитория утверждало «Переименования у
// края нет», при том что `RenameRepository` объявлен контрактом, реализован,
// публично маршрутизируется и покрыт кейсами. Строка выдавала свойство ПРОВАЙДЕРА
// (он пересоздаёт ресурс) за свойство ПЛАТФОРМЫ. Цена — в необратимости:
// поверивший ей не станет искать переименование и снесёт репозиторий вместе со
// всем содержимым.
//
// # Почему он заведён третьим к TestClientDocsDoNotDenyAMechanismThatExists
//
// Тот судит клиентские СТРАНИЦЫ (`.mdx`) и знает один механизм — подписку.
// Провайдер в его корпус не входит ни при каком тексте: его утверждения живут в
// Go-коде и в модулях `.tf`. Половина предмета оставалась незакрытой, и закрыть её
// расширением того гейта нельзя — у него другой корпус и другой признак
// существования механизма.
//
// # Что судится — ПРЕДМЕТ утверждения, а не его форма
//
// Лексикон над естественным языком не отличает законного близнеца от находки, если
// различать по форме отрицания: «краем не поддержан» бывает и правдой. Поэтому
// решает РЕЗОЛВ В КОНТРАКТ ТОГО ДОМЕНА, о котором говорит файл, и вердикт даёт
// дерево, а не словарь:
//
//	отрицание + глагол, который в контракте домена ЕСТЬ   → находка (случай #1646)
//	утверждение + глагола, которого в контракте домена НЕТ → находка (обратная сторона)
//	то и другое при верном контракте                       → молчание (законный близнец)
//
// **Домен решает всё, и это не украшение.** `Move` объявлен в контракте
// балансировщика и НЕ объявлен у vpc, iam и registry. Поэтому «операции переноса у
// края не существует» в файле группы безопасности — правда, а та же фраза в файле
// целевой группы была бы ложью. Гейт, резолвящий по всему дереву контрактов сразу,
// объявил бы находкой шесть исправных утверждений.
//
// Судится ПРЕДЛОЖЕНИЕ, а не файл: иначе отрицание из одного абзаца встретилось бы с
// глаголом из другого и дало бы находку, которой никто не писал. Перенос строки
// внутри абзаца при этом СНИМАЕТСЯ: проза переносится по ширине, и утверждение
// свободно разрывается посередине — деление по строкам теряло бы ровно те
// утверждения, что не поместились в строку.
//
// # ТРИ способа назвать предмет, и все три нужны
//
//  1. **Токеном действия** — `:start`, `:add-routes` в обратных кавычках. Точный
//     резолв в REST-привязки контракта, морфологии не требует, и потому МАРКЕРА НЕ
//     ТРЕБУЕТ ТОЖЕ: назвать несуществующее действие можно и без слов «у края есть».
//     Наблюдалось: провайдер трижды писал «у контракта есть `:add-routes`,
//     `:remove-routes` и `:update-route` — и все три отвечают 501», тогда как vpc не
//     объявляет ни одного из трёх. Требуй здесь маркера — и находка осталась бы вне
//     наблюдения, потому что «у контракта есть» в закрытый набор форм не входит.
//     Правило поэтому такое: названный токен обязан существовать, ЕСЛИ предложение
//     его не отрицает.
//  2. **Службой с методом** — `TargetGroupService/AddTargets` (#1728). Тот же точный
//     резолв и то же правило, что у токена действия. Форма квалифицированная, потому
//     что ГОЛОЕ имя RPC объекта не несёт: `rpc Update` объявлен в шести доменах из
//     шести, и резолв по нему отвечал бы «есть» на любой глагол любого ресурса.
//     Объект живёт в объемлющей службе — она и решает.
//  3. **Отглагольным именем** — «переименование», «перенос». Требует словаря, и
//     словарь узок НАМЕРЕННО (см. ниже).
//
// # Родовое имя действия — НАХОДКА, и здесь стояло обратное (#1728)
//
// Прежняя редакция объявляла «обновление», «удаление», «операцию» и «действие»
// вечно нерезолвящимися и считала их ПЕРЕПИСЬЮ: «видны числом, а не выглядят
// проверенными». Половина довода верна и осталась — резолвить их словарём нельзя, и
// это перемерено: `rpc Update` объявлен в шести доменах из шести (10 · 10 · 3 · 4 ·
// 1 · 5 объявлений), поэтому запись «обновление → Update*» объявила бы находкой ВСЕ
// пять живых отрицаний дерева разом. Вторая половина — вывод «значит считаем
// переписью» — неверна: утверждение, которое не сверяется ни в одну сторону, ничем
// не лучше непроверенного, а выглядит проверенным ровно так же.
//
// Предмет закрывается ТРЕБОВАНИЕМ ФОРМЫ, а не морфологией: действие называют
// токеном (способ 1 либо 2), и проверяемым утверждение становится У АВТОРА. Поэтому
// маркер края плюс родовое имя действия плюс НИ ОДНОГО токена — находка третьего
// вида: «предмет не назван тем, чем край его называет».
//
// Что это дало на дереве: резолвится 16 → 21 из 32, отглагольных без объекта
// 5 → 0. Оставшиеся одиннадцать — предмет ДРУГОЙ (поле, значение, ветка — девять; и
// файл без выводимого домена — два), и они по-прежнему перепись.
//
// # Чего он НЕ судит, и это названо, а не умолчано
//
//  1. **Предмет, который действием не является вовсе.** «Полудиапазона у края нет»,
//     «строка у края есть» — речь о значении и о ресурсе, а не о глаголе; токен
//     здесь назвать нечего, и требовать его значило бы краснеть на законном.
//     Такие утверждения считаются переписью как нерезолвящиеся.
//  2. **Файлы без домена.** Общая оболочка (`flat.go`, `datasources.go`) стабов не
//     импортирует, домена у неё нет, и её утверждения о крае резолвить не с чем.
//     Тоже считаются переписью.
//  3. **Утверждения вне закрытого набора форм.** Автор, написавший отрицание иначе,
//     останется вне наблюдения. Перепись печатает, сколько предложений осмотрено и
//     сколько утверждений опознано, поэтому «ноль находок» отличимо от «ноль
//     прочитанного».
//  4. **Прочие поверхности.** Корпус — провайдер (`.go`) и его модули (`.tf`).
//     Клиентские страницы судит соседний гейт, страницы документации сервисов — свой.
//
// # Следствие для того, кто СНИМАЕТ ложное утверждение
//
// Названный в прозе токен читается как утверждение о крае — значит цитата снятого
// адреса внутри объяснения становится НОВОЙ находкой вместо снятой. Отзыв пишется
// без воспроизведения мёртвых имён: «перечислялись три суффикс-действия» вместо их
// перечня. Это не причуда гейта, а та же практика, которой корпус правил не
// воспроизводит мёртвые координаты в обратных кавычках.
//
// # Чем истекает
//
// ОТ ФАКТА В ДЕРЕВЕ. Пропадёт из контрактов глагол, к которому резолвится хоть одна
// запись словаря, — гейт падает ПРЕДПОСЫЛКОЙ, а не молчит: отрицания стали бы
// правдой, и их надо перечитать, а не оставить под мёртвым запретом.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// edgeDenialMarkers — закрытый набор форм ОТРИЦАНИЯ края.
//
// Набор именно закрытый, а не эвристика: каждая форма доказана инъекцией, а всё,
// чего в нём нет, честно объявлено вне наблюдения (см. шапку, п. 3).
var edgeDenialMarkers = []string{
	"у края нет",
	"у края отсутству",
	"у края не существует",
	"краем не поддерж",
	"край не поддерж",
	"край не умеет",
}

// edgeAffirmationMarkers — закрытый набор форм УТВЕРЖДЕНИЯ о крае.
//
// Обратная сторона того же класса: утверждение о возможности тоже стареет, и
// стареет тише — оно не мешает работать, пока клиент по нему не пойдёт.
var edgeAffirmationMarkers = []string{
	"у края есть",
	"край поддерж",
	"край умеет",
}

// edgeVerbNouns — отглагольное имя в прозе → префиксы имён RPC, которыми это
// действие может быть объявлено в контракте.
//
// Префиксов у записи НЕСКОЛЬКО намеренно: отрицание утверждает, что действия нет
// ВООБЩЕ, поэтому опровергает его любой глагол, который его выражает. Точное имя
// RPC знать не требуется — требуется знать, что ни один его не несёт.
//
// Словарь узок по причине, названной в шапке (п. 1): имя, не называющее объект
// действия, сюда не попадает.
var edgeVerbNouns = map[string][]string{
	"переименован": {"Rename"},
	"перенос":      {"Move", "Transfer", "Relocate"},
	"перенест":     {"Move", "Transfer", "Relocate"},
	"перенесен":    {"Move", "Transfer", "Relocate"},
	"перенесён":    {"Move", "Transfer", "Relocate"},
}

// edgeActionTokenRe — токен действия в обратных кавычках: `:start`, `:add-routes`.
//
// Дефис в наборе НЕ факультативен: обе формы законны и обе живут в дереве
// (`:addCidrBlocks` и `:add-cidr-blocks` объявлены одним контрактом). Первая
// редакция дефис исключала — и пропускала настоящую находку молча, ни красным, ни
// зелёным (testing.md §«Распознаватель обязан знать ВСЕ законные формы»).
var edgeActionTokenRe = regexp.MustCompile("`(:[a-zA-Z][A-Za-z0-9-]*)`")

// edgeMethodTokenRe — метод вместе со СЛУЖБОЙ в обратных кавычках:
// `TargetGroupService/AddTargets`.
//
// # Почему форма квалифицированная, а не голое имя RPC
//
// Голое имя объекта не несёт (см. [EdgeContract.Methods]), поэтому резолв по нему
// отвечал бы «есть» на каждый глагол каждого домена. Квалифицированная форма несёт
// объект службой — тем самым, чем этот класс и решается.
//
// # Почему не любое имя в обратных кавычках
//
// Перепись прозы провайдера на день заведения: слов вида `Xxx` в обратных кавычках
// **16**, и **семь** из них именами RPC не являются вовсе (`StaticRoute`,
// `Required`, `Host`, `MetadataString`, `InvalidArgument`, `ConfirmAbsence` и имя
// пробы). Распознаватель по голому имени объявил бы находкой каждое из них.
// Квалифицированных вхождений в тот же день было **ноль** — то есть форма ничего
// не ломает и коллизий не имеет by construction: косая черта внутри обратных
// кавычек в прозе провайдера означает «служба края», и ничего другого.
var edgeMethodTokenRe = regexp.MustCompile("`([A-Z][A-Za-z0-9]*)/([A-Z][A-Za-z0-9]*)`")

// edgeGenericActionNouns — РОДОВЫЕ имена действия: те, что называют действие, но не
// называют, КАКОЕ.
//
// Отличаются от [edgeVerbNouns] ровно этим. «Переименование» и «перенос» объект
// несут в себе — у домена такой глагол один, и потому они резолвятся. «Операция»,
// «действие», «обновление» не несут: у домена их столько же, сколько ресурсов.
//
// Резолвить их «глаголом + объектом» НЕЛЬЗЯ, и это измерено, а не предположено:
//
//   - имя RPC объекта не несёт (`rpc Update` — в шести доменах из шести), поэтому
//     запись «обновление → Update*» ответила бы «есть» на ВСЕ пять живых отрицаний
//     разом и превратила бы их в находки;
//   - у двух из пяти объектом названо ПОЛЕ («изменяющей операции для этого поля у
//     края нет»), а у поля глагола нет by construction — такое утверждение не
//     резолвится ни при какой морфологии.
//
// Поэтому предмет закрывается ТРЕБОВАНИЕМ ФОРМЫ: действие называют токеном, и
// проверяемым утверждение становится у АВТОРА, а не у распознавателя.
var edgeGenericActionNouns = []string{
	"операц", "действи", "обновлен", "изменен", "удален",
	"создан", "снятие", "снятия", "добавлен", "замен", "правк",
}

// edgeGenericNounWindow — сколько слов ПЕРЕД маркером края образуют предмет
// утверждения.
//
// Окно узкое намеренно: родовое имя, стоящее в предложении ПОСЛЕ маркера либо в
// соседнем придаточном, предметом утверждения не является. Живой близнец из дерева —
// «…означает, что привязка у края есть, — то самое расхождение…, а не удаление»:
// здесь «удаление» отстоит от маркера на девять слов И стоит после него, а предмет
// утверждения — «привязка». Предикат без окна объявил бы это находкой.
const edgeGenericNounWindow = 5

// EdgeContract — то, что домен объявляет: имена RPC и REST-токены действий.
type EdgeContract struct {
	// Domain — каталог контракта (`registry`, `loadbalancer`, …).
	Domain string
	// RPCs — имена объявленных RPC.
	RPCs map[string]bool
	// Actions — суффикс-действия REST-привязок (`:start`), с двоеточием.
	Actions map[string]bool
	// Methods — метод вместе со СЛУЖБОЙ, которой он принадлежит
	// (`TargetGroupService/AddTargets`).
	//
	// Отдельно от [EdgeContract.RPCs], и это несущее. Имя RPC в этом дереве
	// объекта НЕ несёт: `rpc Update` объявлен в шести доменах из шести (10 · 10 ·
	// 3 · 4 · 1 · 5 объявлений), а объект живёт в объемлющей службе. Поэтому
	// «глагол» резолвится сразу во все ресурсы домена, а «служба + глагол» — в
	// один, и различает их именно служба.
	Methods map[string]bool
}

// EdgeSource — один файл корпуса вместе с доменом, о котором он говорит.
type EdgeSource struct {
	// Path — путь от корня дерева (для координаты находки).
	Path string
	// Text — исходник.
	Text string
	// Kind — "go" либо "tf": от него зависит, как из файла достаётся проза.
	Kind string
	// Domain — каталог контракта, выведенный из файла; пусто — домен не выведен,
	// и утверждения такого файла резолвить не с чем.
	Domain string
}

// EdgeClaimFinding — одно утверждение провайдера о крае, разошедшееся с контрактом.
type EdgeClaimFinding struct {
	File     string
	Line     int
	Sentence string
	Domain   string
	// Subject — как предмет был назван: отглагольное имя либо токен действия.
	Subject string
	// Expected — чем предмет опровергнут: имя RPC либо токен из контракта.
	Expected string
	// Affirmative: true — провайдер утверждает предмет, которого нет; false —
	// отрицает предмет, который есть.
	Affirmative bool
	// Unnamed: true — предмет утверждения назван РОДОВЫМ именем действия и ни
	// одним токеном не назван. Вид находки третий, и он про другое: не «предмет
	// разошёлся с контрактом», а «предмет не назван тем, чем край его называет»,
	// поэтому сверить утверждение нельзя ни в одну сторону.
	Unnamed bool
}

// String — текст находки. Называет КООРДИНАТУ, домен, предмет и то, чем он
// опровергнут: находка, не называющая, ЧЕМ она доказана, посылает читателя искать
// не там.
func (f EdgeClaimFinding) String() string {
	if f.Unnamed {
		return fmt.Sprintf("%s:%d: утверждение о крае названо родовым именем %q и не названо "+
			"токеном, поэтому не сверяется с контрактом домена %s ни в одну сторону; назовите "+
			"предмет так, как называет его край — суффикс-действием (`:start`) либо службой с "+
			"методом (`TargetGroupService/AddTargets`): %q",
			f.File, f.Line, f.Subject, f.Domain, f.Sentence)
	}
	if f.Affirmative {
		return fmt.Sprintf("%s:%d: провайдер утверждает %q, но в контракте домена %s этого НЕТ (искали %s): %q",
			f.File, f.Line, f.Subject, f.Domain, f.Expected, f.Sentence)
	}
	return fmt.Sprintf("%s:%d: провайдер отрицает %q, а в контракте домена %s это ЕСТЬ (%s): %q",
		f.File, f.Line, f.Subject, f.Domain, f.Expected, f.Sentence)
}

// EdgeClaimNote — одно опознанное утверждение о крае вместе с тем, удалось ли его
// сверить с контрактом.
//
// Список существует затем, чтобы нерезолвящееся утверждение было видно ПОИМЁННО, а
// не только числом: число говорит «часть предмета не осмотрена», имя — какая именно.
// Без имён слепая зона выглядит проверенной ровно так же, как проверенная часть.
type EdgeClaimNote struct {
	File     string
	Line     int
	Domain   string
	Sentence string
	Resolved bool
}

// String — заметка одной строкой.
func (n EdgeClaimNote) String() string {
	domain := n.Domain
	if domain == "" {
		domain = "домен не выведен"
	}
	state := "НЕ резолвится"
	if n.Resolved {
		state = "резолвится"
	}
	return fmt.Sprintf("%s:%d [%s, %s]: %q", n.File, n.Line, domain, state, n.Sentence)
}

// EdgeClaimCensus — объём осмотренного. Печатается всегда, чтобы «ноль находок»
// было отличимо от «ноль прочитанного», а расширение анализатора — от холостого.
type EdgeClaimCensus struct {
	Files       int
	Texts       int
	Sentences   int
	Claims      int
	Resolved    int
	Resolutions int
	Dictionary  int
	Domains     int
	RPCs        int
}

// String — перепись одной строкой.
//
// Величин две, а не одна, и это несущее: «утверждений о крае» показывает, сколько
// предмет вообще ОСМОТРЕН, «резолвится» — сколько из них удалось сверить с
// контрактом. Одно число скрыло бы ровно ту разницу, ради которой словарь и
// расширяют.
func (c EdgeClaimCensus) String() string {
	return fmt.Sprintf("перепись: файлов %d · текстовых узлов %d · предложений %d · "+
		"утверждений о крае %d (резолвится %d, сверок %d) · записей словаря %d · доменов %d · глаголов контрактов %d",
		c.Files, c.Texts, c.Sentences, c.Claims, c.Resolved, c.Resolutions, c.Dictionary, c.Domains, c.RPCs)
}

// ScanProviderEdgeClaims — сверяет каждое утверждение провайдера о крае с
// контрактом того домена, о котором говорит файл.
func ScanProviderEdgeClaims(sources []EdgeSource, contracts map[string]EdgeContract) ([]EdgeClaimFinding, []EdgeClaimNote, EdgeClaimCensus, error) {
	census := EdgeClaimCensus{Dictionary: len(edgeVerbNouns), Domains: len(contracts)}
	for _, c := range contracts {
		census.RPCs += len(c.RPCs)
	}
	var (
		findings []EdgeClaimFinding
		notes    []EdgeClaimNote
	)

	ordered := append([]EdgeSource(nil), sources...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })

	for _, src := range ordered {
		units, err := edgeTextUnits(src)
		if err != nil {
			return nil, nil, census, err
		}
		census.Files++
		contract, hasContract := contracts[src.Domain]

		for _, unit := range units {
			census.Texts++
			for _, sentence := range splitEdgeClaimSentences(unit.text) {
				census.Sentences++
				verdict := judgeEdgeSentence(src, unit.line, sentence, contract, hasContract)
				census.Claims += verdict.claims
				census.Resolved += verdict.resolved
				census.Resolutions += verdict.resolutions
				findings = append(findings, verdict.findings...)
				notes = append(notes, verdict.notes...)
			}
		}
	}
	return findings, notes, census, nil
}

// edgeTextUnit — один блок прозы вместе со строкой, с которой он начинается.
type edgeTextUnit struct {
	text string
	line int
}

// edgeTextUnits — проза файла.
//
// Для Go читаются комментарии и строковые литералы со свёрнутой конкатенацией:
// описание схемы собирается из литералов через `+`, и утверждение свободно
// разрывается на границе строк. Для `.tf` берётся ВЕСЬ текст файла — и это не
// упрощение, а полнота: у конфигурации проза живёт и в комментариях `#`, и в
// значениях `description`, и перечислять эти формы значило бы завести слепую зону
// там, где её можно не заводить вовсе. Кода в `.tf` русская фраза не образует.
func edgeTextUnits(src EdgeSource) ([]edgeTextUnit, error) {
	if src.Kind == "tf" {
		return []edgeTextUnit{{text: src.Text, line: 1}}, nil
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, src.Path, src.Text, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("разбор %s: %w", src.Path, err)
	}

	units := make([]edgeTextUnit, 0, 64)
	for _, group := range file.Comments {
		units = append(units, edgeTextUnit{text: group.Text(), line: fset.Position(group.Pos()).Line})
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BinaryExpr:
			if joined, ok := foldStringConcat(node); ok {
				units = append(units, edgeTextUnit{text: joined, line: fset.Position(node.Pos()).Line})
				return false // литералы уже учтены свёрнутыми
			}
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				if v, uerr := strconv.Unquote(node.Value); uerr == nil {
					units = append(units, edgeTextUnit{text: v, line: fset.Position(node.Pos()).Line})
				}
			}
		}
		return true
	})
	return units, nil
}

// foldStringConcat — свёртка `"a" + "b" + …` в одну строку. Второй результат
// false, если хоть один операнд строковым литералом не является.
func foldStringConcat(expr ast.Expr) (string, bool) {
	switch node := expr.(type) {
	case *ast.BinaryExpr:
		if node.Op != token.ADD {
			return "", false
		}
		left, ok := foldStringConcat(node.X)
		if !ok {
			return "", false
		}
		right, ok := foldStringConcat(node.Y)
		if !ok {
			return "", false
		}
		return left + right, true
	case *ast.BasicLit:
		if node.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(node.Value)
		if err != nil {
			return "", false
		}
		return v, true
	}
	return "", false
}

// splitEdgeClaimSentences — деление прозы на предложения.
//
// Абзацы разделяются пустой строкой, а перенос строки ВНУТРИ абзаца снимается:
// проза переносится по ширине, и утверждение свободно разрывается посередине
// («…операции переноса группы между\nсетями у края не существует»). Деление по
// строкам теряло бы ровно такие утверждения — молча, потому что предмет и маркер
// оказывались бы в разных единицах суждения.
//
// Судить предложение всё же обязательно: отрицание из одного абзаца, встретившись
// с глаголом из другого, дало бы находку, которой никто не писал.
func splitEdgeClaimSentences(text string) []string {
	out := make([]string, 0, 16)
	for _, para := range strings.Split(text, "\n\n") {
		joined := strings.Join(strings.Fields(para), " ")
		for _, s := range strings.FieldsFunc(joined, func(r rune) bool {
			return r == '.' || r == ';' || r == '!' || r == '?'
		}) {
			if t := strings.TrimSpace(s); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

// edgeSentenceVerdict — исход суждения по одному предложению.
type edgeSentenceVerdict struct {
	findings []EdgeClaimFinding
	notes    []EdgeClaimNote
	claims   int
	// resolved — 0 либо 1: СКОЛЬКО УТВЕРЖДЕНИЙ сверено, а не сколько проверок сделано.
	resolved int
	// resolutions — сколько отдельных сверок (токенов плюс имён) выполнено. Величины
	// разные: одно предложение с тремя токенами — это одно утверждение и три сверки,
	// и смешивать их значит объявлять осмотренным больше, чем осмотрено.
	resolutions int
}

// judgeEdgeSentence — вердикт по одному предложению.
func judgeEdgeSentence(src EdgeSource, line int, sentence string, contract EdgeContract, hasContract bool) edgeSentenceVerdict {
	lower := strings.ToLower(sentence)
	denies := containsAnyEdgeMarker(lower, edgeDenialMarkers)
	affirms := containsAnyEdgeMarker(lower, edgeAffirmationMarkers)

	trimmed := strings.TrimSpace(sentence)
	note := EdgeClaimNote{File: src.Path, Line: line, Domain: src.Domain, Sentence: trimmed}
	tokens := edgeActionTokenRe.FindAllStringSubmatch(trimmed, -1)
	methods := edgeMethodTokenRe.FindAllStringSubmatch(trimmed, -1)
	if len(tokens) == 0 && len(methods) == 0 && !denies && !affirms {
		return edgeSentenceVerdict{}
	}

	verdict := edgeSentenceVerdict{claims: 1}
	if !hasContract {
		verdict.notes = append(verdict.notes, note)
		// Домен файла не выведен — резолвить не с чем. Утверждение осмотрено и
		// посчитано, но проверенным НЕ считается.
		return verdict
	}

	// Способ 1 — токен действия. Точен, морфологии и маркера не требует: назвать
	// действие края значит утверждать, что оно есть, если тут же его не отрицают.
	resolutions := 0
	for _, m := range tokens {
		tok := m[1]
		resolutions++
		exists := contract.Actions[tok]
		if f, ok := edgeFinding(src, line, trimmed, contract.Domain, tok,
			"суффикс-действие "+tok, denies, true, exists); ok {
			verdict.findings = append(verdict.findings, f)
		}
	}

	// Способ 2 — служба вместе с методом. Точен по той же причине, что и токен
	// действия, и МАРКЕРА не требует тоже: назвать метод края значит утверждать,
	// что он есть, если тут же его не отрицают.
	for _, m := range methods {
		qualified := m[1] + "/" + m[2]
		resolutions++
		exists := contract.Methods[qualified]
		if f, ok := edgeFinding(src, line, trimmed, contract.Domain, qualified,
			"rpc "+m[2]+" службы "+m[1], denies, true, exists); ok {
			verdict.findings = append(verdict.findings, f)
		}
	}

	// Способ 3 — отглагольное имя из словаря.
	nouns := make([]string, 0, len(edgeVerbNouns))
	for noun := range edgeVerbNouns {
		if strings.Contains(lower, noun) {
			nouns = append(nouns, noun)
		}
	}
	if !denies && !affirms {
		nouns = nil // без маркера отглагольное имя утверждением о крае не является
	}
	sort.Strings(nouns)
	for _, noun := range nouns {
		prefixes := edgeVerbNouns[noun]
		resolutions++
		match, exists := contractVerbFor(contract.RPCs, prefixes)
		expected := "rpc " + strings.Join(prefixes, "*|") + "*"
		if exists {
			expected = "rpc " + match
		}
		if f, ok := edgeFinding(src, line, trimmed, contract.Domain, noun,
			expected, denies, affirms, exists); ok {
			verdict.findings = append(verdict.findings, f)
		}
	}

	// Ничем не резолвится, а предмет назван РОДОВЫМ именем действия — находка
	// третьего вида. Требование формы, а не морфологии: см. edgeGenericActionNouns.
	if resolutions == 0 {
		if noun, ok := genericActionSubject(lower); ok {
			verdict.findings = append(verdict.findings, EdgeClaimFinding{
				File: src.Path, Line: line, Sentence: trimmed,
				Domain: contract.Domain, Subject: noun, Unnamed: true,
			})
		}
	}

	verdict.resolutions = resolutions
	if resolutions > 0 {
		verdict.resolved = 1
	}
	note.Resolved = resolutions > 0
	verdict.notes = append(verdict.notes, note)
	return verdict
}

// genericActionSubject — названо ли предметом утверждения РОДОВОЕ имя действия.
//
// Предметом считается то, что стоит в [edgeGenericNounWindow] словах ПЕРЕД
// маркером края. Позиция здесь несущая, а не оптимизация: родовое имя из соседнего
// придаточного предметом утверждения не является, и предикат по всему предложению
// объявлял бы находкой законное утверждение о ресурсе.
func genericActionSubject(lower string) (string, bool) {
	for _, marker := range append(append([]string{}, edgeDenialMarkers...), edgeAffirmationMarkers...) {
		idx := strings.Index(lower, marker)
		if idx < 0 {
			continue
		}
		words := strings.Fields(lower[:idx])
		if len(words) > edgeGenericNounWindow {
			words = words[len(words)-edgeGenericNounWindow:]
		}
		window := strings.Join(words, " ")
		for _, noun := range edgeGenericActionNouns {
			if strings.Contains(window, noun) {
				return noun, true
			}
		}
	}
	return "", false
}

// edgeFinding — находка, если утверждение разошлось с контрактом.
func edgeFinding(src EdgeSource, line int, sentence, domain, subject, expected string, denies, affirms, exists bool) (EdgeClaimFinding, bool) {
	switch {
	case denies && exists:
		return EdgeClaimFinding{File: src.Path, Line: line, Sentence: sentence,
			Domain: domain, Subject: subject, Expected: expected, Affirmative: false}, true
	case affirms && !denies && !exists:
		return EdgeClaimFinding{File: src.Path, Line: line, Sentence: sentence,
			Domain: domain, Subject: subject, Expected: expected, Affirmative: true}, true
	}
	return EdgeClaimFinding{}, false
}

// contractVerbFor — первый RPC контракта, чьё имя начинается одним из префиксов.
func contractVerbFor(rpcs map[string]bool, prefixes []string) (string, bool) {
	names := make([]string, 0, len(rpcs))
	for name := range rpcs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, p := range prefixes {
			if strings.HasPrefix(name, p) {
				return name, true
			}
		}
	}
	return "", false
}

// EdgeClaimPremiseHolds — предпосылка гейта: хоть одна запись словаря обязана
// резолвиться хоть в одном контракте.
//
// Без этой проверки исчезновение глагола из контрактов сделало бы отрицающую
// половину гейта вакуумной МОЛЧА: её вход перестал бы быть представимым, а счётчик
// утверждений продолжал бы расти и вердикт оставался бы зелёным.
func EdgeClaimPremiseHolds(contracts map[string]EdgeContract) (string, bool) {
	var dead []string
	for noun, prefixes := range edgeVerbNouns {
		found := false
		for _, c := range contracts {
			if _, ok := contractVerbFor(c.RPCs, prefixes); ok {
				found = true
				break
			}
		}
		if !found {
			dead = append(dead, fmt.Sprintf("%q → %s*", noun, strings.Join(prefixes, "*|")))
		}
	}
	if len(dead) == 0 {
		return "", true
	}
	sort.Strings(dead)
	return strings.Join(dead, ", "), false
}

// containsAnyEdgeMarker — встречается ли в тексте хоть один маркер набора.
func containsAnyEdgeMarker(text string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}
