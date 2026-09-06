// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// licensecompat_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что гейт совместимости
// лицензий способен упасть, и того, что он молчит на законном.
//
// # Почему инъекция синтетическими рёбрами, а не синтетическим деревом
//
// Предмет проверки — ПРАВИЛО совместимости и разрешение уровня по пути, а не
// обход дерева: обход общий с гейтом границы фундамента и доказан там. Подача
// рёбер напрямую делает инъекцию одно-фактной — мир кейса отличается от мира
// его законного близнеца РОВНО импортируемым путём, всё остальное совпадает
// дословно. Заводя ради этого файлы и каталоги, мы меняли бы по два-три факта
// сразу и не знали бы, который дал красное.
//
// # Путь импорта при этом РЕАЛЬНЫЙ, а перевод — тот же
//
// Каждое ребро инъекции собирается через `treePathOfImport` — ту же функцию,
// которой пользуется гейт. Значит инъекция судит и перевод пути тоже: подмени
// его — и красные кейсы позеленеют, а этот файл об этом скажет.
//
// # Инъекция роняет ТОЛЬКО проверяемое
//
// Ни один кейс не трогает дерева, индекса git и чужих гейтов: вход — срез
// структур в памяти. Поэтому красное здесь не может прийти от соседа.

// injEdge — ребро инъекции. Путь импорта переводится в путь дерева тем же
// вызовом, что и в гейте: инъекция обязана идти по проверяемому пути, а не по
// своей копии.
func injEdge(fromDir, imp, kind string) licenseEdge {
	tree, _ := treePathOfImport(imp)
	return licenseEdge{FromDir: fromDir, ToTree: tree, Import: imp, Kind: kind, Files: 1}
}

const (
	// Реальные пути дерева, взятые дословно, чтобы кейс не проверял выдумку.
	injApachePkg   = "github.com/PRO-Robotech/kacho/pkg/ids"
	injBuslPkg     = "github.com/PRO-Robotech/kacho/gateway/internal/restmux"
	injAgplPkg     = "github.com/PRO-Robotech/kaname/internal/apps/kaname/moduleroles"
	injVendoredPkg = "github.com/PRO-Robotech/kacho/proto/google/api"
	injExternalPkg = "google.golang.org/grpc"

	injBuslDir   = "gateway/internal/restmux"
	injApacheDir = "pkg/db"
	injAgplDir   = "services/iam/internal/handler"
)

// TestLicenseTierForDirResolvesTheDirectoryOfEachTier — ЛОВУШКА, из-за которой
// гейт мог бы зеленеть на всём.
//
// Записи отображения заканчиваются слэшем, а каталог приходит без него. Каталог
// `services/iam` не подошёл бы под запись `services/iam/` и уехал бы в умолчание
// BUSL — тогда вынесенный под AGPL продукт сошёл бы за монорепо, и ровно те
// рёбра, ради которых гейт заведён, стали бы законными. Молча.
func TestLicenseTierForDirResolvesTheDirectoryOfEachTier(t *testing.T) {
	cases := []struct {
		dir  string
		want string
	}{
		{"services/iam", licenseAGPL},
		{"services/iam/internal/handler", licenseAGPL},
		{"pkg", licenseApache},
		{"pkg/db", licenseApache},
		{"proto", licenseApache},
		{"proto/google/api", ""},
		{"gateway/internal/restmux", licenseBUSL},
		{"services/vpc", licenseBUSL},
		{"internal/repohygiene", licenseBUSL},
		{".", licenseBUSL},
		{"", licenseBUSL},
	}
	for _, c := range cases {
		if got := licenseTierForDir(c.dir).SPDX; got != c.want {
			t.Errorf("уровень каталога %q: получено %q, ожидалось %q", c.dir, got, c.want)
		}
	}
}

// TestLicenseCompatibleAnswersEveryPairOfTiers — правило совместимости целиком,
// обе стороны по каждой паре.
func TestLicenseCompatibleAnswersEveryPairOfTiers(t *testing.T) {
	tiers := map[string]licenseTier{
		"apache": {Name: "фундамент", SPDX: licenseApache},
		"busl":   {Name: "монорепо", SPDX: licenseBUSL},
		"agpl":   {Name: "вынесенный продукт", SPDX: licenseAGPL},
		"третья": {Name: "третья сторона", SPDX: ""},
	}
	cases := []struct {
		from, to string
		want     bool
	}{
		{"apache", "apache", true},
		{"busl", "apache", true},
		{"agpl", "apache", true},
		{"третья", "apache", true},
		{"busl", "busl", true},
		{"agpl", "agpl", true},

		{"agpl", "busl", false},     // §10 AGPL: BUSL налагает доп. ограничения
		{"busl", "agpl", false},     // платформа стала бы производной от AGPL
		{"apache", "busl", false},   // фундамент перестал бы быть пермиссивным
		{"apache", "agpl", false},   // то же, и сильнее
		{"третья", "busl", false},   // принять обязательства некому
		{"apache", "третья", false}, // лицензия уровня не объявлена — судить нечем
		{"busl", "третья", false},
		{"agpl", "третья", false},
	}
	for _, c := range cases {
		got, why := licenseCompatible(tiers[c.from], tiers[c.to])
		if got != c.want {
			t.Errorf("%s -> %s: получено %v (%s), ожидалось %v", c.from, c.to, got, why, c.want)
		}
		if !got && strings.TrimSpace(why) == "" {
			t.Errorf("%s -> %s: отказ без причины — находка была бы непонятна", c.from, c.to)
		}
		if got && why != "" {
			t.Errorf("%s -> %s: разрешение с причиной %q — причина принадлежит отказу", c.from, c.to, why)
		}
	}
}

// TestInjectedIncompatibleEdgeIsAFindingWithItsCoordinate — КРАСНАЯ сторона.
//
// По одному кейсу на каждое запрещённое направление, и у каждого — законный
// близнец, отличающийся РОВНО импортируемым путём.
func TestInjectedIncompatibleEdgeIsAFindingWithItsCoordinate(t *testing.T) {
	cases := []struct {
		name     string
		bad      licenseEdge
		twin     licenseEdge
		wantFrom string
		wantTo   string
	}{
		{
			// Предмет задачи #2083: платформа втягивает клиентский пакет службы.
			name:     "платформа BUSL импортирует пакет службы AGPL",
			bad:      injEdge(injBuslDir, injAgplPkg, licenseEdgeProd),
			twin:     injEdge(injBuslDir, injApachePkg, licenseEdgeProd),
			wantFrom: licenseBUSL, wantTo: licenseAGPL,
		},
		{
			// Исходная сторона того же предмета: §10 AGPL.
			name:     "служба AGPL импортирует код BUSL",
			bad:      injEdge(injAgplDir, injBuslPkg, licenseEdgeProd),
			twin:     injEdge(injAgplDir, injApachePkg, licenseEdgeProd),
			wantFrom: licenseAGPL, wantTo: licenseBUSL,
		},
		{
			// Пермиссивность фундамента — утверждение, которое обязано быть
			// верным, иначе перелицензирование `pkg/` ничего не даёт.
			name:     "фундамент Apache импортирует код BUSL",
			bad:      injEdge(injApacheDir, injBuslPkg, licenseEdgeProd),
			twin:     injEdge(injApacheDir, injApachePkg, licenseEdgeProd),
			wantFrom: licenseApache, wantTo: licenseBUSL,
		},
		{
			// Уровень без объявленной лицензии — fail-closed.
			name:     "импорт уровня, лицензия которого не объявлена",
			bad:      injEdge(injApacheDir, injVendoredPkg, licenseEdgeProd),
			twin:     injEdge(injApacheDir, injApachePkg, licenseEdgeProd),
			wantFrom: licenseApache, wantTo: "",
		},
		{
			// Проба распространяется публичным репозиторием наравне с прод-кодом,
			// поэтому вид ребра вердикта не смягчает.
			name:     "то же ребро в пробе судится наравне с прод-кодом",
			bad:      injEdge(injBuslDir, injAgplPkg, licenseEdgeTest),
			twin:     injEdge(injBuslDir, injApachePkg, licenseEdgeTest),
			wantFrom: licenseBUSL, wantTo: licenseAGPL,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// КРАСНОЕ: ребро-нарушитель обязано стать находкой с координатой.
			findings, census := scanLicenseCompat([]licenseEdge{c.bad}, 1, 1)
			if len(findings) != 1 {
				t.Fatalf("находок %d, ожидалась 1 — гейт не увидел нарушения\n%s",
					len(findings), census.String())
			}
			f := findings[0]
			if f.From.SPDX != c.wantFrom || f.To.SPDX != c.wantTo {
				t.Fatalf("пара уровней %q -> %q, ожидалась %q -> %q",
					f.From.SPDX, f.To.SPDX, c.wantFrom, c.wantTo)
			}
			text := f.String()
			for _, want := range []string{c.bad.FromDir, c.bad.Import} {
				if !strings.Contains(text, want) {
					t.Errorf("находка не называет координату %q: %s", want, text)
				}
			}
			if strings.TrimSpace(f.Why) == "" {
				t.Errorf("находка без причины — читатель не поймёт, что чинить: %s", text)
			}

			// ЗЕЛЁНОЕ: законный близнец отличается РОВНО импортируемым путём.
			if c.twin.FromDir != c.bad.FromDir || c.twin.Kind != c.bad.Kind {
				t.Fatalf("близнец отличается больше чем одним фактом: %+v против %+v", c.twin, c.bad)
			}
			twinFindings, twinCensus := scanLicenseCompat([]licenseEdge{c.twin}, 1, 1)
			if len(twinFindings) != 0 {
				t.Fatalf("законный близнец объявлен находкой — гейт ловит форму, а не существо: %v\n%s",
					twinFindings, twinCensus.String())
			}
			if twinCensus.Edges != 1 {
				t.Fatalf("близнец не осмотрен: рёбер в переписи %d, ожидалось 1\n%s",
					twinCensus.Edges, twinCensus.String())
			}
		})
	}
}

// TestLicenseCompatControlAndCensus — КОНТРОЛЬ: набор только законных рёбер даёт
// ноль находок, и перепись при этом не пуста.
//
// Без него молчание гейта было бы неотличимо от молчания мёртвого гейта.
func TestLicenseCompatControlAndCensus(t *testing.T) {
	legal := []licenseEdge{
		injEdge(injBuslDir, injApachePkg, licenseEdgeProd),
		injEdge(injAgplDir, injApachePkg, licenseEdgeProd),
		injEdge(injApacheDir, injApachePkg, licenseEdgeProd),
		injEdge(injAgplDir, injAgplPkg, licenseEdgeTest),
		injEdge(injBuslDir, injBuslPkg, licenseEdgeProd),
		injEdge(injBuslDir, injExternalPkg, licenseEdgeProd),
	}
	findings, census := scanLicenseCompat(legal, 3, 6)
	if len(findings) != 0 {
		t.Fatalf("на законном наборе находок %d — гейт краснеет на исправном: %v", len(findings), findings)
	}
	if census.Edges != 5 {
		t.Fatalf("судимых рёбер %d, ожидалось 5 (шестое — наружу)\n%s", census.Edges, census.String())
	}
	if census.External != 1 {
		t.Fatalf("рёбер наружу %d, ожидалось 1 — чужой модуль обязан быть отделён, "+
			"а не осуждён здесь\n%s", census.External, census.String())
	}
	if census.Prod != 4 || census.Test != 1 {
		t.Fatalf("перепись по виду: прод %d, проба %d; ожидалось 4 и 1\n%s",
			census.Prod, census.Test, census.String())
	}
	// Перепись обязана называть пары поимённо: одно число «рёбер N» скрыло бы
	// ровно тот случай, ради которого гейт заведён.
	// Пять: BUSL->Apache, AGPL->Apache, Apache->Apache, AGPL->AGPL, BUSL->BUSL.
	// Число выписано, а не выведено из длины набора, — иначе утверждение стало бы
	// тождественно истинным и о разделении пар не сказало бы ничего.
	if len(census.Pairs) != 5 {
		t.Fatalf("пар в переписи %d, ожидалось 5\n%s", len(census.Pairs), census.String())
	}
}

// TestLicenseCompatEmptyInputIsNotAVerdict — пустой вход не зачитывается в успех.
//
// Сам отказ выносит гейт дерева (у него есть, с чем сравнить); задача разбора —
// сделать пустоту НАБЛЮДАЕМОЙ, а не сообщить о ней нулём находок.
func TestLicenseCompatEmptyInputIsNotAVerdict(t *testing.T) {
	findings, census := scanLicenseCompat(nil, 0, 0)
	if len(findings) != 0 {
		t.Fatalf("на пустом входе находок %d — их неоткуда взять", len(findings))
	}
	if census.Edges != 0 {
		t.Fatalf("рёбер %d на пустом входе", census.Edges)
	}
	if !strings.Contains(census.String(), "рёбер в продукте 0") {
		t.Fatalf("перепись не называет пустоту числом — «ноль находок» станет "+
			"неотличимо от «ноль прочитанного»: %s", census.String())
	}
}

// TestLicenseCompatFindingsAreDeterministic — порядок находок не зависит от
// порядка входа.
//
// Вход гейта собирается обходом карт, порядок которого случаен; несортированная
// находка меняла бы текст отказа от прогона к прогону при неизменном дереве, и
// разбор красного превратился бы в сличение перестановок.
func TestLicenseCompatFindingsAreDeterministic(t *testing.T) {
	a := []licenseEdge{
		injEdge(injBuslDir, injAgplPkg, licenseEdgeProd),
		injEdge(injApacheDir, injBuslPkg, licenseEdgeProd),
		injEdge(injAgplDir, injBuslPkg, licenseEdgeProd),
	}
	b := []licenseEdge{a[2], a[0], a[1]}

	fa, _ := scanLicenseCompat(a, 3, 3)
	fb, _ := scanLicenseCompat(b, 3, 3)
	if len(fa) != 3 || len(fb) != 3 {
		t.Fatalf("находок %d и %d, ожидалось по 3", len(fa), len(fb))
	}
	for i := range fa {
		if fa[i].String() != fb[i].String() {
			t.Fatalf("порядок находок зависит от порядка входа:\n  %s\n  %s", fa[i], fb[i])
		}
	}
}
