// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmancarvebanddisjoint_test.go — гейт по коллекциям: два набора одного прогона
// не могут вырезать ОДИН И ТОТ ЖЕ /24 из плана общей сети.
//
// # Предмет
//
// Соседний `newmancarveplananchor_test.go` требует, чтобы адрес был взят ИЗ плана.
// Этого мало: попадание в план ничего не говорит о том, что два набора возьмут
// РАЗНЫЕ адреса. Пока позиция внутри плана выбиралась жеребьёвкой по хешу прогона,
// столкновение оставалось вопросом вероятности — и вероятность эта не мала.
//
// Замер (симуляция самой формулы, 20 000 различных runId, перепись нарезок по
// committed-коллекциям набора nlb — 69 штук, план `10.0.0.0/8`, то есть 65 536
// различных /24): **3.40 %** прогонов несли хотя бы одно столкновение; теория дня
// рождения даёт 3.52 %. По прогонам `e2e-newman` за окно 08-13…08-16 столкновение
// наблюдалось дважды при 63 прогонах, где шард nlb дал вердикт, — 3.2 %.
//
// Цена столкновения — не один красный шаг: создание подсети получает синхронный
// отказ «Subnet CIDRs can not overlap», идентификатор не захватывается, и падают
// шаги, которые нарезки не делали. В прогоне 31936500945 одно столкновение дало
// шесть упавших утверждений в двух наборах.
//
// # Требование
//
// Позиция внутри плана ВЫЧИСЛЯЕТСЯ, а не разыгрывается: адресное пространство
// плана делится на равные полосы, набору принадлежит ровно одна (её номер объявлен
// в коллекции литералом), а внутри полосы позицию задаёт порядковый номер нарезки.
// Тогда столкновение невозможно ПО ПОСТРОЕНИЮ, а не «маловероятно». Энтропия
// прогона остаётся — она смещает всю сетку целиком (общий сдвиг сохраняет
// непересечение полос), поэтому два прогона на одном стенде по-прежнему расходятся.
//
// # Что именно проверяется
//
//  1. каждый блок нарезки объявляет свою полосу (`__bandIndex`) и общее число полос
//     (`__bands`);
//  2. в одной коллекции полоса одна — иначе «полоса набора» перестаёт быть
//     свойством набора;
//  3. полосы разных коллекций попарно различны — это и есть непересечение;
//  4. номер полосы лежит ВНУТРИ объявленного числа полос (`0 <= i < __bands`);
//  5. число полос объявлено одинаково всюду и не меньше числа коллекций с
//     нарезками — иначе полосы не помещаются в план;
//  6. тело `__carve4` НЕ зовёт `__ent` — то есть адрес v4 не разыгрывается.
//     Без пункта 6 первые пять пережили бы возврат жеребьёвки молча.
//
// Пункт 4 добавлен позже прочих и не выводится из пункта 3: номера `0,1,2,3,7` при
// `__bands = 5` попарно различны, коллекций ровно пять — оба прежних условия
// выполнены. Но ширина полосы есть `span / __bands`, а позиция берётся по модулю
// `span`, поэтому полоса 7 ложится ПОВЕРХ полосы 2. Замерено исполнением
// отгружаемого JS: столкновение возвращается в **100 %** прогонов, а гейт без этого
// пункта оставался зелёным. Тот же вход отвергает и генератор (проверка при импорте
// рядом с `CIDR_BANDS`) — здесь она backstop на случай правки коллекций мимо него.
//
// Семейство v6 намеренно оставлено на жеребьёвке, и это названо числом, а не
// умолчанием: план `fd00::/8` оставляет 56 свободных бит, поэтому при тех же 69
// нарезках вероятность столкновения ≈ 3·10⁻¹⁴ — предмета у запрета нет.
package repohygiene

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

var (
	reCarveFn       = regexp.MustCompile(`function\s+__carve4\s*\(`)
	reBandIndexDecl = regexp.MustCompile(`var\s+__bandIndex\s*=\s*(\d+)\s*;`)
	reBandsDecl     = regexp.MustCompile(`var\s+__bands\s*=\s*(\d+)\s*;`)
	reEntCall       = regexp.MustCompile(`__ent\s*\(`)
)

type carveBandScan struct {
	files      int            // прочитано коллекций
	withCarve  int            // из них несущих нарезку v4
	blocks     int            // блоков нарезки всего
	bandOf     map[string]int // коллекция → объявленная полоса
	countOf    map[string]int // коллекция → объявленное ЕЮ число полос
	bandsSeen  map[int]bool   // объявленные значения «сколько всего полос»
	drawn      int            // блоков, где адрес v4 всё ещё разыгрывается
	hits       []string
	sawDeclare bool // распознаватель объявления полосы срабатывал хотя бы раз
}

// scanCarveBands — разбор корпуса коллекций. Ключ — путь (он же координата
// находки), значение — содержимое файла.
func scanCarveBands(t *testing.T, corpus map[string][]byte) carveBandScan {
	t.Helper()
	out := carveBandScan{
		bandOf:    map[string]int{},
		countOf:   map[string]int{},
		bandsSeen: map[int]bool{},
	}

	for _, rel := range carveBandCorpusKeys(corpus) {
		out.files++
		var coll pmCollection
		if err := json.Unmarshal(corpus[rel], &coll); err != nil {
			t.Fatalf("%s: коллекция не разбирается: %v — файл не может быть ни засчитан "+
				"в перепись, ни молча пропущен", rel, err)
		}
		bands := map[int]bool{}
		counts := map[int]bool{}
		blocks := 0
		for _, step := range flattenItems(coll.Item, nil) {
			code := stripJSComments(stepScript(step, "prerequest"))
			if !reCarveFn.MatchString(code) {
				continue
			}
			blocks++
			out.blocks++

			if m := reBandIndexDecl.FindStringSubmatch(code); m != nil {
				n, _ := strconv.Atoi(m[1])
				bands[n] = true
				out.sawDeclare = true
			} else {
				out.hits = append(out.hits, rel+" :: шаг «"+step.Name+
					"» режет /24, не объявив своей полосы (__bandIndex)")
			}
			if m := reBandsDecl.FindStringSubmatch(code); m != nil {
				n, _ := strconv.Atoi(m[1])
				out.bandsSeen[n] = true
				counts[n] = true
			} else {
				out.hits = append(out.hits, rel+" :: шаг «"+step.Name+
					"» не объявил числа полос (__bands) — ширину полосы не из чего вычислить")
			}
			if body := carve4Body(code); body != "" && reEntCall.MatchString(body) {
				out.drawn++
				out.hits = append(out.hits, rel+" :: шаг «"+step.Name+
					"» РАЗЫГРЫВАЕТ адрес v4: тело __carve4 зовёт __ent")
			}
		}
		if blocks == 0 {
			continue
		}
		out.withCarve++
		if len(counts) == 1 {
			for n := range counts {
				out.countOf[rel] = n
			}
		}
		switch len(bands) {
		case 0:
		case 1:
			for n := range bands {
				out.bandOf[rel] = n
			}
		default:
			list := make([]int, 0, len(bands))
			for n := range bands {
				list = append(list, n)
			}
			sort.Ints(list)
			out.hits = append(out.hits, fmt.Sprintf(
				"%s :: коллекция объявляет НЕСКОЛЬКО полос %v — полоса перестаёт быть "+
					"свойством набора, и непересечение доказать нечем", rel, list))
		}
	}

	// Полоса обязана лежать ВНУТРИ объявленного числа полос. Попарного различия
	// мало: ширина полосы считается как `span / __bands`, а позиция берётся по
	// модулю `span`, поэтому номер `>= __bands` ложится ПОВЕРХ чужой полосы —
	// номера остаются различными, а адреса совпадают снова. Проверяется до
	// попарного различия: номер вне диапазона делает вопрос «а с кем именно он
	// совпал» бессмысленным — он совпадёт с тем, кого назовёт арифметика.
	for _, rel := range carveBandOwnerKeys(out.bandOf) {
		n, count := out.bandOf[rel], out.countOf[rel]
		if count <= 0 {
			continue // число полос эта коллекция не объявила — уже находка выше
		}
		if n >= count {
			out.hits = append(out.hits, fmt.Sprintf(
				"%s :: полоса %d объявлена при __bands = %d — номер вне диапазона "+
					"[0..%d]. Полоса шириной span/__bands, а позиция берётся по модулю "+
					"span, поэтому такая полоса ложится ПОВЕРХ чужой: адреса двух наборов "+
					"совпадают снова, хотя номера различны. Частая причина — набор сняли "+
					"из CIDR_BANDS, не перенумеровав остальные",
				rel, n, count, count-1))
		}
	}

	// Полосы разных коллекций обязаны различаться — это и есть предмет гейта.
	owner := map[int]string{}
	for _, rel := range carveBandOwnerKeys(out.bandOf) {
		n := out.bandOf[rel]
		if prev, ok := owner[n]; ok {
			out.hits = append(out.hits, fmt.Sprintf(
				"полосу %d делят %s и %s — их адреса пересекаются, и столкновение "+
					"перестаёт быть невозможным", n, prev, rel))
			continue
		}
		owner[n] = rel
	}
	return out
}

// carve4Body — тело функции __carve4 (до первой закрывающей скобки на нулевом
// уровне вложенности). Читается именно тело: `__ent` законно живёт рядом, в
// нарезке v6, и запрет на весь блок был бы запретом не того.
func carve4Body(code string) string {
	loc := reCarveFn.FindStringIndex(code)
	if loc == nil {
		return ""
	}
	depth := 0
	started := false
	for i := loc[1]; i < len(code); i++ {
		switch code[i] {
		case '{':
			depth++
			started = true
		case '}':
			depth--
			if started && depth == 0 {
				return code[loc[1]:i]
			}
		}
	}
	return code[loc[1]:]
}

func carveBandCorpusKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func carveBandOwnerKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestCarvedSubnetsOfOneRunCanNotCollide — по дереву.
func TestCarvedSubnetsOfOneRunCanNotCollide(t *testing.T) {
	root := repoRoot(t)
	corpus := map[string][]byte{}
	for _, sub := range subnetSupernetScanRoots {
		abs := filepath.Join(root, sub)
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		// Состав берётся у ИНДЕКСА, а не обходом диска: рядом с деревом на машине
		// со стендом лежат распаковки чартов и отчёты прогонов, и вердикт стал бы
		// свойством рабочего каталога, а не коммита. Заодно исчезает чтение файла
		// по собранному пути — вместе с подавлением, которое в этом репозитории
		// не читает никто.
		paths, err := treecorpus.UnderWithSuffix(abs, ".postman_collection.json")
		if err != nil {
			t.Fatalf("состав дерева под %s: %v — без него «ноль находок» неотличимо "+
				"от «ноль прочитанного»", sub, err)
		}
		for _, path := range paths {
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("читаю %s: %v", path, rerr)
			}
			rel, _ := filepath.Rel(root, path)
			corpus[rel] = b
		}
	}
	if len(corpus) == 0 {
		t.Fatalf("гейт не прочитал ни одной коллекции (корни: %v) — предпосылка обхода "+
			"сломана, молчание ничего не доказывает", subnetSupernetScanRoots)
	}

	got := scanCarveBands(t, corpus)
	if got.blocks == 0 {
		t.Fatalf("в %d коллекциях не найдено НИ ОДНОГО блока нарезки /24 — распознавание "+
			"предмета сломано, молчание ничего не доказывает", got.files)
	}

	bands := make([]int, 0, len(got.bandsSeen))
	for n := range got.bandsSeen {
		bands = append(bands, n)
	}
	sort.Ints(bands)
	t.Logf("осмотрено коллекций: %d; несут нарезку /24: %d; блоков нарезки: %d; "+
		"объявлено полос: %v; блоков с разыгранным адресом v4: %d",
		got.files, got.withCarve, got.blocks, bands, got.drawn)

	if len(got.hits) > 0 {
		sort.Strings(got.hits)
		t.Errorf("найдено %d нарушений непересечения нарезок:\n  %s\n\n"+
			"Следствие: два набора одного прогона берут один /24, создание подсети "+
			"отвечает «Subnet CIDRs can not overlap», идентификатор не захватывается, и "+
			"падают шаги, которые нарезки не делали.\n\n"+
			"Исход: позиция внутри плана ВЫЧИСЛЯЕТСЯ (полоса набора + порядковый номер "+
			"нарезки), а не разыгрывается. Полосы объявляются таблицей "+
			"`CIDR_BANDS` в services/nlb/tests/newman/scripts/gen.py; новый набор "+
			"обязан получить в ней свой номер, иначе генерация откажет.",
			len(got.hits), strings.Join(got.hits, "\n  "))
		return
	}
	// Предпосылка РАСПОЗНАВАТЕЛЯ — проверяется, только когда находок нет.
	if !got.sawDeclare {
		t.Fatalf("ни один из %d блоков нарезки не распознан как объявляющий полосу — "+
			"распознаватель не подтверждён на живых данных, поэтому «ноль находок» "+
			"тут значит «не смотрел»", got.blocks)
	}
	if len(got.bandsSeen) != 1 {
		t.Errorf("число полос объявлено по-разному: %v — ширина полосы вычисляется из "+
			"него, и разные значения дают разные сетки", bands)
	}
	for n := range got.bandsSeen {
		if n < got.withCarve {
			t.Errorf("объявлено полос %d, а коллекций с нарезкой %d — полосы не помещаются "+
				"в план, и хотя бы две коллекции обязаны делить одну", n, got.withCarve)
		}
	}
}

// bandShape — коллекция с одним шагом нарезки. Меняются ровно три вещи: номер
// полосы, объявленное число полос и тело `__carve4`. Всё остальное — как в
// отгружаемых коллекциях, иначе инъекция доказывала бы что-то про свою фикстуру,
// а не про предмет.
func bandShape(step string, decls string, carveBody string) []byte {
	return []byte(`{"item":[{"name":"C — carve in a seeded network","item":[
      {"name":"` + step + `","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
        "body":{"raw":"{\"networkId\":\"{{existingNetworkId}}\",\"ipv4CidrPrimary\":\"{{_subnetCidr}}\"}"}},
        "event":[{"listen":"prerequest","script":{"exec":[` + decls +
		`"function __carve4(plan) {",` + carveBody + `"}",` +
		`"pm.environment.set('_subnetCidr', __carve4(pm.environment.get('existingNetworkV4Plan')));"]}}]}
    ]}]}`)
}

// bandDecls — объявления полосы и их числа, как их пишет генератор.
func bandDecls(index, bands int) string {
	return fmt.Sprintf("%q,%q,", fmt.Sprintf("  var __bandIndex = %d;", index),
		fmt.Sprintf("  var __bands = %d;", bands))
}

// computedBody — законное тело: позиция ВЫЧИСЛЯЕТСЯ из полосы и номера нарезки.
const computedBody = `"  var span = Math.pow(2, 24 - L);",` +
	`"  var pos = (__bandIndex * Math.floor(span / __bands) + (__seq - 1)) % span;",` +
	`"  return pos + '.0/24';",`

// TestCarveBandGateRedOnBandOutsideDeclaredCount — полоса ВНЕ объявленного числа
// полос обязана быть находкой.
//
// Почему это отдельный пункт, а не следствие попарного различия: номера `0,1,2,3,7`
// при `__bands = 5` **попарно различны** и коллекций ровно пять, поэтому оба прежних
// условия выполняются. Но ширина полосы считается как `span / __bands`, и позиция
// берётся по модулю `span`, — значит полоса 7 ложится ПОВЕРХ полосы 2, и нарезки двух
// наборов совпадают снова. Замерено исполнением отгружаемого JS: столкновение в
// **100 %** прогонов под планом `10.0.0.0/8` (было 0 % до инъекции).
//
// Правдоподобный вход: номер правят руками, и снятие набора из `CIDR_BANDS` без
// перенумерации оставляет дырку в конце — ровно эту форму.
func TestCarveBandGateRedOnBandOutsideDeclaredCount(t *testing.T) {
	got := scanCarveBands(t, map[string][]byte{
		"a.postman_collection.json": bandShape("setup-subnet", bandDecls(0, 5), computedBody),
		"b.postman_collection.json": bandShape("setup-subnet", bandDecls(7, 5), computedBody),
	})

	if got.blocks != 2 {
		t.Fatalf("распознано блоков нарезки %d из 2 — на неразобранном входе находка "+
			"ничего не доказывает", got.blocks)
	}
	if len(got.hits) != 1 {
		t.Fatalf("полоса вне объявленного числа полос обязана быть находкой, получено: %v",
			got.hits)
	}
	if !strings.Contains(got.hits[0], "b.postman_collection.json") ||
		!strings.Contains(got.hits[0], "7") {
		t.Errorf("находка обязана назвать коллекцию и номер полосы, получено: %q", got.hits[0])
	}
}

// TestCarveBandGateSilentOnLawfulBands — та же форма, но номера полос внутри
// объявленного числа. Без этой стороны проверку выше можно было бы написать как
// запрет полос вообще, и первый же ложный срабат снял бы её.
func TestCarveBandGateSilentOnLawfulBands(t *testing.T) {
	got := scanCarveBands(t, map[string][]byte{
		"a.postman_collection.json": bandShape("setup-subnet", bandDecls(0, 5), computedBody),
		"b.postman_collection.json": bandShape("setup-subnet", bandDecls(4, 5), computedBody),
	})

	if got.blocks != 2 || got.withCarve != 2 {
		t.Fatalf("вход не разобран: блоков %d, коллекций с нарезкой %d — молчание на "+
			"неразобранном входе ничего не доказывает", got.blocks, got.withCarve)
	}
	if !got.sawDeclare {
		t.Fatalf("распознаватель объявления полосы не сработал на входе, который его " +
			"содержит: молчание объясняется сломанным распознавателем, а не законностью входа")
	}
	if len(got.hits) != 0 {
		t.Errorf("гейт сработал на законной конструкции той же формы: %v", got.hits)
	}
}

// TestCarveBandGateRedOnSharedBand — положительный контроль попарного различия:
// две коллекции с ОДНОЙ полосой. Стояло в гейте и раньше, но без стоящей пробы,
// поэтому снятие условия прошло бы молча.
func TestCarveBandGateRedOnSharedBand(t *testing.T) {
	got := scanCarveBands(t, map[string][]byte{
		"a.postman_collection.json": bandShape("setup-subnet", bandDecls(1, 5), computedBody),
		"b.postman_collection.json": bandShape("setup-subnet", bandDecls(1, 5), computedBody),
	})

	if got.withCarve != 2 {
		t.Fatalf("вход не разобран: коллекций с нарезкой %d из 2", got.withCarve)
	}
	if len(got.hits) != 1 || !strings.Contains(got.hits[0], "делят") {
		t.Fatalf("общая полоса двух наборов обязана быть находкой, получено: %v", got.hits)
	}
}

// TestCarveBandGateRedOnDrawnAddress — положительный контроль пункта «адрес не
// разыгрывается»: тело `__carve4`, зовущее `__ent`. Это возврат к исходному дефекту
// задачи, и он обязан краснеть даже при верно объявленных полосах.
func TestCarveBandGateRedOnDrawnAddress(t *testing.T) {
	got := scanCarveBands(t, map[string][]byte{
		"a.postman_collection.json": bandShape("setup-subnet", bandDecls(0, 5),
			`"  return __ent(0) + '.' + __ent(1) + '.0/24';",`),
	})

	if got.blocks != 1 {
		t.Fatalf("вход не разобран: блоков %d из 1", got.blocks)
	}
	if got.drawn != 1 || len(got.hits) != 1 {
		t.Fatalf("разыгранный адрес v4 обязан быть находкой: drawn=%d hits=%v",
			got.drawn, got.hits)
	}
}

// TestCarveBandGateReadsCodeNotComment — `__ent` В КОММЕНТАРИИ внутри тела
// `__carve4` розыгрышем не является.
//
// Без этой стороны гейт краснел бы на объяснении вместо исполнения: комментарий про
// розыгрыш пишут ровно там, где от него отказались, — то есть в законном коде
// (`testing.md` §«Гейт читает исполняемую часть, а не текст»).
func TestCarveBandGateReadsCodeNotComment(t *testing.T) {
	got := scanCarveBands(t, map[string][]byte{
		"a.postman_collection.json": bandShape("setup-subnet", bandDecls(0, 5),
			`"  // позиция больше НЕ разыгрывается: тут стоял __ent(k), см. issue #477",`+
				computedBody),
	})

	if got.blocks != 1 {
		t.Fatalf("вход не разобран: блоков %d из 1", got.blocks)
	}
	if got.drawn != 0 || len(got.hits) != 0 {
		t.Errorf("упоминание __ent в КОММЕНТАРИИ засчитано розыгрышем — гейт читает "+
			"текст, а не исполняемую часть: drawn=%d hits=%v", got.drawn, got.hits)
	}
}
