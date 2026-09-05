// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

const (
	providerEdgeClaimCorpusDir = "terraform/internal/provider"
	providerEdgeModulesDir     = "terraform/modules"
	contractRoot               = "proto/kacho/cloud"
)

// rpcDeclRe — ОБЪЯВЛЕНИЕ RPC. Судится объявление, а не упоминание: имя глагола
// встречается и в комментариях, и гейт по подстроке считал бы существующим то, о
// чём контракт только рассуждает.
var rpcDeclRe = regexp.MustCompile(`(?m)^\s*rpc\s+([A-Za-z][A-Za-z0-9]*)\s*\(`)

// serviceDeclRe — ОБЪЯВЛЕНИЕ службы. Нужно, чтобы у метода был объект: имя RPC его
// не несёт (`rpc Update` объявлен в шести доменах из шести), объект живёт здесь.
var serviceDeclRe = regexp.MustCompile(`(?m)^\s*service\s+([A-Za-z][A-Za-z0-9]*)\s*\{`)

// restActionRe — суффикс-действие REST-привязки: `post: "/…/{id}:start"`.
//
// Набор символов ОБЯЗАН совпадать с тем, что распознаётся в прозе
// (edgeActionTokenRe): распознаватель, знающий дефис на одной стороне и не знающий
// на другой, объявляет несуществующим то, что контракт объявляет. Первая редакция
// была именно такой и дала ЧЕТЫРЕ ложные находки на `:add-cidr-blocks` /
// `:remove-cidr-blocks` — токенах, которые контракт vpc несёт.
var restActionRe = regexp.MustCompile(`"(?:/[^"]*?)(:[a-zA-Z][A-Za-z0-9-]*)"`)

// stubImportRe — импорт стабов домена: `nlbv1 "…/kacho/cloud/loadbalancer/v1"`.
//
// Отсюда берётся И домен файла, И соответствие «слово провайдера → каталог
// контракта» (`nlb` → `loadbalancer`). Таблицы соответствий здесь нет намеренно:
// рукописная разошлась бы с деревом молча, а импорт правится вместе с кодом.
var stubImportRe = regexp.MustCompile(`([a-z]+)v1\s+"github\.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/([a-z]+)/v1"`)

// tfResourceRe — тип ресурса в модуле: `kacho_vpc_security_group`.
var tfResourceRe = regexp.MustCompile(`kacho_([a-z]+)_[a-z_]+`)

// TestProviderDoesNotMisstateTheEdgeVerbs — провайдер не отрицает предмет, который
// в контракте ЕГО домена есть, и не утверждает предмет, которого там нет.
//
// Предмет, механика, два способа резолва и границы — в шапке
// ct2_registry_provider_edge_denial.go; здесь они не пересказываются, иначе
// разойдутся.
func TestProviderDoesNotMisstateTheEdgeVerbs(t *testing.T) {
	root := repoRoot(t)

	aliases := providerDomainAliases(t, root)
	if len(aliases) == 0 {
		t.Fatal("соответствий «слово провайдера → каталог контракта» прочитано НОЛЬ: " +
			"домен ни одного файла не выводится, и сверять будет не с чем")
	}

	contracts := edgeContracts(t, root, aliases)
	if len(contracts) == 0 {
		t.Fatal("контрактов прочитано НОЛЬ — вердикт беспредметен")
	}
	if missing, ok := EdgeClaimPremiseHolds(contracts); !ok {
		t.Fatalf("предпосылка гейта отпала: словарь резолвит %s, а таких глаголов НИ В ОДНОМ "+
			"контракте больше нет. Отрицания стали правдой — их надо перечитать, а не "+
			"оставить под мёртвым запретом (снять запись словаря либо перевести её на живой глагол)",
			missing)
	}

	sources := edgeClaimSources(t, root, aliases)
	if len(sources) == 0 {
		t.Fatal("файлов корпуса прочитано НОЛЬ — обход пуст, и «ноль находок» здесь " +
			"означает «ноль прочитанного»")
	}

	findings, notes, census, err := ScanProviderEdgeClaims(sources, contracts)
	if err != nil {
		t.Fatalf("разбор корпуса: %v", err)
	}
	t.Log(census.String())
	if census.Sentences == 0 {
		t.Fatal("предложений прочитано НОЛЬ — читать было нечего")
	}
	if census.Resolved == 0 {
		t.Fatal("резолвится НОЛЬ утверждений: словарь и токены не встретили в дереве ни " +
			"одного предмета, то есть гейт судит пустоту")
	}

	// Нерезолвящиеся утверждения печатаются ПОИМЁННО, а не сводятся в число.
	// Число говорит «часть предмета не осмотрена», имя — какая именно; без имён
	// слепая зона выглядит проверенной ровно так же, как проверенная часть, и
	// следующий читатель прочтёт зелёный вердикт шире, чем он есть.
	for _, n := range notes {
		if !n.Resolved {
			t.Logf("вне резолва: %s", n.String())
		}
	}

	for _, f := range findings {
		t.Error(f.String())
	}
}

// providerDomainAliases — «слово провайдера → каталог контракта», выведенное из
// импортов стабов (`nlb` → `loadbalancer`, `vpc` → `vpc`, …).
func providerDomainAliases(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, f := range providerGoFiles(t, root) {
		for _, m := range stubImportRe.FindAllStringSubmatch(edgeReadFile(t, f), -1) {
			out[m[1]] = m[2]
		}
	}
	return out
}

// edgeContracts — контракты доменов, на которые провайдер вообще ссылается.
func edgeContracts(t *testing.T, root string, aliases map[string]string) map[string]EdgeContract {
	t.Helper()
	dirs := map[string]bool{}
	for _, dir := range aliases {
		dirs[dir] = true
	}
	names := make([]string, 0, len(dirs))
	for d := range dirs {
		names = append(names, d)
	}
	sort.Strings(names)

	out := map[string]EdgeContract{}
	for _, dir := range names {
		files, err := treecorpus.UnderWithSuffix(filepath.Join(root, contractRoot, dir), ".proto")
		if err != nil {
			t.Fatalf("обход контракта %s: %v", dir, err)
		}
		c := EdgeContract{Domain: dir, RPCs: map[string]bool{}, Actions: map[string]bool{},
			Methods: map[string]bool{}}
		for _, f := range files {
			src := edgeReadFile(t, f)
			for _, m := range rpcDeclRe.FindAllStringSubmatch(src, -1) {
				c.RPCs[m[1]] = true
			}
			for _, m := range restActionRe.FindAllStringSubmatch(src, -1) {
				c.Actions[m[1]] = true
			}
			// Метод вместе со службой. Служба берётся ПОСЛЕДНЯЯ объявленная выше
			// метода: в файле контракта их бывает несколько, и приписать все методы
			// первой значило бы объявить существующим `ListenerService/AddTargets`.
			svc := ""
			for _, line := range strings.Split(src, "\n") {
				if m := serviceDeclRe.FindStringSubmatch(line); m != nil {
					svc = m[1]
					continue
				}
				if m := rpcDeclRe.FindStringSubmatch(line); m != nil && svc != "" {
					c.Methods[svc+"/"+m[1]] = true
				}
			}
		}
		if len(c.RPCs) == 0 {
			t.Fatalf("контракт %s не дал ни одного объявления rpc — разбор сломан, "+
				"а не домен пуст", dir)
		}
		if len(c.Methods) == 0 {
			t.Fatalf("контракт %s не дал ни одной пары «служба/метод» — объект методов "+
				"не выведен, и квалифицированный токен резолвить будет не с чем", dir)
		}
		out[dir] = c
	}
	return out
}

// edgeClaimSources — корпус: прод-исходники провайдера и его модули `.tf`.
//
// Домен выводится из ФАЙЛА (`.go` — импорт стабов) либо из МОДУЛЯ (`.tf` — типы
// ресурсов по всему каталогу модуля, потому что `variables.tf` их не содержит).
// Домен, выведенный неоднозначно или не выведенный вовсе, оставляется пустым: тогда
// утверждения файла считаются переписью, но проверенными не объявляются.
func edgeClaimSources(t *testing.T, root string, aliases map[string]string) []EdgeSource {
	t.Helper()
	var out []EdgeSource

	for _, f := range providerGoFiles(t, root) {
		src := edgeReadFile(t, f)
		domains := map[string]bool{}
		for _, m := range stubImportRe.FindAllStringSubmatch(src, -1) {
			domains[m[2]] = true
		}
		out = append(out, EdgeSource{Path: edgeRelTo(root, f), Text: src, Kind: "go",
			Domain: soleKey(domains)})
	}

	tfFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, providerEdgeModulesDir), ".tf")
	if err != nil {
		t.Fatalf("обход модулей: %v", err)
	}
	moduleDomain := map[string]string{}
	byModule := map[string][]string{}
	for _, f := range tfFiles {
		mod := filepath.Dir(f)
		byModule[mod] = append(byModule[mod], f)
	}
	for mod, files := range byModule {
		domains := map[string]bool{}
		for _, f := range files {
			for _, m := range tfResourceRe.FindAllStringSubmatch(edgeReadFile(t, f), -1) {
				if dir, ok := aliases[m[1]]; ok {
					domains[dir] = true
				}
			}
		}
		moduleDomain[mod] = soleKey(domains)
	}
	for _, f := range tfFiles {
		out = append(out, EdgeSource{Path: edgeRelTo(root, f), Text: edgeReadFile(t, f), Kind: "tf",
			Domain: moduleDomain[filepath.Dir(f)]})
	}
	return out
}

// providerGoFiles — прод-исходники провайдера (без проб).
func providerGoFiles(t *testing.T, root string) []string {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, providerEdgeClaimCorpusDir), ".go")
	if err != nil {
		t.Fatalf("обход провайдера: %v", err)
	}
	out := files[:0:0]
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			out = append(out, f)
		}
	}
	return out
}

// soleKey — единственный ключ множества; при ноле или нескольких — пусто.
func soleKey(m map[string]bool) string {
	if len(m) != 1 {
		return ""
	}
	for k := range m {
		return k
	}
	return ""
}

func edgeReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	return string(b)
}

func edgeRelTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}
