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
// «Принимает» здесь — РУЧКА ПОТОКА на запросе, а не страж старта, и различие это
// стоило дефекта (kacho#1633): страж пускает всякое имя с внутренним адресом,
// ручка — только объявленное профилем. Судится второе, оно же уже.
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
// # Сверка идёт с ОБЪЯВЛЕННЫМ перечнем, а не с множеством принимаемых имён
//
// Различие несущее, и здесь стояло второе (kacho#1633). Имён, которые край
// УМЕЕТ резолвить, больше, чем тех, которых посадка ОБЪЯВИЛА владельцами
// журнала: словарь владельцев край строит из `subscriptionStream.owners`, а не
// из карты соединений, и имя вне словаря отвергается запросом.
//
// То есть между двумя множествами есть промежуток — «край принимает, профиль не
// объявил», — и имя из него давало ровно тот отказ, который эта шапка называет
// предметом, при зелёном гейте. Замер на дереве: консоль называет владельцем
// `registry`, перечень его не называет — все четыре гейта этой тройки молчали,
// а каждое открытие списка реестров отвечало бы `400`.
//
// Объявленный перечень при этом СТРОЖЕ принимаемого множества (его подмножество
// держит сосед `subscription_owners_declared_test.go`), поэтому прежний предмет
// — написание вне множества принимаемых, `nlb` вместо `loadbalancer` — судится
// по-прежнему и здесь же.
//
// Обе стороны читаются ОДНОЙ функцией на пакет: объявление — производственным
// разбором `config.Config.SubscriptionOwnerNames` через
// `declaredSubscriptionOwners`. Вторая копия перечня разошлась бы с краем молча:
// обе непусты, обе выглядят действующими.
//
// # Почему ПУСТОЙ объявленный перечень ПРОХОДИТ
//
// Пусто — «владелец не объявлен», законное состояние посадки: ручка отвечает
// `501` с названной причиной, и отвечает так КАЖДОМУ имени. Написание владельца
// в консоли при выключенной возможности предметом не является, и осуждать за
// него нечего. Число объявленных печатается всегда, поэтому ноль виден и отличим
// от «не разобрали».
//
// Это осознанно ОБРАТНОЕ решение к классу «пусто = не сужаем»: там пустой круг
// означал «доверяем всем» и потому требовал отказа, здесь пустой перечень
// означает «никого» и безопасен по построению.
//
// # Что судится на стороне консоли и почему именно это
//
// Судится то, что УХОДИТ В ЗАПРОС: значение поля `owner` предмета потока — хаб
// подставляет его в строку запроса как есть. Рядом судится объявленный тип
// `JournalOwner`: имя, объявленное владельцем и профилем не объявленное, есть та
// же ложь, только на шаг раньше.
//
// Комментарии снимаются ДО разбора: прозы про `nlb` в том файле много (она
// объясняет ровно эту омонимию), и сверка по подстроке краснела бы на
// собственном объяснении.

// consoleSubjectsRel — карта предметов потока консоли относительно корня дерева.
const consoleSubjectsRel = "ui-future/shared/src/lib/subscription/subjects.ts"

// edgeValuesRel — объявление профиля края относительно корня дерева.
//
// Читается он как `values.yaml` (прогон идёт из своего каталога); здесь имя нужно
// ОТКАЗУ: путь, названный от корня, ведёт читателя туда, где правится перечень,
// а `values.yaml` в дереве не один.
const edgeValuesRel = "gateway/deploy/values.yaml"

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

// consoleOwnerJudgementSet — ЕДИНСТВЕННОЕ место, где решается, ЧЕМ судить.
//
// Отдельной функцией ради одного: доказательство способности гейта упасть обязано
// прогонять ТОТ ЖЕ выбор множества, а не его пересказ. Прежняя редакция выбирала
// множество в теле гейта, а инъекция подавала своё — поэтому подмена объявленного
// перечня принимаемым не роняла ничего, и промежуток между ними жил незамеченным
// (kacho#1633).
func consoleOwnerJudgementSet(t *testing.T) []string {
	t.Helper()
	return declaredSubscriptionOwners(t)
}

// judgeConsoleOwners отдаёт имена, которые консоль называет, а профиль владельцами
// не объявил.
//
// Пустой объявленный перечень — законное состояние посадки (возможность
// выключена, ручка отвечает `501` каждому), поэтому находок на нём НЕТ: осуждать
// написание за выключенную возможность не за что. Требовать непустоты значило бы
// заставить гейт судить ПОСТАВКУ, а его предмет — согласие написаний.
func judgeConsoleOwners(declared []string, census consoleOwnerCensus) []string {
	if len(declared) == 0 {
		return nil
	}
	ok := make(map[string]bool, len(declared))
	for _, name := range declared {
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
	declared := consoleOwnerJudgementSet(t)
	accepted := config.Config{}.DomainsWithInternalBackend()

	// Печатаются ОБЕ величины сверки — названное и объявленное, — потому что одно
	// число скрывает ровно тот случай, ради которого гейт заведён. Множество
	// принимаемых стоит рядом третьим: им гейт больше не судит, но именно его
	// избыток над объявленным и есть промежуток, в котором дефект жил.
	t.Logf("перепись: прочитано %d байт %s · имя владельца названо предметом %d× · "+
		"различных имён %d %v · членов объявленного типа %d %v · профиль объявил %d %v "+
		"(%s) · край принимает %d %v",
		len(raw), consoleSubjectsRel, census.occurrences, len(census.names), census.names,
		len(census.union), census.union, len(declared), declared, edgeValuesRel,
		len(accepted), accepted)

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
		t.Fatalf("край не принимает НИ ОДНОГО имени владельца — карта соединений либо " +
			"пуста, либо перестала нести внутренние адреса")
	}
	// Непустоты ОБЪЯВЛЕННОГО перечня здесь нет намеренно: ноль — законная посадка,
	// см. шапку. Он назван переписью выше, поэтому виден и отличим от «не разобрали».

	for _, name := range judgeConsoleOwners(declared, census) {
		t.Errorf("консоль называет владельцем %q, а профиль объявил владельцами журнала "+
			"только %v (%s): каждое открытие страницы такого ресурса даёт `400 unknown "+
			"owner` — и на самом потоке, и на запросе разбора причины. Словарь владельцев "+
			"край строит из этого перечня, а не из карты соединений, поэтому «край умеет "+
			"дозвониться до домена» здесь недостаточно.\nИсходов два: поправить написание "+
			"в %s — имя владельца есть домен КОНТРАКТА (`kacho.cloud.<домен>.v1`), а не "+
			"каталог сервиса в дереве и не сегмент REST-пути, — либо объявить владельца в "+
			"`subscriptionStream.owners` (%s), если поток ему нужен",
			name, declared, edgeValuesRel, consoleSubjectsRel, edgeValuesRel)
	}
}

// TestConsoleOwnerGateFallsOnASwappedNameAndIsSilentOnTheLegalTwin — способность
// гейта упасть И смолчать, доказанная инъекцией.
//
// Прогонов три, и третий несущий: без него молчание на законном близнеце было бы
// неотличимо от молчания мёртвого распознавателя.
func TestConsoleOwnerGateFallsOnASwappedNameAndIsSilentOnTheLegalTwin(t *testing.T) {
	declared := []string{"compute", "loadbalancer", "vpc"}

	swapped := `
// Ключ владельца ` + "`nlb`" + `, путь ` + "`/loadbalancer/v1/`" + ` — проза, судить её нельзя.
export type JournalOwner = "compute" | "nlb" | "vpc";
export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  "load-balancers": { owner: "nlb", kind: "nlb_network_load_balancer" },
};`

	legal := strings.NewReplacer(`"nlb"`, `"loadbalancer"`).Replace(swapped)

	swappedCensus := readConsoleOwners(swapped)
	if got := judgeConsoleOwners(declared, swappedCensus); len(got) != 1 || got[0] != "nlb" {
		t.Errorf("подменённое имя не найдено: находки %v, ожидалась ровно одна — \"nlb\"", got)
	}

	legalCensus := readConsoleOwners(legal)
	if got := judgeConsoleOwners(declared, legalCensus); len(got) != 0 {
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

// TestConsoleOwnerGateJudgesTheDeclaredList — гейт судит ОБЪЯВЛЕННЫЙ перечень
// владельцев, а не множество принимаемых краем имён.
//
// Прогонов три, и третий несущий: без него молчание прежнего свойства было бы
// неотличимо от молчания мёртвого распознавателя.
func TestConsoleOwnerGateJudgesTheDeclaredList(t *testing.T) {
	const src = `
export type JournalOwner = "compute" | "registry" | "vpc";
export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  "compute-instances": { owner: "compute", kind: "compute_instance" },
  registries: { owner: "registry", kind: "registry_registry" },
};`
	census := readConsoleOwners(src)

	// 1. КОНТРОЛЬ — объявлено ровно то, что названо: молчание.
	if got := judgeConsoleOwners([]string{"compute", "registry", "vpc"}, census); len(got) != 0 {
		t.Errorf("контроль: объявленное и названное совпадают, а гейт нашёл %v", got)
	}

	// 2. ИНЪЕКЦИЯ НОВОГО СВОЙСТВА — имя из промежутка «край принимает, профиль не
	// объявил». Ровно то состояние, которое прежняя редакция пропускала: `registry`
	// краем принимается (у него есть внутренний адрес), а перечень его не назвал.
	if got := judgeConsoleOwners([]string{"compute", "vpc"}, census); len(got) != 1 || got[0] != "registry" {
		t.Errorf("инъекция «принимается, но не объявлено»: находки %v, ожидалась ровно одна — \"registry\"", got)
	}

	// 3. ИНЪЕКЦИЯ СТАРОГО СВОЙСТВА — написание вне множества принимаемых (#1440).
	// Прежний предмет обязан остаться судимым: объявленный перечень строже
	// принимаемого множества, поэтому имя, которого край не знает, в нём тем более
	// отсутствует.
	swapped := strings.NewReplacer(`"registry"`, `"nlb"`).Replace(src)
	if got := judgeConsoleOwners([]string{"compute", "registry", "vpc"}, readConsoleOwners(swapped)); len(got) != 1 || got[0] != "nlb" {
		t.Errorf("инъекция «край не принимает вовсе»: находки %v, ожидалась ровно одна — \"nlb\"", got)
	}

	// 3а. ВЫБОР МНОЖЕСТВА, СДЕЛАННЫЙ ГЕЙТОМ, — на настоящем дереве.
	//
	// Три прогона выше подают множество строкой и потому о выборе не говорят
	// ничего: подмена объявленного перечня принимаемым оставила бы их зелёными.
	//
	// ПРОМЕЖУТОК СЧИТАЕТСЯ ОТ АВТОРИТЕТА, А СУДИТ — ВЫБОР ГЕЙТА, и порядок этот
	// несущий. Первая редакция брала промежуток у самого `consoleOwnerJudgementSet`
	// — тогда подмена делала промежуток ПУСТЫМ, ветвь пропускалась «честной
	// границей», и доказательство зеленело ровно на том дефекте, ради которого
	// написано. Проверено подменой: было зелено, стало красно.
	declaredOnTree := declaredSubscriptionOwners(t)
	acceptedOnTree := config.Config{}.DomainsWithInternalBackend()
	gap := ownersTheEdgeWillRefuse(acceptedOnTree, declaredOnTree)
	t.Logf("перепись промежутка: профиль объявил %d %v · край принимает %d %v · "+
		"принимается, но не объявлено %d %v",
		len(declaredOnTree), declaredOnTree, len(acceptedOnTree), acceptedOnTree, len(gap), gap)
	if len(gap) == 0 {
		// Честно названная граница: множества совпали, и выбор между ними стал
		// ненаблюдаемым. Утверждать о нём нечего — но и молчать об этом нельзя.
		t.Log("промежуток пуст: объявленное совпало с принимаемым, и выбор множества " +
			"на этом дереве не наблюдаем — утверждение ниже пропущено")
	} else {
		named := consoleOwnerCensus{occurrences: 1, names: []string{gap[0]}, union: []string{gap[0]}}
		if got := judgeConsoleOwners(consoleOwnerJudgementSet(t), named); len(got) != 1 || got[0] != gap[0] {
			t.Errorf("имя %q край принимает, а профиль владельцем НЕ объявил — гейт обязан "+
				"его найти, а нашёл %v. Судящее множество подменено принимаемым, и промежуток "+
				"между ними снова без держателя", gap[0], got)
		}
	}

	// 4. ПУСТОЙ ОБЪЯВЛЕННЫЙ ПЕРЕЧЕНЬ — законное состояние посадки, а не поломка:
	// ручка отвечает `501` с названной причиной, и написание владельца в консоли
	// при выключенной возможности предметом не является.
	if got := judgeConsoleOwners(nil, census); len(got) != 0 {
		t.Errorf("на пустом объявленном перечне гейт нашёл %v — пусто означает «владелец "+
			"не объявлен», и осуждать за это написание консоли не за что", got)
	}
}
