// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ПУБЛИЧНЫЙ ПРОФИЛЬ РАЗВЁРТЫВАНИЯ НЕ НЕСЁТ КООРДИНАТ ПЛОЩАДКИ (kacho#3040).
//
// Репозиторий публичен, и всё, что лежит в отслеживаемом профиле, — опубликовано.
// Профиль управляемого стенда нёс строку подключения внешнего почтового узла с
// учёткой отправителя, адрес отправителя, адрес первого администратора облака на
// домене владельца, публичный адрес балансировщика в комментарии; пароли-заглушки
// баз из слоя разработки доезжали до управляемых стендов наложением слоёв. Ни
// одно из этого не было секретом в смысле «пароль», и поэтому ни один из
// прежних гейтов этого не видел: секрет искали, а координату — нет.
//
// Координата площадки приходит СЛОЕМ ПЛОЩАДКИ вне git
// (`values.<стенд>-secrets.yaml`, под шаблоном игнорирования) либо ссылкой на
// существующий секрет. В профиле остаются заглушки зарезервированных имён
// (RFC 2606, RFC 6761) и адресов документации (RFC 5737); применить такой
// профиль к кластеру, не заменив заглушки, не даёт страж рендера
// (templates/site-layer-guard.yaml, проба — site_layer_guard_render_test.go).
//
// ЧТО СУДИТСЯ — по разобранному YAML, и значения, и комментарии каждого узла:
//
//   - адрес IPv4, маршрутизируемый в интернете (петля, частные сети, link-local,
//     общий диапазон операторов и диапазоны документации — не координаты);
//   - адрес почты, домен которого не зарезервирован, — вне закрытого перечня
//     адресов, публикуемых намеренно;
//   - узел почтовой полосы (`smtp://`, `smtps://`), который не является ни
//     именем внутри кластера, ни выражением шаблона, ни заглушкой; учётка без
//     домена внутри такого адреса — тоже;
//   - пароль литералом (ключи пароля, непустая строка не-выражение) — по
//     СЛОЖЕННОЙ цепочке каждого стенда таблицы, кроме стендов машины
//     разработчика: заглушка разработки законна ровно там, где её не может
//     достать никто, кроме автора, и находка — там, где она доезжает
//     наложением слоёв;
//
// Значение в выводе НЕ печатается: вывод гейта публичен так же, как профиль.
// Печатается файл, строка и класс.
//
// Способность упасть — инъекции в profiles_carry_no_site_coordinates_injection_test.go.
package deploy_test

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// localStacks — стенды машины разработчика (kind), до которых заглушка пароля
// разработки законно доезжает. Перечень закрыт; запись без стенда в таблице —
// находка (исключению нечего исключать).
var localStacks = map[string]string{
	"dev":      "стенд kind на машине разработчика, поднимается `make dev-up`",
	"dev-prod": "стенд kind в боевой посадке, поднимается `make dev-prod-up`",
}

// publishedAddresses — адреса почты на незарезервированном домене, которые
// публикуются НАМЕРЕННО и учёткой входа не являются. Перечень закрыт;
// неиспользованная запись — находка.
var publishedAddresses = map[string]string{
	"security@kacho.cloud": "контакт раскрытия уязвимостей (security.txt, RFC 9116) — публикуется по назначению",
}

// passwordKeys — ключи, литерал под которыми есть пароль. Ключи имён секрета и
// ключей секрета (`passwordSecretKey`, `existingSecret`) — ссылки, не значения.
var passwordKeys = map[string]bool{
	"password":            true,
	"postgrespassword":    true,
	"replicationpassword": true,
	"adminpassword":       true,
	"userpassword":        true,
	"htpasswd":            true,
}

var (
	ipv4Token     = regexp.MustCompile(`(?:^|[^0-9.])((?:[0-9]{1,3}\.){3}[0-9]{1,3})(?:[^0-9.]|$)`)
	mailToken     = regexp.MustCompile(`([A-Za-z0-9._+-]+)(?:@|%40)([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+)`)
	mailNodeToken = regexp.MustCompile("smtps?://[^\\s\"'`]+")
)

var nonCoordinateNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{
		"100.64.0.0/10",   // общий диапазон операторов (RFC 6598)
		"192.0.2.0/24",    // документация TEST-NET-1 (RFC 5737)
		"198.51.100.0/24", // документация TEST-NET-2
		"203.0.113.0/24",  // документация TEST-NET-3
		"255.255.255.255/32",
	} {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}()

// routableIPv4 — адрес, по которому узел достижим из интернета.
func routableIPv4(ip net.IP) bool {
	ip = ip.To4()
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsMulticast() {
		return false
	}
	for _, n := range nonCoordinateNets {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

// reservedDomain — домен, который не разрешается в интернете по стандарту
// (RFC 2606, RFC 6761, RFC 6762): заглушка, а не чья-то координата.
func reservedDomain(d string) bool {
	d = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
	if d == "" {
		return false
	}
	labels := strings.Split(d, ".")
	switch labels[len(labels)-1] {
	case "invalid", "example", "test", "localhost", "local":
		return true
	}
	for _, sld := range []string{"example.com", "example.net", "example.org"} {
		if d == sld || strings.HasSuffix(d, "."+sld) {
			return true
		}
	}
	return false
}

// clusterOrPlaceholderHost — узел внутри кластера, выражение шаблона либо заглушка.
func clusterOrPlaceholderHost(h string) bool {
	h = strings.ToLower(strings.TrimSpace(h))
	if h == "" || strings.Contains(h, "{{") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return !routableIPv4(ip)
	}
	if !strings.Contains(h, ".") {
		return true
	}
	for _, s := range []string{".svc", ".svc.cluster.local", ".cluster.local"} {
		if strings.HasSuffix(h, s) {
			return true
		}
	}
	return reservedDomain(h)
}

type coordFinding struct {
	File  string
	Line  int
	Class string
}

func (f coordFinding) String() string {
	return fmt.Sprintf("%s:%d · %s", f.File, f.Line, f.Class)
}

// coordCensus — объём осмотренного: гейт, не назвавший его, не отличим от
// гейта, не прочитавшего ничего.
type coordCensus struct {
	Files, Scalars, Comments int
	usedAddresses            map[string]bool
}

// judgeCoordText — ЕДИНСТВЕННЫЙ адъюдикатор текста (значения или комментария):
// зовётся и переписью по дереву, и инъекциями.
func judgeCoordText(file string, line int, text string, used map[string]bool) []coordFinding {
	var out []coordFinding
	for _, m := range ipv4Token.FindAllStringSubmatch(text, -1) {
		if ip := net.ParseIP(m[1]); ip != nil && routableIPv4(ip) {
			out = append(out, coordFinding{file, line, "маршрутизируемый адрес IPv4"})
		}
	}
	for _, m := range mailToken.FindAllStringSubmatch(text, -1) {
		addr := strings.ToLower(m[1] + "@" + m[2])
		if reservedDomain(m[2]) {
			continue
		}
		if _, ok := publishedAddresses[addr]; ok {
			if used != nil {
				used[addr] = true
			}
			continue
		}
		out = append(out, coordFinding{file, line, "адрес почты на незарезервированном домене"})
	}
	for _, raw := range mailNodeToken.FindAllString(text, -1) {
		if strings.Contains(raw, "{{") {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil {
			out = append(out, coordFinding{file, line, "адрес почтового узла не разбирается"})
			continue
		}
		if !clusterOrPlaceholderHost(u.Hostname()) {
			out = append(out, coordFinding{file, line, "почтовый узел вне кластера и не заглушка"})
		}
		if u.User != nil {
			if name := u.User.Username(); name != "" && !strings.Contains(name, "@") &&
				!clusterOrPlaceholderHost(u.Hostname()) {
				out = append(out, coordFinding{file, line, "учётка в адресе почтового узла"})
			}
		}
	}
	return out
}

// judgeProfile — обход разобранного профиля: значения и комментарии каждого узла.
func judgeProfile(file string, raw []byte, c *coordCensus) ([]coordFinding, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	var out []coordFinding
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		for _, cm := range []string{n.HeadComment, n.LineComment, n.FootComment} {
			if cm != "" {
				c.Comments++
				out = append(out, judgeCoordText(file, n.Line, cm, c.usedAddresses)...)
			}
		}
		if n.Kind == yaml.ScalarNode {
			c.Scalars++
			out = append(out, judgeCoordText(file, n.Line, n.Value, c.usedAddresses)...)
		}
		for _, ch := range n.Content {
			walk(ch)
		}
	}
	walk(&root)
	c.Files++
	return out, nil
}

// passwordLiterals — пароли литералом в сложенной цепочке: путь до каждого.
func passwordLiterals(tree map[string]any) []string {
	var out []string
	var walk func(v any, path []string)
	walk = func(v any, path []string) {
		switch x := v.(type) {
		case map[string]any:
			for k, ch := range x {
				walk(ch, append(append([]string(nil), path...), k))
			}
		case []any:
			for i, ch := range x {
				walk(ch, append(append([]string(nil), path...), fmt.Sprint(i)))
			}
		case string:
			if len(path) == 0 || !passwordKeys[strings.ToLower(path[len(path)-1])] {
				return
			}
			if s := strings.TrimSpace(x); s != "" && !strings.Contains(s, "{{") {
				out = append(out, strings.Join(path, "."))
			}
		}
	}
	walk(tree, nil)
	sort.Strings(out)
	return out
}

// judgeStackPasswords — пароли литералом, доезжающие до стенда вне машины
// разработчика. Вход — уже сложенные цепочки; зовётся переписью и инъекцией.
func judgeStackPasswords(folded map[string]map[string]any) []string {
	var out []string
	names := make([]string, 0, len(folded))
	for n := range folded {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, local := localStacks[n]; local {
			continue
		}
		for _, p := range passwordLiterals(folded[n]) {
			out = append(out, fmt.Sprintf("стек %s: пароль литералом по пути %s", n, p))
		}
	}
	return out
}

// trackedProfiles — отслеживаемые профили дерева. Именно отслеживаемые: слой
// площадки вне git на машине оператора законно несёт координаты, и обход диска
// краснел бы там — на правильном дереве.
func trackedProfiles(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", "..", "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files не исполнился (%v) — перечня профилей взять неоткуда; "+
			"это отказ, а не чистое дерево", err)
	}
	profile := regexp.MustCompile(`(^|/)values[^/]*\.ya?ml$`)
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" && profile.MatchString(f) {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return files
}

func TestPublicProfilesCarryNoSiteCoordinates(t *testing.T) {
	files := trackedProfiles(t)
	if len(files) == 0 {
		t.Fatalf("отслеживаемых профилей 0 — предикат перестал их узнавать, а не дерево стало чистым")
	}
	census := &coordCensus{usedAddresses: map[string]bool{}}
	var findings []coordFinding
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join("..", f))
		if err != nil {
			t.Fatalf("профиль %s не читается: %v", f, err)
		}
		got, err := judgeProfile(f, raw, census)
		if err != nil {
			t.Fatalf("профиль %s не разбирается как YAML: %v", f, err)
		}
		findings = append(findings, got...)
	}
	t.Logf("перепись: профилей %d · скаляров %d · комментариев %d · находок %d",
		census.Files, census.Scalars, census.Comments, len(findings))
	if census.Scalars == 0 {
		t.Fatalf("скаляров прочитано 0 — обход пуст")
	}
	for _, f := range findings {
		t.Errorf("координата площадки в публичном профиле: %s (значение не печатается: вывод публичен)", f)
	}
	for addr, why := range publishedAddresses {
		if !census.usedAddresses[addr] {
			t.Errorf("запись перечня публикуемых адресов не использована ни одним профилем (%s) — "+
				"исключению нечего исключать, сними запись", why)
		}
	}
}

func TestStacksOutsideTheDeveloperMachineCarryNoPasswordLiteral(t *testing.T) {
	stacks := deployStacks(t)
	folded := map[string]map[string]any{}
	for n, chain := range stacks {
		var merged map[string]any
		for _, p := range chain {
			merged = mergeValues(merged, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		folded[n] = merged
	}
	for n, why := range localStacks {
		if _, ok := stacks[n]; !ok {
			t.Errorf("стек машины разработчика %q (%s) в таблице не найден — исключению нечего исключать", n, why)
		}
	}
	findings := judgeStackPasswords(folded)
	t.Logf("перепись: стеков %d · машины разработчика %d · находок %d",
		len(folded), len(localStacks), len(findings))
	for _, f := range findings {
		t.Errorf("%s — заглушка разработки доезжает до стенда вне машины разработчика; "+
			"стенд объявляет ссылку на секрет либо пустую величину (пароль порождает чарт)", f)
	}
}
