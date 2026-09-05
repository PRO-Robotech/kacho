// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// docsbuildfiletarget_test.go — команда сборки образа в документе не называет
// Dockerfile, которого в дереве нет.
//
// # Предмет
//
// Полирепо-остаток той же породы, что и переход в несуществующий каталог
// (docscdtarget_test.go), но с другим следствием. Страница установки пишет
// `docker build -f kacho-vpc/Dockerfile -t kacho-vpc:dev .` и рядом объясняет,
// что образ собирается из РОДИТЕЛЬСКОГО каталога, где лежат сиблинг-репозитории
// `kacho-corelib`, `kacho-proto`, `kacho-vpc`. Такой топологии нет: разработка
// идёт в одном репозитории, Dockerfile лежит в `services/<svc>/Dockerfile`, а
// контекст сборки — корень этого репозитория (`COPY . .`).
//
// Читатель копирует строку и получает отказ демона на несуществующем файле.
// Документ не исполняется, поэтому отказ достаётся тому, кто скопировал, и
// выглядит как его ошибка.
//
// # Почему гейт, а не разовая правка
//
// Класс объявлялся vpc-специфичным. Поиск ПО СВОЙСТВУ («процитированная команда
// сборки, чей -f не резолвится»), а не по названному месту, нашёл его и у geo —
// то есть разовая правка страницы vpc унесла бы с собой читателя, который класс
// обнаружил, и оставила второй экземпляр в дереве.
//
// # Что считается находкой
//
// Тройка (документ, строка, путь), где путь при флаге `-f`/`--file` команды
// `docker build` не разрешается в ОТСЛЕЖИВАЕМЫЙ файл НИ ПОД ОДНИМ прочтением
// рабочего каталога: ни от корня репозитория, ни от каталога документа, ни от
// любого его предка. Предпосылка намеренно слабая — путь, не разрешимый ни при
// каком прочтении, неверен при всяком.
//
// # Что находкой НЕ является
//
//   - запись-образец: путь содержит подстановку (`$VAR`, `<repo>`, `{x}`, `…`)
//     либо абсолютен. Резолвить нечего;
//   - `docker build` без `-f`: файл берётся из контекста, называть нечего;
//   - путь, разрешимый хотя бы под одним прочтением, — включая
//     `docker build -f host/Dockerfile` со страницы каталога `ui-future/deploy`.
//
// # Перепись
//
// Печатается: документов осмотрено, кусков по видам, цитат `docker build`, из
// них с флагом файла, записей-образцов, отслеживаемых файлов. Ноль цитат —
// ОТКАЗ, а не успех: гейт, чей предмет — отсутствие, молчит одинаково и когда
// предмета нет, и когда сломан разбор. Условие самоистекающее: исчезнут цитаты
// сборки из документов — гейт попросит себя перечитать.
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

var (
	// Команда сборки образа. Слово `build` обязано идти следом за `docker`:
	// `docker buildx bake` и `docker builder prune` предметом не являются.
	docDockerBuildRe = regexp.MustCompile(`\bdocker[\t ]+build\b`)
	// Флаг файла в ОСТАТКЕ строки после `docker build` — иначе `-f` соседней
	// команды той же строки (`make -f …  && docker build .`) читался бы как наш.
	docDockerFileFlagRe = regexp.MustCompile(`(?:^|[\t ])(?:-f|--file)(?:[\t ]+|=)([^\s;&|)'"]+)`)
	// Подстановка/образец: резолвить нечего.
	docBuildNotationRe = regexp.MustCompile(`[$*{}<>]|\.\.\.|…`)
)

type docBuildFinding struct {
	doc  string
	line int
	kind string
	path string
}

func (f docBuildFinding) String() string {
	return fmt.Sprintf("%s:%d (%s) — docker build -f %s", f.doc, f.line, f.kind, f.path)
}

// docBuildCensus — объём осмотренного. Печатается всегда, чтобы «ноль находок»
// было отличимо от «ноль прочитанного».
type docBuildCensus struct {
	docs      int
	chunks    map[string]int
	citations int
	withFlag  int
	notation  int
	files     int
}

func (c docBuildCensus) String() string {
	return fmt.Sprintf(
		"документов %d; кусков: %s %d, %s %d, %s %d; цитат `docker build` %d, "+
			"из них с флагом файла %d; записей-образцов %d; файлов в индексе %d",
		c.docs,
		regionFenced, c.chunks[regionFenced], regionSpan, c.chunks[regionSpan],
		regionMDXTpl, c.chunks[regionMDXTpl],
		c.citations, c.withFlag, c.notation, c.files)
}

// isDocBuildNotation — путь является образцом, а не координатой.
func isDocBuildNotation(p string) bool {
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") || strings.Contains(p, `\`) {
		return true
	}
	if len(p) >= 2 && p[1] == ':' {
		return true // путь чужой оболочки: D:\Repos\…
	}
	return docBuildNotationRe.MatchString(p)
}

// docBuildBases — прочтения рабочего каталога для документа: корень репозитория,
// каталог документа и каждый его предок.
func docBuildBases(doc string) []string {
	bases := []string{""}
	dir := filepath.ToSlash(filepath.Dir(doc))
	for dir != "." && dir != "/" && dir != "" {
		bases = append(bases, dir)
		dir = filepath.ToSlash(filepath.Dir(dir))
	}
	return bases
}

func docBuildResolves(p string, bases []string, files map[string]bool) bool {
	for _, base := range bases {
		cand := filepath.ToSlash(filepath.Clean(filepath.Join(base, p)))
		if files[cand] {
			return true
		}
	}
	return false
}

// scanDocBuildFileTargets судит каждую цитату `docker build` в исполняемых
// областях docs против множества отслеживаемых файлов.
func scanDocBuildFileTargets(docs []string, files map[string]bool, read func(rel string) ([]byte, error)) ([]docBuildFinding, docBuildCensus, error) {
	census := docBuildCensus{docs: len(docs), chunks: map[string]int{}, files: len(files)}
	var findings []docBuildFinding
	for _, rel := range docs {
		body, err := read(rel)
		if err != nil {
			return nil, census, fmt.Errorf("%s: %w — документ не прочитан, а непрочитанный "+
				"документ обязан быть отказом, а не молчаливым нулём", rel, err)
		}
		bases := docBuildBases(rel)
		for _, ch := range executableChunks(string(body), strings.HasSuffix(rel, ".mdx")) {
			census.chunks[ch.kind]++
			loc := docDockerBuildRe.FindStringIndex(ch.text)
			if loc == nil {
				continue
			}
			census.citations++
			m := docDockerFileFlagRe.FindStringSubmatch(ch.text[loc[1]:])
			if m == nil {
				continue
			}
			census.withFlag++
			p := m[1]
			if isDocBuildNotation(p) {
				census.notation++
				continue
			}
			if !docBuildResolves(p, bases, files) {
				findings = append(findings, docBuildFinding{rel, ch.line, ch.kind, p})
			}
		}
	}
	return findings, census, nil
}

// ── гейт на дереве ───────────────────────────────────────────────────────────

func TestDocsBuildCommandsNameADockerfileTheTreeHas(t *testing.T) {
	root := repoRoot(t)
	docs, files := trackedDocsAndFiles(t, root)

	osRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("открыть корень %s: %v", root, err)
	}
	defer func() { _ = osRoot.Close() }()

	findings, census, err := scanDocBuildFileTargets(docs, files, osRoot.ReadFile)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	t.Logf("перепись: %s", census)

	// Положительная половина. Гейт утверждает ОТСУТСТВИЕ, а такой гейт молчит
	// одинаково и когда предмета нет, и когда сломан разбор областей документа.
	// Ноль цитат означает «не прочитано», пока не доказано обратное.
	if census.citations == 0 {
		t.Fatalf("предпосылка не выполняется: в %d документах не найдено ни одной цитаты "+
			"`docker build`. Либо страницы перестали приводить сборку образа (тогда гейту "+
			"нечего стеречь и его надо снять вместе с предметом), либо сломан разбор "+
			"исполняемых областей — и тогда весь корпус молча не читается, ровно тот дефект, "+
			"ради которого гейт заведён", census.docs)
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Fatalf("%d команд(ы) сборки называют Dockerfile, которого нет в дереве ни под одним "+
			"прочтением рабочего каталога:%s\n\nDockerfile сервиса лежит в "+
			"services/<svc>/Dockerfile, контекст сборки — корень ЭТОГО репозитория "+
			"(`COPY . .`). Сиблинг-репозиториев kacho-<part> рядом нет: разработка идёт в "+
			"одном репозитории.\nПерепись: %s",
			len(findings), b.String(), census)
	}
}

// trackedDocsAndFiles — отслеживаемые документы и множество файлов индекса.
// Единица счёта — элемент git-индекса, а не то, что лежит на диске: иначе
// объявление и поведение разъезжаются молча.
func trackedDocsAndFiles(t *testing.T, root string) ([]string, map[string]bool) {
	t.Helper()
	out, err := gitenv.Command(root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — без переписи «ноль находок» неотличимо от "+
			"«ноль прочитанного»", err)
	}
	files := map[string]bool{}
	var docs []string
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p == "" || strings.Contains(p, "/node_modules/") {
			continue
		}
		files[p] = true
		if strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".mdx") {
			docs = append(docs, p)
		}
	}
	if len(docs) == 0 {
		t.Fatal("отслеживаемых документов ноль — гейту нечего осматривать")
	}
	return docs, files
}
