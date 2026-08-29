// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// subscription_owner_naming_test.go — ИМЯ ВЛАДЕЛЬЦА журнала есть ключ карты
// СОЕДИНЕНИЙ края, а не путь в дереве и не ключ блока адресов чарта.
//
// # Предмет — омонимия, которая уже дважды разрешалась спором вместо замера
//
// Слово `backends` означает здесь две разные вещи:
//
//   - `backends:` в объявлении чарта — блок АДРЕСОВ, ключи `nlb` / `nlbInternal`;
//   - `proxy.Backends` в коде края — карта СОЕДИНЕНИЙ, ключи `loadbalancer` /
//     `loadbalancerInternal`.
//
// Шаблон подаёт первый адрес во вторую карту ПОД ДРУГИМ ИМЕНЕМ — через
// переменную окружения, — поэтому по объявлению чарта имя владельца не читается
// вовсе. Владельца резолвит вторая карта, значит принимаемое имя `loadbalancer`,
// а `nlb` не резолвится и даёт отказ старта.
//
// Цена уже наступала (kacho#1454): один разбор назвал верным именем то, что
// уронило бы старт края; другой прочёл объяснение и заключил обратное верному.
// Оба раза спор разрешался чтением кода, а не документа, — то есть свойство
// держалось прозой, которую каждый читал по-своему.
//
// # Почему сверка идёт с КОДОМ, а не с текстом объявления
//
// Множество принимаемых имён приезжает вызовом `config.Config.DomainsWithInternalBackend`
// — той самой функции, которой пользуется край. Читать вместо неё исходник как
// текст значило бы завести второе суждение об одном предмете: оно разошлось бы с
// первым молча, потому что оба непусты и оба выглядят действующими.

// ownerNamingFinding — одна находка о карте псевдонимов.
type ownerNamingFinding struct {
	owner string
	dir   string
	why   string
}

// judgeOwnerTreeDirAliases судит карту [ownerTreeDirAliases] против множества
// имён, которые край принимает.
//
// Отдельной функцией — ради ОДНОГО: доказательство способности гейта упасть
// обязано прогонять ТУ ЖЕ функцию суждения, а не её копию. Состав дерева и
// множество имён подаются входом, поэтому инъекция не трогает ни дерева, ни
// конфигурации.
//
// Находкой считается каждое из трёх, и все три — разные способы солгать:
//
//  1. ключ псевдонима край НЕ принимает — псевдоним заведён для имени, которого
//     не бывает, и перебор ведёт в никуда;
//  2. значение псевдонима край ПРИНИМАЕТ — тогда это не путь в дереве, а второе
//     имя владельца, и объяснение «псевдоним про каталог» ложно;
//  3. значения-каталога в дереве нет — перебор осматривает несуществующее.
func judgeOwnerTreeDirAliases(
	accepted []string,
	aliases map[string][]string,
	dirExists func(dir string) bool,
) []ownerNamingFinding {
	acceptedSet := make(map[string]bool, len(accepted))
	for _, name := range accepted {
		acceptedSet[name] = true
	}

	owners := make([]string, 0, len(aliases))
	for owner := range aliases {
		owners = append(owners, owner)
	}
	sort.Strings(owners)

	findings := make([]ownerNamingFinding, 0, 4)
	for _, owner := range owners {
		if !acceptedSet[owner] {
			findings = append(findings, ownerNamingFinding{owner: owner, why: "имени нет среди принимаемых краем"})
		}
		for _, dir := range aliases[owner] {
			if acceptedSet[dir] {
				findings = append(findings, ownerNamingFinding{owner: owner, dir: dir,
					why: "каталог совпал с ПРИНИМАЕМЫМ именем — это второе имя владельца, а не путь в дереве"})
			}
			if !dirExists(dir) {
				findings = append(findings, ownerNamingFinding{owner: owner, dir: dir,
					why: "каталога сервиса с таким именем в дереве нет"})
			}
		}
	}
	return findings
}

// servicesDirExists — существует ли каталог сервиса с таким именем.
func servicesDirExists(dir string) bool {
	info, err := os.Stat(filepath.Join("..", "..", "services", dir))
	return err == nil && info.IsDir()
}

// TestOwnerNameIsTheBackendKeyOfTheEdgeNotTheTreePath — псевдонимы пробы чарта
// сходятся с тем, что край действительно принимает.
func TestOwnerNameIsTheBackendKeyOfTheEdgeNotTheTreePath(t *testing.T) {
	accepted := config.Config{}.DomainsWithInternalBackend()

	t.Logf("перепись: имён принимает край %d %v · псевдонимов каталога объявлено %d %v",
		len(accepted), accepted, len(ownerTreeDirAliases), ownerTreeDirAliases)

	if len(accepted) == 0 {
		t.Fatalf("край не принимает НИ ОДНОГО имени владельца — гейт ничего не сверял; " +
			"карта соединений либо пуста, либо перестала нести внутренние адреса")
	}

	for _, f := range judgeOwnerTreeDirAliases(accepted, ownerTreeDirAliases, servicesDirExists) {
		if f.dir == "" {
			t.Errorf("владелец %q: %s (край принимает %v) — псевдоним объявлен для имени, "+
				"которого край не знает, и перебор каталога никогда не сработает",
				f.owner, f.why, accepted)
			continue
		}
		t.Errorf("владелец %q, каталог %q: %s — псевдоним обязан называть ПУТЬ В ДЕРЕВЕ, "+
			"а имя владельца берётся из карты соединений края", f.owner, f.dir, f.why)
	}
}

// TestEveryOwnerAcceptedByTheEdgeIsResolvableToATreeDir — у каждого принимаемого
// имени есть каталог, в котором проба чарта найдёт его потолок.
//
// Обратная сторона предыдущего утверждения. Без неё гейт судил бы только
// объявленные псевдонимы и молчал бы о владельце, чей каталог зовётся иначе, а
// псевдонима ему не завели: такой владелец, будучи объявлен, дал бы «потолок не
// найден» — отказ верный, но наступающий на выкатке, а не здесь.
//
// Домены без журнала (`iam`, `geo`, `quota`) под требование НЕ подпадают:
// владельцем их не объявляют, и каталог им не нужен. Требование привязано к
// ФАКТУ — принимаемому имени, у которого каталог обязан находиться.
func TestEveryOwnerAcceptedByTheEdgeIsResolvableToATreeDir(t *testing.T) {
	accepted := config.Config{}.DomainsWithInternalBackend()
	resolved, unresolved := 0, make([]string, 0, 2)

	for _, owner := range accepted {
		found := false
		for _, dir := range append([]string{owner}, ownerTreeDirAliases[owner]...) {
			if servicesDirExists(dir) {
				found = true
				break
			}
		}
		if found {
			resolved++
			continue
		}
		unresolved = append(unresolved, owner)
	}

	t.Logf("перепись: имён принимает край %d · каталог резолвится у %d · не резолвится у %d %v",
		len(accepted), resolved, len(unresolved), unresolved)

	if len(unresolved) > 0 {
		t.Errorf("имена %s край принимает, а каталога сервиса у них нет ни под своим именем, "+
			"ни под псевдонимом: объявив такого владельца, оператор узнает об этом отказом "+
			"выкатки, а не здесь — заведи псевдоним в ownerTreeDirAliases",
			strings.Join(unresolved, ", "))
	}
}
