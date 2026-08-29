// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clientdocsresourceowner.go — анализатор «клиентская документация не приписывает
// ресурс чужому домену».
//
// # Предмет
//
// Имя домена в этом продукте — не проза, а ЗНАЧЕНИЕ ПАРАМЕТРА. Им адресуется
// поток изменений (`/subscription/v1/events?owner=…`, закрытый словарь), по нему
// выбирается REST-префикс, по нему же край маршрутизирует операцию через префикс
// её идентификатора. Строка таблицы владения, называющая чужой домен, поэтому
// ПРОИЗВОДИТ НЕВЕРНЫЙ ВЫЗОВ, а не только неверное впечатление: клиент, которому
// нужны изменения дисков, идёт в вычисления — где предмета нет вовсе.
//
// Замер на день заведения (kacho#1392): раскол блочного хранения в отдельный
// домен завершён давно, а таблицы владения пережили его в ЧЕТЫРЁХ местах двух
// сайтов — вводная страница края, таблица маршрутизации операций по префиксу,
// вводная страница vpc и её же обзор архитектуры. Все четыре называли `Disk` /
// `Image` / `Snapshot` у вычислений. Постановка задачи говорила о двух местах
// одного сайта; перепись по имени механизма нашла вдвое больше — это и есть
// довод в пользу гейта, а не третьей рукописной таблицы.
//
// # Что судит анализатор
//
// Владение выводится из ДЕРЕВА КОНТРАКТОВ: служба `<Ресурс>Service` в
// `proto/kacho/cloud/<домен>/v1/` объявляет, что `<Ресурс>` служит `<домен>`.
// Имя, за которым стоит ровно один домен, считается принадлежащим ему; имя,
// встреченное у нескольких (`Quota` — у каждого домена своя), из словаря
// ВЫВОДИТСЯ: о нём нельзя сказать «владелец такой-то», и судить его значило бы
// краснеть на верном тексте.
//
// В клиентской документации распознаётся СТРОКА ВЛАДЕНИЯ — строка таблицы
// (`<tr>`), которая называет ровно один домен и несёт ячейку с перечнем имён
// через косую черту. Каждое имя такой ячейки, известное словарю платформы,
// обязано принадлежать названному строкой домену.
//
// # ЧЕГО ОН НЕ СУДИТ, и это названо числом, а не умолчанием
//
//  1. ИМЯ ВНЕ СЛОВАРЯ ПЛАТФОРМЫ не судится вовсе, и таких в ячейках владения
//     сегодня двадцать два (`AuthN`, `Protocol`, `SA`, `POST`, `Geography`, …).
//     Среди них живут и СНЯТЫЕ имена — `Disk` был именно таким: домена за ним
//     не стоит ни одного, поэтому «владелец не тот» о нём не высказывается.
//     Вариант «имя, которого не служит никто, — находка» проверен и ОТВЕРГНУТ
//     замером: он даёт находки на `POST` / `GET` (методы HTTP в той же ячейке),
//     на `SA` (сокращение служебной учётки) и на `DeleteTag` (имя глагола, а не
//     ресурса). Гейт, у которого половина находок ложные, отключают первым — и
//     вместе с ним перестают читать настоящие. Перепись печатает это число,
//     поэтому слепая зона видна, а не подразумевается.
//
//  2. ПРОЗА не судится: перечень владельцев, написанный абзацем, а не строкой
//     таблицы, вне охвата. Такое место в дереве было (обзор архитектуры vpc) и
//     чинилось руками. Расширять распознаватель на прозу без замера нельзя: он
//     стал бы находить домен и ресурс в любом предложении, где они соседствуют
//     законно («consumer ссылается на Volume у storage»).
//
//  3. ПОЛНОТА не судится. Строка, назвавшая два ресурса из четырёх, верна в том,
//     что назвала. Неполнота — другой предикат, и у него другой признак.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных файлов документации, ноль служб в контрактах, ноль
// распознанных строк владения либо ноль рассуженных имён — «находок ноль»
// неотличимо от «прочитано ноль».
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

// ClientDocsResourceOwnerOptions — вход анализатора.
type ClientDocsResourceOwnerOptions struct {
	// Root — корень дерева.
	Root string
	// ProtoRoot — каталог контрактов относительно Root. Владение выводится
	// отсюда и только отсюда.
	ProtoRoot string
	// DomainAliases — имена доменов, под которыми домен контракта известен
	// клиентской документации, если они не совпадают. Единственный сегодня —
	// балансировщик: каталог контракта `loadbalancer`, backend-ключ и короткое
	// имя в документации `nlb`.
	DomainAliases map[string]string
}

// ClientDocsResourceOwnerCensus — объём осмотренного. Печатается всегда: «ноль
// находок» обязано быть отличимо от «ноль прочитанного».
type ClientDocsResourceOwnerCensus struct {
	ProtoFiles   int
	DocFiles     int
	OwnedNames   int
	AmbiguousOut int
	OwnershipRow int
	NamesJudged  int
	NamesOutside int
	OutsideWords []string
}

// ClientDocsResourceOwnerFinding — одна находка.
type ClientDocsResourceOwnerFinding struct {
	File   string
	Line   int
	Domain string
	Name   string
	Owner  string
}

func (f ClientDocsResourceOwnerFinding) String() string {
	return fmt.Sprintf("%s:%d: строка называет домен %q, но %q служит домен %q",
		f.File, f.Line, f.Domain, f.Name, f.Owner)
}

var (
	// clientDocsProtoServiceRe — объявление службы. Читается ОБЪЯВЛЕНИЕ, а не упоминание:
	// имя службы встречается и в комментариях, и предикат по подстроке краснел
	// бы на собственном объяснении.
	clientDocsProtoServiceRe = regexp.MustCompile(`(?m)^service\s+([A-Za-z][A-Za-z0-9]*)\s*\{`)
	// clientDocsCellRe — ячейка строки таблицы.
	clientDocsCellRe = regexp.MustCompile(`(?s)<td>(.*?)</td>`)
	// clientDocsTagRe — разметка внутри ячейки, снимается перед разбором.
	clientDocsTagRe = regexp.MustCompile(`<[^>]+>`)
	// clientDocsLeadNameRe — ведущее имя сегмента, разделённого косой чертой. Берётся
	// именно ведущее: за именем в этих ячейках регулярно идёт пояснение в
	// скобках («Instance / MachineType (peer-консумер VPC)»), и требование
	// «сегмент целиком есть имя» вывело бы такую строку из-под наблюдения.
	clientDocsLeadNameRe = regexp.MustCompile(`^\s*([A-Z][A-Za-z]+)`)
)

// AuditClientDocsResourceOwner выносит вердикт о дереве.
func AuditClientDocsResourceOwner(
	opts ClientDocsResourceOwnerOptions,
	log io.Writer,
) ([]ClientDocsResourceOwnerFinding, ClientDocsResourceOwnerCensus, error) {
	var census ClientDocsResourceOwnerCensus

	owners, ambiguous, protoFiles, err := clientDocsOwnerMap(opts)
	if err != nil {
		return nil, census, err
	}
	census.ProtoFiles = protoFiles
	census.OwnedNames = len(owners)
	census.AmbiguousOut = len(ambiguous)

	docs, err := clientDocsPages(opts.Root)
	if err != nil {
		return nil, census, err
	}
	census.DocFiles = len(docs)

	domainRe, err := clientDocsDomainRe(owners, opts.DomainAliases)
	if err != nil {
		return nil, census, err
	}

	outside := map[string]int{}
	var findings []ClientDocsResourceOwnerFinding

	for _, rel := range docs {
		// #nosec G304 -- путь получен обходом дерева документации ЭТОГО репозитория, не извне
		raw, err := os.ReadFile(filepath.Join(opts.Root, rel))
		if err != nil {
			return nil, census, fmt.Errorf("%s: %w", rel, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, "<tr>") {
				continue
			}
			domain, ok := clientDocsSoleDomain(line, domainRe, opts.DomainAliases)
			if !ok {
				continue
			}
			names, ok := clientDocsOwnershipCell(line)
			if !ok {
				continue
			}
			census.OwnershipRow++
			for _, n := range names {
				owner, known := owners[n]
				if !known {
					outside[n]++
					census.NamesOutside++
					continue
				}
				census.NamesJudged++
				if owner == domain {
					continue
				}
				findings = append(findings, ClientDocsResourceOwnerFinding{
					File: rel, Line: i + 1, Domain: domain, Name: n, Owner: owner,
				})
			}
		}
	}

	for w := range outside {
		census.OutsideWords = append(census.OutsideWords, w)
	}
	sort.Strings(census.OutsideWords)

	if log != nil {
		_, _ = fmt.Fprintf(log,
			"перепись: контрактов %d · страниц документации %d · имён с единственным владельцем %d "+
				"(имён у нескольких доменов, выведено из словаря: %d) · строк владения распознано %d · "+
				"имён рассужено %d · имён вне словаря платформы, НЕ судятся: %d %v\n",
			census.ProtoFiles, census.DocFiles, census.OwnedNames, census.AmbiguousOut,
			census.OwnershipRow, census.NamesJudged, census.NamesOutside, census.OutsideWords)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Name < findings[j].Name
	})
	return findings, census, nil
}

// clientDocsOwnerMap строит карту «имя ресурса → домен» ИЗ КОНТРАКТОВ.
// Имя, за которым стоит больше одного домена, возвращается отдельно и не судится.
func clientDocsOwnerMap(opts ClientDocsResourceOwnerOptions) (map[string]string, []string, int, error) {
	root := filepath.Join(opts.Root, opts.ProtoRoot, "kacho", "cloud")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("каталог контрактов %s: %w", root, err)
	}
	seen := map[string]map[string]bool{}
	files := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		domain := e.Name()
		v1 := filepath.Join(root, domain, "v1")
		protos, err := filepath.Glob(filepath.Join(v1, "*.proto"))
		if err != nil {
			return nil, nil, 0, err
		}
		for _, p := range protos {
			// #nosec G304 -- путь получен обходом каталога контрактов ЭТОГО дерева
			raw, err := os.ReadFile(p)
			if err != nil {
				return nil, nil, 0, err
			}
			files++
			for _, m := range clientDocsProtoServiceRe.FindAllStringSubmatch(string(raw), -1) {
				svc := m[1]
				if strings.HasPrefix(svc, "Internal") || !strings.HasSuffix(svc, "Service") {
					continue
				}
				name := strings.TrimSuffix(svc, "Service")
				if name == "" {
					continue
				}
				if seen[name] == nil {
					seen[name] = map[string]bool{}
				}
				seen[name][domain] = true
			}
		}
	}
	owners := map[string]string{}
	var ambiguous []string
	for name, doms := range seen {
		if len(doms) != 1 {
			ambiguous = append(ambiguous, name)
			continue
		}
		for d := range doms {
			owners[name] = d
		}
	}
	sort.Strings(ambiguous)
	return owners, ambiguous, files, nil
}

// clientDocsPages — страницы клиентской документации и данные, из которых они
// рендерятся. Перечень ВЫВОДИТСЯ обходом дерева: выписанный разошёлся бы с ним
// молча, и новая страница осталась бы вне наблюдения — невидимо.
func clientDocsPages(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "build", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if !strings.Contains(rel, "/docs/content/") && !strings.Contains(rel, "/docs/src/") {
			return nil
		}
		switch filepath.Ext(rel) {
		case ".mdx", ".md", ".ts", ".tsx":
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// clientDocsDomainRe — как домен называет себя клиентской документации.
func clientDocsDomainRe(owners map[string]string, aliases map[string]string) (*regexp.Regexp, error) {
	set := map[string]bool{}
	for _, d := range owners {
		set[d] = true
	}
	for alias := range aliases {
		set[alias] = true
	}
	keys := make([]string, 0, len(set))
	for d := range set {
		keys = append(keys, regexp.QuoteMeta(d))
	}
	sort.Strings(keys)
	alt := strings.Join(keys, "|")
	return regexp.Compile(
		`kacho[-.](?:cloud\.)?(` + alt + `)\b` +
			`|/(` + alt + `)/v1/` +
			`|<(?:code|strong|td)>(` + alt + `)</`)
}

// clientDocsSoleDomain — домен строки, если он ровно один. Строка, называющая
// два домена, о владении не высказывается: она про связь, а не про владение.
func clientDocsSoleDomain(line string, re *regexp.Regexp, aliases map[string]string) (string, bool) {
	found := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(line, -1) {
		for _, g := range m[1:] {
			if g == "" {
				continue
			}
			if canon, ok := aliases[g]; ok {
				g = canon
			}
			found[g] = true
		}
	}
	if len(found) != 1 {
		return "", false
	}
	for d := range found {
		return d, true
	}
	return "", false
}

// clientDocsOwnershipCell — первая ячейка строки, несущая перечень имён через
// косую черту. Она и есть «что этот домен служит».
func clientDocsOwnershipCell(line string) ([]string, bool) {
	for _, m := range clientDocsCellRe.FindAllStringSubmatch(line, -1) {
		txt := clientDocsTagRe.ReplaceAllString(m[1], "")
		if !strings.Contains(txt, "/") {
			continue
		}
		var names []string
		for _, seg := range strings.Split(txt, "/") {
			if hit := clientDocsLeadNameRe.FindStringSubmatch(seg); hit != nil {
				names = append(names, hit[1])
			}
		}
		if len(names) < 2 {
			continue
		}
		return names, true
	}
	return nil, false
}
