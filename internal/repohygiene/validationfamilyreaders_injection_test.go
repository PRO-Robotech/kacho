// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// validationfamilyreaders_injection_test.go — ДОКАЗАТЕЛЬСТВО способности переписи
// падать и молчать (задача #1255, приёмка PROTO-1, сценарий Л-06).
//
// # ЗАЧЕМ ИНЪЕКЦИЯ ИМЕННО ЗДЕСЬ
//
// Перепись Л-04 утверждает ОТРИЦАНИЕ («проверок, читающих снятое семейство, ноль»),
// а отрицание без доказательства способности упасть остаётся утверждением о себе
// самом: предикат, не находящий НИЧЕГО НИКОГДА, даёт ровно тот же зелёный.
//
// # ПОЛНОТА ПЕРЕЧНЯ ФОРМ — ПО ОДНОЙ ИНЪЕКЦИИ НА ФОРМУ
//
// Предикат ищет предмет ПО ОБРАЗЦУ, поэтому обязан знать ВСЕ законные формы его
// записи; форма, о которой он не знает, даёт не красное и не зелёное — она даёт
// молчание, и всё записанное в ней оказывается вне наблюдения.
//
// Форм записи опции в этом дереве три, и каждая проверяется отдельно:
//
//	строковый литерал        MandatoryOption: "(required) = true"
//	регулярное выражение     regexp.MustCompile(`\(\s*required\s*\)\s*=\s*true`)
//	текст контракта в пробе  "message M { string s = 1 [(pattern) = \"…\"]; }"
//
// Вторая — та, на которой сужение первых редакций приёмки НЕ ВИДЕЛО ровно тот
// экземпляр, ради которого полоса написана: буквальной подстроки `(required) = true`
// в прод-анализаторе формы подписки не было ни разу.
//
// # ЗАКОННЫЕ БЛИЗНЕЦЫ — НЕ ПОБАЙТОВЫЕ КОПИИ
//
// Молчание проверяется на том, что от находки отличается ОДНОЙ осью, а не на
// пустом файле: иначе доказано было бы лишь «на пустоте молчит».

// injectionCorpusWithout — законный корпус: файл читает контракты и не называет
// ни одной снятой опции.
func injectionCorpusWithout() map[string]string {
	return map[string]string{
		"internal/x/legal_test.go": "package x\n\n" +
			"// читает контракт, но снятого семейства не называет\n" +
			"var body, _ = os.ReadFile(\"proto/kacho/cloud/vpc/v1/network_service.proto\")\n" +
			"var re = regexp.MustCompile(`\\(\\s*deprecated\\s*\\)\\s*=\\s*true`)\n",
	}
}

func requireNamed(t *testing.T, findings []string, want string) {
	t.Helper()
	for _, f := range findings {
		if f == want {
			return
		}
	}
	t.Fatalf("находка не названа поимённо: ждали %q, получили %v", want, findings)
}

func requireQuiet(t *testing.T, findings []string, c familyReaderCensus) {
	t.Helper()
	if len(findings) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %v", findings)
	}
	if c.scanned == 0 {
		t.Fatal("молчание на ПУСТОМ обходе ничего не доказывает — осмотрено ноль файлов")
	}
}

// TestFamilyReaderCensusCatchesEveryFormOfTheOption — сторона дефекта, по одной
// инъекции на каждую форму записи.
func TestFamilyReaderCensusCatchesEveryFormOfTheOption(t *testing.T) {
	forms := []struct {
		name string
		body string
	}{
		{
			name: "строковый литерал в конфигурации",
			body: "package x\n\nvar cfg = Opts{\n" +
				"\tProtoRoot:       \"proto\",\n" +
				"\tMandatoryOption: \"(required) = true\",\n}\n",
		},
		{
			name: "РЕГУЛЯРНОЕ ВЫРАЖЕНИЕ (форма, на которой сужался предикат приёмки)",
			body: "package x\n\n" +
				"var src, _ = os.ReadFile(\"form.proto\")\n" +
				"var re = regexp.MustCompile(`\\(\\s*required\\s*\\)\\s*=\\s*true`)\n",
		},
		{
			name: "текст контракта внутри пробы",
			body: "package x\n\nvar fixture = []byte(\"message M {\\n" +
				"  string name = 2 [(pattern) = \\\"[a-z]+\\\"];\\n}\\n\")\n" +
				"var _ = os.ReadFile(\"x.proto\")\n",
		},
		{
			name: "групповая опция на oneof",
			body: "package x\n\nvar fixture = \"oneof target {\\n" +
				"    option (exactly_one) = true;\\n  }\"\n" +
				"var _ = filepath.Walk(\"proto\", nil)\n",
		},
	}

	for _, f := range forms {
		t.Run(f.name, func(t *testing.T) {
			corpus := injectionCorpusWithout()
			corpus["internal/x/injected_test.go"] = f.body
			findings, c := auditValidationFamilyReaders(corpus)
			requireNamed(t, findings, "internal/x/injected_test.go")
			if c.scanned != 2 {
				t.Fatalf("осмотрено %d файлов, ждали 2 — перепись не сходится с корпусом", c.scanned)
			}
			t.Logf("перепись инъекции: осмотрено %d · называют опцию %d · обращаются к контрактам %d · находок %d",
				c.scanned, c.named, c.reaching, c.findings)
		})
	}
}

// TestFamilyReaderCensusIsQuietOnLegalTwins — сторона молчания. Каждый близнец
// отличается от находки РОВНО ОДНОЙ осью, и ось названа.
func TestFamilyReaderCensusIsQuietOnLegalTwins(t *testing.T) {
	twins := []struct {
		name string
		body string
		// ось — то единственное, чем близнец отличается от находки.
		axis string
	}{
		{
			name: "идентификатор `value` в позиции СТРЕЛКИ",
			axis: "знак присваивания двойной (`=>`), а не одинарный",
			body: "package x\n\nvar _ = os.ReadFile(\"x.proto\")\n" +
				"var out = xs.map((value) => (value < 0 ? 0 : value))\n",
		},
		{
			name: "идентификатор `value` в позиции СРАВНЕНИЯ",
			axis: "знак присваивания двойной (`==`), а не одинарный",
			body: "package x\n\nvar _ = os.ReadFile(\"x.proto\")\n" +
				"func f() { if strings.TrimSpace(value) == \"\" { return } }\n",
		},
		{
			name: "имя функции перед скобкой",
			axis: "скобке предшествует ИМЯ — это вызов, а не объявление опции",
			body: "package x\n\nvar _ = os.ReadFile(\"x.proto\")\n" +
				"var n = computeSize(size) = 0\n",
		},
		{
			name: "опция названа, но файл контрактов НЕ ЧИТАЕТ",
			axis: "нет обращения к контракту — значит это не проверка о нём",
			body: "package x\n\nvar doc = \"поле помечали `(required) = true`\"\nvar s = doc + \"\"\n",
		},
		{
			name: "опция названа только в КОММЕНТАРИИ",
			axis: "разбор снятия обязан называть снятое и находкой не является",
			body: "package x\n\n// прежде здесь стояло `(required) = true` — снято\n" +
				"var _ = os.ReadFile(\"x.proto\")\n",
		},
		{
			name: "документация называет опцию",
			axis: "документация вне охвата по решению, и число пропущенного печатается",
			body: "Поле объявлялось `(required) = true` и читало `x.proto`.\n",
		},
	}

	for _, tw := range twins {
		t.Run(tw.name, func(t *testing.T) {
			corpus := injectionCorpusWithout()
			path := "internal/x/twin_test.go"
			if strings.Contains(tw.name, "документация") {
				path = "docs/twin.md"
			}
			corpus[path] = tw.body
			findings, c := auditValidationFamilyReaders(corpus)
			requireQuiet(t, findings, c)
			t.Logf("молчит; ось отличия: %s (осмотрено %d, документации пропущено %d)",
				tw.axis, c.scanned, c.skippedDoc)
		})
	}
}

// TestFamilyReaderExemptionExpiresWithItsSubject — послабление живёт РОВНО пока
// живёт его предмет.
//
// Запись, которой больше нечего исключать, — находка, а не безобидный остаток:
// иначе она молча унаследует следующую слепую зону. Проверяются обе стороны.
func TestFamilyReaderExemptionExpiresWithItsSubject(t *testing.T) {
	const exempt = "internal/repohygiene/validationfamilyreaders_injection_test.go"
	if _, ok := familyReaderExemptions[exempt]; !ok {
		t.Fatalf("предпосылка пробы исчезла: %s больше не значится в ведомости", exempt)
	}

	t.Run("предмет ЕСТЬ — послабление молчит и засчитано", func(t *testing.T) {
		corpus := injectionCorpusWithout()
		corpus[exempt] = "package x\n\nvar _ = os.ReadFile(\"x.proto\")\n" +
			"var re = regexp.MustCompile(`\\(\\s*required\\s*\\)\\s*=\\s*true`)\n"
		findings, c := auditValidationFamilyReaders(corpus)
		if len(findings) != 0 {
			t.Fatalf("файл с живым предметом объявлен находкой: %v", findings)
		}
		if c.exempt != 1 {
			t.Fatalf("освобождений засчитано %d, ждали 1 — послабление не сработало", c.exempt)
		}
	})

	t.Run("предмет ИСЧЕЗ — послабление объявлено просроченным", func(t *testing.T) {
		corpus := injectionCorpusWithout()
		// Тот же путь, но опции в нём больше нет: исключать стало нечего.
		corpus[exempt] = "package x\n\nvar _ = os.ReadFile(\"x.proto\")\n"
		findings, c := auditValidationFamilyReaders(corpus)
		if len(findings) != 1 || !strings.Contains(findings[0], "ПОСЛАБЛЕНИЕ ПОТЕРЯЛО ПРЕДМЕТ") {
			t.Fatalf("истёкшее послабление не объявлено находкой: %v", findings)
		}
		if !strings.Contains(findings[0], exempt) {
			t.Fatalf("находка не называет записи: %v", findings)
		}
		if c.exempt != 0 {
			t.Fatalf("освобождений засчитано %d при исчезнувшем предмете", c.exempt)
		}
		t.Logf("просрочено: %s", findings[0])
	})
}

// TestTreeCorpusPartitionRefusesToLoseFilesSilently — доказательство способности
// ПРЕДПОСЫЛКИ ОХВАТА падать и молчать.
//
// # Почему у арифметики отдельная инъекция
//
// Предпосылка «сумма трёх исходов сходится с составом индекса» выглядит
// самоочевидной, и ровно поэтому её никто не проверяет. Между тем она стережёт
// не описку в сложении, а МОЛЧАЛИВУЮ ПОТЕРЮ: до этой правки охват задавался
// рукописным перечнем девяти каталогов верхнего уровня, и 89 отслеживаемых
// файлов (`tests/`, `.github/`, `docs/`, корень) не читались вовсе — при зелёном
// гейте и без единой строки об этом. Перечень ВКЛЮЧАЕМОГО расходится с деревом
// молча; сумма, сверенная с индексом, — единственное, что об этом скажет.
//
// Оси четыре, и каждая — отдельный исход разбиения, а не оттенок одного.
func TestTreeCorpusPartitionRefusesToLoseFilesSilently(t *testing.T) {
	const root = "/repo"
	ok := func(path string) ([]byte, error) { return []byte("package x\n"), nil }

	t.Run("законный близнец: состав сходится — отказа нет", func(t *testing.T) {
		tracked := []string{root + "/internal/a.go", root + "/tests/b.py", root + "/.github/c.yml"}
		corpus, cov, err := partitionTreeCorpus(root, tracked, familySubjectPrefixes, ok)
		if err != nil {
			t.Fatalf("отказ на сходящемся составе: %v", err)
		}
		if len(corpus) != 3 || cov.tracked != 3 || cov.subject != 0 || cov.unreadable != 0 {
			t.Fatalf("перепись неверна: корпус %d, отслеживается %d, предмет %d, не прочитано %d",
				len(corpus), cov.tracked, cov.subject, cov.unreadable)
		}
		// Полоса, ради которой охват расширен: она обязана быть В КОРПУСЕ.
		for _, want := range []string{"tests/b.py", ".github/c.yml"} {
			if _, in := corpus[want]; !in {
				t.Fatalf("%s вне корпуса — охват снова сузился до перечня каталогов", want)
			}
		}
	})

	t.Run("ПОТЕРЯ МОЛЧА: два пути схлопнулись в один ключ — отказ назван", func(t *testing.T) {
		// Реальная форма потери: состав индекса даёт два элемента, а корпус —
		// отображение, поэтому совпавший ключ поглощает предыдущий и число
		// осмотренного тихо уменьшается на единицу.
		tracked := []string{root + "/internal/a.go", root + "/internal/./a.go"}
		_, cov, err := partitionTreeCorpus(root, tracked, familySubjectPrefixes, ok)
		if err == nil {
			t.Fatal("потеря файла не объявлена: разбиение отдало корпус, из которого файл исчез молча")
		}
		if !strings.Contains(err.Error(), "выпала из наблюдения молча") {
			t.Fatalf("отказ не называет предмета: %v", err)
		}
		if cov.tracked != 2 {
			t.Fatalf("перепись не назвала состава индекса: отслеживается %d", cov.tracked)
		}
		t.Logf("названо: %v", err)
	})

	t.Run("ПРЕДМЕТ исключён и ПОСЧИТАН, а не потерян", func(t *testing.T) {
		tracked := []string{root + "/proto/kacho/cloud/x.proto", root + "/pkg/api/x.pb.go", root + "/internal/a.go"}
		corpus, cov, err := partitionTreeCorpus(root, tracked, familySubjectPrefixes, ok)
		if err != nil {
			t.Fatalf("отказ при исключении предмета: %v", err)
		}
		if cov.subject != 2 || len(corpus) != 1 {
			t.Fatalf("предмет посчитан %d, корпус %d — ждали 2 и 1", cov.subject, len(corpus))
		}
	})

	t.Run("НЕПРОЧТЁННОЕ названо поимённо, а не проглочено", func(t *testing.T) {
		tracked := []string{root + "/internal/a.go", root + "/internal/gone.go"}
		read := func(path string) ([]byte, error) {
			if strings.HasSuffix(path, "gone.go") {
				return nil, errNoSuchFileForInjection{}
			}
			return []byte("package x\n"), nil
		}
		_, cov, err := partitionTreeCorpus(root, tracked, familySubjectPrefixes, read)
		if err != nil {
			t.Fatalf("непрочтённый файл объявлен потерей: %v", err)
		}
		if cov.unreadable != 1 || len(cov.unreadableNames) != 1 ||
			!strings.Contains(cov.unreadableNames[0], "internal/gone.go") {
			t.Fatalf("непрочтённое не названо: %d — %v", cov.unreadable, cov.unreadableNames)
		}
	})
}

// errNoSuchFileForInjection — отказ чтения для инъекции выше. Своим типом, а не
// текстовой строкой: инъекция обязана подавать читателю НАСТОЯЩИЙ отказ, а не
// его изображение.
type errNoSuchFileForInjection struct{}

func (errNoSuchFileForInjection) Error() string { return "нет такого файла" }
