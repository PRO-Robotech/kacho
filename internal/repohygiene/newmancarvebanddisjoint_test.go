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
//  4. число полос объявлено одинаково всюду и не меньше числа коллекций с
//     нарезками — иначе полосы не помещаются в план;
//  5. тело `__carve4` НЕ зовёт `__ent` — то есть адрес v4 не разыгрывается.
//     Без пункта 5 первые четыре пережили бы возврат жеребьёвки молча.
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
	bandsSeen  map[int]bool   // объявленные значения «сколько всего полос»
	drawn      int            // блоков, где адрес v4 всё ещё разыгрывается
	hits       []string
	sawDeclare bool // распознаватель объявления полосы срабатывал хотя бы раз
}

// scanCarveBands — разбор корпуса коллекций. Ключ — путь (он же координата
// находки), значение — содержимое файла.
func scanCarveBands(t *testing.T, corpus map[string][]byte) carveBandScan {
	t.Helper()
	out := carveBandScan{bandOf: map[string]int{}, bandsSeen: map[int]bool{}}

	for _, rel := range carveBandCorpusKeys(corpus) {
		out.files++
		var coll pmCollection
		if err := json.Unmarshal(corpus[rel], &coll); err != nil {
			t.Fatalf("%s: коллекция не разбирается: %v — файл не может быть ни засчитан "+
				"в перепись, ни молча пропущен", rel, err)
		}
		bands := map[int]bool{}
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
		_ = filepath.Walk(filepath.Join(root, sub), func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil //nolint:nilerr // недоступный подкаталог не подменяет вердикт
			}
			if !strings.HasSuffix(p, ".postman_collection.json") {
				return nil
			}
			b, rerr := os.ReadFile(p) //nolint:gosec // путь получен обходом дерева репозитория
			if rerr != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			corpus[rel] = b
			return nil
		})
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
