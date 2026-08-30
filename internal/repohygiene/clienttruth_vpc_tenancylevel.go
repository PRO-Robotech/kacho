// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_vpc_tenancylevel.go — анализатор «контейнер, названный комментарием
// контракта, существует в дереве контрактов».
//
// # Предмет
//
// Иерархия аренды и область уникальности имени — решение первого дня, которое
// потом не переиграть: по справочнику контрактов планируют тенант-модель, а
// переделка после запуска означает миграцию всех имён и всех выдач. Комментарий,
// назвавший уровень вложенности, которого у продукта нет, читается как обещание
// этого уровня — и обнаруживается не при чтении, а при попытке найти поле с его
// идентификатором.
//
// Замер на день заведения (kacho#1595): контракты vpc и compute сорок четыре раза
// называли контейнером ресурса `folder` — «ID of the folder that the network
// belongs to», «The name must be unique within the folder», «in the specified
// folder», — при том что полей `folder_id` в дереве контрактов НОЛЬ, а
// фактическая иерархия Account → Project. Клиентская документация тех же
// ресурсов при этом говорит «уникально в пределах проекта»: расходился контракт,
// а не страница.
//
// # Что судит анализатор
//
// В комментариях контрактов распознаётся УТВЕРЖДЕНИЕ О КОНТЕЙНЕРЕ — три формы,
// перечисленные закрыто и выведенные из дерева, а не из головы (§«Чего он не
// судит», п. 1):
//
//	unique within the <контейнер>
//	in the specified <контейнер>
//	ID of the <контейнер> (that|to) …
//
// Названный контейнер обязан иметь СЛЕД В ДЕРЕВЕ — хотя бы один из трёх:
// объявленное поле `<контейнер>_id` либо `<контейнер>`, объявленное сообщение
// `<Контейнер>`, либо сегмент имени пакета. Три вида следа, а не один, потому что
// контейнером законно называют и ресурс (`project`, `network`), и вещь без
// собственного идентификатора (`IAM domain` — вся установка, у неё нет id и не
// должно быть).
//
// Ни одного следа — находка: продукт обещает уровень, которого у него нет.
//
// # Самоистечение
//
// Заведут уровень по-настоящему — появится поле `folder_id`, и анализатор
// замолчит сам. Послабление здесь поэтому не нужно: предикат снятия и есть
// появление предмета.
//
// # ЧЕГО ОН НЕ СУДИТ, и это названо числом, а не умолчанием
//
//  1. ФОРМА ВНЕ ПЕРЕЧНЯ не судится. Перечень закрыт и получен переписью по
//     дереву, а не придуман: расширять его без замера нельзя — распознаватель
//     начал бы находить контейнер в любом предложении, где рядом стоят
//     существительное и предлог. Упоминание уровня ПРОЗОЙ («Project — заменитель
//     Folder из прежней модели») вне охвата НАМЕРЕННО: это верное утверждение об
//     ОТСУТСТВИИ уровня, и краснеть на нём значило бы требовать умолчания о
//     собственной истории.
//
//  2. ПОЛНОТА не судится: молчание о контейнере нарушением не является.
//
//  3. ВЕРНОСТЬ КОНКРЕТНОГО контейнера не судится — только его существование.
//     Комментарий, назвавший `network` там, где верен `project`, анализатор
//     пропустит: оба существуют. Это другой предикат, и у него другой признак.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных файлов контракта, ноль объявленных полей либо ноль
// распознанных утверждений — «находок ноль» неотличимо от «прочитано ноль».
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

// TenancyLevelOptions — вход анализатора.
type TenancyLevelOptions struct {
	// Root — корень дерева.
	Root string
	// ProtoRoot — каталог контрактов относительно Root.
	ProtoRoot string
}

// TenancyLevelCensus — объём осмотренного. Печатается всегда: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type TenancyLevelCensus struct {
	// ProtoFiles — прочитано файлов контракта.
	ProtoFiles int
	// Fields — объявлено полей (след первого вида).
	Fields int
	// Messages — объявлено сообщений (след второго вида).
	Messages int
	// PackageWords — сегментов имён пакетов (след третьего вида).
	PackageWords int
	// Claims — распознано утверждений о контейнере.
	Claims int
	// Traced — из них имеют след в дереве.
	Traced int
}

// TenancyLevelFinding — одна находка.
type TenancyLevelFinding struct {
	// File, Line — координата утверждения.
	File string
	Line int
	// Level — контейнер, названный комментарием, как он написан.
	Level string
	// Candidates — написания, которыми след искался. Печатаются, чтобы
	// читающий видел, ЧТО именно не нашлось, а не только что «не нашлось».
	Candidates []string
}

func (f TenancyLevelFinding) String() string {
	return fmt.Sprintf("%s:%d: комментарий называет контейнером %q, а следа в дереве нет (искали %s)",
		f.File, f.Line, f.Level, strings.Join(f.Candidates, ", "))
}

var (
	// tenancyClaimRes — ЗАКРЫТЫЙ перечень форм утверждения о контейнере.
	// Выведен переписью по дереву контрактов; расширение — только с замером.
	tenancyClaimRes = []*regexp.Regexp{
		regexp.MustCompile(`unique within the ([A-Za-z][A-Za-z0-9]*)(?:\s+([a-z][a-z0-9]*))?`),
		regexp.MustCompile(`in the specified ([A-Za-z][A-Za-z0-9]*)(?:\s+([a-z][a-z0-9]*))?`),
		regexp.MustCompile(`(?i)\bID of the ([A-Za-z][A-Za-z0-9]*)(?:\s+([a-z][a-z0-9]*))?\s+(?:that|to)\b`),
	}

	// tenancyFieldRe — ОБЪЯВЛЕНИЕ поля. Читается объявление, а не упоминание:
	// имена полей встречаются и в комментариях, и предикат по подстроке нашёл бы
	// след у контейнера, которого в дереве нет, — то есть замолчал бы ровно на
	// том входе, ради которого написан.
	tenancyFieldRe = regexp.MustCompile(`^\s*(?:repeated\s+|optional\s+)?[A-Za-z0-9_.<>, ]+\s+([a-z][a-z0-9_]*)\s*=\s*\d+\s*;`)

	// tenancyMessageRe — объявление сообщения либо перечисления.
	tenancyMessageRe = regexp.MustCompile(`^\s*(?:message|enum)\s+([A-Za-z][A-Za-z0-9_]*)`)

	// tenancyPackageRe — объявление пакета.
	tenancyPackageRe = regexp.MustCompile(`^\s*package\s+([a-z][a-z0-9_.]*)\s*;`)
)

// AuditTenancyLevels читает дерево контрактов и возвращает находки и перепись.
func AuditTenancyLevels(
	opts TenancyLevelOptions, log io.Writer,
) ([]TenancyLevelFinding, TenancyLevelCensus, error) {
	var census TenancyLevelCensus

	protoAbs := filepath.Join(opts.Root, filepath.FromSlash(opts.ProtoRoot))
	type claim struct {
		file  string
		line  int
		level string
		words []string
	}
	var (
		claims  []claim
		fields  = map[string]bool{}
		msgs    = map[string]bool{}
		pkgWord = map[string]bool{}
		files   []string
	)

	walkErr := filepath.Walk(protoAbs, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir() || !strings.HasSuffix(path, ".proto") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if walkErr != nil {
		return nil, census, walkErr
	}
	sort.Strings(files)

	for _, path := range files {
		rel, rerr := filepath.Rel(opts.Root, path)
		if rerr != nil {
			return nil, census, rerr
		}
		rel = filepath.ToSlash(rel)
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, census, rerr
		}
		census.ProtoFiles++
		for i, line := range strings.Split(string(body), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "//") {
				if m := tenancyFieldRe.FindStringSubmatch(line); m != nil {
					fields[m[1]] = true
				}
				if m := tenancyMessageRe.FindStringSubmatch(line); m != nil {
					msgs[tenancySnake(m[1])] = true
				}
				if m := tenancyPackageRe.FindStringSubmatch(line); m != nil {
					for _, seg := range strings.Split(m[1], ".") {
						pkgWord[seg] = true
					}
				}
				continue
			}
			for _, rx := range tenancyClaimRes {
				for _, m := range rx.FindAllStringSubmatch(line, -1) {
					lvl := m[1]
					words := []string{m[1]}
					if m[2] != "" {
						lvl = m[1] + " " + m[2]
						words = append(words, m[2], m[1]+"_"+m[2])
					}
					claims = append(claims, claim{file: rel, line: i + 1, level: lvl, words: words})
				}
			}
		}
	}
	census.Fields = len(fields)
	census.Messages = len(msgs)
	census.PackageWords = len(pkgWord)
	census.Claims = len(claims)

	var findings []TenancyLevelFinding
	for _, c := range claims {
		var cand []string
		traced := false
		for _, w := range c.words {
			s := tenancySnake(w)
			cand = append(cand, s+"_id", s)
			if fields[s+"_id"] || fields[s] || msgs[s] || pkgWord[s] {
				traced = true
				break
			}
		}
		if traced {
			census.Traced++
			continue
		}
		findings = append(findings, TenancyLevelFinding{
			File: c.file, Line: c.line, Level: c.level, Candidates: cand,
		})
	}

	if log != nil {
		fmt.Fprintf(log,
			"перепись: файлов контракта %d · полей %d · сообщений %d · сегментов пакета %d · "+
				"утверждений о контейнере %d (со следом %d)\n",
			census.ProtoFiles, census.Fields, census.Messages, census.PackageWords,
			census.Claims, census.Traced)
	}
	return findings, census, nil
}

// tenancySnake — верблюжья запись в змеиную, с разбором подряд идущих заглавных.
// `CidrGroup` → `cidr_group`, `NIC` → `nic`, `IAM` → `iam`. Наивный разбор «перед
// каждой заглавной — подчёркивание» дал бы `n_i_c`, и след аббревиатуры не
// нашёлся бы ни разу: анализатор краснел бы на верных комментариях.
func tenancySnake(w string) string {
	if strings.Contains(w, "_") {
		return strings.ToLower(w)
	}
	r := []rune(w)
	var b strings.Builder
	for i, c := range r {
		upper := c >= 'A' && c <= 'Z'
		if upper && i > 0 {
			prevLower := r[i-1] >= 'a' && r[i-1] <= 'z'
			nextLower := i+1 < len(r) && r[i+1] >= 'a' && r[i+1] <= 'z'
			if prevLower || nextLower {
				b.WriteByte('_')
			}
		}
		if upper {
			b.WriteRune(c - 'A' + 'a')
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}
