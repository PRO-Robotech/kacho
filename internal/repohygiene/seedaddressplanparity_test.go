// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// seedaddressplanparity_test.go — гейт по посевным скриптам: авторы одной
// фикстурной координаты согласны в её адресном плане.
//
// # Предмет
//
// Имя вроде `existingNetworkId` набор видит как ОДНУ сеть, а авторов у неё
// несколько: умбрелла-поток сеет своим скриптом, отдельный прогон — своим. Кейсы
// режут подсети в этой сети адресами из собственной энтропии, и с тех пор, как
// нарезка следует ОПУБЛИКОВАННОМУ плану (`carve_cidr_pre`, гейт
// `TestOutOfCaseCarveTakesItsCidrFromThePublishedPlan`), мимо плана они не уходят
// ни у одного автора. Требование согласия этим не снимается — оно меняет предмет:
//
//   - ШИРИНА плана задаёт, сколько различных адресов нарезка вообще может выдать,
//     и это НЕ видно из кейса. Замер: под `10.0.0.0/8` помощнику доступно 65536
//     различных /24, под `10.196.0.0/16` — 256. Набор nlb режет за прогон порядка
//     семи десятков подсетей в одной сети, где пересечение отвергается сразу
//     (`Subnet CIDRs can not overlap`): при 256 позициях столкновение внутри
//     прогона практически неизбежно, при 65536 — единицы процентов. То есть от
//     того, ЧЕЙ посев отработал, зависит устойчивость набора, а сам набор об этом
//     ничего не сообщает;
//   - всё, что написано ПРО план в другом месте дерева (литеральная фикстура,
//     кейс на пересечение, страница документации), верно у одного автора и ложно
//     у другого — два места об одном предмете, из которых верно одно.
//
// # Согласие — это ПОКРЫТИЕ, а не совпадение строк
//
// Автор вправе объявить рядом лишнюю, более узкую сеть для собственных нужд: она
// укладывается в чужой план, и адрес, законный у одного, у другого проходит.
// Расхождение — когда блок одного автора не умещается ни в один блок другого:
// тогда тот же адрес один принимает, а второй отвергает. Поэтому сравниваются
// префиксы, а не тексты, и пустой план семейства покрытием НЕ считается.
//
// # Почему гейт ищет ПРОИЗВОДИТЕЛЕЙ, а не сверяет два известных файла
//
// Сверка пары имён закрыла бы найденный экземпляр и пропустила бы следующий: автор
// добавляется отдельным файлом, и никто не обязан помнить про этот тест. Гейт
// поэтому находит всех, кто ПУБЛИКУЕТ координату, и требует согласия между ними —
// список авторов выводится из дерева, а не выписывается здесь.
//
// # Область сверки — НАБОР, а не имя координаты
//
// Одно и то же имя (`existingNetworkId`) публикуют посевы разных наборов, и это
// РАЗНЫЕ сети в разных файлах окружения. Сверять их между собой значило бы объявить
// находкой то, что находкой не является, — и первое же ложное срабатывание сняло бы
// гейт. Авторы поэтому группируются по набору, имя которого берётся из имени
// посевного файла, и сверка идёт внутри группы.
//
// # Предпосылка собственного молчания
//
// Гейт падает, если не нашлось ни одного набора с двумя и более авторами: тогда
// сверять было нечего, и «ноль расхождений» означало бы «не смотрел», а не
// «согласны». Тот же отказ приходит, если переименовали координату или сменилась
// схема имён посевных файлов, — молчание из-за сломанного распознавания отличимо от
// молчания из-за согласия.
package repohygiene

import (
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// seedPlanScanRoots — где живут посевные скрипты.
var seedPlanScanRoots = []string{
	filepath.Join("deploy", "scripts"),
	filepath.Join("tests", "authz-fixtures"),
}

type seedPlanAuthor struct {
	file string
	svc  string
	v4   []string
	v6   []string
}

type seedPlanScan struct {
	filesRead   int
	producers   int
	multiAuthor int // наборов, у координаты которых больше одного автора
	compared    int
	hits        []string
}

var (
	// Публикация координаты: `existingNetworkId=$NET_ID` (shell) либо
	// `out["existingNetworkId"] = net` (python).
	reSeedPublishNet = regexp.MustCompile(`(?m)^\s*(?:\w+\[")?existingNetworkId("\])?\s*=`)
	// Объявление плана: имя поля, а следом список в скобках. Содержимое списка
	// разбирается отдельно — оно бывает и литералом (python), и подстановкой,
	// склеенной shell-кавычками, поэтому одним выражением его не взять.
	reSeedPlanField = regexp.MustCompile(`ipv([46])CidrBlocks[^\[]{0,8}\[([^\]]*)\]`)
	// Присваивание имени: `NET_SUPERNET_V4="10.0.0.0/8"` (shell) и
	// `_NET_PLAN_V4 = "10.196.0.0/16"` (python) — одна форма с точностью до
	// пробелов и кавычек. Разбирать только shell значило бы читать питоновского
	// автора как объявившего ПУСТОЙ план: гейт краснел бы на согласии, а первое
	// же ложное срабатывание сняло бы его вместе с предметом.
	reSeedAssign      = regexp.MustCompile(`(?m)^\s*(\w+)\s*=\s*['"` + "`" + `]([^'"` + "`" + `]*)['"` + "`" + `]`)
	reSeedVarRef      = regexp.MustCompile(`\$\{?(\w+)\}?`)
	reSeedNameRef     = regexp.MustCompile(`[A-Za-z_]\w*`)
	reSeedCidrLiteral = regexp.MustCompile(`\b(?:\d{1,3}(?:\.\d{1,3}){3}|[0-9a-fA-F]{0,4}(?::[0-9a-fA-F]{0,4}){1,7})/\d{1,3}\b`)
)

// TestSeedAddressPlanParityAcrossAuthors — планы всех авторов одной фикстурной
// координаты покрывают друг друга.
func TestSeedAddressPlanParityAcrossAuthors(t *testing.T) {
	root := repoRoot(t)
	scan := scanSeedAddressPlans(t, root)

	if scan.filesRead == 0 {
		t.Fatalf("гейт не прочитал ни одного посевного скрипта в %v — предпосылка "+
			"обхода сломана, молчание ничего не доказывает", seedPlanScanRoots)
	}
	if scan.multiAuthor == 0 {
		t.Fatalf("гейт не нашёл ни одного набора, у координаты existingNetworkId которого "+
			"больше одного автора (авторов всего %d в %d файлах) — сверять было нечего, и "+
			"«ноль расхождений» означало бы «не смотрел». Если координату переименовали "+
			"либо изменилась схема имён посевных файлов, признак публикации и разбор имени "+
			"набора в этом гейте обязаны переехать вместе с ними",
			scan.producers, scan.filesRead)
	}
	t.Logf("осмотрено посевных скриптов: %d; авторов координаты existingNetworkId: %d; "+
		"наборов с несколькими авторами: %d; сверок плана: %d",
		scan.filesRead, scan.producers, scan.multiAuthor, scan.compared)

	if len(scan.hits) > 0 {
		sort.Strings(scan.hits)
		t.Errorf("планы авторов одной фикстурной координаты НЕ покрывают друг друга:\n  %s\n\n"+
			"Следствие: ширина плана задаёт, сколько различных адресов набор вообще может "+
			"нарезать, и из кейса это не видно. Под более узким автором позиций остаются "+
			"единицы сотен, пересечение подсетей отвергается сразу, и прогон краснеет тем "+
			"чаще, чем больше нарезок, — то есть устойчивость набора становится функцией "+
			"того, ЧЕЙ посев отработал. Один поток при этом остаётся зелёным, и расхождение "+
			"выглядит нестабильностью продукта.\n\n"+
			"Исход: свести планы авторов к одному. Подгонять адреса кейсов под самого узкого "+
			"автора нельзя — их энтропия и есть то, что разводит параллельные прогоны.",
			strings.Join(scan.hits, "\n  "))
	}
}

func scanSeedAddressPlans(t *testing.T, root string) seedPlanScan {
	t.Helper()
	out := seedPlanScan{}
	var authors []seedPlanAuthor

	for _, sub := range seedPlanScanRoots {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			t.Fatalf("каталог %s не найден (%v) — область обхода гейта сломана", sub, err)
		}
		err := rootedWalk(base, func(rel string) bool {
			name := filepath.Base(rel)
			return (strings.HasPrefix(name, "seed-") && strings.HasSuffix(name, ".sh")) ||
				(strings.HasPrefix(name, "prodseed_") && strings.HasSuffix(name, ".py"))
		}, func(abs string, body []byte) error {
			out.filesRead++
			rel, relErr := filepath.Rel(root, abs)
			if relErr != nil {
				return relErr
			}
			if a, ok := seedAuthorOf(string(body)); ok {
				a.file = filepath.ToSlash(rel)
				a.svc = seedServiceOf(filepath.Base(rel))
				authors = append(authors, a)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", sub, err)
		}
	}

	out.producers = len(authors)
	// Одинаковое ИМЯ координаты у разных наборов — разные сети в разных файлах
	// окружения. Сверять их между собой значило бы объявить находкой то, что находкой
	// не является, и первым же ложным срабатыванием гейт был бы снят. Поэтому авторы
	// группируются по НАБОРУ, который они сеют, и имя набора берётся из имени файла.
	byService := map[string][]seedPlanAuthor{}
	for _, a := range authors {
		byService[a.svc] = append(byService[a.svc], a)
	}
	svcs := make([]string, 0, len(byService))
	for svc := range byService {
		svcs = append(svcs, svc)
	}
	sort.Strings(svcs)

	for _, svc := range svcs {
		group := byService[svc]
		if len(group) < 2 {
			continue // один автор — сверять не с кем, и это не находка
		}
		out.multiAuthor++
		sort.Slice(group, func(i, j int) bool { return group[i].file < group[j].file })
		ref := group[0]
		for _, a := range group[1:] {
			out.compared++
			for _, fam := range []struct {
				name        string
				left, right []string
			}{
				{"IPv4", ref.v4, a.v4},
				{"IPv6", ref.v6, a.v6},
			} {
				// Сравниваются не строки, а ПОКРЫТИЕ. Автор вправе объявить лишнюю,
				// более узкую сеть для собственных нужд — она укладывается в чужой
				// план и расхождением не является. Расхождение — когда блок одного
				// автора не умещается ни в один блок другого: тогда адрес, законный
				// у первого, у второго отвергается.
				for _, miss := range blocksNotCoveredBy(fam.left, fam.right) {
					out.hits = append(out.hits, "["+svc+"] план "+fam.name+" "+miss+" из "+
						ref.file+" не укладывается ни в один блок "+a.file+" ["+
						strings.Join(fam.right, ", ")+"]")
				}
				for _, miss := range blocksNotCoveredBy(fam.right, fam.left) {
					out.hits = append(out.hits, "["+svc+"] план "+fam.name+" "+miss+" из "+
						a.file+" не укладывается ни в один блок "+ref.file+" ["+
						strings.Join(fam.left, ", ")+"]")
				}
			}
		}
	}
	return out
}

// seedServiceOf — набор, который сеет файл: `seed-<svc>-fixtures.sh` либо
// `prodseed_<svc>_ext.py`. Имя набора и есть область, внутри которой координата
// означает ОДНУ сеть.
func seedServiceOf(name string) string {
	if m := regexp.MustCompile(`^seed-([a-z0-9]+)-`).FindStringSubmatch(name); m != nil {
		return m[1]
	}
	if m := regexp.MustCompile(`^prodseed_([a-z0-9]+)_`).FindStringSubmatch(name); m != nil {
		return m[1]
	}
	return strings.TrimSuffix(strings.TrimSuffix(name, ".sh"), ".py")
}

// blocksNotCoveredBy — блоки left, не лежащие ни внутри одного блока right.
// Пустой right покрытием НЕ считается: автор, не объявивший план семейства вовсе,
// подсеть этого семейства не принимает, и это ровно расхождение, а не «нет
// ограничения».
func blocksNotCoveredBy(left, right []string) []string {
	var out []string
	for _, l := range left {
		lp, err := netip.ParsePrefix(l)
		if err != nil {
			continue
		}
		covered := false
		for _, r := range right {
			rp, rerr := netip.ParsePrefix(r)
			if rerr != nil {
				continue
			}
			if rp.Bits() <= lp.Bits() && rp.Masked().Contains(lp.Masked().Addr()) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, l)
		}
	}
	return out
}

// seedAuthorOf — файл публикует координату и какой план при этом объявляет.
// Значение поля плана берётся литералом либо через одну подстановку имени —
// `$NAME` у shell-автора, голое `NAME` у питоновского.
func seedAuthorOf(body string) (seedPlanAuthor, bool) {
	if !reSeedPublishNet.MatchString(body) {
		return seedPlanAuthor{}, false
	}
	vars := map[string]string{}
	for _, m := range reSeedAssign.FindAllStringSubmatch(body, -1) {
		vars[m[1]] = m[2]
	}
	a := seedPlanAuthor{}
	seen := map[string]bool{}
	for _, m := range reSeedPlanField.FindAllStringSubmatch(body, -1) {
		// Содержимое списка: сперва раскрываем подстановки (shell-автор пишет план
		// переменной), затем снимаем литералы. Нераскрытая подстановка молча
		// исчезла бы, и планы «совпали» бы пустотой — поэтому она подставляется, а
		// не пропускается.
		seg := reSeedVarRef.ReplaceAllStringFunc(m[2], func(ref string) string {
			r := reSeedVarRef.FindStringSubmatch(ref)
			return vars[r[1]]
		})
		// Питоновский автор пишет имя без «$» — раскрываем и голое имя, но
		// ТОЛЬКО известное: неизвестное остаётся как есть и просто не даёт
		// литерала, а не подменяется пустотой.
		seg = reSeedNameRef.ReplaceAllStringFunc(seg, func(ref string) string {
			if v, ok := vars[ref]; ok {
				return v
			}
			return ref
		})
		for _, val := range reSeedCidrLiteral.FindAllString(seg, -1) {
			key := m[1] + val
			if seen[key] {
				continue
			}
			seen[key] = true
			if m[1] == "4" {
				a.v4 = append(a.v4, val)
			} else {
				a.v6 = append(a.v6, val)
			}
		}
	}
	sort.Strings(a.v4)
	sort.Strings(a.v6)
	return a, true
}

// ---- инъекция в обе стороны ----

// TestSeedAddressPlanGateRedOnDivergingAuthors — два автора, разные планы: гейт
// краснеет и называет ОБА файла и оба плана.
func TestSeedAddressPlanGateRedOnDivergingAuthors(t *testing.T) {
	shell, okShell := seedAuthorOf(`
NET_SUPERNET_V4="10.0.0.0/8"
NET_SUPERNET_V6="fd00::/8"
body='{"ipv4CidrBlocks":["'"$NET_SUPERNET_V4"'"],"ipv6CidrBlocks":["'"$NET_SUPERNET_V6"'"]}'
existingNetworkId=$NET_ID
`)
	py, okPy := seedAuthorOf(`
net = create("/vpc/v1/networks",
             {"ipv4CidrBlocks": ["10.196.0.0/16"], "ipv6CidrBlocks": ["fd00:196::/48"]},
             "networkId")
out["existingNetworkId"] = net
`)
	if !okShell || !okPy {
		t.Fatalf("производители не распознаны: shell=%v python=%v — гейт не читает "+
			"публикацию координаты", okShell, okPy)
	}
	if strings.Join(shell.v4, ",") != "10.0.0.0/8" || strings.Join(shell.v6, ",") != "fd00::/8" {
		t.Fatalf("shell-план разобран неверно: v4=%v v6=%v — подстановка переменной "+
			"не раскрыта, и гейт сравнивал бы пустоту", shell.v4, shell.v6)
	}
	if strings.Join(py.v4, ",") == strings.Join(shell.v4, ",") {
		t.Fatalf("расхождение планов v4 не обнаружено: %v против %v", shell.v4, py.v4)
	}
	if strings.Join(py.v6, ",") == strings.Join(shell.v6, ",") {
		t.Fatalf("расхождение планов v6 не обнаружено: %v против %v", shell.v6, py.v6)
	}
}

// TestSeedAddressPlanGateSilentOnAgreeingAuthors — две законные конструкции той же
// формы: авторы, объявившие ОДИН план разным синтаксисом (подстановка против
// литерала), и файл, который координату не публикует вовсе, — он не автор, и его
// собственный план к делу не относится.
func TestSeedAddressPlanGateSilentOnAgreeingAuthors(t *testing.T) {
	shell, _ := seedAuthorOf(`
NET_SUPERNET_V4="10.0.0.0/8"
NET_SUPERNET_V6="fd00::/8"
body='{"ipv4CidrBlocks":["'"$NET_SUPERNET_V4"'"],"ipv6CidrBlocks":["'"$NET_SUPERNET_V6"'"]}'
existingNetworkId=$NET_ID
`)
	py, _ := seedAuthorOf(`
net = create("/vpc/v1/networks",
             {"ipv4CidrBlocks": ["10.0.0.0/8"], "ipv6CidrBlocks": ["fd00::/8"]},
             "networkId")
out["existingNetworkId"] = net
`)
	if strings.Join(shell.v4, ",") != strings.Join(py.v4, ",") ||
		strings.Join(shell.v6, ",") != strings.Join(py.v6, ",") {
		t.Fatalf("гейт нашёл расхождение там, где планы совпадают: shell v4=%v v6=%v; "+
			"python v4=%v v6=%v", shell.v4, shell.v6, py.v4, py.v6)
	}

	// Тот же план, объявленный питоновским автором ЧЕРЕЗ ИМЯ. Форма живая: именно
	// так план и публикуется рядом с сетью, чтобы не разъехаться со своим же
	// объявлением. Пока разбирались только shell-подстановки, этот автор читался
	// как объявивший ПУСТОЙ план — гейт краснел на согласии и был бы снят первым
	// же таким срабатыванием.
	named, okNamed := seedAuthorOf(`
_NET_PLAN_V4 = "10.0.0.0/8"
_NET_PLAN_V6 = "fd00::/8"
net = create("/vpc/v1/networks",
             {"ipv4CidrBlocks": [_NET_PLAN_V4], "ipv6CidrBlocks": [_NET_PLAN_V6]},
             "networkId")
out["existingNetworkId"] = net
out["existingNetworkV4Plan"] = _NET_PLAN_V4
`)
	if !okNamed {
		t.Fatalf("питоновский автор не распознан вовсе")
	}
	if strings.Join(named.v4, ",") != "10.0.0.0/8" || strings.Join(named.v6, ",") != "fd00::/8" {
		t.Fatalf("план, объявленный через имя, разобран как v4=%v v6=%v — подстановка "+
			"голого имени не раскрыта, и гейт сравнивал бы пустоту", named.v4, named.v6)
	}
	if miss := blocksNotCoveredBy(shell.v4, named.v4); len(miss) > 0 {
		t.Fatalf("гейт нашёл расхождение между авторами ОДНОГО плана: %v", miss)
	}
	if _, ok := seedAuthorOf(`
net = create("/vpc/v1/networks", {"ipv4CidrBlocks": ["10.42.0.0/16"]}, "networkId")
out["someOtherNetworkId"] = net
`); ok {
		t.Fatalf("файл, не публикующий координату, засчитан её автором — гейт сверял бы " +
			"планы разных сетей")
	}

	// Различитель НАБОРА: то же имя координаты у другого набора — другая сеть, и
	// сверять их нельзя. Без этого гейт сравнил бы план nlb с планом compute и
	// покраснел бы на паре, которая друг другу ничем не обязана.
	for name, want := range map[string]string{
		"seed-nlb-fixtures.sh":    "nlb",
		"prodseed_nlb_ext.py":     "nlb",
		"prodseed_compute_ext.py": "compute",
		"prodseed_vpc_ext.py":     "vpc",
	} {
		if got := seedServiceOf(name); got != want {
			t.Errorf("набор файла %s разобран как %q, ожидалось %q — авторы разных "+
				"наборов попали бы в одну сверку", name, got, want)
		}
	}
}
