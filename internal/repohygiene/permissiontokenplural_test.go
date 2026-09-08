// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// permissiontokenplural_test.go — гейт: в токене права
// `<домен>.<ресурс>.<глагол>` имя ресурса стоит во МНОЖЕСТВЕННОМ числе РОВНО
// ОДИН РАЗ.
//
// # Предмет
//
// Токен права — часть контракта: он показывается арендатору страницей ролей,
// перечисляется в правах роли и живёт в выданных грантах. Искажённое имя
// (`vpc.gatewaies.get`, `vpc.subnetses.list`) читается как обычное: та же форма
// в аннотации, та же форма в каталоге, та же форма при чтении роли через API.
// Ничто в конвейере на него не жалуется — генератор каталога токен НЕ
// синтезирует, он ЧИТАЕТ его из аннотации, — поэтому опечатка в имени
// доезжает до контракта молча и переживает любое ревью диффа.
//
// # Почему НАД АННОТАЦИЯМИ, а не над каталогом
//
// `permission_catalog.json` — СЛЕД, а не источник: он производится из этих
// аннотаций. Гейт над JSON краснел бы на том же дефекте, но чинить его можно
// было бы правкой JSON — то есть правкой следа при живом источнике, после чего
// первая же регенерация вернула бы искажение. Поэтому предмет проверки —
// `proto/kacho/cloud/<домен>/v1/*.proto`.
//
// # Предикат и почему он не суффиксный
//
// Наивное «оканчивается на -ses» НЕ РАБОТАЕТ, и это проверено, а не
// предположено: `addresses` — законная форма множественного числа от `address`
// и тоже оканчивается на `ses`. Первая редакция этого гейта была написана
// именно так и дала 13 ложных находок на одном законном имени. Поэтому
// предикат не смотрит на суффикс, а спрашивает, сколько раз имя было
// просклонено:
//
//	seg считается ИСКАЖЁННЫМ, если
//	  (а) seg выглядит просклонённым, но склонение неверно
//	      (`gatewaies`: от `gateway` получается `gateways`) — MALFORMED; либо
//	  (б) снятие склонения даёт форму, которая САМА является правильным
//	      множественным числом (`subnetses` → `subnets` = мн.ч. от `subnet`) —
//	      DOUBLE.
//
// Имя, которое склонённым не выглядит вовсе (`authorize`,
// `resourceLifecycle`, `access_bindings_by_account`), гейт НЕ трогает: это
// осознанно не-ресурсные токены, и требовать от них множественного числа
// значило бы ловить форму вместо существа. Оба направления закреплены таблицей
// в `TestPermissionTokenPluralPredicateControls` — включая `addresses` как
// положительный контроль на тот самый ложный срабат.
//
// # Чем доказана способность упасть
//
// `TestVpcPermissionTokenPluralInjection` подсаживает искажённый токен в КОПИЮ
// настоящего файла дерева и требует находку с координатой; рядом — законный
// близнец той же формы, на котором гейт обязан молчать. Без второй половины
// гейт ловил бы форму аннотации, а не искажение имени.
//
// # Объём осмотренного печатается
//
// «Ноль находок» обязано быть отличимо от «ноль прочитанного», поэтому гейт
// печатает число файлов, записей и различных токенов и ОТКАЗЫВАЕТ, если не
// прочитал ни одной записи.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/contractroot"
)

// permTokAnnotation — строка аннотации права в .proto. Аннотация односложна
// (одна строка на запись), поэтому разбор идёт по строкам и несёт номер строки:
// находка без координаты заставляет искать её глазами по всему домену.
var permTokAnnotation = regexp.MustCompile(`\(kacho\.iam\.authz\.v1\.permission\)\s*=\s*"([^"]*)"`)

// permTokVerdict — классификация имени ресурса в токене.
type permTokVerdict string

const (
	permTokOK             permTokVerdict = "ok"                 // просклонено ровно один раз
	permTokNotPluralized  permTokVerdict = "не-склонённое"      // склонённым не выглядит — не предмет гейта
	permTokMalformedPlrl  permTokVerdict = "неверное-склонение" // выглядит склонённым, склонение неверно
	permTokDoublePluralzd permTokVerdict = "двойное-множественное"
)

// permTokRecord — одна запись каталога прав, прочитанная из аннотации.
type permTokRecord struct {
	File     string // путь относительно корня репозитория
	Line     int
	Token    string
	Domain   string
	Resource string
	Verb     string
}

// permTokPluralize — форма множественного числа по правилу английского языка,
// достаточному для словаря токенов (все имена — латиница в snake/camel).
func permTokPluralize(w string) string {
	switch {
	case strings.HasSuffix(w, "s"), strings.HasSuffix(w, "x"),
		strings.HasSuffix(w, "z"), strings.HasSuffix(w, "ch"),
		strings.HasSuffix(w, "sh"):
		return w + "es"
	case len(w) >= 2 && strings.HasSuffix(w, "y") && !permTokIsVowel(w[len(w)-2]):
		return w[:len(w)-1] + "ies"
	default:
		return w + "s"
	}
}

// permTokDepluralize — снятие ОДНОГО склонения. Возвращает слово без
// изменений, если оно склонённым не выглядит.
func permTokDepluralize(w string) string {
	if strings.HasSuffix(w, "ies") && len(w) > 4 {
		return w[:len(w)-3] + "y"
	}
	for _, s := range []string{"ses", "xes", "zes", "ches", "shes"} {
		if strings.HasSuffix(w, s) {
			return w[:len(w)-2]
		}
	}
	if strings.HasSuffix(w, "s") {
		return w[:len(w)-1]
	}
	return w
}

func permTokIsVowel(b byte) bool { return strings.IndexByte("aeiouAEIOU", b) >= 0 }

// permTokLooksPluralized — слово несёт признак склонения.
func permTokLooksPluralized(w string) bool { return permTokDepluralize(w) != w }

// permTokIsWellFormedPlural — слово является ПРАВИЛЬНЫМ множественным числом
// своей единственной формы.
func permTokIsWellFormedPlural(w string) bool {
	d := permTokDepluralize(w)
	return d != w && permTokPluralize(d) == w
}

// permTokClassify — вердикт по имени ресурса.
func permTokClassify(seg string) permTokVerdict {
	if !permTokLooksPluralized(seg) {
		return permTokNotPluralized
	}
	if !permTokIsWellFormedPlural(seg) {
		return permTokMalformedPlrl
	}
	if permTokIsWellFormedPlural(permTokDepluralize(seg)) {
		return permTokDoublePluralzd
	}
	return permTokOK
}

// permTokDistorted — вердикт означает искажение имени (предмет гейта).
func permTokDistorted(v permTokVerdict) bool {
	return v == permTokMalformedPlrl || v == permTokDoublePluralzd
}

// permTokSuggest — имя, к которому искажение приводится: множественное число
// ровно один раз.
func permTokSuggest(seg string) string {
	switch permTokClassify(seg) {
	case permTokMalformedPlrl:
		return permTokPluralize(permTokDepluralize(seg))
	case permTokDoublePluralzd:
		return permTokDepluralize(seg)
	default:
		return seg
	}
}

// permTokScan — разбор аннотаций одного файла. Чистая функция над (имя,
// содержимое): та же функция читает и настоящее дерево, и подсаженную копию,
// поэтому проба инъекции проверяет ТОТ ЖЕ разбор, который исполняется на
// вердикте, а не свою копию.
func permTokScan(rel string, body []byte) []permTokRecord {
	var out []permTokRecord
	for i, line := range strings.Split(string(body), "\n") {
		m := permTokAnnotation.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		tok := m[1]
		rec := permTokRecord{File: rel, Line: i + 1, Token: tok}
		if parts := strings.Split(tok, "."); len(parts) == 3 {
			rec.Domain, rec.Resource, rec.Verb = parts[0], parts[1], parts[2]
		}
		out = append(out, rec)
	}
	return out
}

// permTokReadDomain — записи одного домена + число прочитанных файлов.
func permTokReadDomain(t *testing.T, root, domain string) (files int, recs []permTokRecord) {
	t.Helper()
	// Корень домена РЕЗОЛВИТСЯ обходом объявленных корней: литерал не нашёл бы
	// домена, переехавшего под второй корень, и гейт объявил бы находкой
	// собственную слепоту — «каталог proto не читается».
	_, domainDir, ok := contractroot.ResolveDomain(filepath.Join(root, "proto"), domain)
	if !ok {
		t.Fatalf("домен %s не резолвится ни под одним объявленным корнем %v", domain, contractroot.Roots)
	}
	dir := filepath.Join(domainDir, "v1")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("домен %s: каталог proto не читается: %v — «ноль находок» тут был бы "+
			"«ноль прочитанного»", domain, err)
	}
	err := rootedWalk(dir,
		func(rel string) bool { return strings.HasSuffix(rel, ".proto") },
		func(abs string, body []byte) error {
			files++
			rel, relErr := filepath.Rel(root, abs)
			if relErr != nil {
				rel = abs
			}
			recs = append(recs, permTokScan(filepath.ToSlash(rel), body)...)
			return nil
		})
	if err != nil {
		t.Fatalf("обход proto домена %s: %v", domain, err)
	}
	return files, recs
}

// permTokUnique — число различных токенов.
func permTokUnique(recs []permTokRecord) int {
	set := map[string]struct{}{}
	for _, r := range recs {
		set[r.Token] = struct{}{}
	}
	return len(set)
}

// permTokFindings — искажённые записи среди прочитанных. `<exempt>` и любой
// токен не из трёх сегментов предметом не являются: у них нет имени ресурса,
// про которое можно спрашивать число.
func permTokFindings(recs []permTokRecord) []permTokRecord {
	var out []permTokRecord
	for _, r := range recs {
		if r.Resource == "" {
			continue
		}
		if permTokDistorted(permTokClassify(r.Resource)) {
			out = append(out, r)
		}
	}
	return out
}

// TestVpcPermissionTokenPluralizedExactlyOnce — гейт домена vpc: ни одного
// искажённого имени ресурса в аннотациях права.
func TestVpcPermissionTokenPluralizedExactlyOnce(t *testing.T) {
	root := repoRoot(t)
	files, recs := permTokReadDomain(t, root, "vpc")

	t.Logf("осмотрено: файлов %d, записей каталога прав %d, различных токенов %d",
		files, len(recs), permTokUnique(recs))
	if len(recs) == 0 {
		t.Fatal("прочитано НОЛЬ записей — вердикт был бы свойством пустого набора, " +
			"а не дерева")
	}

	findings := permTokFindings(recs)
	if len(findings) == 0 {
		return
	}

	segs := map[string]struct{}{}
	for _, f := range findings {
		segs[f.Resource] = struct{}{}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	var b strings.Builder
	fmt.Fprintf(&b, "искажённых записей %d, различных искажённых имён %d — "+
		"имя ресурса в токене права обязано стоять во множественном числе РОВНО ОДИН РАЗ.\n"+
		"Токен — часть контракта: он виден арендатору и живёт в выданных правах, "+
		"поэтому опечатка в нём не косметика.\n", len(findings), len(segs))
	for _, f := range findings {
		fmt.Fprintf(&b, "  %s:%d  %s  (%s: %s → %s)\n",
			f.File, f.Line, f.Token, permTokClassify(f.Resource),
			f.Resource, permTokSuggest(f.Resource))
	}
	b.WriteString("Правится в АННОТАЦИИ .proto (источник), затем регенерацией каталога " +
		"`make -C gateway permission-catalog-apply` + синхронизацией копии iam. " +
		"Правка permission_catalog.json руками исходом не является: это след, " +
		"и первая регенерация вернёт искажение.")
	t.Error(b.String())
}

// TestVpcPermissionTokenPluralInjection — доказательство способности упасть, в
// ОБЕ стороны, на настоящем файле дерева.
//
// Проба судит РОВНО ОДНУ строку — ту, в которую подсажен токен, — и потому не
// зависит от состояния остального файла. Первая редакция брала находки по всему
// файлу, и «законный близнец» краснел на ПОСТОРОННИХ искажениях того же файла:
// проба утверждала бы о дереве, а не о подстановке, и позеленела бы сама от
// починки источника. Вторая половина того же промаха: якорем была взята первая
// аннотация файла, которая на момент написания УЖЕ несла `vpc.gatewaies.get`,
// поэтому подстановка была тождественной и не проверяла ничего.
func TestVpcPermissionTokenPluralInjection(t *testing.T) {
	root := repoRoot(t)
	const rel = "proto/kacho/cloud/vpc/v1/gateway_service.proto"
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("чтение %s: %v", rel, err)
	}
	lines := strings.Split(string(body), "\n")

	// Производитель входа — настоящая аннотация этого файла, а не синтетическая
	// строка: подставляется только ЗНАЧЕНИЕ токена, форма строки остаётся той,
	// которую разбирает гейт на вердикте.
	idx := -1
	for i, l := range lines {
		if permTokAnnotation.MatchString(l) {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("%s: аннотаций права не найдено — предпосылка пробы не выполняется, "+
			"инъекции не во что подсаживать", rel)
	}

	// inject возвращает запись, прочитанную гейтом ИЗ ПОДСТАВЛЕННОЙ строки.
	inject := func(t *testing.T, token string) permTokRecord {
		t.Helper()
		m := permTokAnnotation.FindStringSubmatch(lines[idx])
		mutated := append([]string(nil), lines...)
		mutated[idx] = strings.Replace(lines[idx], m[0],
			strings.Replace(m[0], m[1], token, 1), 1)
		if !strings.Contains(mutated[idx], `"`+token+`"`) {
			t.Fatalf("подстановка не дала ожидаемого входа (%q) — проба ничего "+
				"не проверяет", token)
		}
		for _, r := range permTokScan(rel, []byte(strings.Join(mutated, "\n"))) {
			if r.Line == idx+1 {
				return r
			}
		}
		t.Fatalf("гейт не прочитал подставленную строку %d — разбор не видит своего входа",
			idx+1)
		return permTokRecord{}
	}

	t.Run("подсаженное искажённое имя — гейт краснеет и называет координату", func(t *testing.T) {
		hit := inject(t, "vpc.gatewaies.get")
		if !permTokDistorted(permTokClassify(hit.Resource)) {
			t.Fatalf("подсаженный `vpc.gatewaies.get` признан законным (%s) — "+
				"гейт не способен упасть на своём предмете",
				permTokClassify(hit.Resource))
		}
		if hit.File != rel || hit.Line != idx+1 {
			t.Errorf("находка без верной координаты: %+v, ожидалось %s:%d",
				hit, rel, idx+1)
		}
		if got := permTokSuggest(hit.Resource); got != "gateways" {
			t.Errorf("исправление названо неверно: %s → %s, ожидалось gateways",
				hit.Resource, got)
		}
	})

	t.Run("законный близнец той же формы — гейт молчит", func(t *testing.T) {
		hit := inject(t, "vpc.gateways.get")
		if permTokDistorted(permTokClassify(hit.Resource)) {
			t.Errorf("`vpc.gateways.get` — законное имя, но гейт дал вердикт %s: "+
				"он ловит форму аннотации, а не искажение имени",
				permTokClassify(hit.Resource))
		}
	})
}

// TestPermissionTokenPluralPredicateControls — предикат закреплён в обе
// стороны. `addresses` стоит здесь не для полноты: суффиксная редакция этого
// гейта дала на нём 13 ложных находок.
func TestPermissionTokenPluralPredicateControls(t *testing.T) {
	for _, c := range []struct {
		seg  string
		want permTokVerdict
	}{
		// законные имена — гейт обязан молчать
		{"addresses", permTokOK},
		{"used_addresses", permTokOK},
		{"addresses_by_subnets", permTokOK},
		{"address_pools", permTokOK},
		{"gateways", permTokOK},
		{"subnets", permTokOK},
		{"networks", permTokOK},
		{"network_interfaces", permTokOK},
		{"security_group_rules", permTokOK},
		{"machineTypes", permTokOK},
		{"zones", permTokOK},
		// не-ресурсные токены — не предмет гейта
		{"authorize", permTokNotPluralized},
		{"resourceLifecycle", permTokNotPluralized},
		{"access_bindings_by_account", permTokNotPluralized},
		// искажения
		{"gatewaies", permTokMalformedPlrl},
		{"gatewayses", permTokDoublePluralzd},
		{"subnetses", permTokDoublePluralzd},
		{"addresseses", permTokDoublePluralzd},
		{"used_addresseses", permTokDoublePluralzd},
		{"network_cidr_blockses", permTokDoublePluralzd},
		{"security_group_ruleses", permTokDoublePluralzd},
		{"route_tableses", permTokDoublePluralzd},
	} {
		if got := permTokClassify(c.seg); got != c.want {
			t.Errorf("%s: вердикт %s, ожидался %s", c.seg, got, c.want)
		}
	}
}

// permTokRemainderOutsideVpc — ИЗМЕРЕННЫЙ остаток того же класса в доменах, до
// которых это изменение не доходит. Записан числами, а не словом «есть», чтобы
// он не стал невидимым: класс закрыт в vpc, а не в дереве.
//
// Запись САМОИСТЕКАЕТ. Расхождение с деревом — находка в ОБЕ стороны:
//   - счёт ВЫРОС ⇒ заведено новое искажение, править аннотацию;
//   - счёт УПАЛ ⇒ домен вычищен, привести это число тем же изменением
//     (иначе останется утверждение, пережившее свой предмет).
//
// Перемерено при слиянии со стволом: у compute было 5, стало 2 — три искажённых
// имени ушли вместе с дублями блочного хранения, снятыми производственной формой
// compute. Это ровно тот исход, который запись и предусматривает: счёт упал, домен
// вычищен чужим изменением, число приведено тем же, которым обнаружено. Осталось
// два (`instanceses`, `instance_operationses`); у iam — 13, не менялось.
var permTokRemainderOutsideVpc = map[string]int{
	"compute": 2,
	"iam":     13,
}

// TestPermissionTokenDistortionRemainderOutsideVpc — бухгалтерия остатка.
// Отдельным тестом от гейта vpc: устаревшая запись остатка не должна прятать
// свойство, которое держит гейт домена.
func TestPermissionTokenDistortionRemainderOutsideVpc(t *testing.T) {
	root := repoRoot(t)
	for _, domain := range []string{"compute", "iam"} {
		files, recs := permTokReadDomain(t, root, domain)
		got := len(permTokFindings(recs))
		t.Logf("%s: файлов %d, записей %d, искажённых %d",
			domain, files, len(recs), got)
		if want := permTokRemainderOutsideVpc[domain]; got != want {
			t.Errorf("%s: искажённых записей %d, объявлено %d. "+
				"Больше ⇒ заведено новое искажение (править аннотацию .proto); "+
				"меньше ⇒ домен вычищен, поправь это число в "+
				"permTokRemainderOutsideVpc тем же изменением.", domain, got, want)
		}
	}
}
