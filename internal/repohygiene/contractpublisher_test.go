// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
	"github.com/PRO-Robotech/kacho/internal/contractsource"
)

// contractpublisher_test.go — гейты над ТРЕМЯ деревьями: платформа, служба
// доступа, общий фундамент. Предмет разобран у судей (contractpublisher.go);
// здесь — только добыча входа.
//
// Способность падать доказывает инъекция (contractpublisher_injection_test.go),
// а не эти прогоны.

// declaredCompositionModules — модули состава, чьи деревья заглушек сверяются.
//
// Собственный модуль назван первым и его дом — ЭТО дерево (индекс git); у
// остальных дом — кэш модулей по версии из `go.mod`.
var declaredCompositionModules = []struct {
	Module  string
	StubDir string // каталог дерева заглушек ОТНОСИТЕЛЬНО корня модуля
}{
	{"github.com/PRO-Robotech/kacho", "pkg/api"},
	{"github.com/PRO-Robotech/kaname", "pkg/api"},
	{"github.com/PRO-Robotech/corelib", "api"},
}

// protoPathOfStub — путь контракта, из которого порождена заглушка.
//
// Разбор идёт по СУФФИКСУ ИМЕНИ, а не по обрезанию последнего расширения: у
// одного контракта заглушек до трёх, и каждая несёт свой суффикс
// (`x.pb.go` · `x_grpc.pb.go` · `x.pb.gw.go`). Обрезание дало бы у второй путь
// `…_grpc.proto`, которого не существует, — то есть ТРИ разных «пути» у одного
// файла контракта, и дубль между модулями перестал бы совпадать.
//
// Файл, не являющийся заглушкой (рукописная проба рядом со стабами), даёт
// (пусто, false) — это НЕ находка: такие файлы в дереве заглушек бывают
// законно, и их 78 из 165 у платформы.
func protoPathOfStub(rel string) (string, bool) {
	slash := filepath.ToSlash(rel)
	switch {
	case strings.HasSuffix(slash, "_grpc.pb.go"):
		return strings.TrimSuffix(slash, "_grpc.pb.go") + ".proto", true
	case strings.HasSuffix(slash, ".pb.gw.go"):
		return strings.TrimSuffix(slash, ".pb.gw.go") + ".proto", true
	case strings.HasSuffix(slash, ".pb.go"):
		return strings.TrimSuffix(slash, ".pb.go") + ".proto", true
	}
	return "", false
}

// TestEveryContractPathHasExactlyOnePublishingModule — путь `.proto`
// публикуется заглушками РОВНО ОДНИМ модулем состава.
//
// Обход: собственное дерево — из ИНДЕКСА git (обход диска читал бы игнорируемые
// каталоги, и вердикт стал бы свойством рабочего каталога); чужие — обходом кэша
// модулей, который и ЕСТЬ распакованный проверенный коммит.
func TestEveryContractPathHasExactlyOnePublishingModule(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	var pubs []contractPublication
	filesRead := 0

	for _, m := range declaredCompositionModules {
		var files []string
		if m.Module == ownModulePath {
			tracked, err := treecorpus.Under(filepath.Join(root, m.StubDir))
			if err != nil {
				t.Fatalf("состав дерева заглушек %s: %v", m.StubDir, err)
			}
			for _, abs := range tracked {
				rel, rerr := filepath.Rel(filepath.Join(root, m.StubDir), abs)
				if rerr != nil {
					t.Fatalf("относительный путь для %s: %v", abs, rerr)
				}
				files = append(files, rel)
			}
		} else {
			dir, err := declaredModuleRootDir(root, m.Module, os.ReadFile, gomodcacheDir)
			if err != nil {
				t.Fatalf("дом модуля %s: %v — вердикт о единственности публикатора "+
					"относился бы к непрочитанному дереву", m.Module, err)
			}
			stubs := filepath.Join(dir, m.StubDir)
			werr := filepath.Walk(stubs, func(p string, fi os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if fi.IsDir() || !strings.HasSuffix(p, ".go") {
					return nil
				}
				rel, rerr := filepath.Rel(stubs, p)
				if rerr != nil {
					return rerr
				}
				files = append(files, rel)
				return nil
			})
			if werr != nil {
				t.Fatalf("обход дерева заглушек модуля %s (%s): %v", m.Module, stubs, werr)
			}
		}

		filesRead += len(files)
		paths := map[string]struct{}{}
		for _, rel := range files {
			if path, ok := protoPathOfStub(rel); ok {
				paths[path] = struct{}{}
			}
		}
		ordered := make([]string, 0, len(paths))
		for path := range paths {
			ordered = append(ordered, path)
		}
		sort.Strings(ordered)
		pubs = append(pubs, contractPublication{Module: m.Module, Paths: ordered})
		t.Logf("модуль %s: файлов дерева заглушек %d, путей .proto %d",
			m.Module, len(files), len(ordered))
	}

	faults, census := AuditContractPublishers(pubs, filesRead)
	t.Logf("перепись: %s", census.String())

	if len(faults) > 0 {
		t.Fatalf("путь .proto публикуется больше чем одним модулем (%d):\n  %s\n%s",
			len(faults), strings.Join(faults, "\n  "), census.String())
	}
}

// TestExternalContractRootsAreNotTrackedInThisTree — корень, объявленный
// приезжающим модулем, НЕ отслеживается в `proto/` этого дерева.
//
// # Почему это отдельный судья, а не проверка внутри резолвера
//
// `internal/contractsource` намеренно берёт корень ИЗ ДЕРЕВА, если он там есть:
// иначе воспроизводка порождения от копии собранного дерева (ось A13 гейта
// разреза) была бы отвергнута как незаконная. Значит запрет двойного дома
// держится ЗДЕСЬ, над индексом git, и своим предикатом.
//
// # Проба собственной предпосылки
//
// Ноль отслеживаемых путей — половина утверждения. Вторая половина: корень
// обязан СУЩЕСТВОВАТЬ в своём модуле. Без неё «в дереве его нет» было бы
// истинным и для корня, которого нет вообще нигде, — то есть утверждением,
// неспособным упасть.
func TestExternalContractRootsAreNotTrackedInThisTree(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	if len(contractsource.ExternalRootModules) == 0 {
		t.Fatal("внешних корней контракта не объявлено ни одного: сверять нечего, и " +
			"зелёное здесь означало бы «не спрашивали», а не «двойного дома нет»")
	}

	roots := make([]string, 0, len(contractsource.ExternalRootModules))
	for r := range contractsource.ExternalRootModules {
		roots = append(roots, r)
	}
	sort.Strings(roots)

	var faults []string
	tracked := 0
	for _, r := range roots {
		files, err := treecorpus.Under(filepath.Join(root, "proto", r))
		switch {
		case err == nil && len(files) > 0:
			tracked += len(files)
			faults = append(faults, fmt.Sprintf(
				"корень %q объявлен приезжающим модулем %s, а в индексе этого дерева под "+
					"proto/%s отслеживается %d файлов: один путь .proto оказался бы в двух "+
					"деревьях, и модуль, порождающий по нему заглушки, стал бы вторым "+
					"публикатором — паника регистрации дескрипторов у транзитивного потребителя",
				r, contractsource.ExternalRootModules[r], r, len(files)))
		case err == nil:
			// Каталог есть, файлов в индексе нет — то же нарушение по существу,
			// но назвать его надо иначе: пустой каталог в дереве не даёт дубля.
			faults = append(faults, fmt.Sprintf(
				"корень %q объявлен внешним, а каталог proto/%s в дереве существует и пуст: "+
					"запись без файлов не даёт дубля, но и предметом не является — снимите каталог",
				r, r))
		}
		// Вторая половина: корень обязан существовать в своём модуле.
		dir, derr := contractsource.Dir(root, r)
		if derr != nil {
			faults = append(faults, fmt.Sprintf(
				"корень %q объявлен внешним, но его дерева нет и в модуле: %v — "+
					"утверждение «в этом дереве его нет» истинно by construction и потому "+
					"не является утверждением", r, derr))
			continue
		}
		protos, ferr := contractsource.Files(root, r, ".proto")
		if ferr != nil {
			faults = append(faults, fmt.Sprintf(
				"корень %q резолвится в %s, но контрактов под ним не прочитано: %v", r, dir, ferr))
			continue
		}
		t.Logf("корень %q: в индексе этого дерева 0, в модуле %s — контрактов %d",
			r, contractsource.ExternalRootModules[r], len(protos))
	}

	t.Logf("перепись: внешних корней объявлено %d · отслеживаемых файлов под ними в этом дереве %d",
		len(roots), tracked)

	if len(faults) > 0 {
		t.Fatalf("двойной дом дерева контрактов (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
}

// shellExternalRootModules — перечень «корень → модуль» из ПРИСВАИВАНИЯ массива
// в библиотеке раскладки. Читается присваивание, а не упоминание: имя массива
// встречается и в комментариях, объясняющих сам перечень.
func shellExternalRootModules(t *testing.T, root string) (map[string]string, int) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(root, "gateway", "scripts", "lib", "stage-proto-tree.sh")) // #nosec G304 -- путь собран из корня дерева
	if err != nil {
		t.Fatalf("чтение библиотеки раскладки: %v", err)
	}
	body := string(src)
	mentions := strings.Count(body, "KACHO_PROTO_ROOT_MODULES")
	if mentions == 0 {
		t.Fatalf("имя массива KACHO_PROTO_ROOT_MODULES не встречено в библиотеке раскладки "+
			"ни разу при %d прочитанных байтах: разбор ищет присваивание, и ноль упоминаний "+
			"означает, что перечень переехал, а не что он пуст", len(src))
	}
	out := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "KACHO_PROTO_ROOT_MODULES=(") {
			continue
		}
		inner := trimmed[strings.Index(trimmed, "(")+1:]
		inner = strings.TrimSuffix(inner, ")")
		for _, field := range strings.Fields(inner) {
			field = strings.Trim(field, `"'`)
			rootName, module, ok := strings.Cut(field, "=")
			if !ok {
				t.Fatalf("запись перечня оболочки %q не имеет формы «корень=модуль»: "+
					"разбор по знаку равенства — часть контракта объявления", field)
			}
			out[rootName] = module
		}
	}
	return out, mentions
}

// TestExternalContractRootModulesAgreeBetweenGoAndShell — два объявления одного
// перечня не расходятся.
func TestExternalContractRootModulesAgreeBetweenGoAndShell(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	fromShell, mentions := shellExternalRootModules(t, root)
	faults, census := AuditExternalRootDeclarations(contractsource.ExternalRootModules, fromShell, mentions)
	t.Logf("перепись: %s", census.String())

	if len(faults) > 0 {
		t.Fatalf("объявления внешних корней разошлись (%d):\n  %s\n%s",
			len(faults), strings.Join(faults, "\n  "), census.String())
	}
}

// gomodcacheDir — каталог кэша модулей. Спрашивается сам `go`: форма пути кэша
// (регистро-экранирование) — его внутреннее дело, и собранный вручную путь
// разошёлся бы с ним молча.
func gomodcacheDir() (string, error) {
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
