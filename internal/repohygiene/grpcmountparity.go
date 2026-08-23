// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// grpcmountparity.go — анализатор «сервис объявлен контрактом, но композиционный
// корень его не монтирует».
//
// # Что он ищет
//
// gRPC-сервис, которого нет ни на одном листенере своего бинаря, отвечает
// `Unimplemented` — тем же, чем отвечает несуществующий сервис. Симптома, кроме
// «RPC не работает», это не производит, а на административной поверхности не
// производит и его: тенант туда не ходит, и заметит только дежурный, уже в
// инциденте. Класс не гипотетический — сервис уже был в дескрипторах, в таблице
// маршрутов и в каталоге прав и не был смонтирован; чинилось руками, без гейта.
//
// # Почему предмет ВЫЧИСЛЯЕТСЯ, а не перечисляется
//
// Два сервиса дерева несут рукописные списки `servedPublicServiceDescs` /
// `servedInternalServiceDescs`, объявляющие, что смонтировано. Список — это
// УТВЕРЖДЕНИЕ о композиционном корне, а не измерение его: удали регистрацию из
// `main.go`, и список продолжит утверждать прежнее, а гейт останется зелёным.
// Здесь смонтированное берётся РАЗБОРОМ композиционного корня, а объявленное —
// из сгенерированных стабов (`ServiceName` в `ServiceDesc`), то есть обе стороны
// читаются оттуда, где они на самом деле живут.
//
// Разбор идёт по AST, а не по тексту: вызов в комментарии не должен считаться
// монтированием, а снос вызова с сохранением объясняющей его фразы — обычная
// форма удаления, которую текстовый поиск от настоящего монтирования не отличает.
//
// # Принадлежность пакета бинарю
//
// Бинарь ВЛАДЕЕТ proto-пакетом, если монтирует хотя бы один его сервис. Это
// свойство наблюдаемое, а не объявленное: потребитель чужого домена импортирует
// его стабы ради КЛИЕНТА и `Register…ServiceServer` не зовёт, поэтому владельцем
// не становится. Домен, у которого не смонтирован НИ ОДИН сервис, этим правилом
// не покрыт — такой пропуск ловится другой полосой (маршрутизируемостью на краю),
// и здесь честно назван переписью «владеющих пакетов».
//
// # Исключение живёт, пока у него есть предмет
//
// Сервис, намеренно не поднимаемый по gRPC (обслуживается иначе либо не
// реализован вовсе), вносится в `Allow`. Запись, которой нечего исключать —
// потому что сервис смонтирован или потому что его больше нет в контракте, —
// сама является находкой: иначе слепое пятно унаследует следующий сервис,
// которому достанется это имя.
//
// Из этого правила есть РОВНО ОДНО изъятие, и оно не послабление, а граница
// предмета: запись про сервис пакета, которым не владеет НИ ОДИН бинарь, лежит
// вне того, о чём анализатор берётся судить (см. абзац про принадлежность выше).
// Такую запись он не объявляет ни живой, ни истёкшей — он её СЧИТАЕТ и печатает
// счёт, чтобы «промолчали» было отличимо от «рассмотрели и не нашли». Как только
// пакетом начинают владеть, запись судится обычным порядком.
//
// Прежде такая запись объявлялась истёкшей, и это было утверждением о том, о чём
// анализатор судить не берётся: пропуск целого домена он не ловит by
// construction. Вдобавок вердикт получался НЕИСПОЛНИМЫМ в паре с гейтом
// достижимости каталога — тот требует, чтобы решение «не монтируем» было
// записано здесь и нигде больше, — и домен, чей сервер написан, а корни его ещё
// не берут, не имел законного состояния ни с записью, ни без неё.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// MountOptions — вход анализатора.
type MountOptions struct {
	// Root — корень репозитория.
	Root string
	// APIRoot — путь (относительно Root) к сгенерированным стабам.
	APIRoot string
	// ModulePath — import-префикс модуля, чтобы отличить стабы от прочих импортов.
	ModulePath string
	// Roots — каталоги (относительно Root), внутри которых ищутся композиционные
	// корни. Композиционный корень — пакет, лежащий непосредственно в `cmd/`.
	Roots []string
	// Allow — FQN сервисов, намеренно не поднимаемых по gRPC.
	Allow []string
}

// MountCensus — то, что анализатор прочитал. Ноль находок обязано быть отличимо
// от нуля прочитанного.
type MountCensus struct {
	StubFiles      int
	ProtoPackages  int
	DeclaredSvcs   int
	CmdPackages    int
	CmdFiles       int
	OwnedPackages  int
	MountedSvcs    int
	OwningBinaries int
	// UnownedAllow — записи исключений, чей пакет не принадлежит ни одному
	// бинарю. Они лежат ВНЕ предмета анализатора, и их число печатается, чтобы
	// «промолчали» было отличимо от «рассмотрели и не нашли».
	UnownedAllow int
}

// MountFinding — одна находка.
type MountFinding struct {
	Kind   string // "unmounted" | "stale-allow"
	FQN    string
	Binary string // для "unmounted" — композиционный корень, где домен смонтирован частично
	Reason string
}

func (f MountFinding) String() string {
	if f.Binary != "" {
		return f.Kind + " " + f.FQN + " (" + f.Binary + "): " + f.Reason
	}
	return f.Kind + " " + f.FQN + ": " + f.Reason
}

var serviceNameRe = regexp.MustCompile(`ServiceName:\s*"([A-Za-z0-9_.]+)"`)

// AuditGRPCMountParity сводит объявленное контрактом со смонтированным в
// композиционных корнях.
func AuditGRPCMountParity(opts MountOptions, out io.Writer) ([]MountFinding, MountCensus, error) {
	var c MountCensus

	declared, dirToProto, err := declaredServices(filepath.Join(opts.Root, opts.APIRoot), &c)
	if err != nil {
		return nil, c, err
	}
	if c.StubFiles == 0 {
		return nil, c, fmt.Errorf("не прочитано ни одного файла стабов в %q — предмет не найден, "+
			"и любой вердикт ниже беспредметен", filepath.Join(opts.Root, opts.APIRoot))
	}

	mounted, err := mountedServices(opts, dirToProto, &c)
	if err != nil {
		return nil, c, err
	}
	if c.CmdFiles == 0 {
		return nil, c, fmt.Errorf("не прочитано ни одного файла композиционных корней в %v — "+
			"«ничего не смонтировано» получено даром", opts.Roots)
	}

	allow := map[string]struct{}{}
	for _, a := range opts.Allow {
		allow[a] = struct{}{}
	}
	usedAllow := map[string]struct{}{}

	var findings []MountFinding
	allMounted := map[string]struct{}{}
	for _, set := range mounted {
		for fqn := range set {
			allMounted[fqn] = struct{}{}
		}
	}

	for _, bin := range sortedStrKeys(mounted) {
		set := mounted[bin]
		owned := map[string]struct{}{}
		for fqn := range set {
			owned[fqn[:strings.LastIndexByte(fqn, '.')]] = struct{}{}
		}
		if len(set) > 0 {
			c.OwningBinaries++
		}
		c.OwnedPackages += len(owned)
		c.MountedSvcs += len(set)
		for _, pkg := range sortedStrKeys2(owned) {
			for _, svc := range declared[pkg] {
				fqn := pkg + "." + svc
				if _, ok := set[fqn]; ok {
					continue
				}
				if _, ok := allow[fqn]; ok {
					usedAllow[fqn] = struct{}{}
					continue
				}
				findings = append(findings, MountFinding{
					Kind:   "unmounted",
					FQN:    fqn,
					Binary: bin,
					Reason: "объявлен контрактом, но композиционный корень его не монтирует — " +
						"каждый его RPC отвечает Unimplemented, тем же, чем отвечает сервис, которого нет",
				})
			}
		}
	}

	// Пакеты, которыми владеет хоть один бинарь. Владение — свойство наблюдаемое:
	// бинарь владеет пакетом, если монтирует хотя бы один его сервис.
	ownedPkgs := map[string]struct{}{}
	for fqn := range allMounted {
		if dot := strings.LastIndexByte(fqn, '.'); dot > 0 {
			ownedPkgs[fqn[:dot]] = struct{}{}
		}
	}

	// Исключение живёт, пока у него есть предмет.
	for _, a := range opts.Allow {
		if _, used := usedAllow[a]; used {
			continue
		}
		if _, mountedNow := allMounted[a]; !mountedNow && declaredSomewhere(declared, a) {
			// Пакет, которым не владеет НИ ОДИН бинарь, лежит ВНЕ предмета этого
			// анализатора — так объявлено его собственной шапкой: «домен, у
			// которого не смонтирован ни один сервис, этим правилом не покрыт».
			//
			// Прежде такая запись объявлялась истёкшей, и это было утверждением
			// о том, о чём анализатор судить не берётся: пропуск целого домена он
			// не ловит by construction, а значит и «запись лишняя» доказать не
			// может. Хуже того, вердикт получался НЕИСПОЛНИМЫМ в паре с соседним
			// гейтом: достижимость каталога требует, чтобы решение «не монтируем»
			// было записано ЗДЕСЬ и нигде больше, — и тогда домен, поднимаемый
			// следующей фазой, не имел законного состояния ни с записью, ни без
			// неё.
			//
			// Молчание здесь дырой не становится: запись перестаёт быть
			// беспредметной ровно тогда, когда пакетом начинают владеть, — и с
			// этого момента она судится обеими ветвями выше.
			if pkg := a[:strings.LastIndexByte(a, '.')]; func() bool {
				_, owned := ownedPkgs[pkg]
				return !owned
			}() {
				c.UnownedAllow++
				continue
			}
		}
		reason := "исключение больше нечего исключать: сервис СМОНТИРОВАН"
		if _, mountedNow := allMounted[a]; !mountedNow {
			if !declaredSomewhere(declared, a) {
				reason = "исключение больше нечего исключать: такого сервиса нет в контракте"
			}
		}
		findings = append(findings, MountFinding{Kind: "stale-allow", FQN: a, Reason: reason})
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].FQN < findings[j].FQN
	})

	if out != nil {
		_, _ = fmt.Fprintf(out, "перепись: стабов %d файлов, proto-пакетов %d, сервисов объявлено %d; "+
			"композиционных корней %d (файлов %d), из них монтирующих %d; "+
			"владеющих пакетов %d, смонтировано сервисов %d; исключений вне предмета "+
			"(пакетом никто не владеет) %d; находок %d\n",
			c.StubFiles, c.ProtoPackages, c.DeclaredSvcs, c.CmdPackages, c.CmdFiles,
			c.OwningBinaries, c.OwnedPackages, c.MountedSvcs, c.UnownedAllow, len(findings))
	}
	return findings, c, nil
}

func declaredSomewhere(declared map[string][]string, fqn string) bool {
	i := strings.LastIndexByte(fqn, '.')
	if i < 0 {
		return false
	}
	for _, s := range declared[fqn[:i]] {
		if s == fqn[i+1:] {
			return true
		}
	}
	return false
}

// declaredServices читает сгенерированные стабы: `ServiceName` в `ServiceDesc` —
// то самое имя, по которому grpc-go диспатчит вызов.
func declaredServices(apiRoot string, c *MountCensus) (map[string][]string, map[string]string, error) {
	declared := map[string][]string{}
	dirToProto := map[string]string{}
	err := rootedWalk(apiRoot, func(rel string) bool {
		return strings.HasSuffix(rel, "_grpc.pb.go")
	}, func(path string, b []byte) error {
		c.StubFiles++
		for _, m := range serviceNameRe.FindAllStringSubmatch(string(b), -1) {
			full := m[1]
			i := strings.LastIndexByte(full, '.')
			if i < 0 {
				continue
			}
			pkg, svc := full[:i], full[i+1:]
			declared[pkg] = append(declared[pkg], svc)
			dirToProto[filepath.Dir(path)] = pkg
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	for pkg := range declared {
		sort.Strings(declared[pkg])
		declared[pkg] = dedupe(declared[pkg])
		c.DeclaredSvcs += len(declared[pkg])
	}
	c.ProtoPackages = len(declared)
	return declared, dirToProto, nil
}

// mountedServices разбирает композиционные корни и собирает FQN сервисов,
// поднятых вызовом `Register<X>ServiceServer`.
func mountedServices(opts MountOptions, dirToProto map[string]string, c *MountCensus) (map[string]map[string]struct{}, error) {
	out := map[string]map[string]struct{}{}
	for _, root := range opts.Roots {
		base := filepath.Join(opts.Root, root)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() || filepath.Base(filepath.Dir(path)) != "cmd" {
				return nil
			}
			rel, _ := filepath.Rel(opts.Root, path)
			c.CmdPackages++
			set, files, err := mountedInPackage(path, opts, dirToProto)
			if err != nil {
				return err
			}
			c.CmdFiles += files
			if len(set) > 0 {
				out[filepath.ToSlash(rel)] = set
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func mountedInPackage(dir string, opts MountOptions, dirToProto map[string]string) (map[string]struct{}, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	set := map[string]struct{}{}
	files := 0
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, files, fmt.Errorf("parse %s: %w", filepath.Join(dir, name), err)
		}
		files++
		alias := aliasToProtoPackage(f, opts, dirToProto)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			svc, ok := serviceFromRegisterName(sel.Sel.Name)
			if !ok {
				return true
			}
			pkg, ok := alias[ident.Name]
			if !ok {
				return true
			}
			set[pkg+"."+svc] = struct{}{}
			return true
		})
	}
	return set, files, nil
}

// serviceFromRegisterName: `RegisterInternalZoneServiceServer` → `InternalZoneService`.
func serviceFromRegisterName(name string) (string, bool) {
	if !strings.HasPrefix(name, "Register") || !strings.HasSuffix(name, "ServiceServer") {
		return "", false
	}
	body := strings.TrimPrefix(name, "Register")
	body = strings.TrimSuffix(body, "Server")
	if body == "Service" || body == "" {
		return "", false
	}
	return body, true
}

// aliasToProtoPackage сопоставляет локальное имя импорта стабов с proto-пакетом.
// Имя берётся из явного алиаса, иначе — из имени пакета в каталоге стабов, а не
// из последнего сегмента пути: у сгенерированных пакетов он «v1».
func aliasToProtoPackage(f *ast.File, opts MountOptions, dirToProto map[string]string) map[string]string {
	out := map[string]string{}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !strings.HasPrefix(p, opts.ModulePath+"/"+opts.APIRoot+"/") {
			continue
		}
		dir := filepath.Join(opts.Root, strings.TrimPrefix(p, opts.ModulePath+"/"))
		proto, ok := dirToProto[dir]
		if !ok {
			continue
		}
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		} else {
			name = goPackageName(dir)
		}
		if name == "" || name == "_" || name == "." {
			continue
		}
		out[name] = proto
	}
	return out
}

func goPackageName(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil,
			parser.PackageClauseOnly|parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		return f.Name.Name
	}
	return ""
}

func dedupe(in []string) []string {
	out := in[:0]
	var prev string
	for i, s := range in {
		if i > 0 && s == prev {
			continue
		}
		out = append(out, s)
		prev = s
	}
	return out
}

func sortedStrKeys(m map[string]map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedStrKeys2(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
