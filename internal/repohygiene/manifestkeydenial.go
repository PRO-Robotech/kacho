// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// manifestkeydenial.go — приёмка объявила ключ манифеста незаводимым, а ключ в
// дереве ЗАВЕДЁН (задача продукта #1876).
//
// # Предмет
//
// Приёмка пинит ревизию, и её утверждение о дереве — ЗАМЕР на этой ревизии, а
// не ложь. Опасно не утверждение, а его форма: «`classes` не заводится» вместе
// с обоснованием читается как ДЕЙСТВУЮЩИЙ ЗАПРЕТ, и следующий, дойдя до него
// раньше, чем до кода, чинит код под неверный документ.
//
// Ровно так и вышло: §2.6а приёмки `#1778` назвала СВОЙ предикат пересмотра —
// «появится раскрыватель с владельцем, `classes` добавляется аддитивно», —
// предикат сработал, а перепроверить его было некому. Замер на ревизии круга 3
// (`ab771fe83`): ключа нет, файлов раскрывателя ноль. Замер на линии: ключ
// заведён (`roles.go` несёт `yaml:"classes"`), файлов раскрывателя шестнадцать.
// Оба замера верны — деревья разные.
//
// # Что здесь считается УТВЕРЖДЕНИЕМ о ключе
//
//	| `classes` | **не заводится** | …        ← утверждение: ключ ПОДЛЕЖАЩЕЕ
//	`classes` не существует ни в контракте    ← утверждение
//	Колонка жизненного цикла у `roles` не     ← НЕ утверждение: речь о колонке,
//	  заводится                                 ключ — уточнение
//	развернуть его в `verbs` некому            ← НЕ утверждение: ключ дополнение
//
// Разбор требует, чтобы ключ ОТКРЫВАЛ клаузу (начало строки, ячейка таблицы,
// выделение, скобка) либо стоял после слова «ключ», и чтобы отрицание шло за ним
// вплотную. Предикат «слово рядом со словом» отвергнут замером: на этом дереве
// он давал десять попаданий, из которых шесть — о колонке таблицы, об импорте и
// о внешне-адресуемой координате. Гейт, у которого 60 % находок ложные,
// отключают первым.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **Утверждение, где ключ не открывает клаузу** («Почему `classes` не
//     заводится»): единица суждения — ПАРА (документ, ключ), а не строка,
//     поэтому документ ловится другими своими строками. Перепись печатает
//     число распознанных строк, чтобы полнота не выдавалась за большую.
//  2. **Утверждение в блок-цитате**: в этом корпусе `>` несёт исторический
//     разбор и цитату снятого текста — они свидетельствуют о сделанном и правке
//     не подлежат. Там же живёт маркер состояния, поэтому иначе гейт считал бы
//     собственное объяснение проверяемого.
//  3. **Утверждение об отсутствии в СХЕМЕ или контракте**, а не о ключе
//     манифеста: разбор судит только имена, которые манифест сегодня несёт.
//
// # Самоистечение — в ОБЕ стороны
//
// Живой ключ без маркера — находка. Маркер, называющий ключ, которого манифест
// больше не несёт, — тоже находка: послабление, которому нечего исключать,
// унаследует следующая слепая зона.
//
// # ЧТЕНИЕ — ВНЕ ОБХОДА, и это не стиль
//
// Обход сообщает обратному вызову путь, описывающий состояние файловой системы
// на момент `lstat`, а не на момент чтения: между ними каталог можно подменить
// ссылкой, и чтение уйдёт наружу дерева. Поэтому обход здесь только СОБИРАЕТ
// относительные имена, а абсолютный путь склеивается с корнем уже после него —
// тем же порядком, каким это делает `clienttruth_treefiles.go` для своего
// семейства.
//
// Держится сканером безопасности на пине конвейера: возврат к чтению внутри
// обхода роняет джобу `gosec` анализатором G122 (файловая операция в обратном
// вызове обхода). Подавлять G122 здесь нечем — он и есть признак того самого
// класса, а не ложное срабатывание. G304 (путь-переменная в чтении) остаётся и
// подавлен по месту с причиной: имя приходит из осмотренного дерева, не извне.
package repohygiene

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ManifestKeyDenialOptions — что считать манифестом и что приёмкой.
type ManifestKeyDenialOptions struct {
	// Root — корень дерева.
	Root string
	// ManifestDirSuffix — каталог, чьи файлы Go объявляют ключи манифеста.
	ManifestDirSuffix string
	// AcceptanceDirSuffix — каталог приёмок, живущих рядом с кодом сервиса.
	AcceptanceDirSuffix string
}

// DefaultManifestKeyDenialOptions — раскладка этого дерева.
func DefaultManifestKeyDenialOptions(root string) ManifestKeyDenialOptions {
	return ManifestKeyDenialOptions{
		Root:                root,
		ManifestDirSuffix:   "internal/manifest",
		AcceptanceDirSuffix: "docs/engineering/acceptance",
	}
}

// ManifestKeyDenialKind — род находки.
type ManifestKeyDenialKind string

const (
	// DenialOfALiveKey — документ объявляет незаводимым ключ, который заведён.
	DenialOfALiveKey ManifestKeyDenialKind = "живой ключ объявлен незаводимым"
	// MarkerWithoutASubject — маркер состояния о ключе, которого манифест не несёт.
	MarkerWithoutASubject ManifestKeyDenialKind = "маркер состояния потерял предмет"
)

// ManifestKeyDenialFinding — одна находка с координатой.
type ManifestKeyDenialFinding struct {
	File string
	Line int
	Key  string
	Kind ManifestKeyDenialKind
	Text string
}

func (f ManifestKeyDenialFinding) String() string {
	return fmt.Sprintf("%s:%d: ключ `%s` — %s: %s", f.File, f.Line, f.Key, f.Kind, f.Text)
}

// ManifestKeyDenialCensus — объём осмотренного. Печатается всегда: «ноль
// находок» обязано быть отличимо от «ноль прочитанного».
type ManifestKeyDenialCensus struct {
	ManifestFiles int
	LiveKeys      int
	DocFiles      int
	DocLines      int
	ClaimLines    int
	MarkerBlocks  int
	KeysMarked    int
}

var (
	reYAMLTag = regexp.MustCompile("yaml:\"([a-zA-Z][a-zA-Z0-9_]*)")
	// reRevision — ревизия, на которой снят замер маркера. Без неё маркер
	// утверждал бы о дереве вообще, а утверждение о дереве без ревизии
	// проверить нечем.
	reRevision   = regexp.MustCompile(`(^|[^0-9a-zA-Z])[0-9a-f]{7,40}([^0-9a-zA-Z]|$)`)
	reBackticked = regexp.MustCompile("`([a-zA-Z][a-zA-Z0-9_]*)`")
)

// denialAfter — отрицание существования, идущее ЗА ключом вплотную.
const denialAfter = `[\s|*»)]{0,6}(?:не\s+заводится|не\s+заведён|не\s+заведен|не\s+существует)`

// claimLead — чем клауза открывается, чтобы ключ был в ней подлежащим: начало
// строки, ячейка таблицы, выделение, кавычка, скобка, тире — либо слово «ключ».
const claimLead = `(?:^|[|(«*–—\-]\s*|ключ(?:а|ом|у|е|ей)?\s+)`

func claimRE(key string) *regexp.Regexp {
	return regexp.MustCompile(claimLead + "`" + regexp.QuoteMeta(key) + "`" + denialAfter)
}

// AuditManifestKeyDenial — вердикт и перепись. Ошибка возвращается только там,
// где читать было нечем; пустой обход вызывающий судит по переписи.
func AuditManifestKeyDenial(opts ManifestKeyDenialOptions, log io.Writer) ([]ManifestKeyDenialFinding, ManifestKeyDenialCensus, error) {
	var census ManifestKeyDenialCensus

	live := map[string]bool{}
	var manifests, docs []string

	err := filepath.WalkDir(opts.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(opts.Root, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))

		switch {
		case strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") &&
			(strings.HasSuffix(dir, opts.ManifestDirSuffix) || strings.Contains(dir, opts.ManifestDirSuffix+"/")):
			manifests = append(manifests, rel)
		case strings.HasSuffix(rel, ".md") && strings.HasSuffix(dir, opts.AcceptanceDirSuffix):
			docs = append(docs, rel)
		}
		return nil
	})
	if err != nil {
		return nil, census, fmt.Errorf("обход дерева %s: %w", opts.Root, err)
	}
	sort.Strings(manifests)

	for _, rel := range manifests {
		// Путь склеен из корня осматриваемого дерева и ОТНОСИТЕЛЬНОГО имени,
		// пришедшего из обхода этого же дерева; подставить посторонний файл извне
		// нечем.
		raw, rderr := os.ReadFile(filepath.Join(opts.Root, filepath.FromSlash(rel)))
		if rderr != nil {
			// Файл, названный обходом и не прочитанный, — НАХОДКА, а не пропуск:
			// перепись перестала бы относиться к дереву, которое она назвала.
			return nil, census, fmt.Errorf("чтение манифеста %s: %w", rel, rderr)
		}
		census.ManifestFiles++
		for _, m := range reYAMLTag.FindAllStringSubmatch(string(raw), -1) {
			live[m[1]] = true
		}
	}
	census.LiveKeys = len(live)
	census.DocFiles = len(docs)
	sort.Strings(docs)

	var findings []ManifestKeyDenialFinding
	res := map[string]*regexp.Regexp{}
	for k := range live {
		res[k] = claimRE(k)
	}

	for _, rel := range docs {
		// Путь склеен из корня осматриваемого дерева и ОТНОСИТЕЛЬНОГО имени,
		// пришедшего из обхода этого же дерева; подставить посторонний файл извне
		// нечем.
		raw, rderr := os.ReadFile(filepath.Join(opts.Root, filepath.FromSlash(rel)))
		if rderr != nil {
			return nil, census, rderr
		}
		lines := strings.Split(string(raw), "\n")
		census.DocLines += len(lines)

		marked, blocks, markerFindings := markersIn(rel, lines, live)
		census.MarkerBlocks += blocks
		census.KeysMarked += len(marked)
		findings = append(findings, markerFindings...)

		for i, line := range lines {
			if isQuoted(line) {
				continue
			}
			for key, re := range res {
				if !strings.Contains(line, "`"+key+"`") || !re.MatchString(line) {
					continue
				}
				census.ClaimLines++
				if marked[key] {
					continue
				}
				findings = append(findings, ManifestKeyDenialFinding{
					File: rel, Line: i + 1, Key: key,
					Kind: DenialOfALiveKey, Text: strings.TrimSpace(line),
				})
			}
		}
	}

	sort.Slice(findings, func(a, b int) bool {
		if findings[a].File != findings[b].File {
			return findings[a].File < findings[b].File
		}
		return findings[a].Line < findings[b].Line
	})

	if log != nil {
		_, _ = fmt.Fprintf(log, "перепись: файлов манифеста %d · живых ключей %d · приёмок %d · строк %d\n",
			census.ManifestFiles, census.LiveKeys, census.DocFiles, census.DocLines)
		_, _ = fmt.Fprintf(log, "перепись: строк-утверждений %d · блоков состояния %d · ключей под маркером %d · находок %d\n",
			census.ClaimLines, census.MarkerBlocks, census.KeysMarked, len(findings))
	}
	return findings, census, nil
}

// isQuoted — блок-цитата: исторический разбор, цитата снятого текста и сам
// маркер состояния. Правке не подлежит и утверждением не считается.
func isQuoted(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), ">")
}

// markersIn — маркеры состояния документа. Маркер обязан нести слово
// СОСТОЯНИЕ, ключ в обратных кавычках и РЕВИЗИЮ: без ревизии он утверждал бы о
// дереве вообще, и проверить его было бы нечем.
func markersIn(rel string, lines []string, live map[string]bool) (map[string]bool, int, []ManifestKeyDenialFinding) {
	marked := map[string]bool{}
	var findings []ManifestKeyDenialFinding
	blocks := 0

	for i := 0; i < len(lines); i++ {
		if !isQuoted(lines[i]) {
			continue
		}
		start := i
		var block []string
		for i < len(lines) && isQuoted(lines[i]) {
			block = append(block, lines[i])
			i++
		}
		text := strings.Join(block, "\n")
		if !strings.Contains(text, "СОСТОЯНИЕ") || !reRevision.MatchString(text) {
			continue
		}
		blocks++
		// Предметом маркера считается ТОЛЬКО ключ, названный в одной строке со
		// словом СОСТОЯНИЕ. Иначе имя, помянутое во врезке вскользь, накрывало
		// бы собой чужое утверждение — маркер стал бы маской.
		var subjectLine string
		for _, l := range block {
			if strings.Contains(l, "СОСТОЯНИЕ") {
				subjectLine += l + "\n"
			}
		}
		for _, m := range reBackticked.FindAllStringSubmatch(subjectLine, -1) {
			key := m[1]
			if live[key] {
				marked[key] = true
				continue
			}
			if isManifestKeyShaped(key) {
				findings = append(findings, ManifestKeyDenialFinding{
					File: rel, Line: start + 1, Key: key,
					Kind: MarkerWithoutASubject,
					Text: "манифест этого ключа не несёт — маркеру нечего снимать",
				})
			}
		}
	}
	return marked, blocks, findings
}

// isManifestKeyShaped — отсекает от разбора маркера обычные слова и координаты:
// предметом маркера бывает только имя ключа, а имена ключей манифеста в этом
// дереве записаны нижним верблюжьим регистром без разделителей.
func isManifestKeyShaped(s string) bool {
	if len(s) < 3 || len(s) > 24 {
		return false
	}
	if s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			continue
		}
		return false
	}
	return true
}
