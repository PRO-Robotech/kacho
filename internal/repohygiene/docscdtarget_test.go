// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// docscdtarget_test.go — команда в документе не переходит в каталог, которого
// в дереве нет.
//
// # Предмет
//
// Полирепо-остаток: страница пишет `cd ../kacho-deploy && make dev-up`. Каталога
// с таким именем в дереве нет ни под одним прочтением «где я стою» — стенд
// поднимается целью каталога deploy того же репозитория. Читатель копирует
// строку в терминал и получает отказ оболочки, а не платформу.
//
// # Почему гейт, а не разовая правка
//
// Этот класс уже сводили к нулю — и объявленный ноль оказался ложным по двум
// независимым причинам, каждой из которых хватало в одиночку:
//
//  1. Читатель видел только огороженный блок и код-спан. Исполняемая оболочка
//     страниц сайта документации живёт в MDX-регионе <CodeBlock>{dedent`…`} — это ни то,
//     ни другое, поэтому регион не читался вовсе, а его цитаты не попадали даже
//     в перепись осмотренного: «ноль находок» было неотличимо от «ноль
//     прочитанного» ровно там, где лежали находки.
//  2. Сопоставитель `cd` был привязан к нулевой колонке. MDX-регион отступлен ПО
//     ПОСТРОЕНИЮ (помощник dedent для того и существует), поэтому даже
//     прочитанный регион отдал бы ноль совпадений.
//
// Разовая правка снимает шесть строк и уносит с собой читателя, который их
// нашёл. Гейт остаётся в дереве, краснеет в CI и называет координату.
//
// # Что считается находкой
//
// Тройка (документ, строка, цель `cd`), где цель не разрешается НИ ПОД ОДНИМ
// прочтением рабочего каталога: ни от корня репозитория, ни от каталога
// документа, ни от любого его предка. Предпосылка намеренно слабая — цель,
// не разрешимая ни при каком прочтении, неверна при всяком.
//
// # Что находкой НЕ является
//
//   - запись-образец: цель содержит подстановку (`$DIR`, `<repo>`, `{x}`, `…`),
//     абсолютный путь, `-`, `~` или путь чужой оболочки (`D:\…`). Резолвить
//     нечего, и «не резолвится» было бы обвинением на пустом месте;
//   - цель, разрешимая хотя бы под одним прочтением, — включая `cd ..` и
//     `cd docs` со страницы этого же сайта.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// ── чтение исполняемых областей документа ────────────────────────────────────

const (
	regionFenced = "огороженный блок"
	regionSpan   = "код-спан"
	regionMDXTpl = "MDX-регион"
)

var (
	docFenceRe = regexp.MustCompile("^[\t ]*(?:```+|~~~+)")
	docSpanRe  = regexp.MustCompile("`([^`\n]+)`")
	// Шаблонный литерал JS, открытый внутри MDX: строка ЗАКАНЧИВАЕТСЯ обратной
	// кавычкой, которую вводит выражение-контейнер, помощник, присваивание или
	// вызов. Требование вводящего знака оставляет за бортом обычную прозу с
	// непарной кавычкой: проза не кончается на `{dedent` + кавычка.
	docTplOpenRe  = regexp.MustCompile("(?:\\{[\t ]*(?:dedent|String\\.raw)?[\t ]*|[=(,][\t ]*)`[\t ]*$")
	docTplCloseRe = regexp.MustCompile("^[\t ]*`[\t ]*[})];?[\t ]*$|^[\t ]*`[\t ]*$")
	// Ведущий отступ здесь не украшение: MDX-регион отступлен по построению, и
	// сопоставитель, привязанный к нулевой колонке, читает весь регион как не
	// содержащий `cd` вовсе.
	docCdRe = regexp.MustCompile(`(?:^[\t ]*|[;&|][\t ]*|\([\t ]*)cd[\t ]+([^\s;&|)]+)`)
	// Подстановка/образец: резолвить нечего.
	docCdNotationRe = regexp.MustCompile(`[$*{}<>]|\.\.\.|…`)
)

// docChunk — исполняемый кусок документа: строка огороженного блока или
// MDX-региона, либо содержимое одного код-спана.
type docChunk struct {
	line int
	kind string
	text string
}

// executableChunks разбирает документ на исполняемые куски трёх видов.
// isMDX включает разбор шаблонного литерала — в обычном markdown такой формы
// нет, и включать её там значило бы читать прозу как код.
func executableChunks(body string, isMDX bool) []docChunk {
	var out []docChunk
	fenced, inTpl := false, false
	for i, line := range strings.Split(body, "\n") {
		lineno := i + 1
		switch {
		case inTpl:
			if docTplCloseRe.MatchString(line) {
				inTpl = false
				continue
			}
			out = append(out, docChunk{lineno, regionMDXTpl, line})
		case docFenceRe.MatchString(line):
			fenced = !fenced
		case fenced:
			out = append(out, docChunk{lineno, regionFenced, line})
		case isMDX && docTplOpenRe.MatchString(line):
			inTpl = true
		default:
			for _, m := range docSpanRe.FindAllStringSubmatch(line, -1) {
				out = append(out, docChunk{lineno, regionSpan, m[1]})
			}
		}
	}
	return out
}

// ── сам предикат ─────────────────────────────────────────────────────────────

type docCdFinding struct {
	doc    string
	line   int
	kind   string
	target string
}

func (f docCdFinding) String() string {
	return fmt.Sprintf("%s:%d (%s) — cd %s", f.doc, f.line, f.kind, f.target)
}

// docCdCensus — объём осмотренного. Печатается всегда, чтобы «ноль находок» было
// отличимо от «ноль прочитанного».
type docCdCensus struct {
	docs      int
	chunks    map[string]int
	citations map[string]int
	notation  int
	dirs      int
}

func (c docCdCensus) String() string {
	return fmt.Sprintf(
		"документов %d; кусков: %s %d, %s %d, %s %d; цитат `cd`: %s %d, %s %d, %s %d; "+
			"записей-образцов %d; каталогов в индексе %d",
		c.docs,
		regionFenced, c.chunks[regionFenced], regionSpan, c.chunks[regionSpan],
		regionMDXTpl, c.chunks[regionMDXTpl],
		regionFenced, c.citations[regionFenced], regionSpan, c.citations[regionSpan],
		regionMDXTpl, c.citations[regionMDXTpl],
		c.notation, c.dirs)
}

// scanDocCdTargets судит каждую цитату `cd` в исполняемых областях docs против
// множества каталогов dirs. read отдаёт содержимое документа по его пути
// относительно корня осматриваемого дерева.
func scanDocCdTargets(docs []string, dirs map[string]bool, read func(rel string) ([]byte, error)) ([]docCdFinding, docCdCensus, error) {
	census := docCdCensus{
		docs:      len(docs),
		chunks:    map[string]int{},
		citations: map[string]int{},
		dirs:      len(dirs),
	}
	var findings []docCdFinding
	for _, rel := range docs {
		body, err := read(rel)
		if err != nil {
			return nil, census, fmt.Errorf("%s: %w — документ не прочитан, а непрочитанный "+
				"документ нельзя ни засчитать в перепись, ни молча пропустить", rel, err)
		}
		home := filepath.ToSlash(filepath.Dir(rel))
		if home == "." {
			home = ""
		}
		bases := []string{home}
		for cur := home; cur != ""; {
			cur = filepath.ToSlash(filepath.Dir(cur))
			if cur == "." {
				cur = ""
			}
			bases = append(bases, cur)
		}
		for _, ch := range executableChunks(string(body), strings.HasSuffix(rel, ".mdx")) {
			census.chunks[ch.kind]++
			for _, m := range docCdRe.FindAllStringSubmatch(ch.text, -1) {
				census.citations[ch.kind]++
				target := strings.Trim(m[1], `'"`)
				if isDocCdNotation(target) {
					census.notation++
					continue
				}
				if !docCdResolvesUnderAnyBase(target, bases, dirs) {
					findings = append(findings, docCdFinding{rel, ch.line, ch.kind, target})
				}
			}
		}
	}
	return findings, census, nil
}

func isDocCdNotation(target string) bool {
	switch {
	case target == "-", target == "~":
		return true
	case strings.HasPrefix(target, "/"), strings.HasPrefix(target, "~"),
		strings.HasPrefix(target, "$"), strings.Contains(target, `\`):
		return true
	case len(target) >= 2 && target[1] == ':':
		return true // путь чужой оболочки: D:\Repos\…
	}
	return docCdNotationRe.MatchString(target)
}

func docCdResolvesUnderAnyBase(target string, bases []string, dirs map[string]bool) bool {
	for _, base := range bases {
		cand := filepath.ToSlash(filepath.Clean(filepath.Join(base, target)))
		if cand == "." || cand == "" || dirs[cand] {
			return true
		}
	}
	return false
}

// ── гейт на дереве ───────────────────────────────────────────────────────────

func TestDocsDoNotChangeIntoADirectoryTheTreeDoesNotHave(t *testing.T) {
	root := repoRoot(t)
	docs, dirs := trackedDocsAndDirs(t, root)

	osRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("открыть корень %s: %v", root, err)
	}
	defer func() { _ = osRoot.Close() }()

	findings, census, err := scanDocCdTargets(docs, dirs, osRoot.ReadFile)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	t.Logf("перепись: %s", census)

	// Предпосылка гейта: в корпусе ЕСТЬ MDX-регионы. Без них третий читатель
	// лишний, и молчать он будет не потому, что дерево чистое. Условие
	// самоистекающее: исчезнут регионы — гейт покраснеет и попросит себя
	// перечитать, а не тихо перестанет что-либо проверять.
	if census.chunks[regionMDXTpl] == 0 {
		t.Fatalf("предпосылка не выполняется: в %d документах не найдено ни одного %s "+
			"<CodeBlock>{dedent`…`}. Либо страницы перестали его использовать (тогда "+
			"третий читатель здесь больше не нужен), либо разбор региона сломан "+
			"(тогда весь регион молча не читается — ровно тот дефект, ради которого "+
			"гейт заведён)", census.docs, regionMDXTpl)
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Fatalf("%d команд(ы) переходят в каталог, которого нет в дереве ни под одним "+
			"прочтением рабочего каталога:%s\n\nЦель каталога того же репозитория "+
			"называется флагом -C (`make -C deploy dev-up`), а не переходом в соседний "+
			"репозиторий, которого рядом нет.\nПерепись: %s",
			len(findings), b.String(), census)
	}
}

// trackedDocsAndDirs — отслеживаемые документы и множество каталогов индекса.
// Единица счёта — элемент git-индекса, а не то, что лежит на диске: иначе
// объявление и поведение разъезжаются молча.
func trackedDocsAndDirs(t *testing.T, root string) ([]string, map[string]bool) {
	t.Helper()
	out, err := gitenv.Command(root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — без переписи «ноль находок» неотличимо от "+
			"«ноль прочитанного»", err)
	}
	dirs := map[string]bool{}
	var docs []string
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p == "" || strings.Contains(p, "/node_modules/") {
			continue
		}
		for i, part := range strings.Split(p, "/") {
			_ = part
			if i > 0 {
				dirs[strings.Join(strings.Split(p, "/")[:i], "/")] = true
			}
		}
		if strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".mdx") {
			docs = append(docs, p)
		}
	}
	if len(docs) == 0 {
		t.Fatal("отслеживаемых документов ноль — гейту нечего осматривать")
	}
	return docs, dirs
}

// ── контроль в обе стороны ───────────────────────────────────────────────────

// fixtureTree строит крошечное дерево и отдаёт аргументы предиката так же, как
// их получает боевой прогон.
func fixtureTree(t *testing.T, files map[string]string) ([]string, map[string]bool, func(string) ([]byte, error)) {
	t.Helper()
	dirs := map[string]bool{}
	var docs []string
	for p := range files {
		parts := strings.Split(p, "/")
		for i := 1; i < len(parts); i++ {
			dirs[strings.Join(parts[:i], "/")] = true
		}
		if strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".mdx") {
			docs = append(docs, p)
		}
	}
	return docs, dirs, func(rel string) ([]byte, error) {
		body, ok := files[rel]
		if !ok {
			return nil, fmt.Errorf("нет такого файла: %s", rel)
		}
		return []byte(body), nil
	}
}

const mdxStandPage = `---
title: deploy
---

## Локальный dev-стенд

<CodeBlock language="bash">
  {dedent` + "`" + `
    %s
  ` + "`" + `}
</CodeBlock>
`

func TestDocsCdGate_ProvenByInjection(t *testing.T) {
	// Дерево-близнец боевого: страница сайта документации сервиса, каталог стенда
	// deploy лежит в корне репозитория, соседнего репозитория стенда нет.
	tree := func(command string) map[string]string {
		return map[string]string{
			"services/vpc/docs/content/install/deploy.mdx": fmt.Sprintf(mdxStandPage, command),
			"deploy/Makefile": "dev-up:\n\t@true\n",
		}
	}

	t.Run("дефект возвращён — гейт краснеет и называет координату", func(t *testing.T) {
		docs, dirs, read := fixtureTree(t, tree("cd ../kacho-deploy && make dev-up"))
		findings, census, err := scanDocCdTargets(docs, dirs, read)
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		if census.citations[regionMDXTpl] != 1 {
			t.Fatalf("цитата в %s не прочитана (%d) — гейт молчал бы не потому, что "+
				"дерево чистое: %s", regionMDXTpl, census.citations[regionMDXTpl], census)
		}
		if len(findings) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d: %v", len(findings), findings)
		}
		got := findings[0]
		if got.doc != "services/vpc/docs/content/install/deploy.mdx" || got.line != 9 ||
			got.kind != regionMDXTpl || got.target != "../kacho-deploy" {
			t.Fatalf("находка не называет координату: %s", got)
		}
	})

	t.Run("законный близнец той же формы — гейт молчит", func(t *testing.T) {
		// Та же страница, тот же регион, тот же глагол, отступ тот же — меняется
		// только существо: каталог в дереве есть.
		docs, dirs, read := fixtureTree(t, tree("cd deploy && make dev-up"))
		findings, census, err := scanDocCdTargets(docs, dirs, read)
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		if census.citations[regionMDXTpl] != 1 {
			t.Fatalf("близнец должен быть ПРОЧИТАН, иначе молчание ничего не значит: %s", census)
		}
		if len(findings) != 0 {
			t.Fatalf("ложное срабатывание на законной конструкции: %v", findings)
		}
	})

	t.Run("исправленная форма без перехода вовсе — гейт молчит", func(t *testing.T) {
		docs, dirs, read := fixtureTree(t, tree("make -C deploy dev-up"))
		findings, _, err := scanDocCdTargets(docs, dirs, read)
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("ложное срабатывание на исправленной форме: %v", findings)
		}
	})

	t.Run("запись-образец не судится", func(t *testing.T) {
		docs, dirs, read := fixtureTree(t, tree("cd $STAND_DIR && make dev-up"))
		findings, census, err := scanDocCdTargets(docs, dirs, read)
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		if census.notation != 1 || len(findings) != 0 {
			t.Fatalf("подстановка обязана уйти в образцы: образцов %d, находок %v",
				census.notation, findings)
		}
	})
}

// TestDocsCdGate_ReadsTheRegionsItClaims — обе причины, по которым прошлый
// читатель объявил ноль там, где было шесть. Проба держит каждую отдельно:
// сломается любая — покраснеет именно она, а не «где-то ноль».
func TestDocsCdGate_ReadsTheRegionsItClaims(t *testing.T) {
	t.Run("MDX-регион читается, а огороженным блоком не является", func(t *testing.T) {
		chunks := executableChunks(fmt.Sprintf(mdxStandPage, "cd deploy"), true)
		var seen []string
		for _, c := range chunks {
			if c.kind == regionMDXTpl {
				seen = append(seen, strings.TrimSpace(c.text))
			}
		}
		if len(seen) != 1 || seen[0] != "cd deploy" {
			t.Fatalf("содержимое %s не прочитано: %#v", regionMDXTpl, seen)
		}
		for _, c := range chunks {
			if c.kind == regionFenced {
				t.Fatalf("%s ошибочно принят за %s — тогда его чтение зависело бы от "+
					"разбора заборов: %#v", regionMDXTpl, regionFenced, c)
			}
		}
	})

	t.Run("в обычном markdown шаблонный литерал не разбирается", func(t *testing.T) {
		// Проза с непарной кавычкой не должна открывать регион и утаскивать
		// следующие абзацы в «код».
		body := "Строка с непарной кавычкой `\nи следующий абзац: cd ../kacho-deploy\n"
		for _, c := range executableChunks(body, false) {
			if c.kind == regionMDXTpl {
				t.Fatalf("проза прочитана как %s: %#v", regionMDXTpl, c)
			}
		}
	})

	t.Run("отступ не прячет команду от сопоставителя", func(t *testing.T) {
		indented := "    cd ../kacho-deploy && make dev-up"
		if m := docCdRe.FindStringSubmatch(indented); m == nil || m[1] != "../kacho-deploy" {
			t.Fatalf("сопоставитель привязан к нулевой колонке — MDX-регион отступлен "+
				"по построению, и весь он читался бы как не содержащий `cd`: %#v", m)
		}
		if m := docCdRe.FindStringSubmatch("cd ../kacho-deploy"); m == nil {
			t.Fatalf("сопоставитель перестал видеть команду без отступа")
		}
	})
}
