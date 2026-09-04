// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_vpc_protoprefix.go — анализатор «префикс, названный комментарием
// контракта, принадлежит тому типу, который комментарий называет».
//
// # Предмет
//
// id-префикс — не мнемоника, а часть неизменяемой внешней координаты (ban #15), и
// продукт прямо предлагает читать по нему тип: «тип ресурса читается по
// префиксу». Комментарий контракта, приписавший префикс не тому типу, ломает
// именно то, что обещано: вызывающий закладывает разбор префикса в маршрутизацию
// и журналы, а справочник расходится сам с собой о том, что этот префикс значит.
//
// Замер на день заведения (kacho#1601): `enp` — префикс ОПЕРАЦИИ домена vpc
// (`ids.PrefixOperationVPC`, по нему край маршрутизирует `OperationService.Get`),
// а два комментария контракта называли им сетевой интерфейс, чей префикс `nic`
// (`ids.PrefixNetworkInterface`). Клиентская документация при этом верна и
// внутренне непротиворечива: её таблица префиксов даёт `nic` → NetworkInterface и
// `enp` → Operation в одном списке. То есть расходился с деревом контракт, а не
// страница.
//
// # Что судит анализатор
//
// Словарь префиксов выводится из ЕДИНСТВЕННОГО источника — объявлений констант
// `Prefix<Имя> = "<значение>"` в [PrefixSourceRel]. Второй рукописной таблицы не
// заводится: она разошлась бы с первой молча.
//
// В комментариях контрактов распознаётся УТВЕРЖДЕНИЕ О ПРЕФИКСЕ — строка,
// несущая `prefix "<значение>"`. Из той же строки берутся имена в верблюжьей
// записи; если ровно одно из них известно словарю, названный префикс обязан
// совпасть с префиксом этого имени.
//
// # Домен решает, какое имя чей префикс
//
// Одно и то же слово значит разное в разных доменах: `Image` вычислений — `fd8`
// (`PrefixImage`), `Image` хранилища — `img` (`PrefixStorageImage`). Поэтому имя
// резолвится СНАЧАЛА доменно-квалифицированной константой (`Prefix<Домен><Имя>`,
// домен берётся из пути файла контракта), и только затем голой. Без этого правила
// анализатор краснел бы на верном комментарии образа хранилища — проверено
// инъекцией.
//
// # ЧЕГО ОН НЕ СУДИТ, и это названо числом, а не умолчанием
//
//  1. СТРОКА БЕЗ ИЗВЕСТНОГО ИМЕНИ не судится. Утверждение «(prefix "img")» рядом
//     с одним лишь именем службы или домена сказать не о чем: тип, которому
//     префикс приписан, в строке не назван.
//
//  2. СТРОКА С НЕСКОЛЬКИМИ известными именами не судится — о котором из них
//     сказано, неизвестно, и судить её значило бы краснеть наугад.
//
//  3. ПОЛНОТА не судится: молчание о префиксе нарушением не является.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных файлов контракта, ноль имён в словаре либо ноль распознанных
// утверждений — «находок ноль» неотличимо от «прочитано ноль».
package repohygiene

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// PrefixSourceRel — единственный источник словаря префиксов. Объявлен здесь, а не
// у вызывающего: литерал координаты, повторённый по местам вызова, разъезжается
// молча.
const PrefixSourceRel = "pkg/ids/ids.go"

// ProtoPrefixClaimOptions — вход анализатора.
type ProtoPrefixClaimOptions struct {
	// Tree — СОСТАВ дерева, а не его корень: гейт берёт индекс git
	// (`treecorpus.NewTree`), инъекционная проба — синтетическое дерево
	// (`treecorpus.SyntheticTree`). Разбор — clienttruth_treefiles.go.
	Tree *treecorpus.Tree

	// ProtoRoot — каталог контрактов относительно корня дерева.
	ProtoRoot string

	// PrefixSource — файл с объявлениями констант префиксов относительно корня
	// дерева. Пустое значение означает [PrefixSourceRel].
	PrefixSource string

	// Exemptions — послабления: утверждения, которые сегодня неверны и правятся
	// НЕ здесь. Каждое обязано истекать само: запись, которой больше нечего
	// исключать, — находка (`testing.md` §«Гейт на класс», п.5).
	Exemptions []ProtoPrefixClaimExemption
}

// ProtoPrefixClaimExemption — одно послабление. Ключ — предмет (файл, имя,
// названный префикс), а не номер строки: номер сдвигается от любой соседней
// правки, и послабление истекало бы по чужой причине.
type ProtoPrefixClaimExemption struct {
	// File — путь файла контракта относительно Root.
	File string
	// Name — имя в верблюжьей записи, о котором сделано утверждение.
	Name string
	// Prefix — префикс, названный комментарием.
	Prefix string
	// Reason — почему запись стоит и что её снимет. Пустая запрещена: послабление
	// без причины неотличимо от забытого.
	Reason string
}

// ProtoPrefixClaimCensus — объём осмотренного. Печатается всегда: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type ProtoPrefixClaimCensus struct {
	// ProtoFiles — прочитано файлов контракта.
	ProtoFiles int
	// KnownNames — имён в словаре префиксов.
	KnownNames int
	// Claims — распознано утверждений о префиксе.
	Claims int
	// Judged — из них рассужено (ровно одно известное имя в строке).
	Judged int
	// NoName — не рассужено: известного имени в строке нет.
	NoName int
	// Ambiguous — не рассужено: известных имён в строке несколько.
	Ambiguous int
	// DomainQualified — резолвов, где выиграла доменно-квалифицированная
	// константа. Ноль означает, что правило домена не сработало ни разу, — на
	// этом дереве это уже находка о самом анализаторе.
	DomainQualified int
	// Exempted — находок, снятых послаблением.
	Exempted int
}

// ProtoPrefixClaimFinding — одна находка.
type ProtoPrefixClaimFinding struct {
	// File, Line — координата утверждения.
	File string
	Line int
	// Name — имя, о котором сделано утверждение.
	Name string
	// Claimed — префикс, названный комментарием.
	Claimed string
	// Actual — префикс этого имени по словарю; пуст у устаревшего послабления.
	Actual string
	// StaleExemption — запись послабления потеряла предмет.
	StaleExemption bool
	// Reason — причина послабления (только у устаревшего).
	Reason string
}

func (f ProtoPrefixClaimFinding) String() string {
	if f.StaleExemption {
		return fmt.Sprintf("%s: послабление на %q → %q потеряло предмет (%s) — снимите запись",
			f.File, f.Name, f.Claimed, f.Reason)
	}
	return fmt.Sprintf("%s:%d: комментарий приписывает %q префикс %q, а его префикс — %q",
		f.File, f.Line, f.Name, f.Claimed, f.Actual)
}

var (
	// protoPrefixConstRe — ОБЪЯВЛЕНИЕ константы префикса. Читается объявление, а
	// не упоминание: имена констант встречаются и в комментариях того же файла,
	// и предикат по подстроке собрал бы словарь из прозы о нём.
	protoPrefixConstRe = regexp.MustCompile(`(?m)^\s*Prefix([A-Za-z0-9]+)\s*=\s*"([a-z0-9]+)"`)

	// protoPrefixClaimRe — утверждение о префиксе в комментарии.
	protoPrefixClaimRe = regexp.MustCompile(`prefix "([a-z0-9]+)"`)

	// protoPrefixCamelRe — имя в верблюжьей записи. Слово целиком: отбор по
	// подстроке нашёл бы в `NetworkInterface` ещё и `Network`, строка стала бы
	// многоимённой и защита выродилась бы в молчание ровно на том дефекте, ради
	// которого написана.
	protoPrefixCamelRe = regexp.MustCompile(`\b[A-Z][A-Za-z0-9]*\b`)
)

// AuditProtoPrefixClaims читает дерево контрактов и возвращает находки и перепись.
func AuditProtoPrefixClaims(
	opts ProtoPrefixClaimOptions, log io.Writer,
) ([]ProtoPrefixClaimFinding, ProtoPrefixClaimCensus, error) {
	var census ProtoPrefixClaimCensus

	src := opts.PrefixSource
	if src == "" {
		src = PrefixSourceRel
	}
	raw, err := clientTruthReadTreeFile(opts.Tree, src)
	if err != nil {
		return nil, census, fmt.Errorf("источник словаря префиксов %s: %w", src, err)
	}
	// dict — имя константы без `Prefix` → значение. Доменная квалификация
	// разбирается на месте резолва, а не здесь: одно и то же имя обязано
	// разрешаться по-разному в разных доменах.
	dict := map[string]string{}
	for _, m := range protoPrefixConstRe.FindAllStringSubmatch(string(raw), -1) {
		dict[m[1]] = m[2]
	}
	census.KnownNames = len(dict)

	// Имена доменов — из каталогов дерева контрактов. Выводятся, а не
	// выписываются: рукописный перечень разошёлся бы с деревом молча.
	var findings []ProtoPrefixClaimFinding
	matched := map[string]bool{}

	for _, rel := range clientTruthTreeFiles(opts.Tree, opts.ProtoRoot, true, ".proto") {
		body, rerr := clientTruthReadTreeFile(opts.Tree, rel)
		if rerr != nil {
			return nil, census, rerr
		}
		census.ProtoFiles++
		domain := protoDomainOf(rel)

		for i, line := range strings.Split(string(body), "\n") {
			claim := protoPrefixClaimRe.FindStringSubmatch(line)
			if claim == nil {
				continue
			}
			census.Claims++

			var names []string
			seen := map[string]bool{}
			for _, w := range protoPrefixCamelRe.FindAllString(line, -1) {
				if seen[w] {
					continue
				}
				if _, _, known := resolvePrefix(dict, domain, w); known {
					seen[w] = true
					names = append(names, w)
				}
			}
			switch len(names) {
			case 0:
				census.NoName++
				continue
			case 1:
			default:
				census.Ambiguous++
				continue
			}
			census.Judged++

			actual, qualified, _ := resolvePrefix(dict, domain, names[0])
			if qualified {
				census.DomainQualified++
			}
			if actual == claim[1] {
				continue
			}
			f := ProtoPrefixClaimFinding{
				File: rel, Line: i + 1, Name: names[0], Claimed: claim[1], Actual: actual,
			}
			if key, ok := exemptedPrefixClaim(opts.Exemptions, f); ok {
				matched[key] = true
				census.Exempted++
				continue
			}
			findings = append(findings, f)
		}
	}

	// Послабление, которому больше нечего исключать, — находка: иначе слепая зона
	// переживёт свой предмет и достанется следующему как «тут так принято».
	for _, e := range opts.Exemptions {
		if matched[prefixClaimKey(e.File, e.Name, e.Prefix)] {
			continue
		}
		findings = append(findings, ProtoPrefixClaimFinding{
			File: e.File, Name: e.Name, Claimed: e.Prefix,
			StaleExemption: true, Reason: e.Reason,
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
			"перепись: файлов контракта %d · имён в словаре %d · утверждений о префиксе %d "+
				"(рассужено %d, без имени %d, многоимённых %d) · резолвов по домену %d · снято послаблением %d\n",
			census.ProtoFiles, census.KnownNames, census.Claims,
			census.Judged, census.NoName, census.Ambiguous,
			census.DomainQualified, census.Exempted)
	}
	return findings, census, nil
}

// protoDomainOf — имя домена из пути контракта `<root>/kacho/cloud/<домен>/…`.
// Пустое, если путь этой формы не имеет: тогда доменная квалификация не
// применяется и имя резолвится голой константой.
func protoDomainOf(rel string) string {
	parts := strings.Split(rel, "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "cloud" {
			return parts[i+1]
		}
	}
	return ""
}

// resolvePrefix — префикс имени в данном домене. Доменно-квалифицированная
// константа СИЛЬНЕЕ голой: `Image` хранилища и `Image` вычислений — разные
// префиксы, и без этого правила верный комментарий одного из них объявлялся бы
// находкой.
//
// Возвращает: значение префикса · выиграла ли доменная квалификация · известно ли
// имя словарю. Третье значение отделено от второго намеренно: слить их значило бы
// считать «имя не известно» и «имя известно голой константой» одним состоянием, а
// на них построены РАЗНЫЕ ветви — отбор имён строки и вывод резолва.
func resolvePrefix(dict map[string]string, domain, name string) (string, bool, bool) {
	if domain != "" {
		q := strings.ToUpper(domain[:1]) + domain[1:] + name
		if v, ok := dict[q]; ok {
			return v, true, true
		}
	}
	v, ok := dict[name]
	return v, false, ok
}

func prefixClaimKey(file, name, prefix string) string {
	return file + "\x00" + name + "\x00" + prefix
}

func exemptedPrefixClaim(
	list []ProtoPrefixClaimExemption, f ProtoPrefixClaimFinding,
) (string, bool) {
	key := prefixClaimKey(f.File, f.Name, f.Claimed)
	for _, e := range list {
		if prefixClaimKey(e.File, e.Name, e.Prefix) == key {
			return key, true
		}
	}
	return "", false
}
