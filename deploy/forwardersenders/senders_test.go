// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package forwardersenders — КТО ФАКТИЧЕСКИ ЗВОНИТ каждому сервису, выведенное
// из графа вызовов дерева разбором Go, а не выписанное рукой (задача #924).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ
//
// Сервис, принимающий переданную личность, сужает круг законных отправителей
// списком SAN. Отправитель ВНЕ круга не «ходит под своим сертификатом» — его
// переданная личность СНИМАЕТСЯ, вызов идёт без принципала, и проверка прав
// отвечает отказом, который потребитель разворачивает в «объекта нет». Круг,
// суженный до одного шлюза, сужен идеально и при этом неверен; увидеть это
// можно только сопоставлением с графом вызовов. Здесь граф выводится; круги
// читает из рендера каждого стенда deploy/tests/helm/trusted-forwarder-profiles-test.sh
// и сопоставляет с этим выводом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СЧИТАЕТСЯ «ОТПРАВИТЕЛЕМ» (форма предиката — дословно из задачи)
//
// Компонент (каталог `services/<имя>` или `gateway` → `api-gateway`) — отправитель
// сервиса R, если его НЕ-ТЕСТОВЫЙ код:
//
//	(1) строит типизированного клиента стабов R: `X.New<…>ServiceClient`;
//	(2) регистрирует REST-прокси на R: `X.Register<…>Handler`,
//	    `…HandlerFromEndpoint`, `…HandlerClient` — прокси звонит R по сети.
//	    `…HandlerServer` отправителем НЕ является: он вешает на прокси
//	    локальную реализацию, сетевого вызова нет;
//	(3) импортирует пакет дерева ВНЕ своего каталога, который (сам либо через
//	    свои импорты пакетов дерева) делает (1) или (2) — посредник: так geo
//	    зовёт iam через pkg/authz/authziam, и предикат по имени конструктора в
//	    самом компоненте его не видел бы.
//
// Обращение к сервису самому себе отправителем не считается.
//
// ВЛАДЕЛЕЦ СТАБОВ выводится, а не выписывается: пакет стабов принадлежит тому
// компоненту, который в не-тестовом коде регистрирует его сервер
// (`X.Register<…>Server`). Пакет, чей сервер регистрируют НЕСКОЛЬКО компонентов
// (ход операций, поток изменений), атрибутировать нельзя: адресата решает
// соединение, а не тип. Такие рёбра печатаются переписью как слепая зона —
// числом и координатами, а не молчанием. Пакет, чей сервер живёт ВНЕ дерева,
// атрибутируется ведомостью outOfTreeOwners с причиной; запись, у которой
// в дереве появился свой сервер, — находка (послабление истекает само).
//
// Читается КОД, а не текст: разбор Go отбрасывает комментарии и строки, поэтому
// образец конструктора в комментарии (`xxxv1.NewXxxServiceClient` в
// services/vpc) отправителя не создаёт.
//
// ГРАНИЦА. Предикат — «строит клиента», а не «передаёт личность на этом вызове»:
// последнее решается вызовом `auth.PropagateOutgoing` на каждом RPC, и его
// отслеживание потребовало бы разбора потока данных. Сегодня вызов обёртки есть
// у каждого компонента, строящего клиентов.
package forwardersenders

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// repoRoot — корень дерева от каталога этого пакета.
const repoRoot = "../.."

// outOfTreeOwners — пакеты стабов, чей сервер живёт ВНЕ дерева, и сервис-адресат.
// Ключ — префикс пути импорта. Каждая запись несёт причину; запись, чей префикс
// получил в дереве свою регистрацию сервера, — находка: вывод перекрыл ведомость.
var outOfTreeOwners = map[string]struct{ owner, reason string }{
	"github.com/PRO-Robotech/kaname/": {
		owner:  "iam",
		reason: "служба доступа — отдельный продукт, её сервер в этом дереве не собирается",
	},
}

var (
	clientCtor   = regexp.MustCompile(`^New[A-Za-z0-9]*ServiceClient$`)
	proxyReg     = regexp.MustCompile(`^Register[A-Za-z0-9]*Handler(FromEndpoint|Client)?$`)
	serverReg    = regexp.MustCompile(`^Register[A-Za-z0-9]*Server$`)
	generatedPkg = regexp.MustCompile(`(\.pb\.go|\.pb\.gw\.go|_grpc\.pb\.go)$`)
)

// Result — вывод отправителей и перепись.
type Result struct {
	Senders    map[string]map[string][]string `json:"senders"` // адресат → отправитель → улики
	Owners     map[string]string              `json:"owners"`  // пакет стабов → адресат
	Ambiguous  []string                       `json:"ambiguous"`
	Unowned    []string                       `json:"unowned"`
	Unresolved []string                       `json:"unresolved"`
	Expired    []string                       `json:"expired"`
	Census     map[string]int                 `json:"census"`
}

type file struct {
	rel     string
	pkgDir  string // каталог пакета от корня
	pkgName string
	ast     *ast.File
	imports map[string]string // имя в файле → путь импорта
}

// component — компонент, которому принадлежит путь, либо "".
func component(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	switch {
	case len(parts) >= 3 && parts[0] == "services":
		return parts[1]
	case len(parts) >= 2 && parts[0] == "gateway":
		return "api-gateway"
	}
	return ""
}

func componentDir(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	switch {
	case len(parts) >= 3 && parts[0] == "services":
		return "services/" + parts[1] + "/"
	case len(parts) >= 2 && parts[0] == "gateway":
		return "gateway/"
	}
	return ""
}

// Derive — вывод по набору исходников (путь от корня → текст). module — путь
// модуля дерева.
func Derive(module string, sources map[string]string) (*Result, error) {
	res := &Result{
		Senders: map[string]map[string][]string{},
		Owners:  map[string]string{},
		Census:  map[string]int{},
	}
	fset := token.NewFileSet()
	var files []*file
	pkgName := map[string]string{} // каталог → имя пакета
	for rel, src := range sources {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("разбор %s: %w", rel, err)
		}
		dir := path.Dir(filepath.ToSlash(rel))
		pkgName[dir] = f.Name.Name
		files = append(files, &file{rel: filepath.ToSlash(rel), pkgDir: dir, pkgName: f.Name.Name, ast: f})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })

	inTree := func(imp string) (string, bool) {
		if imp == module {
			return ".", true
		}
		if strings.HasPrefix(imp, module+"/") {
			return strings.TrimPrefix(imp, module+"/"), true
		}
		return "", false
	}
	for _, f := range files {
		f.imports = map[string]string{}
		for _, is := range f.ast.Imports {
			p, _ := strconv.Unquote(is.Path.Value)
			name := ""
			if is.Name != nil {
				name = is.Name.Name
			} else if dir, ok := inTree(p); ok && pkgName[dir] != "" {
				name = pkgName[dir]
			} else {
				name = path.Base(p)
			}
			if name == "_" || name == "." {
				continue
			}
			f.imports[name] = p
		}
	}

	// Пакеты стабов: где регистрируют сервер — там владелец.
	serverOf := map[string]map[string]bool{} // путь стабов → компоненты
	type use struct {
		file, pkgPath, sel string
		line               int
		kind               string // ctor | proxy
	}
	var uses []use
	generatedDirs := map[string]bool{}
	for _, f := range files {
		if generatedPkg.MatchString(f.rel) {
			generatedDirs[f.pkgDir] = true
		}
	}
	for _, f := range files {
		ast.Inspect(f.ast, func(n ast.Node) bool {
			se, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := se.X.(*ast.Ident)
			if !ok {
				return true
			}
			name := se.Sel.Name
			kind := ""
			switch {
			case clientCtor.MatchString(name):
				kind = "ctor"
			case proxyReg.MatchString(name):
				kind = "proxy"
			// `…HandlerServer` — локальная реализация на прокси, а не сервер
			// пакета: засчитав её, шлюз стал бы вторым «владельцем» каждого
			// пакета, который он проксирует в процессе.
			case serverReg.MatchString(name) && !strings.HasSuffix(name, "HandlerServer"):
				kind = "server"
			default:
				return true
			}
			// Сгенерированные стабы сами несут эти имена — они предмет, а не отправитель.
			if generatedDirs[f.pkgDir] {
				return true
			}
			line := fset.Position(se.Pos()).Line
			p, ok := f.imports[id.Name]
			if !ok {
				// Квалификатор в форме конструктора, который НЕ разрешился в импорт:
				// либо импорт без псевдонима, чьё имя пакета вне дерева не читается,
				// либо переменная с таким методом. Молча пропустить нельзя — ребро
				// выпало бы; вывод называет его, а проба на дереве отказывает.
				if kind != "server" {
					res.Unresolved = append(res.Unresolved, fmt.Sprintf("%s:%d %s.%s", f.rel, line, id.Name, name))
				}
				return true
			}
			if kind == "server" {
				if c := component(f.rel); c != "" {
					if serverOf[p] == nil {
						serverOf[p] = map[string]bool{}
					}
					serverOf[p][c] = true
				}
				return true
			}
			uses = append(uses, use{file: f.rel, pkgPath: p, sel: id.Name + "." + name, line: line, kind: kind})
			return true
		})
	}

	owner := func(p string) (string, string) { // адресат, отказ
		if cs := serverOf[p]; len(cs) == 1 {
			for c := range cs {
				return c, ""
			}
		} else if len(cs) > 1 {
			var names []string
			for c := range cs {
				names = append(names, c)
			}
			sort.Strings(names)
			return "", "ambiguous:" + strings.Join(names, ",")
		}
		for pref, o := range outOfTreeOwners {
			if strings.HasPrefix(p, pref) {
				return o.owner, ""
			}
		}
		return "", "unowned"
	}
	for pref := range outOfTreeOwners {
		for p := range serverOf {
			if strings.HasPrefix(p, pref) {
				res.Expired = append(res.Expired, fmt.Sprintf(
					"ведомость outOfTreeOwners атрибутирует %s рукой, но сервер пакета %s регистрируется в дереве — запись без предмета", pref, p))
			}
		}
	}

	// Посредники: пакет дерева вне каталога компонента, чей код строит клиентов.
	libRecv := map[string]map[string][]string{} // каталог пакета → адресат → улики
	libImports := map[string]map[string]bool{}  // каталог → каталоги дерева, которые он импортирует
	for _, f := range files {
		if component(f.rel) != "" || generatedDirs[f.pkgDir] {
			continue
		}
		for _, p := range f.imports {
			if dir, ok := inTree(p); ok && !generatedDirs[dir] {
				if libImports[f.pkgDir] == nil {
					libImports[f.pkgDir] = map[string]bool{}
				}
				libImports[f.pkgDir][dir] = true
			}
		}
	}

	addEdge := func(recv, sender, ev string) {
		if recv == sender {
			return
		}
		if res.Senders[recv] == nil {
			res.Senders[recv] = map[string][]string{}
		}
		res.Senders[recv][sender] = append(res.Senders[recv][sender], ev)
	}
	ambiguous := map[string]bool{}
	unowned := map[string]bool{}
	for _, u := range uses {
		recv, why := owner(u.pkgPath)
		ev := fmt.Sprintf("%s:%d %s", u.file, u.line, u.sel)
		res.Census["вызовов-конструкторов"]++
		if recv == "" {
			if strings.HasPrefix(why, "ambiguous:") {
				ambiguous[fmt.Sprintf("%s (владельцев %s)", ev, strings.TrimPrefix(why, "ambiguous:"))] = true
			} else {
				unowned[fmt.Sprintf("%s (пакет %s)", ev, u.pkgPath)] = true
			}
			continue
		}
		dir := path.Dir(u.file)
		if c := component(u.file); c != "" {
			addEdge(recv, c, ev)
			continue
		}
		if libRecv[dir] == nil {
			libRecv[dir] = map[string][]string{}
		}
		libRecv[dir][recv] = append(libRecv[dir][recv], ev)
	}
	// Замыкание по импортам посредников.
	for changed := true; changed; {
		changed = false
		for dir, deps := range libImports {
			for dep := range deps {
				for recv, evs := range libRecv[dep] {
					if libRecv[dir] == nil {
						libRecv[dir] = map[string][]string{}
					}
					if len(libRecv[dir][recv]) == 0 {
						libRecv[dir][recv] = append([]string{"через " + dep}, evs...)
						changed = true
					}
				}
			}
		}
	}
	// Компонент, импортирующий посредника ВНЕ своего каталога.
	for _, f := range files {
		c := component(f.rel)
		if c == "" {
			continue
		}
		for _, p := range f.imports {
			dir, ok := inTree(p)
			if !ok || strings.HasPrefix(dir+"/", componentDir(f.rel)) {
				continue
			}
			for recv, evs := range libRecv[dir] {
				addEdge(recv, c, fmt.Sprintf("%s импортирует посредника %s (%s)", f.rel, dir, evs[0]))
			}
		}
	}

	for p := range serverOf {
		if r, _ := owner(p); r != "" {
			res.Owners[p] = r
		}
	}
	for a := range ambiguous {
		res.Ambiguous = append(res.Ambiguous, a)
	}
	for u := range unowned {
		res.Unowned = append(res.Unowned, u)
	}
	sort.Strings(res.Ambiguous)
	sort.Strings(res.Unowned)
	sort.Strings(res.Expired)
	sort.Strings(res.Unresolved)
	res.Census["файлов-разобрано"] = len(files)
	res.Census["адресатов"] = len(res.Senders)
	edges := 0
	for _, s := range res.Senders {
		edges += len(s)
	}
	res.Census["рёбер"] = edges
	return res, nil
}

// treeSources — не-тестовые исходники Go компонентов и посредников, из индекса git.
func treeSources(t *testing.T) (string, map[string]string) {
	t.Helper()
	out, err := gitenv.Command("", "-C", repoRoot, "ls-files", "-z", "--",
		"services/*.go", "gateway/*.go", "pkg/*.go", "internal/*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files не отработал (%v) — обход не прочитал НИ ОДНОГО файла", err)
	}
	src := map[string]string{}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || strings.HasSuffix(rel, "_test.go") || strings.Contains(rel, "/testdata/") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(repoRoot, rel)) // #nosec G304 -- путь из индекса git этого дерева
		if err != nil {
			t.Fatalf("не прочитан отслеживаемый файл %s (%v) — обход сузился бы молча", rel, err)
		}
		src[rel] = string(b)
	}
	if len(src) == 0 {
		t.Fatal("обход не прочитал НИ ОДНОГО исходника — «ноль отправителей» значило бы «ноль прочитанного»")
	}
	mod, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		t.Fatalf("go.mod не читается: %v", err)
	}
	m := regexp.MustCompile(`(?m)^module\s+(\S+)`).FindSubmatch(mod)
	if m == nil {
		t.Fatal("в go.mod нет строки module — пути дерева не отличить от чужих")
	}
	return string(m[1]), src
}

// TestForwarderSendersAreDerivedFromTheCallGraph — вывод отправителей на дереве.
//
// Сам по себе он не судит круги — это делает гейт рендера, которому вывод отдаётся
// файлом (KACHO_FORWARDER_SENDERS_OUT). Здесь утверждаются предпосылки вывода:
// разобрано больше нуля, у каждого вызова-конструктора установлен адресат либо он
// назван слепой зоной, ведомость рукописных владельцев не пережила своего предмета.
func TestForwarderSendersAreDerivedFromTheCallGraph(t *testing.T) {
	module, src := treeSources(t)
	res, err := Derive(module, src)
	if err != nil {
		t.Fatalf("вывод не состоялся: %v", err)
	}
	if len(res.Unowned) > 0 {
		t.Fatalf("у стабов этих вызовов не установлен владелец — ни регистрации сервера в дереве, "+
			"ни записи outOfTreeOwners; ребро не атрибутировано и выпало бы молча:\n  %s",
			strings.Join(res.Unowned, "\n  "))
	}
	if len(res.Expired) > 0 {
		t.Fatalf("послабление пережило предмет:\n  %s", strings.Join(res.Expired, "\n  "))
	}
	if len(res.Unresolved) > 0 {
		t.Fatalf("квалификатор в форме конструктора не разрешился в импорт — ребро не выведено:\n  %s",
			strings.Join(res.Unresolved, "\n  "))
	}
	if res.Census["рёбер"] == 0 {
		t.Fatal("ни одного ребра — либо предикат ослеп, либо граф пуст; «ноль отправителей» не есть «никто не звонит»")
	}
	var recv []string
	for r, s := range res.Senders {
		var names []string
		for n := range s {
			names = append(names, n)
		}
		sort.Strings(names)
		recv = append(recv, fmt.Sprintf("%s ← %s", r, strings.Join(names, ", ")))
	}
	sort.Strings(recv)
	t.Logf("перепись: файлов разобрано %d; вызовов-конструкторов %d; адресатов %d; рёбер %d; слепая зона (адресат не единственный) %d",
		res.Census["файлов-разобрано"], res.Census["вызовов-конструкторов"], res.Census["адресатов"],
		res.Census["рёбер"], len(res.Ambiguous))
	for _, r := range recv {
		t.Log("  " + r)
	}
	for _, a := range res.Ambiguous {
		t.Log("  слепая зона: " + a)
	}
	if out := os.Getenv("KACHO_FORWARDER_SENDERS_OUT"); out != "" {
		b, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			t.Fatalf("вывод не сериализован: %v", err)
		}
		if err := os.WriteFile(out, b, 0o600); err != nil {
			t.Fatalf("вывод не записан в %s: %v", out, err)
		}
	}
}

// ── Инъекции: каждая форма предиката краснеет, законный близнец молчит ─────

const mod = "example.test/tree"

func stubs(dir, pkg string) string {
	return fmt.Sprintf("package %s\n\nfunc New%sServiceClient(any) any { return nil }\n"+
		"func Register%sServiceServer(any, any) {}\n"+
		"func Register%sServiceHandlerFromEndpoint(any, any, string, any) error { return nil }\n"+
		"func Register%sServiceHandlerServer(any, any, any) error { return nil }\n", pkg, dir, dir, dir, dir)
}

// tree — минимальное дерево: у beta свой сервер, alpha — возможный отправитель.
func tree(extra map[string]string) map[string]string {
	t := map[string]string{
		"pkg/api/beta/v1/beta.pb.go": stubs("Beta", "betav1"),
		"services/beta/cmd/beta/main.go": `package main
import betav1 "example.test/tree/pkg/api/beta/v1"
func main() { betav1.RegisterBetaServiceServer(nil, nil) }
`,
		"services/alpha/cmd/alpha/main.go": "package main\nfunc main() {}\n",
	}
	for k, v := range extra {
		t[k] = v
	}
	return t
}

func senders(t *testing.T, src map[string]string, recv string) []string {
	t.Helper()
	res, err := Derive(mod, src)
	if err != nil {
		t.Fatalf("вывод не состоялся: %v", err)
	}
	var out []string
	for s := range res.Senders[recv] {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func TestForwarderSendersDerivation_EachFormRedsAndItsTwinIsSilent(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]string
		want  []string
	}{
		{"прямой конструктор клиента → отправитель", map[string]string{
			"services/alpha/internal/c/c.go": `package c
import betav1 "example.test/tree/pkg/api/beta/v1"
var _ = betav1.NewBetaServiceClient(nil)
`}, []string{"alpha"}},
		{"тот же конструктор в КОММЕНТАРИИ и строке → не отправитель", map[string]string{
			"services/alpha/internal/c/c.go": `package c
// betav1.NewBetaServiceClient(conn)
var s = "betav1.NewBetaServiceClient(conn)"
`}, nil},
		{"тот же конструктор в _test.go → не отправитель", map[string]string{
			"services/alpha/internal/c/c_test.go": `package c
import betav1 "example.test/tree/pkg/api/beta/v1"
var _ = betav1.NewBetaServiceClient(nil)
`}, nil},
		{"REST-прокси …HandlerFromEndpoint → отправитель", map[string]string{
			"gateway/internal/mux/mux.go": `package mux
import betav1 "example.test/tree/pkg/api/beta/v1"
var _ = betav1.RegisterBetaServiceHandlerFromEndpoint(nil, nil, "", nil)
`}, []string{"api-gateway"}},
		{"…HandlerServer (локальная реализация) → не отправитель", map[string]string{
			"gateway/internal/mux/mux.go": `package mux
import betav1 "example.test/tree/pkg/api/beta/v1"
var _ = betav1.RegisterBetaServiceHandlerServer(nil, nil, nil)
`}, nil},
		{"посредник дерева, строящий клиента, импортирован → отправитель", map[string]string{
			"pkg/authz/betacheck/check.go": `package betacheck
import betav1 "example.test/tree/pkg/api/beta/v1"
func NewCheck(c any) any { return betav1.NewBetaServiceClient(c) }
`,
			"services/alpha/cmd/alpha/serve.go": `package main
import "example.test/tree/pkg/authz/betacheck"
var _ = betacheck.NewCheck
`}, []string{"alpha"}},
		{"посредник через второго посредника → отправитель", map[string]string{
			"pkg/authz/betacheck/check.go": `package betacheck
import betav1 "example.test/tree/pkg/api/beta/v1"
func NewCheck(c any) any { return betav1.NewBetaServiceClient(c) }
`,
			"pkg/authz/wrap/wrap.go": `package wrap
import "example.test/tree/pkg/authz/betacheck"
var New = betacheck.NewCheck
`,
			"services/alpha/cmd/alpha/serve.go": `package main
import "example.test/tree/pkg/authz/wrap"
var _ = wrap.New
`}, []string{"alpha"}},
		{"сервис зовёт свои же стабы → не отправитель сам себе", map[string]string{
			"services/beta/internal/self/self.go": `package self
import betav1 "example.test/tree/pkg/api/beta/v1"
var _ = betav1.NewBetaServiceClient(nil)
`}, nil},
		{"импорт без псевдонима → имя из объявления пакета", map[string]string{
			"services/alpha/internal/c/c.go": `package c
import "example.test/tree/pkg/api/beta/v1"
var _ = betav1.NewBetaServiceClient(nil)
`}, []string{"alpha"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := senders(t, tree(tc.extra), "beta")
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("отправители beta: получено %v, ждали %v", got, tc.want)
			}
		})
	}
}

func TestForwarderSendersDerivation_OwnershipIsDerivedOrNamed(t *testing.T) {
	// Два компонента регистрируют один сервер — адресат не единственный, ребро
	// в слепой зоне, а не приписано наугад.
	src := tree(map[string]string{
		"services/gamma/cmd/gamma/main.go": `package main
import betav1 "example.test/tree/pkg/api/beta/v1"
func main() { betav1.RegisterBetaServiceServer(nil, nil) }
`,
		"services/alpha/internal/c/c.go": `package c
import betav1 "example.test/tree/pkg/api/beta/v1"
var _ = betav1.NewBetaServiceClient(nil)
`})
	res, err := Derive(mod, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Ambiguous) != 1 || len(res.Senders["beta"]) != 0 {
		t.Fatalf("два владельца: ждали одно ребро в слепой зоне и ни одного приписанного, получено ambiguous=%v senders=%v",
			res.Ambiguous, res.Senders)
	}
	// Стабы без сервера в дереве и без записи ведомости — владелец не установлен.
	src = map[string]string{
		"pkg/api/delta/v1/delta.pb.go": stubs("Delta", "deltav1"),
		"services/alpha/internal/c/c.go": `package c
import deltav1 "example.test/tree/pkg/api/delta/v1"
var _ = deltav1.NewDeltaServiceClient(nil)
`}
	res, err = Derive(mod, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Unowned) != 1 {
		t.Fatalf("стабы без владельца обязаны быть названы, получено unowned=%v", res.Unowned)
	}
	// Ведомость рукописных владельцев истекает, когда сервер появился в дереве.
	src = map[string]string{
		"services/iamlike/cmd/x/main.go": `package main
import iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"
func main() { iamv1.RegisterIAMServiceServer(nil, nil) }
`}
	res, err = Derive(mod, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Expired) == 0 {
		t.Fatal("запись outOfTreeOwners при сервере в дереве обязана стать находкой")
	}
}
