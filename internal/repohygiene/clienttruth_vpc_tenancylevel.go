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
// Имя контейнера СОСТАВНОЕ — до трёх слов, без различения регистра, до первого
// не-существительного ([tenancyStopWords]). Названный контейнер обязан иметь
// СЛЕД В ДЕРЕВЕ — хотя бы один из трёх: объявленное поле `<часть>_id` либо
// `<часть>`, объявленное сообщение `<Часть>`, либо сегмент имени пакета; часть —
// любой непрерывный кусок имени ([tenancyNameParts]). Три вида следа, а не один,
// потому что контейнером законно называют и ресурс (`project`, `network`), и
// вещь без собственного идентификатора (`IAM domain` — вся установка, у неё нет
// id и не должно быть).
//
// Ни одного следа — находка: продукт обещает уровень, которого у него нет.
//
// # Составное имя: чем расширение оплачено (замерено 2026-08-30)
//
// Прежний захват брал одно слово плюс необязательное второе в НИЖНЕМ регистре.
// На нём анализатор дал ЛОЖНОЕ КРАСНОЕ: соседняя полоса записала верное «is
// unique within the PARENT LOAD BALANCER — NOT within the project» (авторитет —
// ограничение уникальности `listeners_lb_name_uniq (load_balancer_id, name)`), а
// распознаватель увидел `PARENT` и следа ему не нашёл. Трёхсловное имя («User
// membership row») он не видел ВОВСЕ — ни красного, ни зелёного, просто
// невидимость.
//
// Обе величины переписи названы, потому что порознь каждая обманывает. Замер —
// на СВЕДЁННОМ дереве волны, то есть на том, где ложное красное и наблюдалось:
//
//	                        утверждений   составных   со следом   находок
//	до расширения                   296           —         294         2  (обе ложные)
//	после расширения                303         104         303         0
//
// Утверждений стало БОЛЬШЕ — значит расширение не холостое: семь именных групп
// из трёх слов («User membership row», «source volume used», «Security Group
// resource») раньше не распознавались ни одной формой. Изменись только «со
// следом», это была бы не различающая способность, а помилование.
//
// РЕВЕРСНЫЙ КОНТРОЛЬ на настоящих данных: то же дерево на ревизии ДО починки
// уровня `folder` даёт **44 находки и до расширения, и после**. Расширение не
// молчит ни на одной из них.
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
//     1а. ИМЯ ДЛИННЕЕ ТРЁХ СЛОВ не судится целиком — берутся первые три. Предел
//     взят по дереву (длиннее не встретилось), а не по вкусу; удлинять его
//     без замера нельзя по причине из п. 1.
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
	"regexp"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TenancyLevelOptions — вход анализатора.
type TenancyLevelOptions struct {
	// Tree — СОСТАВ дерева, а не его корень: гейт берёт индекс git
	// (`treecorpus.NewTree`), инъекционная проба — синтетическое дерево
	// (`treecorpus.SyntheticTree`). Разбор — clienttruth_treefiles.go.
	Tree *treecorpus.Tree
	// ProtoRoot — каталог контрактов относительно корня дерева.
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
	// MultiWord — из них с ИМЕНЕМ ИЗ НЕСКОЛЬКИХ СЛОВ. Печатается отдельно:
	// ноль здесь означал бы, что расширение распознавателя на составное имя
	// холостое, и это надо видеть, а не выводить.
	MultiWord int
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

// tenancyPhrase — имя контейнера: до трёх слов подряд. Три, а не больше:
// перепись по дереву не знает контейнеров длиннее («PARENT LOAD BALANCER»,
// «User membership row» — предел), а более широкий захват начал бы утягивать в
// имя соседнее предложение.
const tenancyPhrase = `[A-Za-z][A-Za-z0-9]*(?:[ \t]+[A-Za-z][A-Za-z0-9]*){0,2}`

// tenancyStopWords — закрытый класс НЕ-СУЩЕСТВИТЕЛЬНЫХ, на которых имя
// контейнера кончается.
//
// Без него захват уезжает за конец именной группы, и защита ВЫРОЖДАЕТСЯ в
// молчание: «in the specified folder and network» дало бы кандидата `network`,
// тот нашёл бы след, и находка про несуществующий `folder` исчезла бы. Реверсная
// проба это и утверждает — на дереве до починки находок 44 и до расширения, и
// после.
var tenancyStopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"by": true, "for": true, "from": true, "in": true, "is": true, "it": true,
	"its": true, "not": true, "of": true, "on": true, "or": true, "that": true,
	"the": true, "their": true, "this": true, "to": true, "used": true,
	"using": true, "when": true, "where": true, "which": true, "whose": true,
	"with": true,
}

var (
	// tenancyClaimRes — ЗАКРЫТЫЙ перечень форм утверждения о контейнере.
	// Выведен переписью по дереву контрактов; расширение — только с замером.
	//
	// Имя контейнера захватывается ЦЕЛИКОМ, до трёх слов и без различения
	// регистра. Прежняя форма брала одно слово плюс необязательное второе в
	// нижнем регистре — и на «PARENT LOAD BALANCER» видела `PARENT`, а
	// трёхсловное имя («User membership row») не видела ВОВСЕ: ни красного, ни
	// зелёного, просто невидимость. Замер расширения — в шапке файла.
	tenancyClaimRes = []*regexp.Regexp{
		regexp.MustCompile(`unique within the (` + tenancyPhrase + `)`),
		regexp.MustCompile(`in the specified (` + tenancyPhrase + `)`),
		regexp.MustCompile(`(?i)\bID of the (` + tenancyPhrase + `)\s+(?:that|to)\b`),
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
	)

	for _, rel := range clientTruthTreeFiles(opts.Tree, opts.ProtoRoot, true, ".proto") {
		body, rerr := clientTruthReadTreeFile(opts.Tree, rel)
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
					words := tenancyNounPhrase(m[1])
					if len(words) == 0 {
						continue
					}
					if len(words) > 1 {
						census.MultiWord++
					}
					claims = append(claims, claim{
						file: rel, line: i + 1,
						level: strings.Join(words, " "), words: words,
					})
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
		for _, part := range tenancyNameParts(c.words) {
			cand = append(cand, part+"_id", part)
			if fields[part+"_id"] || fields[part] || msgs[part] || pkgWord[part] {
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
		_, _ = fmt.Fprintf(log,
			"перепись: файлов контракта %d · полей %d · сообщений %d · сегментов пакета %d · "+
				"утверждений о контейнере %d (составных %d, со следом %d)\n",
			census.ProtoFiles, census.Fields, census.Messages, census.PackageWords,
			census.Claims, census.MultiWord, census.Traced)
	}
	return findings, census, nil
}

// tenancyNounPhrase — именная группа контейнера: слова захвата до первого
// НЕ-СУЩЕСТВИТЕЛЬНОГО.
//
// Обрезка обязательна, и вот чем она платит за себя: без неё «in the specified
// folder and network» дало бы кандидата `network`, тот нашёл бы след, и находка
// про несуществующий `folder` ИСЧЕЗЛА БЫ. То есть расширение захвата без
// стоп-слов не расширяет наблюдение, а сужает его — молча.
func tenancyNounPhrase(raw string) []string {
	var out []string
	for _, w := range strings.Fields(raw) {
		if tenancyStopWords[strings.ToLower(w)] {
			break
		}
		out = append(out, w)
	}
	return out
}

// tenancyNameParts — написания, которыми ищется след составного имени: все
// непрерывные куски именной группы, от целого к частям.
//
// Части нужны ОБЕ стороны, и каждая — из наблюдения, а не из симметрии:
// уточнение впереди («IAM domain» — след даёт `iam`, имя домена платформы) и
// головное существительное сзади («PARENT LOAD BALANCER» — след даёт
// `load_balancer_id`). Судить только целую фразу значило бы требовать, чтобы
// контракт писал имя контейнера ровно так, как названо поле, — он так не пишет
// и не должен.
//
// ЧЕМ ЭТО ОГРАНИЧЕНО, названо прямо: составное имя считается существующим, если
// след есть у ЛЮБОЙ его части. Имя, чья часть случайно совпала с чужим
// ресурсом, пройдёт молча. Границу держат стоп-слова: за пределы именной группы
// захват не уходит, поэтому «случайной частью» может стать только слово того же
// имени, а не слово соседнего предложения.
func tenancyNameParts(words []string) []string {
	n := len(words)
	out := make([]string, 0, n*(n+1)/2)
	for size := n; size >= 1; size-- {
		for i := 0; i+size <= n; i++ {
			parts := make([]string, 0, size)
			for _, w := range words[i : i+size] {
				parts = append(parts, tenancySnake(w))
			}
			out = append(out, strings.Join(parts, "_"))
		}
	}
	return out
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
