// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// console_subscription_owner_test.go — КОНСОЛЬ называет владельца журнала тем же
// именем, которое край принимает.
//
// # Предмет — та же омонимия, но в ТРЕТЬЕМ месте
//
// Сосед по каталогу (subscription_owner_naming_test.go) уже держит одну пару:
// объявление чарта против карты соединений края. Консоль — третья сторона того
// же предмета, и гейт её не осматривал вовсе. Она и разошлась: карта предметов
// потока клала в запрос `owner: "nlb"` — каталог сервиса в дереве, — тогда как
// край принимает имя домена контракта `loadbalancer`.
//
// Наблюдалось (kacho#1440, сквозная проба консоли): страница списка
// балансировщиков давала ДВА отказа `400` подряд — на открытии потока и на
// запросе разбора причины, — и оба оставались в журнале браузера. Список при
// этом работал: опрос возвращается по построению, поэтому дефект не виден ни
// одному утверждению о содержимом страницы. Своя документация края
// (`gateway/docs/content/api/subscription.mdx`) называла верное имя всё это
// время; расходились не факты, а места.
//
// # Почему сверка идёт с КОДОМ, а не с текстом объявления
//
// Множество принимаемых имён приезжает вызовом
// `config.Config.DomainsWithInternalBackend` — той самой функцией, которой
// пользуется край на старте. Вторая копия перечня разошлась бы с ним молча:
// обе непусты, обе выглядят действующими.
//
// # Что судится на стороне консоли и почему именно это
//
// Судится то, что УХОДИТ В ЗАПРОС: значение поля `owner` предмета потока — хаб
// подставляет его в строку запроса как есть. Рядом судится объявленный тип
// `JournalOwner`: имя, объявленное владельцем и краем не принимаемое, есть та же
// ложь, только на шаг раньше.
//
// Комментарии снимаются ДО разбора: прозы про `nlb` в том файле много (она
// объясняет ровно эту омонимию), и сверка по подстроке краснела бы на
// собственном объяснении.

// consoleSubjectsRel — карта предметов потока консоли относительно корня дерева.
const consoleSubjectsRel = "ui-future/shared/src/lib/subscription/subjects.ts"

// Формы, в которых имя владельца записано в этом файле законно. Их две, и обе
// осматриваются: значение поля предмета (уходит в запрос) и член объявленного
// типа (обещание о множестве имён).
var (
	consoleOwnerField = regexp.MustCompile(`owner\s*:\s*"([^"]*)"`)
	consoleOwnerUnion = regexp.MustCompile(`type\s+JournalOwner\s*=\s*([^;]*);`)
	consoleOwnerQuote = regexp.MustCompile(`"([^"]*)"`)
)

// consoleOwnerCensus — что гейт прочитал на стороне консоли.
type consoleOwnerCensus struct {
	// occurrences — сколько раз имя названо ПРЕДМЕТОМ потока. Ноль означает
	// «ничего не прочитано», а не «нарушений нет».
	occurrences int
	names       []string
	union       []string
}

// readConsoleOwners читает имена владельцев из исходника карты предметов.
//
// Отдельной функцией — ради ОДНОГО: доказательство способности гейта упасть
// обязано прогонять ТУ ЖЕ функцию суждения, а не её копию. Исходник подаётся
// входом, поэтому инъекция не трогает дерева.
func readConsoleOwners(src string) consoleOwnerCensus {
	code := stripTSComments(src)

	seen := map[string]bool{}
	census := consoleOwnerCensus{}
	for _, m := range consoleOwnerField.FindAllStringSubmatch(code, -1) {
		census.occurrences++
		if !seen[m[1]] {
			seen[m[1]] = true
			census.names = append(census.names, m[1])
		}
	}
	sort.Strings(census.names)

	if m := consoleOwnerUnion.FindStringSubmatch(code); m != nil {
		for _, q := range consoleOwnerQuote.FindAllStringSubmatch(m[1], -1) {
			census.union = append(census.union, q[1])
		}
		sort.Strings(census.union)
	}
	return census
}

// stripTSComments снимает комментарии, не трогая содержимого строк.
//
// Строки уважаются намеренно: путь вида `"https://…"` иначе обрезался бы по
// `//`, и гейт судил бы обрубок.
func stripTSComments(src string) string {
	var out strings.Builder
	out.Grow(len(src))
	for i := 0; i < len(src); {
		switch {
		case src[i] == '"' || src[i] == '\'' || src[i] == '`':
			quote := src[i]
			out.WriteByte(src[i])
			i++
			for i < len(src) {
				if src[i] == '\\' && i+1 < len(src) {
					out.WriteString(src[i : i+2])
					i += 2
					continue
				}
				out.WriteByte(src[i])
				if src[i] == quote {
					i++
					break
				}
				i++
			}
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				i = len(src)
				continue
			}
			i += 2 + end + 2
		default:
			out.WriteByte(src[i])
			i++
		}
	}
	return out.String()
}

// judgeConsoleOwners отдаёт имена, которые консоль называет, а край не принимает.
func judgeConsoleOwners(accepted []string, census consoleOwnerCensus) []string {
	ok := make(map[string]bool, len(accepted))
	for _, name := range accepted {
		ok[name] = true
	}
	bad := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, name := range append(append([]string{}, census.names...), census.union...) {
		if ok[name] || seen[name] {
			continue
		}
		seen[name] = true
		bad = append(bad, name)
	}
	sort.Strings(bad)
	return bad
}

// TestConsoleNamesTheOwnerTheEdgeAccepts — имя владельца в консоли есть имя,
// которое ручка потока принимает.
func TestConsoleNamesTheOwnerTheEdgeAccepts(t *testing.T) {
	path := filepath.Join("..", "..", filepath.FromSlash(consoleSubjectsRel))
	raw, err := os.ReadFile(path) // #nosec G304 -- путь дерева, не вход пользователя
	if err != nil {
		t.Fatalf("карта предметов потока консоли не прочитана (%s): %v — гейт не осмотрел "+
			"ничего и не вправе молчать", consoleSubjectsRel, err)
	}

	census := readConsoleOwners(string(raw))
	accepted := config.Config{}.DomainsWithInternalBackend()

	t.Logf("перепись: прочитано %d байт %s · имя владельца названо предметом %d× · "+
		"различных имён %d %v · членов объявленного типа %d %v · край принимает %d %v",
		len(raw), consoleSubjectsRel, census.occurrences, len(census.names), census.names,
		len(census.union), census.union, len(accepted), accepted)

	if census.occurrences == 0 {
		t.Fatalf("в %s не найдено ни одного имени владельца в форме `owner: \"…\"` — "+
			"либо форма записи сменилась, и распознаватель её не знает, либо файл переехал; "+
			"в обоих случаях «нарушений нет» здесь означало бы «ничего не прочитано»",
			consoleSubjectsRel)
	}
	if len(census.union) == 0 {
		t.Fatalf("в %s не найдено объявление типа JournalOwner — распознаватель не знает "+
			"формы, в которой консоль объявляет множество имён", consoleSubjectsRel)
	}
	if len(accepted) == 0 {
		t.Fatalf("край не принимает НИ ОДНОГО имени владельца — гейт ничего не сверял; " +
			"карта соединений либо пуста, либо перестала нести внутренние адреса")
	}

	for _, name := range judgeConsoleOwners(accepted, census) {
		t.Errorf("консоль называет владельцем %q, а ручка потока принимает только %v: "+
			"каждое открытие страницы такого ресурса даёт `400 unknown owner` — и на самом "+
			"потоке, и на запросе разбора причины. Имя владельца есть домен КОНТРАКТА "+
			"(`kacho.cloud.<домен>.v1`), а не каталог сервиса в дереве и не сегмент "+
			"REST-пути. Правится в %s", name, accepted, consoleSubjectsRel)
	}
}

// TestConsoleOwnerGateFallsOnASwappedNameAndIsSilentOnTheLegalTwin — способность
// гейта упасть И смолчать, доказанная инъекцией.
//
// Прогонов три, и третий несущий: без него молчание на законном близнеце было бы
// неотличимо от молчания мёртвого распознавателя.
func TestConsoleOwnerGateFallsOnASwappedNameAndIsSilentOnTheLegalTwin(t *testing.T) {
	accepted := []string{"compute", "loadbalancer", "vpc"}

	swapped := `
// Ключ владельца ` + "`nlb`" + `, путь ` + "`/loadbalancer/v1/`" + ` — проза, судить её нельзя.
export type JournalOwner = "compute" | "nlb" | "vpc";
export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  "load-balancers": { owner: "nlb", kind: "nlb_network_load_balancer" },
};`

	legal := strings.NewReplacer(`"nlb"`, `"loadbalancer"`).Replace(swapped)

	swappedCensus := readConsoleOwners(swapped)
	if got := judgeConsoleOwners(accepted, swappedCensus); len(got) != 1 || got[0] != "nlb" {
		t.Errorf("подменённое имя не найдено: находки %v, ожидалась ровно одна — \"nlb\"", got)
	}

	legalCensus := readConsoleOwners(legal)
	if got := judgeConsoleOwners(accepted, legalCensus); len(got) != 0 {
		t.Errorf("законный близнец объявлен нарушением: %v — гейт ловит форму, а не существо", got)
	}

	// Проза про `nlb` осталась в ОБОИХ входах, и в законном она находкой не стала:
	// комментарий снимается до разбора. Без этого утверждения гейт краснел бы на
	// собственном объяснении, которого в судимом файле много.
	if !strings.Contains(legal, "`nlb`") {
		t.Fatalf("законный близнец потерял прозу про nlb — контроль на комментарий стал вакуумным")
	}

	// Пустой вход обязан читаться как «ничего не прочитано», а не как «чисто».
	if empty := readConsoleOwners("// только комментарий\n"); empty.occurrences != 0 || len(empty.union) != 0 {
		t.Errorf("на входе без объявлений распознаватель насчитал %d предметов и %d членов типа",
			empty.occurrences, len(empty.union))
	}
}
