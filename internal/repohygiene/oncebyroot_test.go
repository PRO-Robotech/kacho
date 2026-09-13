// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// oncebyroot_test.go — обход дерева, НЕ зависящий от пробы, считается один раз
// на процесс.
//
// # Предмет, и он измерен, а не предположен
//
// Гейты этого пакета обходят дерево каждый сам: помощник обхода у каждого свой,
// и это решение принято осознанно (`artifactgates/doc.go`: связывать пакеты ради
// двадцати строк дороже, чем повторить их). Плата за него — не в том, что
// помощник повторён, а в том, что ОДНО И ТО ЖЕ дерево обходится заново на
// КАЖДУЮ пробу одного и того же гейта: вердикт у всех проб одинаков by
// construction, потому что дерево между ними не меняется.
//
// Профиль процессора пакета (`go test -cpuprofile ... -short -count=1`, без
// `-race`: компилятора C на машине замера нет, поэтому доля, которую даёт именно
// `-race`, локально НЕ измерима — вердикт по ней даст только конвейер):
//
//	CPU пакета                       220.4 с (32 потока, 17.8 с стены)
//	go/parser.ParseFile               89.5 с — 40.6 %
//	regexp.(*Regexp).doExecute        72.3 с — 32.8 %
//	io/fs.WalkDir (rootedWalk)        15.5 с —  7.0 %
//	runtime.gcBgMarkWorker            11.8 с —  5.4 %
//	os.ReadFile                        9.4 с —  4.3 %
//	os/exec.(*Cmd).Run                 1.3 с —  0.6 %  ← подпроцессы НЕ предмет
//
// Один проход разбора по всему корпусу Go дерева (3589 файлов, 45.3 МБ) стоит
// 0.61 с. Значит 89.5 с — это ≈145 проходов по корпусу: предмет починки
// ПОВТОРНОСТЬ, а не объём дерева.
//
// # ЕДИНИЦА СЧЁТА: сумма времени проб работой НЕ ЯВЛЯЕТСЯ, и я на этом ошибся
//
// Первая прикидка бралась из `go test -json`: сумма Elapsed проб одного stem
// минус самая дорогая его проба дала 99.6 с из 353.6 с (28.2 %), и это число
// НЕВЕРНО как оценка снимаемой работы. При 32-кратном параллелизме Elapsed
// пробы — это преимущественно ОЖИДАНИЕ процессора, а не её собственный расход:
// четыре пробы гейта полос сообщали по 6.6 с каждая, тогда как их обходы стоили
// вместе меньше секунды CPU.
//
// Проверено прямым замером, а не рассуждением: изолированный прогон девяти проб,
// переведённых на эту обёртку (по три прогона на сторону, разброс < 0.1 с):
//
//	до   4.55 с стены · 19.20 с CPU
//	после 4.18 с стены · 12.95 с CPU   → −6.25 с CPU, −33 % их собственного
//
// В пакете целиком это −6.3 с из ≈265 с, то есть ≈2.4 %, и отдельно от разброса
// полного прогона (±10 с на трёх прогонах) эта величина НЕ отделяется. Заявлять
// по ней ускорение пакета нельзя; заявляется ровно измеренное — треть CPU этих
// девяти проб.
//
// # Что из 145 проходов остаётся — и почему не этой обёрткой
//
// Повтор разбора живёт в основном МЕЖДУ гейтами, а не внутри одного: разбор
// зовут ≈150 разных сканирующих функций, и каждая спрашивает корпус о своём.
// Обёртка ниже этого не трогает by construction — она мемоизирует обход ОДНОГО
// гейта. Общий разобранный корпус на пакет закрыл бы остальное (потолок — 40.6 %
// CPU), но требует единого набора позиций на 229 мест вызова `parser.ParseFile`
// и удержания разобранного корпуса в памяти; ни цена памяти под `-race`, ни
// поведение на ранере локально не проверяемы, и «медленно» способно обратиться в
// «не выполнилось». Это отдельное решение, а не следствие этой правки.
//
// # Что делает onceByRoot и чего он НЕ делает
//
// Обёртка считает compute РОВНО ОДИН РАЗ на процесс и отдаёт тот же результат
// всем пробам. Это НЕ кеш между прогонами: `go test` кеширует вердикт пакета, и
// именно поэтому пакет отказывается работать на кешируемом прогоне
// (cachedverdictmain_test.go). Здесь предмет другой — повтор ВНУТРИ одного
// прогона, где дерево заведомо одно и то же.
//
// # Почему отказ на втором, ДРУГОМ корне
//
// Инъекционные пробы подают синтетический корень, и ответ про чужое дерево был
// бы неотличим от ответа про своё: гейт печатал бы перепись настоящего дерева
// над синтетикой и молчал. Поэтому второй иной корень — ОТКАЗ с обоими путями в
// тексте, а не тихий ответ. Инъекция обязана звать НЕмемоизированную форму.
//
// # Почему параметрический тип, а не копия на гейт
//
// Тип результата у каждого гейта свой (перечень мест · корпус файлов · ведомость),
// а тело обёртки — одно и то же. Копия на гейт означала бы восемь строк
// синхронизации, повторённых столько раз, сколько гейтов, — и первая же из них,
// написанная иначе, дала бы гонку, которую видно только под `-race`.
//
// # Значение отдаётся ТОЛЬКО ДЛЯ ЧТЕНИЯ
//
// Один и тот же срез (или карта) достаётся всем пробам, и пробы идут
// параллельно. Проба, дописывающая в него или сортирующая его, портит вердикт
// соседней — и под `-race` это гонка. Изменять результат внутри compute
// (сортировка перед возвратом) законно: до первого возврата его не видит никто.
package repohygiene

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// onceByRoot возвращает форму compute, считающую результат один раз на процесс.
//
// Второй вызов с ТЕМ ЖЕ корнем отдаёт сохранённый результат (включая ошибку:
// отказ тоже не пересчитывается — иначе перепись печаталась бы дважды). Второй
// вызов с ДРУГИМ корнем — ошибка, называющая оба пути.
func onceByRoot[T any](compute func(root string) (T, error)) func(root string) (T, error) {
	var (
		mu      sync.Mutex
		done    bool
		forRoot string
		val     T
		err     error
	)
	return func(root string) (T, error) {
		mu.Lock()
		defer mu.Unlock()
		if done {
			if root != forRoot {
				var zero T
				return zero, fmt.Errorf("обход уже посчитан для корня %s, "+
					"спрошен %s: отдать первый результат значило бы напечатать "+
					"перепись ЧУЖОГО дерева — инъекция обязана звать "+
					"немемоизированную форму", forRoot, root)
			}
			return val, err
		}
		val, err = compute(root)
		forRoot, done = root, true
		return val, err
	}
}

// onceByRootUsingProbe — та же однократность для обхода, который сообщает об
// отказе ЧЕРЕЗ *testing.T текущей пробы.
//
// Помощники этого пакета устроены так по соглашению (`repoRoot`, `skipPath`,
// `corelibPackageGoFiles`), и переписывать их на возврат ошибки ради мемоизации
// значило бы менять больше, чем предмет правки.
//
// # Отказ НЕ мемоизируется, и это не упущение
//
// `t.Fatalf` уводит горутину (runtime.Goexit), значит compute не возвращается и
// результат не сохраняется. Следующая проба посчитает обход заново и УПАДЁТ САМА:
// ни одна не получит пустого значения молча. Цена — повторный обход на красном
// дереве; она принята, потому что обратное (запомнить пустой результат) сделало бы
// молчание соседних проб неотличимым от их успеха.
func onceByRootUsingProbe[T any](compute func(t *testing.T, root string) T) func(t *testing.T, root string) T {
	var (
		mu      sync.Mutex
		done    bool
		forRoot string
		val     T
	)
	return func(t *testing.T, root string) T {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if done {
			if root != forRoot {
				t.Fatalf("обход уже посчитан для корня %s, спрошен %s: отдать первый "+
					"результат значило бы напечатать перепись ЧУЖОГО дерева — "+
					"инъекция обязана звать немемоизированную форму", forRoot, root)
			}
			return val
		}
		computed := compute(t, root)
		val, forRoot, done = computed, root, true
		return val
	}
}

// TestOnceByRootComputesExactlyOnceForTheSameRoot — повторный вопрос про тот же
// корень обхода не производит.
func TestOnceByRootComputesExactlyOnceForTheSameRoot(t *testing.T) {
	t.Parallel()

	runs := 0
	get := onceByRoot(func(root string) (string, error) {
		runs++
		return "corpus of " + root, nil
	})

	for i := range 4 {
		got, err := get("/tree")
		if err != nil {
			t.Fatalf("вызов %d: %v", i+1, err)
		}
		if got != "corpus of /tree" {
			t.Fatalf("вызов %d отдал %q", i+1, got)
		}
	}
	t.Logf("вызовов 4, обходов %d", runs)
	if runs != 1 {
		t.Fatalf("обходов %d при 4 вызовах — повтор не снят, и замер, ради которого "+
			"обёртка написана, остаётся верным", runs)
	}
}

// TestOnceByRootKeepsTheRefusalAndDoesNotRetry — отказ тоже считается один раз.
//
// Иначе пять проб напечатали бы пять переписей отказа, и цена обхода вернулась
// бы целиком ровно в тот прогон, где дерево сломано.
func TestOnceByRootKeepsTheRefusalAndDoesNotRetry(t *testing.T) {
	t.Parallel()

	want := errors.New("индекс не прочитан")
	runs := 0
	get := onceByRoot(func(string) (int, error) {
		runs++
		return 0, want
	})

	for i := range 3 {
		if _, err := get("/tree"); !errors.Is(err, want) {
			t.Fatalf("вызов %d отдал %v, ожидался сохранённый отказ", i+1, err)
		}
	}
	t.Logf("вызовов 3, обходов %d", runs)
	if runs != 1 {
		t.Fatalf("обходов %d — отказ пересчитывается", runs)
	}
}

// TestOnceByRootRefusesASecondDifferentRoot — про чужое дерево обёртка не
// отвечает.
//
// Проба — не украшение: без отказа инъекция, забывшая позвать
// немемоизированную форму, получила бы перепись НАСТОЯЩЕГО дерева над
// синтетикой и прошла бы зелёной, ничего не проверив.
func TestOnceByRootRefusesASecondDifferentRoot(t *testing.T) {
	t.Parallel()

	get := onceByRoot(func(root string) (string, error) { return root, nil })

	if _, err := get("/real"); err != nil {
		t.Fatalf("первый корень: %v", err)
	}
	got, err := get("/synthetic")
	if err == nil {
		t.Fatalf("второй, ДРУГОЙ корень отдал %q без отказа — ответ про чужое "+
			"дерево неотличим от ответа про своё", got)
	}
	for _, want := range []string{"/real", "/synthetic"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("текст отказа не называет %s: %v", want, err)
		}
	}
	t.Logf("отказ: %v", err)
}

// TestOnceByRootComputesOnceUnderConcurrentProbes — пробы идут параллельно, и
// обход обязан состояться один раз на всех, а не по одному на пробу.
func TestOnceByRootComputesOnceUnderConcurrentProbes(t *testing.T) {
	t.Parallel()

	const callers = 16
	var mu sync.Mutex
	runs := 0
	get := onceByRoot(func(root string) (string, error) {
		mu.Lock()
		runs++
		mu.Unlock()
		return root, nil
	})

	var wg sync.WaitGroup
	results := make([]string, callers)
	errs := make([]error, callers)
	wg.Add(callers)
	for i := range callers {
		go func() {
			defer wg.Done()
			results[i], errs[i] = get("/tree")
		}()
	}
	wg.Wait()

	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("вызывающий %d: %v", i, errs[i])
		}
		if results[i] != "/tree" {
			t.Fatalf("вызывающий %d получил %q", i, results[i])
		}
	}
	mu.Lock()
	got := runs
	mu.Unlock()
	t.Logf("вызывающих %d, обходов %d", callers, got)
	if got != 1 {
		t.Fatalf("обходов %d при %d одновременных вызывающих", got, callers)
	}
}

// TestOnceByRootUsingProbeComputesOnceAndKeepsNoFailedResult — успех считается
// один раз; уход горутины (то, чем работает t.Fatalf) НЕ запоминается.
//
// Вторая половина — не украшение: запомни обёртка пустое значение, соседняя проба
// получила бы его молча, и «ноль находок» перестало бы отличаться от «ноль
// прочитанного» ровно на красном дереве.
func TestOnceByRootUsingProbeComputesOnceAndKeepsNoFailedResult(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	runs := 0
	get := onceByRootUsingProbe(func(_ *testing.T, root string) string {
		mu.Lock()
		runs++
		n := runs
		mu.Unlock()
		if n == 1 {
			runtime.Goexit() // ровно то, что делает t.Fatalf
		}
		return "corpus of " + root
	})

	// Первый вызывающий уходит, как ушла бы упавшая проба.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = get(t, "/tree")
	}()
	<-done

	mu.Lock()
	afterGoexit := runs
	mu.Unlock()
	if afterGoexit != 1 {
		t.Fatalf("обходов после ухода %d, ожидался 1", afterGoexit)
	}

	// Вторая проба обязана посчитать заново, а не получить пустое значение.
	for i := range 3 {
		if got := get(t, "/tree"); got != "corpus of /tree" {
			t.Fatalf("вызов %d после ухода отдал %q", i+1, got)
		}
	}
	mu.Lock()
	total := runs
	mu.Unlock()
	t.Logf("обходов всего %d (уход 1 + пересчёт 1)", total)
	if total != 2 {
		t.Fatalf("обходов %d — либо отказ запомнен, либо успех пересчитывается", total)
	}
}
