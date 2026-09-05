// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_addr_verbcanon.go — анализатор «суффикс-действие в адресе записан
// КАНОНОМ платформы».
//
// # Предмет
//
// Дополнительное действие над ресурсом адресуется суффиксом `:verb`
// (`api-conventions.md` §«Naming / формат»). Клиент, узнавший написание у одного
// ресурса, строит по нему путь у соседнего — это и есть польза от конвенции.
// Написание, отличное у соседа, стоит круга на КАЖДЫЙ новый глагол, а промах даёт
// `404` без тела: отказ не подсказывает верную запись.
//
// # Канон не выбран здесь — он ИЗМЕРЕН и объявлен четырьмя местами
//
// Замер на день заведения (kacho#1624), предикат в шапке пробы:
//
//	верблюжья запись (и одно слово)  51 путь — все домены, включая сам vpc
//	дефисная запись                   6 путей — network, subnet, cidrGroup домена vpc
//
// Канон — верблюжья запись, и это не вкус большинства. Её независимо объявляют:
//
//  1. `api-conventions.md` §«Naming / формат» — примером служит `:addCidrBlocks`,
//     причём именно у подсети, то есть у одного из шести отступающих путей;
//  2. каталог прав: запись действия строится из ИМЕНИ МЕТОДА контракта, поэтому
//     для тех же трёх ресурсов там стоит `…addCidrBlocks` — камелем, тогда как
//     адрес у них дефисный;
//  3. имя самого метода контракта (`AddCidrBlocks`);
//  4. пятьдесят один путь остального дерева.
//
// Дефисная запись, таким образом, — отступление ОДНОЙ поверхности из четырёх, а
// не вторая конвенция.
//
// # Что судит анализатор
//
// Суффикс `:verb` каждого адреса, объявленного в контракте, обязан отвечать
// нижней верблюжьей записи: `^[a-z][a-zA-Z0-9]*$`. Дефис и подчёркивание в
// суффиксе — находка.
//
// # Чего он НЕ судит, и это названо, а не умолчано
//
//  1. САМ ГЛАГОЛ. Что действие названо `move`, а не `relocate`, анализатор не
//     решает: предмет — запись, а не словарь.
//  2. ПУТЬ ДО СУФФИКСА. Регистр сегментов ресурса (`addressPools`,
//     `networkLoadBalancers`) имеет свою конвенцию и свою историю.
//  3. НАЛИЧИЕ суффикс-действия. Молчание — не нарушение.
//
// # Что он НЕ ЧИНИТ, и почему это сказано прямо
//
// Смена написания у landed-глагола — ЛОМАЮЩЕЕ изменение: путь уже опубликован.
// Правильный ход — новый канон основным адресом плюс прежняя запись
// дополнительной привязкой (`additional_bindings`), и он требует работы в
// плоскости края, которая идёт СВОИМ изменением (`kacho#1624`, задача-преемник).
// Поэтому шесть дефисных путей стоят здесь ПОСЛАБЛЕНИЯМИ, а не суженным обходом:
// суженный обход молчал бы навсегда, а послабление ИСТЕКАЕТ САМО — как только
// путь переведён, ему нечего исключать, и анализатор требует снять запись.
//
// Ценность анализатора до того дня — в другом: класс перестаёт РАСТИ. Седьмой
// дефисный путь, заведённый по образцу соседнего, краснеет в тот же день.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных файлов контракта либо ноль распознанных адресов — «находок
// ноль» неотличимо от «прочитано ноль».
package repohygiene

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// VerbCanonOptions — вход анализатора.
type VerbCanonOptions struct {
	// Tree — СОСТАВ дерева, а не его корень: гейт берёт индекс git
	// (`treecorpus.NewTree`), инъекционная проба — синтетическое дерево
	// (`treecorpus.SyntheticTree`). Разбор — clienttruth_treefiles.go.
	Tree *treecorpus.Tree
	// ProtoRoot — каталог контрактов относительно корня дерева.
	ProtoRoot string
	// Exemptions — послабления; каждое обязано истекать само.
	Exemptions []VerbCanonExemption
}

// VerbCanonExemption — одно послабление. Ключ — предмет (файл и сам адрес), а не
// номер строки: номер сдвигается от любой соседней правки, и послабление истекало
// бы по чужой причине.
type VerbCanonExemption struct {
	// File — путь файла контракта относительно Root.
	File string
	// Path — адрес целиком, как он записан в контракте.
	Path string
	// Reason — почему запись стоит и что её снимет. Пустая запрещена.
	Reason string
}

// VerbCanonCensus — объём осмотренного.
type VerbCanonCensus struct {
	// ProtoFiles — прочитано файлов контракта.
	ProtoFiles int
	// Paths — распознано адресов.
	Paths int
	// WithVerb — из них несущих суффикс-действие.
	WithVerb int
	// Canonical — из них записанных каноном.
	Canonical int
	// Exempted — находок, снятых послаблением.
	Exempted int
}

// VerbCanonFinding — одна находка.
type VerbCanonFinding struct {
	// File, Line — координата адреса.
	File string
	Line int
	// Path — адрес целиком.
	Path string
	// Verb — суффикс-действие, как он записан.
	Verb string
	// Canonical — тот же глагол в каноне; пуст у устаревшего послабления.
	Canonical string
	// StaleExemption — запись послабления потеряла предмет.
	StaleExemption bool
	// Reason — причина послабления (только у устаревшего).
	Reason string
}

func (f VerbCanonFinding) String() string {
	if f.StaleExemption {
		return fmt.Sprintf("%s: послабление на %q потеряло предмет (%s) — снимите запись",
			f.File, f.Path, f.Reason)
	}
	return fmt.Sprintf("%s:%d: суффикс-действие %q записан не каноном — ожидается %q (адрес %q)",
		f.File, f.Line, f.Verb, f.Canonical, f.Path)
}

var (
	// verbCanonPathRe — АДРЕС, объявленный контрактом. Отбирается строковый
	// литерал, начинающийся со слэша: так записаны все привязки, и основная, и
	// дополнительная (`additional_bindings`). Читать саму опцию не нужно — предмет
	// живёт в литерале, а форм записи опции в дереве несколько.
	verbCanonPathRe = regexp.MustCompile(`"(/[A-Za-z0-9_./{}=*-]*:[A-Za-z0-9_-]+)"`)

	// verbCanonSuffixRe — суффикс-действие: всё после ПОСЛЕДНЕГО двоеточия.
	verbCanonSuffixRe = regexp.MustCompile(`:([A-Za-z0-9_-]+)$`)

	// verbCanonRe — канон: нижняя верблюжья запись.
	verbCanonRe = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
)

// canonicalVerb — тот же глагол в каноне: разделители снимаются, следующее за
// ними слово поднимается заглавной. Нужен ДИАГНОСТИКЕ, а не вердикту: находка,
// называющая лишь «не канон», заставляет читателя выдумывать верную запись
// (`testing.md` §«Гейт на класс», п.8).
func canonicalVerb(verb string) string {
	parts := strings.FieldsFunc(verb, func(r rune) bool { return r == '-' || r == '_' })
	if len(parts) == 0 {
		return verb
	}
	out := strings.ToLower(parts[0])
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		out += strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return out
}

// AuditVerbCanon читает дерево контрактов и возвращает находки и перепись.
func AuditVerbCanon(
	opts VerbCanonOptions, log io.Writer,
) ([]VerbCanonFinding, VerbCanonCensus, error) {
	var census VerbCanonCensus
	var findings []VerbCanonFinding
	matched := map[string]bool{}

	for _, rel := range clientTruthTreeFiles(opts.Tree, opts.ProtoRoot, true, ".proto") {
		body, rerr := clientTruthReadTreeFile(opts.Tree, rel)
		if rerr != nil {
			return nil, census, rerr
		}
		census.ProtoFiles++

		for i, line := range strings.Split(string(body), "\n") {
			for _, m := range verbCanonPathRe.FindAllStringSubmatch(line, -1) {
				census.Paths++
				sm := verbCanonSuffixRe.FindStringSubmatch(m[1])
				if sm == nil {
					continue
				}
				census.WithVerb++
				verb := sm[1]
				if verbCanonRe.MatchString(verb) {
					census.Canonical++
					continue
				}
				f := VerbCanonFinding{
					File: rel, Line: i + 1, Path: m[1],
					Verb: verb, Canonical: canonicalVerb(verb),
				}
				if key, ok := exemptedVerbCanon(opts.Exemptions, f); ok {
					matched[key] = true
					census.Exempted++
					continue
				}
				findings = append(findings, f)
			}
		}
	}

	// Послабление, которому больше нечего исключать, — находка.
	for _, e := range opts.Exemptions {
		if matched[verbCanonKey(e.File, e.Path)] {
			continue
		}
		findings = append(findings, VerbCanonFinding{
			File: e.File, Path: e.Path, StaleExemption: true, Reason: e.Reason,
		})
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	if log != nil {
		_, _ = fmt.Fprintf(log,
			"перепись: файлов контракта %d · адресов %d · из них с суффикс-действием %d "+
				"(каноном %d, снято послаблением %d)\n",
			census.ProtoFiles, census.Paths, census.WithVerb,
			census.Canonical, census.Exempted)
	}
	return findings, census, nil
}

func verbCanonKey(file, path string) string { return file + "\x00" + path }

func exemptedVerbCanon(list []VerbCanonExemption, f VerbCanonFinding) (string, bool) {
	key := verbCanonKey(f.File, f.Path)
	for _, e := range list {
		if verbCanonKey(e.File, e.Path) == key {
			return key, true
		}
	}
	return "", false
}
