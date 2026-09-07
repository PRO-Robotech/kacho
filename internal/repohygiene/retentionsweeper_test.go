// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retentionsweeper_test.go — ОБЪЯВЛЕННЫЙ УБОРЩИК ОБЯЗАН ИМЕТЬ ПРОД-ВЫЗЫВАЮЩЕГО
// (задача #1292, приёмка `retention-sweep-has-a-caller.md`, RET-SWP-10…14).
//
// # Предмет
//
// Уборщик без вызывающего — это не «мёртвый код». Это таблица, растущая без
// ограничения, и утверждения дерева о работающем сборщике, ставшие ложью. В iam
// таких уборщиков было два, прод-вызывающих — ноль у обоих, и у одного не было
// даже пробы; при этом ВОСЕМЬ мест дерева говорили в настоящем времени, что
// сборщик убирает.
//
// Провязка закрывает это один раз. Свойство впредь держит гейт: текст, ставший
// истинным однажды, снова становится ложью молча.
//
// # Почему единица счёта — «служба», а не «каталог, который импортирует»
//
// Первое, что просится, — засчитывать вызывающего из своего каталога либо из
// каталога, который его ИМПОРТИРУЕТ. На этом дереве такое правило неверно:
// уборщик провязывается ЧЕРЕЗ ПОРТ, и каталог реестра уборки хранилища не
// импортирует вовсе — он принимает интерфейс. Правило объявило бы находкой
// живую провязку.
//
// Поэтому граница — СЛУЖБА (`services/<имя>`, `gateway`, `pkg`, `internal`).
// Она решает то, ради чего заводилась: имя `Reap` в дереве носят два разных
// типа, и без границы вызывающий шлюза покрывал бы уборщика iam. Остаток назван
// прямо: однофамильцы ВНУТРИ одной службы гейтом не различаются.
package repohygiene

import (
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// retentionSweeperRoots — где ищутся уборщики и их вызывающие.
var retentionSweeperRoots = []string{"services", "gateway", "pkg", "internal"}

// retentionSweeperLedger — уборщики ЧУЖИХ полос, у которых вызывающего нет и
// чинит их не эта работа.
//
// Не прощение: каждая запись несёт предмет и номер задачи, а гейт РОНЯЕТ прогон
// на записи, у которой уборщик получил вызывающего либо исчез (RET-SWP-13).
// Послабление без предмета унаследует следующая слепая зона.
var retentionSweeperLedger = []struct {
	Qualified string
	Subject   string
	Issue     string
}{
	// Пусто — и это ЦЕЛЬ ведомости, а не её отсутствие. Единственная запись (#1294,
	// мёртвый дубль уборки дренированных целей в nlb) истекла сама: предмет снят той
	// же волной, гейт назвал запись потерявшей предмет, запись убрана. Гейт на пустой
	// ведомости ОБЯЗАН проходить — иначе он подталкивал бы держать запись ради зелёного.
}

// serviceUnitOf — служба, которой принадлежит каталог.
//
// Тело переехало в `MigrationOwnerOf` (`tablegrowth.go`), и это не косметика:
// ту же границу спрашивает соседний гейт роста таблиц (#1356), а он живёт в
// НЕ-тестовом файле и объявления из тестового не видит. Две реализации одной
// границы разошлись бы молча — и разошлись бы именно там, где расхождение не
// видно: обе отвечают верно на обычном пути. Поведение здесь не меняется.
func serviceUnitOf(dir string) string { return MigrationOwnerOf(dir) }

// retentionSweeperVerdict — ЧИСТОЕ суждение по уже прочитанному дереву.
//
// Отделено от обхода намеренно: инъекция подаёт сюда синтетический корпус и
// проверяет, что суждение способно упасть И способно смолчать, — на настоящем
// дереве ни того ни другого не показать, не сломав его.
func retentionSweeperVerdict(
	sweepers []RetentionSweeper,
	callersByName map[string]map[string][]string, // имя метода → служба → объемлющие функции
	ledger map[string]string, // Qualified → номер задачи
) (findings []string, stale []string, wired int) {
	for _, s := range sweepers {
		unit := serviceUnitOf(s.Dir)
		self := s.Qualified()
		hasCaller := false
		for _, enclosing := range callersByName[s.Name][unit] {
			if enclosing == self {
				// Уборщик, зовущий сам себя, вызывающим себе не является:
				// иначе рекурсия объявила бы его провязанным.
				continue
			}
			hasCaller = true
			break
		}
		if hasCaller {
			wired++
			if issue, listed := ledger[self]; listed {
				stale = append(stale, s.File+":"+strconv.Itoa(s.Line)+" "+self+
					" — запись ведомости "+issue+" ПОТЕРЯЛА ПРЕДМЕТ: у уборщика появился вызывающий. "+
					"Снимите запись: послабление без предмета унаследует следующая слепая зона")
			}
			continue
		}
		if _, listed := ledger[self]; listed {
			continue
		}
		findings = append(findings, s.File+":"+strconv.Itoa(s.Line)+" "+self+
			" — объявленный уборщик по сроку БЕЗ прод-вызывающего в службе "+unit+
			". Оператор: "+s.SQL+
			". Это не мёртвый код: таблица растёт без ограничения, а всякий текст дерева, "+
			"утверждающий, что сборщик работает, становится ложью. Исходов три — провязать; "+
			"снять уборщик вместе с этими утверждениями; либо завести ПРЕДМЕТ (задачу) и запись "+
			"в ведомости retentionSweeperLedger с номером и причиной")
	}
	// Запись ведомости, чьего уборщика в дереве больше НЕТ, — тоже находка.
	seen := map[string]bool{}
	for _, s := range sweepers {
		seen[s.Qualified()] = true
	}
	for q, issue := range ledger {
		if !seen[q] {
			stale = append(stale, q+" — запись ведомости "+issue+" ПОТЕРЯЛА ПРЕДМЕТ: "+
				"уборщика с таким именем в дереве нет вовсе. Снимите запись")
		}
	}
	sort.Strings(findings)
	sort.Strings(stale)
	return findings, stale, wired
}

// TestDeclaredRetentionSweepersHaveAProductionCaller — сам гейт (RET-SWP-10).
func TestDeclaredRetentionSweepersHaveAProductionCaller(t *testing.T) {
	root := repoRoot(t)

	var (
		sweepers []RetentionSweeper
		scanned  int
		census   RetentionSweeperCensus
	)
	callersByName := map[string]map[string][]string{}

	walkOwnerRegisterGoFiles(t, root, retentionSweeperRoots, func(rel string, body []byte) {
		scanned++
		dir := path.Dir(filepath.ToSlash(rel))

		found, c, err := ScanRetentionSweepers(rel, dir, body)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		census.Functions += c.Functions
		census.Literals += c.Literals
		census.Deletes += c.Deletes
		census.Sweepers += c.Sweepers
		census.NamedValues += c.NamedValues
		census.Named += c.Named
		sweepers = append(sweepers, found...)

		calls, err := ScanMethodCallNames(rel, body)
		if err != nil {
			t.Fatalf("разбор вызовов %s: %v", rel, err)
		}
		unit := serviceUnitOf(dir)
		for name, enclosings := range calls {
			if callersByName[name] == nil {
				callersByName[name] = map[string][]string{}
			}
			callersByName[name][unit] = append(callersByName[name][unit], enclosings...)
		}
	})

	ledger := map[string]string{}
	for _, e := range retentionSweeperLedger {
		ledger[e.Qualified] = e.Issue
	}
	findings, stale, wired := retentionSweeperVerdict(sweepers, callersByName, ledger)

	// Объём осмотренного — ОТДЕЛЬНОЕ утверждение: «ноль находок» обязано быть
	// отличимо от «ноль прочитанного».
	t.Logf("перепись: файлов Go прочитано %d, функций осмотрено %d, строковых литералов %d, "+
		"из них удаляют строки %d; пакетных строковых значений %d, из них назвали уборщика %d; "+
		"признаны уборщиками по сроку %d; с прод-вызывающим %d; в ведомости чужих полос %d; находок %d",
		scanned, census.Functions, census.Literals, census.Deletes,
		census.NamedValues, census.Named, len(sweepers),
		wired, len(retentionSweeperLedger), len(findings))

	// Предпосылки разбора. Пустой обход и ноль распознанных уборщиков означают
	// не благополучие, а слепоту: гейт молчал бы и на дереве без единой уборки.
	if scanned == 0 {
		t.Fatal("прочитано ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	if len(sweepers) == 0 {
		t.Fatal("уборщиков по сроку в дереве не найдено ни одного — предикат разбора разъехался с " +
			"кодом. Уборка в этом дереве есть (её объявляют iam, gateway, nlb, registry), значит " +
			"молчание гейта означает слепоту, а не отсутствие предмета")
	}

	for _, s := range stale {
		t.Errorf("ведомость: %s", s)
	}
	for _, f := range findings {
		t.Errorf("уборщик без вызывающего: %s", f)
	}
}

// TestRetentionSweeperGateIsSilentOnForeignInjectionFixtures — RET-SWP-12.
//
// Фикстуры инъекции ЧУЖИХ гейтов содержат `DELETE … WHERE expires_at <= $1` как
// законный близнец. Они лежат в `_test.go`, то есть в обход не попадают вовсе, —
// и это УТВЕРЖДАЕТСЯ, а не подразумевается: обход, однажды переставший
// пропускать пробы, объявил бы находкой чужую фикстуру, а её «починка»
// сломала бы гейт, которому она принадлежит.
func TestRetentionSweeperGateIsSilentOnForeignInjectionFixtures(t *testing.T) {
	root := repoRoot(t)
	const foreign = "internal/repohygiene/assertionadmissioncalls_injection_test.go"

	// Предпосылка: фикстура на месте и в самом деле несёт уборщика. Проба,
	// потерявшая свой предмет, молчала бы ни о чём.
	tt := newTrackedTree(t, root)
	if !tt.hasFile(foreign) {
		t.Fatalf("фикстуры чужого гейта (%s) в составе дерева нет: проба потеряла предмет "+
			"и её молчание больше ничего не утверждает", foreign)
	}

	seen := 0
	walkOwnerRegisterGoFiles(t, root, retentionSweeperRoots, func(rel string, _ []byte) {
		if filepath.ToSlash(rel) == foreign {
			seen++
		}
	})
	if seen != 0 {
		t.Errorf("обход гейта прочитал фикстуру чужого гейта (%s): её синтетический уборщик "+
			"станет находкой, а «починка» сломает гейт, которому фикстура принадлежит", foreign)
	}
}

// retentionLoopFile — координата фоновой петли уборки.
const retentionLoopFile = "services/iam/internal/apps/kaname/retention/sweeper.go"

// TestRetentionLoopIsVisibleToTheFanoutGate — RET-SWP-09.
//
// # Почему это отдельное утверждение, а не следствие зелёного соседа
//
// Гейт раскладки по репликам требует запись `РЕПЛИКИ:` у КАЖДОЙ РАСПОЗНАННОЙ
// петли. Петля, которую он не распознал, требований не получает и молчит вместе
// с ним: «записи нет» и «петли нет» дают один и тот же зелёный. Ровно так в
// этом дереве БЫЛА невидима петля `secretsweep` — и её невидимость нашли не
// гейтом, а глазами; сегодня она видна и запись несёт (задача #1264). Настоящее
// время здесь держалось дольше своего предмета, и это ровно тот класс, который
// стережёт сам гейт.
//
// Различитель при этом — НЕ форма записи канала, и здесь стояло обратное.
// Прежняя редакция объявляла несущей деталью выбор между `<-ticker.C` и `<-c`,
// потому что голого идентификатора распознаватель тогда не знал. Задача #1264
// это слепое пятно закрыла (`replicafanout.go`, `tickerChannelVars`), и обе
// формы тикера видны одинаково. Видимость держит теперь ДВИЖИТЕЛЬ: тик, пауза
// или ожидание уведомления, — а не узел, которым тик записан.
func TestRetentionLoopIsVisibleToTheFanoutGate(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	if !tt.hasFile(retentionLoopFile) {
		t.Fatalf("файла петли уборки (%s) в составе дерева нет: либо уборка снята — тогда "+
			"снимается и это утверждение вместе с ней, — либо она переехала, и проба "+
			"стережёт координату, которой больше не существует", retentionLoopFile)
	}

	census, err := scanBackgroundLoops(root)
	if err != nil {
		t.Fatalf("обход фоновых петель: %v", err)
	}
	if census.FilesRead == 0 {
		t.Fatal("обход прочитал ноль файлов — его молчание ничего не значит")
	}

	var found *bgLoop
	for i := range census.Loops {
		if census.Loops[i].File == retentionLoopFile {
			found = &census.Loops[i]
			break
		}
	}
	t.Logf("перепись: фоновых петель в дереве %d; петля уборки распознана: %v",
		len(census.Loops), found != nil)

	if found == nil {
		t.Fatalf("петля уборки (%s) гейтом раскладки по репликам НЕ РАСПОЗНАНА. "+
			"Требований она не получает, и её молчание неотличимо от молчания петли с "+
			"годной записью. Смотреть надо на ДВИЖИТЕЛЬ: распознаются тик (селектор "+
			"`<-ticker.C` и переменная, в которую этот канал положили здесь же), "+
			"`<-time.After(…)`/`<-time.Tick(…)`, `time.Sleep` и ожидание уведомления "+
			"базы. Канал, пришедший ИЗ ДАННЫХ ВЫЗОВА, движителем не считается — и это "+
			"единственная форма, которой петля уборки стать не должна", retentionLoopFile)
	}
	if found.Driver == "" {
		t.Errorf("петля уборки распознана без движителя — разбор её не читал")
	}
	if found.Kind != "на-реплику" {
		t.Errorf("вид исхода петли уборки = %q, ожидался «на-реплику»: уборка есть условный "+
			"оператор с пределом партии и клеймом строк, поэтому вторая реплика уносит "+
			"только остаток", found.Kind)
	}
	if found.Bad != "" {
		t.Errorf("запись петли уборки негодна: %s", found.Bad)
	}
}

// TestRetentionLoopFormIsTheRecognisedOne — чем ДЕРЖИТСЯ видимость петли уборки.
//
// Проба выше утверждает, что петля видна. Эта — чем именно: движителем, а не
// узлом, которым движитель записан.
//
// # Здесь стояло обратное, и оно САМО назвало свой предикат истечения
//
// Прежняя редакция пиннила слепое пятно распознавателя: селектор `<-ticker.C`
// распознаётся, голый идентификатор `<-c` — нет, — и писала прямо, что,
// расширив распознаватель, следующий читатель узнает отсюда, что выбор формы в
// петле уборки перестал быть несущим. Растяжка сработала: задача #1264 пятно
// закрыла осознанно (её приёмка объявила расширение в объёме — `§5.2`
// `expired-credential-reclaim.md`), и утверждение перевёрнуто, а не ослаблено.
//
// # Почему третий случай обязателен
//
// Два первых ждут `true`. Распознаватель, отвечающий `true` на всё, прошёл бы их
// оба — отрицание без положительного контроля есть отсутствие утверждения, а
// положительный без отрицания есть его зеркало. Третий случай и есть та
// узость, ради которой расширение писалось узким.
//
// Полное доказательство расширения в обе стороны принадлежит НЕ этой пробе, а
// `replicafanout_injection_test.go` (F · законный близнец · третья сторона):
// оно судит гейт целиком, на синтетическом дереве. Здесь — ровно то, что
// связывает соседнее утверждение о петле УБОРКИ, и потому живёт рядом с ним.
func TestRetentionLoopFormIsTheRecognisedOne(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params string
		body   string
		want   bool
	}{
		{
			name: "чтение через селектор с полем-каналом — распознаётся",
			body: "ticker := time.NewTicker(d)\nfor {\nselect {\ncase <-ctx.Done():\nreturn\ncase <-ticker.C:\nf()\n}\n}",
			want: true,
		},
		{
			name: "канал тикера, положенный в переменную ЗДЕСЬ ЖЕ, — распознаётся (пятно закрыто #1264)",
			body: "c := time.NewTicker(d).C\nfor {\nselect {\ncase <-ctx.Done():\nreturn\ncase <-c:\nf()\n}\n}",
			want: true,
		},
		{
			name:   "канал ИЗ ДАННЫХ ВЫЗОВА — НЕ распознаётся, и на этом держится смысл двух первых",
			params: "in <-chan int",
			body:   "c := in\nfor {\nselect {\ncase <-ctx.Done():\nreturn\ncase v := <-c:\n_ = v\n}\n}",
			want:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package p\n\nimport \"time\"\n\nfunc Start(" + tc.params + ") {\n" + tc.body + "\n}\n"
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
			if err != nil {
				t.Fatalf("разбор синтетики: %v", err)
			}
			got := len(loopsInFile(fset, f, "p.go")) > 0
			if got != tc.want {
				t.Fatalf("распознана=%v, ожидалось %v", got, tc.want)
			}
		})
	}
}
