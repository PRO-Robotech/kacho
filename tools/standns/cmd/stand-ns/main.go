// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Команда stand-ns — Go-половина стенда проб в своём пространстве имён
// (kacho#3102). Оболочку ведёт deploy/scripts/stand-ns.sh; здесь только то,
// что требует РАЗБОРА YAML, а не текста.
//
//	stand-ns relocate -namespace NS -ca-namespace CANS < рендер > рендер
//	    пост-обработчик рендера helm (deploy/helm/plugins/kacho-stand-ns):
//	    переносит стенд в NS, отказывает на непереносимом (tools/standns)
//	stand-ns images -list СЛОЙ…
//	    печатает имена образов частей ЭТОГО дерева, объявленных цепочкой
//	stand-ns images -refs "образ=ссылка …" СЛОЙ…
//	    печатает накладку, ставящую каждому такому образу названную ссылку
//	stand-ns inputs -root КОРЕНЬ -base КОММИТ [-godeps ЗАПИСЬ]
//	    судит каждый путь, отличающий рабочую копию от КОММИТ: вход ли он
//	    сборки образов (правило — tools/standns/inputs.go); код 1 — вход менялся.
//	    С -godeps замыкание пакетов берётся из ЗАПИСИ, а не из `go list` машины
//	stand-ns godeps -root КОРЕНЬ -tracked ПЕРЕЧЕНЬ -untracked ПЕРЕЧЕНЬ > ЗАПИСЬ
//	    замыкание пакетов каждого образа go дерева (`go list -deps`) записью JSON:
//	    её пишет pod сборки инструментов во внешнем кластере над деревом машины,
//	    перечни — `git ls-files` и `git ls-files --others --exclude-standard` машины
//	    (kacho#3353: Go на машине оператора не запускается)
//
// Коды: 0 — исполнено; 1 — находка (часть без ссылки); 2 — отказ входа.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/PRO-Robotech/corelib/gitenv"
	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/internal/localimages"
	"github.com/PRO-Robotech/kacho/tools/standns"
)

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr, os.Args[1:]))
}

func run(stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "stand-ns: режим не назван (relocate | images | inputs | godeps)")
		return 2
	}
	switch args[0] {
	case "relocate":
		return relocate(stdin, stdout, stderr, args[1:])
	case "images":
		return images(stdout, stderr, args[1:])
	case "inputs":
		return inputs(stdout, stderr, args[1:])
	case "godeps":
		return godeps(stdout, stderr, args[1:])
	}
	_, _ = fmt.Fprintf(stderr, "stand-ns: неизвестный режим %q (relocate | images | inputs | godeps)\n", args[0])
	return 2
}

func relocate(stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("relocate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ns := fs.String("namespace", "", "пространство имён стенда")
	ca := fs.String("ca-namespace", "", "пространство ресурсов cert-manager")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	census, err := standns.Relocate(stdin, stdout, standns.Options{Namespace: *ns, CANamespace: *ca})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "stand-ns relocate: ОТКАЗ — %v\n", err)
		return 2
	}
	_, _ = fmt.Fprintf(stderr, "stand-ns relocate → %s: %s\n", *ns, census)
	return 0
}

func images(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("images", flag.ContinueOnError)
	fs.SetOutput(stderr)
	list := fs.Bool("list", false, "напечатать образы частей этого дерева")
	refsArg := fs.String("refs", "", "«образ=ссылка» через пробел или запятую")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	files := fs.Args()
	if len(files) == 0 {
		_, _ = fmt.Fprintln(stderr, "stand-ns images: не названо ни одного слоя — образы выводить не из чего")
		return 2
	}
	folded := map[string]any{}
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "stand-ns images: слой %s не читается (%v)\n", f, err)
			return 2
		}
		var layer map[string]any
		if err := yaml.Unmarshal(raw, &layer); err != nil {
			_, _ = fmt.Fprintf(stderr, "stand-ns images: слой %s не разбирается (%v)\n", f, err)
			return 2
		}
		folded = localimages.Merge(folded, layer)
	}
	if *list {
		names := localimages.TreeImages(folded)
		if len(names) == 0 {
			_, _ = fmt.Fprintln(stderr, "stand-ns images: частей этого дерева цепочка не объявляет ни одной — это не «нечего переводить», а слепой обход")
			return 2
		}
		_, _ = fmt.Fprintln(stdout, strings.Join(names, "\n"))
		return 0
	}
	refs := map[string]string{}
	for _, kv := range strings.FieldsFunc(*refsArg, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
		img, ref, ok := strings.Cut(kv, "=")
		if !ok || img == "" || ref == "" {
			_, _ = fmt.Fprintf(stderr, "stand-ns images: «%s» не в форме образ=ссылка\n", kv)
			return 2
		}
		refs[img] = ref
	}
	overlay, census := localimages.PublishedOverlay(folded, refs)
	_, _ = fmt.Fprintf(stderr, "stand-ns images: объявлений %d, переписано %d, вынесенных частей %d, сторонних %d\n",
		census.Declarations, census.Rewritten, census.External, census.Foreign)
	if len(census.Unresolved) > 0 {
		_, _ = fmt.Fprintf(stderr, "НАХОДКА: части этого дерева без ссылки: %s — стенд исполнял бы смесь двух ревизий\n",
			strings.Join(census.Unresolved, " "))
		return 1
	}
	if census.Rewritten == 0 {
		_, _ = fmt.Fprintln(stderr, "stand-ns images: не переписано ни одного объявления — пустая накладка подняла бы стенд на пинах цепочки")
		return 2
	}
	body, err := yaml.Marshal(overlay)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "stand-ns images: накладка не сериализуется (%v)\n", err)
		return 2
	}
	_, _ = fmt.Fprintf(stdout, "# generated by 'stand-ns images' — не редактировать, не коммитить\n%s", body)
	return 0
}

// inputs — правило различения «вход сборки образа / не вход» над разностью
// рабочей копии и ревизии образов. Коды: 0 — входы не менялись; 1 — менялись
// (перечислены); 2 — отказ входа (дерево не прочитано, контекст не установлен).
func inputs(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("inputs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "корень рабочей копии")
	base := fs.String("base", "", "коммит опубликованных образов")
	recFile := fs.String("godeps", "", "запись замыкания пакетов (stand-ns godeps) вместо go list этой машины")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *root == "" || *base == "" {
		_, _ = fmt.Fprintln(stderr, "stand-ns inputs: нужны -root и -base")
		return 2
	}
	git := func(a ...string) (string, error) {
		cmd := gitenv.Command(*root, a...)
		cmd.Stderr = stderr
		out, err := cmd.Output()
		return string(out), err
	}
	lines := nonEmptyLines
	trackedOut, err := git("ls-files")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "stand-ns inputs: перечень файлов не прочитан (%v)\n", err)
		return 2
	}
	untrackedOut, err := git("ls-files", "--others", "--exclude-standard")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "stand-ns inputs: неотслеживаемые не прочитаны (%v)\n", err)
		return 2
	}
	tracked := lines(trackedOut)
	untracked := lines(untrackedOut)
	dfs, err := readDockerfiles(*root, tracked)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "stand-ns inputs: %v\n", err)
		return 2
	}
	all := append(append([]string(nil), tracked...), untracked...)
	deps := goListDeps(*root, all)
	if *recFile != "" {
		body, err := os.ReadFile(filepath.Clean(*recFile))
		var recs []standns.GoDepsRecord
		if err == nil {
			err = json.Unmarshal(body, &recs)
		}
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "stand-ns inputs: запись замыкания %s не прочитана (%v)\n", *recFile, err)
			return 2
		}
		deps = standns.ReplayGoDeps(recs)
	}
	closure, err := standns.BuildClosure(all, dfs, deps)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "stand-ns inputs: ОТКАЗ — %v\n", err)
		return 2
	}
	diffOut, err := git("diff", "--name-only", *base)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "stand-ns inputs: разность с %s не прочитана (%v)\n", *base, err)
		return 2
	}
	changed := append(lines(diffOut), untracked...)
	verdicts, err := standns.Classify(changed, closure, tracked, func(p string) (string, error) {
		return git("diff", "-U0", *base, "--", p)
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "stand-ns inputs: ОТКАЗ — %v\n", err)
		return 2
	}
	nIn := 0
	for _, v := range verdicts {
		mark := "не вход"
		if v.Input {
			mark = "ВХОД"
			nIn++
		}
		_, _ = fmt.Fprintf(stdout, "  %-7s %s — %s\n", mark, v.Path, v.Why)
	}
	_, _ = fmt.Fprintf(stdout, "stand-ns inputs: Dockerfile %d, контекстов %d, образов go %d; изменённых путей %d, из них входов сборки %d\n",
		closure.Dockerfiles, closure.Contexts, closure.GoImages, len(verdicts), nIn)
	if nIn > 0 {
		return 1
	}
	return 0
}

// godeps — запись замыкания пакетов каждого образа go дерева (режим pod сборки, kacho#3353).
// Перечни путей — машины (в pod нет git); Dockerfile и исходники — из дерева КОРЕНЬ.
// Коды: 0 — запись напечатана; 2 — отказ входа либо замыкание не выведено.
func godeps(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("godeps", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "корень дерева")
	trackedFile := fs.String("tracked", "", "перечень отслеживаемых путей (git ls-files)")
	untrackedFile := fs.String("untracked", "", "перечень новых неигнорируемых путей (git ls-files --others --exclude-standard)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *root == "" || *trackedFile == "" || *untrackedFile == "" {
		_, _ = fmt.Fprintln(stderr, "stand-ns godeps: нужны -root, -tracked и -untracked")
		return 2
	}
	var lists [2][]string
	for i, f := range []string{*trackedFile, *untrackedFile} {
		body, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "stand-ns godeps: перечень %s не прочитан (%v)\n", f, err)
			return 2
		}
		lists[i] = nonEmptyLines(string(body))
	}
	tracked, untracked := lists[0], lists[1]
	if len(tracked) == 0 {
		_, _ = fmt.Fprintln(stderr, "stand-ns godeps: перечень отслеживаемых пуст — слепой обход")
		return 2
	}
	dfs, err := readDockerfiles(*root, tracked)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "stand-ns godeps: %v\n", err)
		return 2
	}
	all := append(append([]string(nil), tracked...), untracked...)
	recs := []standns.GoDepsRecord{}
	closure, err := standns.BuildClosure(all, dfs, standns.RecordGoDeps(goListDeps(*root, all), &recs))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "stand-ns godeps: ОТКАЗ — %v\n", err)
		return 2
	}
	if err := json.NewEncoder(stdout).Encode(recs); err != nil {
		_, _ = fmt.Fprintf(stderr, "stand-ns godeps: запись не напечатана (%v)\n", err)
		return 2
	}
	_, _ = fmt.Fprintf(stderr, "stand-ns godeps: Dockerfile %d, образов go %d, записей замыкания %d\n", closure.Dockerfiles, closure.GoImages, len(recs))
	return 0
}

// readDockerfiles — Dockerfile среди отслеживаемых путей, разобранные.
func readDockerfiles(root string, tracked []string) ([]standns.Dockerfile, error) {
	var dfs []standns.Dockerfile
	for _, t := range tracked {
		b := filepath.Base(t)
		if b != "Dockerfile" && !strings.HasPrefix(b, "Dockerfile.") && !strings.HasSuffix(b, ".Dockerfile") {
			continue
		}
		body, err := os.ReadFile(filepath.Clean(filepath.Join(root, t)))
		if err != nil {
			return nil, fmt.Errorf("%s не читается (%v)", t, err)
		}
		dfs = append(dfs, standns.ParseDockerfile(t, body))
	}
	return dfs, nil
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// goListDeps — замыкание пакетов `go build` теми же GOOS и CGO, что у Dockerfile.
// В замыкание идут ВСЕ не-тестовые .go каталога пакета (build-теги не судятся —
// шире, а не уже) и его встроенные файлы.
func goListDeps(root string, files []string) standns.GoDeps {
	return func(ctx string, mains []string) ([]string, error) {
		dir := filepath.Join(root, ctx)
		pk := make([]string, 0, len(mains))
		for _, m := range mains {
			pk = append(pk, "./"+m)
		}
		// #nosec G204 -- исполняемый файл постоянный (go list); аргументы — пути пакетов
		// из строк `go build` отслеживаемых Dockerfile этого дерева, а не ввод извне.
		cmd := exec.Command("go", append([]string{"list", "-deps", "-f",
			`{{if not .Standard}}{{.Dir}}{{range .EmbedFiles}}{{"\t"}}{{.}}{{end}}{{end}}`}, pk...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOOS=linux", "CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=-mod=mod")
		var errb strings.Builder
		cmd.Stderr = &errb
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("go list -deps: %v: %s", err, strings.TrimSpace(errb.String()))
		}
		absRoot, _ := filepath.Abs(root)
		pkgDirs := map[string]bool{}
		var got []string
		for _, l := range strings.Split(string(out), "\n") {
			f := strings.Split(l, "\t")
			if f[0] == "" {
				continue
			}
			rel, err := filepath.Rel(absRoot, f[0])
			if err != nil || strings.HasPrefix(rel, "..") {
				continue // модуль вне дерева — его фиксирует go.sum
			}
			rel = filepath.ToSlash(rel)
			pkgDirs[rel] = true
			for _, e := range f[1:] {
				got = append(got, rel+"/"+e)
			}
		}
		for _, t := range files {
			d := filepath.ToSlash(filepath.Dir(t))
			if pkgDirs[d] && strings.HasSuffix(t, ".go") && !strings.HasSuffix(t, "_test.go") {
				got = append(got, t)
			}
		}
		return got, nil
	}
}
