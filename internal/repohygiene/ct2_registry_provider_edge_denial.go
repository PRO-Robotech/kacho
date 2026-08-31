// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ct2_registry_provider_edge_denial.go — анализатор «провайдер не высказывается о
// крае неправду».
//
// # Предмет
//
// Описание атрибута провайдера — клиентская поверхность: его читают в справочнике
// и в подсказке редактора. Утверждение о ЧУЖОЙ поверхности, записанное здесь
// прозой, переживает свой предмет молча — ни сборка, ни линтер, ни пробы
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
// Тот судит клиентские СТРАНИЦЫ (`.mdx`) и знает один механизм — подписку. Провайдер
// в его корпус не входит ни при каком тексте: его утверждения живут в Go-коде, в
// описаниях схемы и в шапках типов. Половина предмета оставалась незакрытой, и
// закрыть её расширением того гейта нельзя — у него другой корпус и другой признак
// существования механизма.
//
// # Что судится — ПРЕДМЕТ отрицания, а не его форма
//
// Лексикон над естественным языком не отличает законного близнеца от находки, если
// различать по форме отрицания: «краем не поддержан» бывает и правдой. Поэтому
// решает РЕЗОЛВ: утверждение сопоставляется с контрактом, и вердикт даёт дерево, а
// не словарь.
//
//	отрицание + глагол, который в контракте ЕСТЬ   → находка (случай #1646)
//	утверждение + глагола, которого в контракте НЕТ → находка (обратная сторона)
//	отрицание + предмет, который глаголом не является → молчание (законный близнец:
//	    «Перенос репозитория между реестрами краем не поддержан» — переноса между
//	    реестрами в контракте нет вовсе, и это правда)
//
// Судится ПРЕДЛОЖЕНИЕ, а не файл: иначе отрицание из одного абзаца встретилось бы с
// глаголом из другого и дало бы находку, которой никто не писал.
//
// # Чем истекает
//
// ОТ ФАКТА В ДЕРЕВЕ. Пропадёт из контракта глагол, к которому резолвится хоть один
// глагол словаря, — гейт падает ПРЕДПОСЫЛКОЙ, а не молчит: отрицания стали бы
// правдой, и их надо перечитать, а не оставить под мёртвым запретом.
//
// # Чего он НЕ судит, и это названо, а не умолчано
//
//  1. **Полноту словаря.** Сегодня в нём одна запись — «переименование». Причина не
//     в лени: остальные отглагольные имена («удаление», «создание», «обновление») не
//     называют ПРЕДМЕТ действия, а у registry три разных ресурса с каждым из этих
//     глаголов. Запись, требующая догадки об объекте, давала бы находки на исправном
//     тексте — и её сняли бы первой. Размер словаря печатается переписью, чтобы
//     узость была видна числом, а не подразумевалась.
//  2. **Утверждения вне закрытого набора форм.** Автор, написавший отрицание иначе,
//     останется вне наблюдения. Перепись печатает, сколько предложений осмотрено и
//     сколько утверждений опознано, поэтому «ноль находок» отличимо от «ноль
//     прочитанного».
//  3. **Прочие домены и прочие поверхности.** Корпус — файлы провайдера, ссылающиеся
//     на контракт registry. Расширение на соседей — не правка одной строки: у каждого
//     домена свой контракт и свой словарь.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// edgeDenialMarkers — закрытый набор форм ОТРИЦАНИЯ края.
//
// Набор именно закрытый, а не эвристика: каждая форма доказана инъекцией, а всё,
// чего в нём нет, честно объявлено вне наблюдения (см. шапку, п. 2).
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

// edgeVerbNouns — отглагольное имя в прозе → префикс имени RPC в контракте.
//
// Одна запись сегодня, и это решение, а не недоделка (см. шапку, п. 1).
var edgeVerbNouns = map[string]string{
	"переименован": "Rename",
}

// EdgeClaimFinding — одно утверждение провайдера о крае, разошедшееся с контрактом.
type EdgeClaimFinding struct {
	File     string
	Line     int
	Sentence string
	Noun     string
	Verb     string
	// Affirmative: true — провайдер утверждает глагол, которого нет; false —
	// отрицает глагол, который есть.
	Affirmative bool
}

// String — текст находки. Называет координату, предмет и то, чем он опровергнут:
// находка, не называющая, ЧЕМ она доказана, посылает читателя искать не там.
func (f EdgeClaimFinding) String() string {
	if f.Affirmative {
		return fmt.Sprintf("%s:%d: провайдер утверждает %q, но глагола %s* в контракте registry НЕТ: %q",
			f.File, f.Line, f.Noun, f.Verb, f.Sentence)
	}
	return fmt.Sprintf("%s:%d: провайдер отрицает %q, а глагол %s* в контракте registry ЕСТЬ: %q",
		f.File, f.Line, f.Noun, f.Verb, f.Sentence)
}

// EdgeClaimCensus — объём осмотренного. Печатается всегда, чтобы «ноль находок»
// было отличимо от «ноль прочитанного».
type EdgeClaimCensus struct {
	Files         int
	Texts         int
	Sentences     int
	Claims        int
	Dictionary    int
	ContractVerbs int
}

// String — перепись одной строкой.
func (c EdgeClaimCensus) String() string {
	return fmt.Sprintf("перепись: файлов %d · текстовых узлов %d · предложений %d · "+
		"утверждений о крае %d · записей словаря %d · глаголов контракта %d",
		c.Files, c.Texts, c.Sentences, c.Claims, c.Dictionary, c.ContractVerbs)
}

// ScanProviderEdgeClaims — разбирает исходники провайдера и сверяет каждое
// утверждение о крае с набором глаголов контракта.
//
// sources: путь → исходник Go. contractVerbs: имена RPC контракта registry.
func ScanProviderEdgeClaims(sources map[string]string, contractVerbs map[string]bool) ([]EdgeClaimFinding, EdgeClaimCensus, error) {
	census := EdgeClaimCensus{Dictionary: len(edgeVerbNouns), ContractVerbs: len(contractVerbs)}
	var findings []EdgeClaimFinding

	paths := make([]string, 0, len(sources))
	for p := range sources {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, sources[path], parser.ParseComments)
		if err != nil {
			return nil, census, fmt.Errorf("разбор %s: %w", path, err)
		}
		census.Files++

		for _, unit := range providerTextUnits(file) {
			census.Texts++
			line := fset.Position(unit.pos).Line
			for _, sentence := range splitEdgeClaimSentences(unit.text) {
				census.Sentences++
				found, claimed := judgeEdgeSentence(path, line, sentence, contractVerbs)
				census.Claims += claimed
				findings = append(findings, found...)
			}
		}
	}
	return findings, census, nil
}

// providerTextUnit — один текстовый узел исходника: группа комментариев либо
// строковый литерал (со свёрнутой конкатенацией).
type providerTextUnit struct {
	text string
	pos  token.Pos
}

// providerTextUnits — текст, который в этом файле ЧИТАЕТ человек.
//
// Конкатенация сворачивается намеренно: описание схемы собирается из литералов
// через `+`, и утверждение свободно разрывается на границе строк. Разбор по
// отдельным литералам не увидел бы ровно ту фразу, ради которой гейт заведён, — и
// молчал бы, а не краснел.
func providerTextUnits(file *ast.File) []providerTextUnit {
	units := make([]providerTextUnit, 0, 64)
	for _, group := range file.Comments {
		units = append(units, providerTextUnit{text: group.Text(), pos: group.Pos()})
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BinaryExpr:
			if joined, ok := foldStringConcat(node); ok {
				units = append(units, providerTextUnit{text: joined, pos: node.Pos()})
				return false // литералы уже учтены свёрнутыми
			}
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				if v, err := strconv.Unquote(node.Value); err == nil {
					units = append(units, providerTextUnit{text: v, pos: node.Pos()})
				}
			}
		}
		return true
	})
	return units
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
// Судить надо предложение, а не файл: отрицание из одного абзаца, встретившись с
// глаголом из другого, дало бы находку, которой никто не писал.
func splitEdgeClaimSentences(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == ';' || r == '!' || r == '?' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// judgeEdgeSentence — вердикт по одному предложению. Второй результат — сколько
// утверждений о крае в нём опознано (для переписи, независимо от находок).
func judgeEdgeSentence(path string, line int, sentence string, contractVerbs map[string]bool) ([]EdgeClaimFinding, int) {
	lower := strings.ToLower(sentence)

	denies := containsAnyEdgeMarker(lower, edgeDenialMarkers)
	affirms := containsAnyEdgeMarker(lower, edgeAffirmationMarkers)
	if !denies && !affirms {
		return nil, 0
	}

	nouns := make([]string, 0, len(edgeVerbNouns))
	for noun := range edgeVerbNouns {
		if strings.Contains(lower, noun) {
			nouns = append(nouns, noun)
		}
	}
	if len(nouns) == 0 {
		// Отрицание есть, а предмет глаголом контракта не является — законный
		// близнец, и молчать на нём обязательно.
		return nil, 1
	}
	sort.Strings(nouns)

	var findings []EdgeClaimFinding
	for _, noun := range nouns {
		verb := edgeVerbNouns[noun]
		exists := contractHasVerb(contractVerbs, verb)
		switch {
		case denies && exists:
			findings = append(findings, EdgeClaimFinding{
				File: path, Line: line, Sentence: strings.TrimSpace(sentence),
				Noun: noun, Verb: verb, Affirmative: false})
		case affirms && !exists:
			findings = append(findings, EdgeClaimFinding{
				File: path, Line: line, Sentence: strings.TrimSpace(sentence),
				Noun: noun, Verb: verb, Affirmative: true})
		}
	}
	return findings, len(nouns)
}

// contractHasVerb — есть ли в контракте RPC, чьё имя начинается этим префиксом.
func contractHasVerb(contractVerbs map[string]bool, prefix string) bool {
	for name := range contractVerbs {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// EdgeClaimPremiseHolds — предпосылка гейта: хоть один глагол словаря обязан
// резолвиться в контракте.
//
// Без этой проверки исчезновение глагола из контракта сделало бы отрицающую
// половину гейта вакуумной МОЛЧА: её вход перестал бы быть представимым, а
// счётчик утверждений продолжал бы расти и вердикт оставался бы зелёным.
func EdgeClaimPremiseHolds(contractVerbs map[string]bool) (string, bool) {
	missing := make([]string, 0, len(edgeVerbNouns))
	for noun, verb := range edgeVerbNouns {
		if !contractHasVerb(contractVerbs, verb) {
			missing = append(missing, fmt.Sprintf("%q → %s*", noun, verb))
		}
	}
	if len(missing) == 0 {
		return "", true
	}
	sort.Strings(missing)
	return strings.Join(missing, ", "), false
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
