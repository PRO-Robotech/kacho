// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package standns

import (
	"bufio"
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
)

// ВХОДЫ СБОРКИ ОБРАЗОВ (kacho#3102).
//
// Стенд в своём пространстве имён исполняет ОПУБЛИКОВАННЫЕ образы ревизии REF,
// а чарт — из рабочей копии. Это честно ровно тогда, когда между REF и рабочей
// копией не менялся ни один файл, который доезжает до содержимого образа:
// иначе стенд назывался бы стендом этой копии, а исполнял бы другой код.
//
// Прежнее правило было списком путей «точно не входы» (развёртывание, доки,
// пробы) — то есть объявлением, а не выводом: оно отказывало на оснастке
// стенда (гейт переписи в internal/repohygiene, пакет пинов в internal/localimages),
// которую ни один образ не собирает, и при этом пропускало `deploy/default.conf`
// сайтов документации, который их Dockerfile КОПИРУЕТ в образ.
//
// ПРАВИЛО РАЗЛИЧЕНИЯ — вывод из самих Dockerfile дерева, а не перечень:
//
//  1. Каждый отслеживаемый Dockerfile — вход сам.
//  2. Его контекст — ближайший каталог-предок, в котором существует каждый
//     источник COPY/ADD (кроме `--from`) и каждый пакет `go build`.
//  3. Источник COPY/ADD — вход целиком (каталог — со всем содержимым).
//  4. Единственное сужение: `COPY . .` в стадии, чьи RUN — только вызовы `go`
//     (list, mod, build). Продукт такой стадии — двоичные файлы, а в них
//     попадает ровно транзитивное замыкание собираемых пакетов: все не-тестовые
//     `.go` их каталогов (без учёта build-тегов — шире, а не уже), их встроенные
//     (`go:embed`) файлы и go.mod/go.sum. Стадия с любой иной командой не
//     сужается: её `COPY . .` — весь контекст.
//  5. `.dockerignore` контекста — вход, кроме правки, которая ТОЛЬКО добавляет
//     исключения, не совпадающие ни с одним отслеживаемым файлом: содержимое
//     контекста чистого клона (а конвейер собирает из него) от неё не меняется.
//
// Чего правило НЕ разрешает: любую правку продуктового кода, попадающего в
// двоичный файл, исходник консоли в контексте её образа, Dockerfile, go.mod.
// Ослабления для продуктового кода нет: сужается только то, что доказуемо не
// доезжает до образа.

// Dockerfile — то, что из Dockerfile нужно правилу.
type Dockerfile struct {
	Path    string
	GoMains []string // пакеты `go build`, без ведущего «./»
	Stages  []Stage
}

// Stage — одна стадия FROM.
type Stage struct {
	Sources []string // источники COPY/ADD без --from, как написаны
	GoOnly  bool     // каждый RUN стадии — вызов go
	CopyAll bool     // среди источников есть «.»
}

// ParseDockerfile разбирает инструкции (с продолжениями строк).
func ParseDockerfile(p string, body []byte) Dockerfile {
	df := Dockerfile{Path: p}
	var cur *Stage
	for _, ins := range instructions(body) {
		word, rest, _ := strings.Cut(ins, " ")
		switch strings.ToUpper(word) {
		case "FROM":
			df.Stages = append(df.Stages, Stage{GoOnly: true})
			cur = &df.Stages[len(df.Stages)-1]
		case "COPY", "ADD":
			if cur == nil {
				continue
			}
			args := strings.Fields(rest)
			var plain []string
			from := false
			for _, a := range args {
				if strings.HasPrefix(a, "--") {
					if strings.HasPrefix(a, "--from") {
						from = true
					}
					continue
				}
				plain = append(plain, a)
			}
			if from || len(plain) < 2 {
				continue
			}
			for _, s := range plain[:len(plain)-1] {
				s = strings.TrimPrefix(s, "./")
				if s == "" || s == "." {
					cur.CopyAll = true
					s = "."
				}
				cur.Sources = append(cur.Sources, s)
			}
		case "RUN":
			if cur == nil {
				continue
			}
			goOnly, mains := runIsGoOnly(rest)
			if !goOnly {
				cur.GoOnly = false
			}
			df.GoMains = append(df.GoMains, mains...)
		}
	}
	return df
}

func instructions(body []byte) []string {
	var out []string
	var acc strings.Builder
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		if line == "" && acc.Len() == 0 {
			continue
		}
		if strings.HasSuffix(line, "\\") {
			acc.WriteString(strings.TrimSuffix(line, "\\"))
			acc.WriteString(" ")
			continue
		}
		acc.WriteString(line)
		if s := strings.Join(strings.Fields(acc.String()), " "); s != "" {
			out = append(out, s)
		}
		acc.Reset()
	}
	if s := strings.Join(strings.Fields(acc.String()), " "); s != "" {
		out = append(out, s)
	}
	return out
}

// runIsGoOnly — RUN состоит только из вызовов go; печатает пакеты `go build`.
func runIsGoOnly(rest string) (bool, []string) {
	f := strings.Fields(rest)
	for len(f) > 0 && strings.HasPrefix(f[0], "--") {
		f = f[1:]
	}
	only := true
	var mains []string
	for _, cmd := range strings.Split(strings.Join(f, " "), "&&") {
		w := strings.Fields(cmd)
		for len(w) > 0 && strings.Contains(w[0], "=") && !strings.HasPrefix(w[0], "-") {
			w = w[1:]
		}
		if len(w) == 0 {
			continue
		}
		if w[0] != "go" {
			only = false
			continue
		}
		if len(w) > 1 && w[1] == "build" {
			for _, a := range w[2:] {
				if strings.HasPrefix(a, "./") {
					mains = append(mains, strings.TrimPrefix(a, "./"))
				}
			}
		}
	}
	return only, mains
}

// GoDeps — файлы дерева (пути от корня), доезжающие до двоичных файлов пакетов
// mains, собираемых из контекста ctx. Реальная реализация зовёт `go list -deps`.
type GoDeps func(ctx string, mains []string) ([]string, error)

// Closure — множество входов сборки образов.
type Closure struct {
	exact    map[string]string
	prefixes map[string]string // «каталог/» → почему; «» — весь контекст корня
	ignores  map[string]string // .dockerignore контекстов → почему
	// Перепись для печати.
	Dockerfiles, GoImages, Contexts int
}

// BuildClosure выводит входы из всех Dockerfile дерева. tracked — `git ls-files`.
func BuildClosure(tracked []string, dfs []Dockerfile, deps GoDeps) (Closure, error) {
	c := Closure{exact: map[string]string{}, prefixes: map[string]string{}, ignores: map[string]string{}}
	if len(dfs) == 0 {
		return c, fmt.Errorf("ни одного Dockerfile не прочитано — входы выводить не из чего (слепой обход)")
	}
	files := map[string]bool{}
	for _, t := range tracked {
		files[t] = true
	}
	ctxs := map[string]bool{}
	for _, df := range dfs {
		c.Dockerfiles++
		c.exact[df.Path] = "Dockerfile"
		ctx, err := contextOf(df, tracked, files)
		if err != nil {
			return c, err
		}
		ctxs[ctx] = true
		c.ignores[join(ctx, ".dockerignore")] = "исключения контекста " + df.Path
		reduced := false
		for _, st := range df.Stages {
			for _, s := range st.Sources {
				why := "COPY " + s + " в " + df.Path
				if s == "." {
					if st.GoOnly && len(df.GoMains) > 0 {
						reduced = true
						continue
					}
					c.prefixes[dirPrefix(ctx)] = why
					continue
				}
				if hasMeta(s) {
					for _, t := range tracked {
						if rel, ok := under(ctx, t); ok {
							if m, _ := path.Match(strings.TrimSuffix(s, "/"), rel); m {
								c.exact[t] = why
							}
						}
					}
					continue
				}
				full := join(ctx, strings.TrimSuffix(s, "/"))
				if files[full] {
					c.exact[full] = why
				} else {
					c.prefixes[full+"/"] = why
				}
			}
		}
		if reduced {
			c.GoImages++
			got, err := deps(ctx, df.GoMains)
			if err != nil {
				return c, fmt.Errorf("замыкание пакетов %s не выведено: %w", df.Path, err)
			}
			if len(got) == 0 {
				return c, fmt.Errorf("замыкание пакетов %s пусто — слепой обход", df.Path)
			}
			why := "замыкание go build " + strings.Join(df.GoMains, " ") + " (" + df.Path + ")"
			for _, g := range got {
				c.exact[g] = why
			}
			c.exact[join(ctx, "go.mod")] = why
			c.exact[join(ctx, "go.sum")] = why
		}
	}
	c.Contexts = len(ctxs)
	return c, nil
}

// contextOf — ближайший предок каталога Dockerfile, где существуют все источники.
func contextOf(df Dockerfile, tracked []string, files map[string]bool) (string, error) {
	dir := path.Dir(df.Path)
	if dir == "." {
		dir = ""
	}
	for {
		if contextFits(dir, df, tracked, files) {
			return dir, nil
		}
		if dir == "" {
			return "", fmt.Errorf("контекст сборки %s не установлен: ни в одном каталоге-предке нет всех источников COPY и пакетов go build", df.Path)
		}
		dir = path.Dir(dir)
		if dir == "." {
			dir = ""
		}
	}
}

func contextFits(ctx string, df Dockerfile, tracked []string, files map[string]bool) bool {
	exists := func(rel string) bool {
		rel = strings.TrimSuffix(rel, "/")
		full := join(ctx, rel)
		if hasMeta(rel) {
			for _, t := range tracked {
				if r, ok := under(ctx, t); ok {
					if m, _ := path.Match(rel, r); m {
						return true
					}
				}
			}
			return false
		}
		if files[full] {
			return true
		}
		for _, t := range tracked {
			if strings.HasPrefix(t, full+"/") {
				return true
			}
		}
		return false
	}
	for _, m := range df.GoMains {
		if !exists(m) {
			return false
		}
	}
	for _, st := range df.Stages {
		for _, s := range st.Sources {
			if s != "." && !exists(s) {
				return false
			}
		}
	}
	return true
}

// Why — почему путь вход сборки образа; ok=false — не вход.
func (c Closure) Why(p string) (string, bool) {
	if w, ok := c.exact[p]; ok {
		return w, true
	}
	if w, ok := c.ignores[p]; ok {
		return w, true
	}
	for pre, w := range c.prefixes {
		if pre == "" || strings.HasPrefix(p, pre) {
			return w, true
		}
	}
	return "", false
}

// IsIgnoreFile — путь является .dockerignore контекста какого-то образа.
func (c Closure) IsIgnoreFile(p string) bool { _, ok := c.ignores[p]; return ok }

// IgnoreChangeIsInert — правка .dockerignore контекста ctx, данная разностью
// `git diff -U0`, только добавляет исключения, ни одно из которых не совпадает
// с отслеживаемым файлом контекста. Иначе — вход (с причиной).
func IgnoreChangeIsInert(ctx string, diff string, tracked []string) (bool, string) {
	added := 0
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
			continue
		case strings.HasPrefix(l, "-"):
			if strings.TrimSpace(strings.TrimPrefix(l, "-")) != "" {
				return false, "снято правило исключения «" + strings.TrimPrefix(l, "-") + "» — в контекст вернулись файлы"
			}
		case strings.HasPrefix(l, "+"):
			pat := strings.TrimSpace(strings.TrimPrefix(l, "+"))
			if pat == "" || strings.HasPrefix(pat, "#") {
				continue
			}
			added++
			if strings.HasPrefix(pat, "!") {
				return false, "добавлено обратное правило «" + pat + "» — в контекст возвращаются файлы"
			}
			if strings.Contains(pat, "**") {
				return false, "правило «" + pat + "» с ** не судится — считается входом"
			}
			p := strings.TrimSuffix(strings.TrimPrefix(path.Clean("/"+pat), "/"), "/")
			for _, t := range tracked {
				rel, ok := under(ctx, t)
				if !ok {
					continue
				}
				if ignoreMatches(p, rel) {
					return false, "новое исключение «" + pat + "» убирает из контекста отслеживаемый " + t
				}
			}
		}
	}
	if added == 0 {
		return true, "правка не добавляет правил (комментарии и пустые строки)"
	}
	return true, fmt.Sprintf("добавлено %d исключений, ни одно не совпадает с отслеживаемым файлом — контекст чистого клона тот же", added)
}

func ignoreMatches(pat, rel string) bool {
	parts := strings.Split(rel, "/")
	for i := 1; i <= len(parts); i++ {
		if m, _ := path.Match(pat, strings.Join(parts[:i], "/")); m {
			return true
		}
	}
	return false
}

// Changed — вход ли каждый изменённый путь; печатается в порядке путей.
type Verdict struct {
	Path  string
	Input bool
	Why   string
}

// Classify судит изменённые пути. ignoreDiff даёт разность .dockerignore.
func Classify(changed []string, c Closure, tracked []string, ignoreDiff func(p string) (string, error)) ([]Verdict, error) {
	sorted := append([]string(nil), changed...)
	sort.Strings(sorted)
	var out []Verdict
	for _, p := range sorted {
		if p == "" {
			continue
		}
		if c.IsIgnoreFile(p) {
			d, err := ignoreDiff(p)
			if err != nil {
				return nil, fmt.Errorf("разность %s не прочитана: %w", p, err)
			}
			ctx := path.Dir(p)
			if ctx == "." {
				ctx = ""
			}
			inert, why := IgnoreChangeIsInert(ctx, d, tracked)
			out = append(out, Verdict{Path: p, Input: !inert, Why: why})
			continue
		}
		why, in := c.Why(p)
		if !in {
			why = "не доезжает ни до одного образа"
		}
		out = append(out, Verdict{Path: p, Input: in, Why: why})
	}
	return out, nil
}

func hasMeta(s string) bool { return strings.ContainsAny(s, "*?[") }

func join(ctx, rel string) string {
	if ctx == "" {
		return rel
	}
	return ctx + "/" + rel
}

func dirPrefix(ctx string) string {
	if ctx == "" {
		return ""
	}
	return ctx + "/"
}

func under(ctx, p string) (string, bool) {
	if ctx == "" {
		return p, true
	}
	if strings.HasPrefix(p, ctx+"/") {
		return strings.TrimPrefix(p, ctx+"/"), true
	}
	return "", false
}
