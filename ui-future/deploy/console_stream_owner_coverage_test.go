// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_stream_owner_coverage_test.go — ВЛАДЕЛЕЦ, ОБЪЯВЛЕННЫЙ КРАЮ, ОБЯЗАН
// БЫТЬ ОТОБРАЖЁН КОНСОЛЬЮ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ (#1021)
//
// Опрос списков снимается ровно там, где предмет страницы покрыт потоком, и
// признак покрытия консоль получает от хаба (`hub.covers`). Хаб же открывает
// поток только тем владельцам, которых называет карта предметов
// (`subjects.ts`). Владелец, краем объявленный и картой не названный, потоком
// НЕ ЧИТАЕТСЯ ВОВСЕ — и его списки продолжают опрашиваться, при живом журнале и
// оплаченной посадке.
//
// Отличить это от исправной работы нечем: список работает, поток не открывается,
// ошибки нет ни в одном журнале. Опрос возвращается ПО ПОСТРОЕНИЮ (покрытия нет
// ⇒ `refetchInterval` остаётся числом), поэтому ни одно утверждение о содержимом
// страницы покраснеть не может.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЦЕНА ИЗМЕРЕНА, А НЕ ПРЕДПОЛОЖЕНА
//
// Край объявлял ПЯТЬ владельцев (`compute,loadbalancer,registry,storage,vpc`),
// карта предметов называла ТРИ. Расхождение прожило незамеченным и родило
// ЧЕТЫРЕ места опроса, объяснённых утверждением «журнала у этого домена нет», —
// при живых журналах блочного хранения (три вида) и реестра (один вид).
// Утверждение было выведено из СОБСТВЕННОГО умолчания консоли: «в нашей карте
// его нет, значит журнала нет».
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ СВЕРКА ИДЁТ С ОБЪЯВЛЕНИЕМ КРАЯ, А НЕ С ДЕРЕВОМ СЕРВИСОВ
//
// Служба журнала у сервиса — необходимое условие, а достаточное — то, что край
// ЗНАЕТ этого владельца: перечень закрыт, и имя вне его отвечает `501`. Значит
// множество, о котором консоль обязана иметь мнение, задаёт объявление края, а
// не перепись сервисов.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЗДЕСЬ НЕТ НАМЕРЕННО
//
// ВЕДОМОСТИ ИСКЛЮЧЕНИЙ. Владелец, которого консоль отобразить не может,
// сегодня не существует, и заводить под него послабление значило бы завести
// слепую зону вперёд — под неё уехало бы и настоящее расхождение. Появится
// такой владелец — это решение, и гейт заставит его принять, а не проглядеть.
//
// Обратной стороны — «консоль не называет имени, которого край не принимает» —
// здесь тоже нет: её держит `gateway/deploy/console_subscription_owner_test.go`,
// и второе место об одном предмете разошлось бы с ним молча.

package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// consoleSubjectsRel — карта предметов потока консоли относительно корня дерева.
var consoleSubjectsRel = filepath.Join(
	"ui-future", "shared", "src", "lib", "subscription", "subjects.ts")

// streamOwnerField — имя владельца в том виде, в каком оно УХОДИТ В ЗАПРОС.
//
// Судится значение поля предмета, а не член объявленного типа: тип — обещание о
// множестве имён, а запрос несёт это. Их согласие держит соседний гейт края.
var streamOwnerField = regexp.MustCompile(`owner\s*:\s*"([^"]*)"`)

// declaredStreamOwners — владельцы, объявленные краю посадкой.
//
// Читается у края, а не выписывается здесь: копия перечня была бы вторым местом
// об одном предмете, и разошлась бы она молча — обе непусты, обе выглядят
// действующими, и ни одна не знает о другой.
func declaredStreamOwners(t *testing.T) []string {
	t.Helper()
	path := filepath.Join(repoRootFromTest(t), edgeValuesRel)
	raw, err := os.ReadFile(path) // #nosec G304 -- путь собран из корня этого дерева
	if err != nil {
		t.Fatalf("объявление края %s не читается (%v) — вторая сторона сверки исчезла", path, err)
	}
	var values struct {
		SubscriptionStream struct {
			Owners string `yaml:"owners"`
		} `yaml:"subscriptionStream"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("разбор объявления края %s: %v", path, err)
	}
	return splitOwnerList(values.SubscriptionStream.Owners)
}

// splitOwnerList разбирает перечень через запятую тем же способом, каким его
// читает край: пустые звенья снимаются, порядок не значим.
func splitOwnerList(raw string) []string {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// mappedStreamOwners — владельцы, названные картой предметов консоли.
//
// Отдельной функцией — ради ОДНОГО: доказательство способности гейта упасть
// обязано прогонять ТУ ЖЕ функцию суждения, а не её копию. Исходник подаётся
// входом, поэтому доказательство не трогает дерева.
func mappedStreamOwners(src string) []string {
	code := stripSubjectComments(src)
	seen := map[string]bool{}
	var out []string
	for _, m := range streamOwnerField.FindAllStringSubmatch(code, -1) {
		if m[1] == "" || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

// ownersMissingFromConsole — объявленные краем и не названные картой.
func ownersMissingFromConsole(declared, mapped []string) []string {
	have := map[string]bool{}
	for _, name := range mapped {
		have[name] = true
	}
	var missing []string
	for _, name := range declared {
		if !have[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

// stripSubjectComments снимает комментарии, не трогая содержимого строк.
//
// Прозы про владельцев в этом файле много — она объясняет ровно ту омонимию, из
// которой гейт и вырос, — поэтому сверка по сырому тексту краснела бы на
// собственном объяснении. Строки уважаются намеренно: адрес вида `"https://…"`
// иначе обрезался бы по `//`.
func stripSubjectComments(src string) string {
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

// TestEveryDeclaredStreamOwnerIsMappedByTheConsole — два множества сходятся на
// дереве.
func TestEveryDeclaredStreamOwnerIsMappedByTheConsole(t *testing.T) {
	declared := declaredStreamOwners(t)

	path := filepath.Join(repoRootFromTest(t), consoleSubjectsRel)
	raw, err := os.ReadFile(path) // #nosec G304 -- путь собран из корня этого дерева
	if err != nil {
		t.Fatalf("карта предметов консоли %s не читается (%v) — сверять нечего", path, err)
	}
	mapped := mappedStreamOwners(string(raw))

	t.Logf("осмотрено: край объявляет владельцев %d %v (%s); карта предметов консоли "+
		"называет %d %v (%s)",
		len(declared), declared, edgeValuesRel,
		len(mapped), mapped, consoleSubjectsRel)

	// Премиса, а не вежливость: ноль прочитанных с любой стороны делает молчание
	// гейта неотличимым от «нарушений нет».
	if len(declared) == 0 {
		t.Fatalf("%s не объявляет ни одного владельца в `subscriptionStream.owners` — "+
			"прочитано ноль, и молчание проверки не является утверждением о покрытии",
			edgeValuesRel)
	}
	if len(mapped) == 0 {
		t.Fatalf("%s не называет ни одного владельца — прочитано ноль, и молчание "+
			"проверки не является утверждением о покрытии", consoleSubjectsRel)
	}

	if missing := ownersMissingFromConsole(declared, mapped); len(missing) > 0 {
		t.Fatalf("край объявляет владельцев журнала, которых карта предметов консоли "+
			"НЕ НАЗЫВАЕТ: %v.\n"+
			"Поток этим владельцам не открывается вовсе, значит их списки опрашиваются "+
			"при живом журнале и оплаченной посадке — а отличить это от исправной работы "+
			"нечем: опрос возвращается по построению, ошибки нет ни в одном журнале.\n"+
			"Исходов два: отобразить владельца в %s (спека консоли → владелец и вид, "+
			"написание вида — тип объекта модели прав из `knownKinds` владельца), либо "+
			"снять его из `subscriptionStream.owners` в %s, если поток ему не нужен.",
			missing, consoleSubjectsRel, edgeValuesRel)
	}
}

// TestOwnerCoverageComparisonCanFailAndCanStaySilent — доказательство способности
// суждения упасть, в ОБЕ стороны, на синтетическом входе.
//
// Прогоняются ТЕ ЖЕ функции, которыми судится дерево, а вход подаётся строкой:
// доказательство, трогающее дерево, испортило бы чужую рабочую копию, а
// доказательство на копии функции говорило бы о копии.
func TestOwnerCoverageComparisonCanFailAndCanStaySilent(t *testing.T) {
	const subjectsSrc = `
// Проза про владельца storage здесь намеренно: owner: "storage" в комментарии
// не является объявлением, и гейт, судящий сырой текст, зеленел бы на ней.
export type JournalOwner = "compute" | "vpc";
export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  "compute-instances": { owner: "compute", kind: "compute_instance" },
};
`
	mapped := mappedStreamOwners(subjectsSrc)
	if got, want := strings.Join(mapped, ","), "compute,vpc"; got != want {
		t.Fatalf("разбор карты предметов дал %q, ожидалось %q — комментарий с именем "+
			"владельца прочитан как объявление либо объявление потеряно", got, want)
	}

	cases := []struct {
		name     string
		declared string
		missing  []string
	}{
		{
			name:     "владелец объявлен и отображён — молчание",
			declared: "compute,vpc",
			missing:  nil,
		},
		{
			name:     "владелец объявлен и НЕ отображён — находка по имени",
			declared: "compute,storage,vpc",
			missing:  []string{"storage"},
		},
		{
			name:     "отображено шире объявленного — не предмет ЭТОГО гейта",
			declared: "vpc",
			missing:  nil,
		},
		{
			name:     "перечень с пустыми звеньями разбирается как край",
			declared: " compute , , vpc ",
			missing:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ownersMissingFromConsole(splitOwnerList(tc.declared), mapped)
			if strings.Join(got, ",") != strings.Join(tc.missing, ",") {
				t.Fatalf("недостающие владельцы: получено %v, ожидалось %v", got, tc.missing)
			}
		})
	}
}
