// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package contractsource отвечает на ОДИН вопрос: где на диске лежит дерево
// контрактов данного корня.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// До 2026-09-13 ответ был один для всех корней — `<корень дерева>/proto/<корень>`,
// — и потому его писали литералом: `filepath.Join(root, "proto", "kaname", …)`.
// Литералов таких в дереве десятки, в двадцати девяти файлах Go.
//
// Решением владельца (kacho#2616, исход C) контракты службы доступа уехали в её
// репозиторий: под `proto/` их больше НЕТ, а платформе они по-прежнему нужны —
// край выводит из них 117 записей каталога прав из 350 и 104 строки таблицы
// маршрутов из 308, а два десятка проб судят по ним модель прав. Приезжают они
// опубликованным модулем, каталогом `proto/<корень>` внутри него.
//
// ЧЕМ ПЛОХ ЛИТЕРАЛ ПОСЛЕ ПЕРЕЕЗДА. Он не краснеет и не зеленеет — он ОТКАЗЫВАЕТ
// либо МОЛЧИТ, в зависимости от того, читает проба файл или обходит каталог:
// читающая честно падает «нет такого файла», обходящая получает пустую
// популяцию и печатает «находок 0» по ней. Второе хуже первого: вердикт при
// этом зелёный.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТОТ ПАКЕТ НЕ ДЕЛАЕТ
//
// Он не судит, законно ли корень лежит в двух местах. Корень, физически
// присутствующий в `proto/`, берётся ОТТУДА, и модуль для него не резолвится
// вовсе — иначе всякая воспроизводка порождения от копии собранного дерева
// (gateway/scripts/check-domain-generation.sh, ось A13) отвергалась бы как
// незаконная. Запрет ДВОЙНОЙ публикации одного пути `.proto` держит свой судья
// по своему предикату — над индексом git:
// `internal/repohygiene` TestExternalContractRootsAreNotTrackedInThisTree.
package contractsource

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ExternalRootModules — корни дерева контрактов, ЧЬИХ ИСХОДНИКОВ В ЭТОМ ДЕРЕВЕ
// НЕТ: они приезжают опубликованным модулем Go, каталогом `proto/<корень>`.
//
// ЭТО ВТОРОЕ ОБЪЯВЛЕНИЕ ОДНОГО ПРЕДМЕТА, и оно неизбежно: первое — массив
// `KACHO_PROTO_ROOT_MODULES` в gateway/scripts/lib/stage-proto-tree.sh, а
// оболочка не может импортировать пакет Go. Расхождение двух объявлений обязано
// краснеть, и его держит `internal/repohygiene`
// TestExternalContractRootModulesAgreeBetweenGoAndShell — тем же разбором ПРИСВАИВАНИЯ,
// которым сверяется перечень самих корней (contractrootparity.go).
var ExternalRootModules = map[string]string{
	"kaname": "github.com/PRO-Robotech/kaname",
}

type moduleDirKey struct{ repoRoot, module string }

var (
	moduleDirMu    sync.Mutex
	moduleDirCache = map[moduleDirKey]string{}
)

// RootOf — корень дерева контрактов, названный путём относительно `proto/`.
// Пустой путь — ошибка, а не «корень по умолчанию»: умолчания у корня нет.
func RootOf(relToProto string) (string, error) {
	clean := strings.TrimPrefix(filepath.ToSlash(relToProto), "/")
	if clean == "" {
		return "", fmt.Errorf("contractsource: пустой путь относительно proto/ — корня в нём не названо")
	}
	return strings.SplitN(clean, "/", 2)[0], nil
}

// Dir — каталог на диске, в котором лежит дерево КОРНЯ.
//
// Для корня, лежащего в этом дереве, — `<repoRoot>/proto/<root>`. Для корня из
// [ExternalRootModules], которого в дереве нет, — `<каталог модуля>/proto/<root>`.
func Dir(repoRoot, root string) (string, error) {
	inTree := filepath.Join(repoRoot, "proto", root)
	if st, err := os.Stat(inTree); err == nil && st.IsDir() {
		return inTree, nil
	}
	module, external := ExternalRootModules[root]
	if !external {
		return "", fmt.Errorf(
			"contractsource: дерева корня %q нет в %s, и внешним модулем он не объявлен — "+
				"назови его в ExternalRootModules либо исправь путь", root, inTree)
	}
	modDir, err := moduleDir(repoRoot, module)
	if err != nil {
		return "", err
	}
	out := filepath.Join(modDir, "proto", root)
	st, err := os.Stat(out)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf(
			"contractsource: модуль %s резолвится (%s), но каталога proto/%s в нём нет — "+
				"версия модуля не несёт дерева контрактов", module, modDir, root)
	}
	return out, nil
}

// Path — абсолютный путь к файлу или каталогу дерева контрактов, названному
// путём ОТНОСИТЕЛЬНО `proto/` (например "kaname/cloud/iam/v1/fga_model.fga").
func Path(repoRoot, relToProto string) (string, error) {
	root, err := RootOf(relToProto)
	if err != nil {
		return "", err
	}
	dir, err := Dir(repoRoot, root)
	if err != nil {
		return "", err
	}
	rest := strings.TrimPrefix(filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(relToProto), "/")), root)
	return filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(rest, "/"))), nil
}

// Files — пути файлов дерева контрактов под relToProto, чьё имя кончается одним
// из suffixes (пустой перечень — все файлы). Пути АБСОЛЮТНЫЕ, порядок
// устойчивый.
//
// ИСТОЧНИК СОСТАВА РАЗНЫЙ У ДВУХ ВИДОВ КОРНЯ, и это не небрежность. Для корня в
// этом дереве состав обязан браться из ИНДЕКСА git: обход диска читал бы
// игнорируемые каталоги — рабочие копии агентов, распаковки чартов, отчёты
// прогонов, — и вердикт стал бы свойством рабочего каталога, а не коммита. Для
// корня из кэша модулей верно обратное: кэш и ЕСТЬ распаковка проверенного
// коммита, индекса git у него нет by construction, а игнорируемых файлов в нём
// не бывает — там обход диска и есть обход коммита.
//
// Пустой состав — ОТКАЗ, а не пустой срез: вызывающий не обязан отличать
// «файлов нет» от «прочитано не то дерево», и здесь это решается за него.
func Files(repoRoot, relToProto string, suffixes ...string) ([]string, error) {
	root, err := RootOf(relToProto)
	if err != nil {
		return nil, err
	}
	dir, err := Path(repoRoot, relToProto)
	if err != nil {
		return nil, err
	}
	var out []string
	if _, external := ExternalRootModules[root]; external && !strings.HasPrefix(dir, filepath.Join(repoRoot, "proto")+string(os.PathSeparator)) {
		err = filepath.Walk(dir, func(p string, fi os.FileInfo, werr error) error {
			if werr != nil {
				return werr
			}
			if fi.IsDir() {
				return nil
			}
			if matchesSuffix(p, suffixes) {
				out = append(out, p)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("contractsource: обход %s: %w", dir, err)
		}
	} else {
		cmd := exec.Command("git", "-C", repoRoot, "ls-files", "-z", "--full-name", "--", filepath.ToSlash(filepath.Join("proto", relToProto)))
		raw, cerr := cmd.Output()
		if cerr != nil {
			return nil, fmt.Errorf("contractsource: git ls-files в %s: %w", dir, cerr)
		}
		for _, rel := range strings.Split(string(raw), "\x00") {
			if rel == "" {
				continue
			}
			p := filepath.Join(repoRoot, filepath.FromSlash(rel))
			if matchesSuffix(p, suffixes) {
				out = append(out, p)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf(
			"contractsource: под %s не найдено ни одного файла%s — пустой состав есть отказ обхода, "+
				"а не пустое дерево: вердикт по нему был бы свойством непрочитанного", dir, suffixList(suffixes))
	}
	sort.Strings(out)
	return out, nil
}

func matchesSuffix(p string, suffixes []string) bool {
	if len(suffixes) == 0 {
		return true
	}
	for _, s := range suffixes {
		if strings.HasSuffix(p, s) {
			return true
		}
	}
	return false
}

func suffixList(suffixes []string) string {
	if len(suffixes) == 0 {
		return ""
	}
	return " с суффиксом " + strings.Join(suffixes, "|")
}

// moduleDir — каталог распакованного модуля. Спрашивается сам `go`, а не
// собирается путь в кэше: форма пути кэша (регистро-экранирование) — его
// внутреннее дело, и собранный вручную путь разошёлся бы с ним молча.
func moduleDir(repoRoot, module string) (string, error) {
	key := moduleDirKey{repoRoot: repoRoot, module: module}
	moduleDirMu.Lock()
	defer moduleDirMu.Unlock()
	if d, ok := moduleDirCache[key]; ok {
		return d, nil
	}
	ask := func() string {
		cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", module)
		cmd.Dir = repoRoot
		raw, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(raw))
	}
	dir := ask()
	if dir == "" {
		// Кэш модулей мог быть не прогрет: одна попытка добора, и только одна.
		dl := exec.Command("go", "mod", "download", module)
		dl.Dir = repoRoot
		_ = dl.Run()
		dir = ask()
	}
	if dir == "" {
		return "", fmt.Errorf(
			"contractsource: модуль %s не резолвится из %s — дерево его контрактов взять неоткуда", module, repoRoot)
	}
	moduleDirCache[key] = dir
	return dir, nil
}
