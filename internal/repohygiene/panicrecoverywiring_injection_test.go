// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт восстановления паники СПОСОБЕН упасть — и что
// падает он на существе, а не на форме.
//
// Обе пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву (auditPanicRecoveryWiring),
// на синтетическом дереве: проба, повторяющая логику гейта своей копией,
// доказывала бы свойство копии.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// синтетическое общее звено — распознаётся по существу: возвращает
// grpc-интерсептор и зовёт recover().
const synthSharedLink = `package grpcsrv

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func UnaryPanicRecovery() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return h(ctx, req)
	}
}

func StreamPanicRecovery() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return h(srv, ss)
	}
}
`

// ЛОВУШКА ТЕМЫ. Файл называется recovery.go, функция называется
// startLRORecovery, в тексте пять раз слово Recovery — и к панике всё это не
// имеет никакого отношения: это разрешитель осиротевших операций. Текстовый
// гейт нашёл бы здесь «recovery» и позеленел бы при снятой защите.
const synthLRORecoveryDecoy = `package main

// Durable LRO recovery wiring: recovery of orphaned operations after a restart.
// Recovery here means operation recovery, not panic recovery.
func startLRORecovery() error {
	// RecoverAll before serving traffic; periodic Run as a backstop.
	return nil
}
`

// composition root СО звеном.
const synthRootWired = `package main

import (
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/corelib/grpcsrv"
)

func serve() {
	publicUnary := []grpc.UnaryServerInterceptor{
		grpcsrv.UnaryPanicRecovery(),
		authzUnary(),
	}
	publicStream := []grpc.StreamServerInterceptor{
		grpcsrv.StreamPanicRecovery(),
	}
	_ = grpcsrv.NewServer(
		grpc.ChainUnaryInterceptor(publicUnary...),
		grpc.ChainStreamInterceptor(publicStream...),
	)
}

func authzUnary() grpc.UnaryServerInterceptor { return nil }
`

// composition root БЕЗ звена — та же форма, звено снято.
const synthRootUnwired = `package main

import (
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/corelib/grpcsrv"
)

func serve() {
	publicUnary := []grpc.UnaryServerInterceptor{
		authzUnary(),
	}
	publicStream := []grpc.StreamServerInterceptor{}
	_ = grpcsrv.NewServer(
		grpc.ChainUnaryInterceptor(publicUnary...),
		grpc.ChainStreamInterceptor(publicStream...),
	)
}

func authzUnary() grpc.UnaryServerInterceptor { return nil }
`

// synthTree строит минимальное дерево: go.mod (по нему резолвятся импорты
// модуля), общее звено и один сервис с одним композиционным корнем.
//
// Дерево инициализируется как репозиторий, и файлы кладутся в индекс: гейт берёт
// состав у `pkg/treecorpus`, то есть у git, а не у диска. Фикстура, которая
// этого не делает, оставляет гейт с пустым составом — и он молчит не потому, что
// нарушения нет, а потому, что смотреть было не на что. Направление «гейт обязан
// покраснеть» такую пустоту не отличило бы от исправного дерева.
//
// Коммита нет намеренно: `git ls-files` показывает и проиндексированное, а
// `commit` потребовал бы личности автора, которой в чистом окружении прогона нет
// (ровно этот класс уже ронял конвейер: проба, чья предпосылка невыполнима там,
// где она исполняется).
func synthTree(t *testing.T, rootGo string, extra map[string]string) string {
	t.Helper()
	return synthTreeAt(t, t.TempDir(), rootGo, extra)
}

// synthTreeNested — ТО ЖЕ дерево, но вложенное в каталог со своим `go.mod`.
//
// Отличается от synthTree РОВНО одним фактом — где лежит копия, — и в этом весь
// смысл: вердикт обязан совпасть. Объемлющий модуль назван путём продукта
// намеренно: в жизни это другой checkout того же продукта (рабочая копия под
// `project/kacho/tmp/`), а не посторонний проект. Пакетов он не несёт, поэтому
// разрешение импорта, ушедшее за границу, попадает в несуществующий каталог —
// ровно то, что наблюдалось (#2429).
func synthTreeNested(t *testing.T, rootGo string, extra map[string]string) string {
	t.Helper()
	parent := t.TempDir()
	if err := os.WriteFile(filepath.Join(parent, "go.mod"),
		[]byte("module github.com/PRO-Robotech/kacho\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("объемлющий go.mod: %v", err)
	}
	inner := filepath.Join(parent, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", inner, err)
	}
	return synthTreeAt(t, inner, rootGo, extra)
}

func synthTreeAt(t *testing.T, root, rootGo string, extra map[string]string) string {
	t.Helper()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("go.mod", "module github.com/PRO-Robotech/kacho\n\ngo 1.25\n")
	write("pkg/grpcsrv/panicrecovery.go", synthSharedLink)
	write("services/demo/cmd/demo/main.go", rootGo)
	for rel, body := range extra {
		write(rel, body)
	}
	synthTrack(t, root)
	return root
}

// synthTrack делает временный каталог рабочим деревом git и индексирует всё
// записанное. Отказ здесь — Fatal, а не пропуск: гейт на неотслеживаемом дереве
// вернул бы «ноль находок», и проба зазеленела бы на несделанной работе.
func synthTrack(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
	} {
		cmd := gitenv.Command(root, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v в синтетическом дереве: %v\n%s", args, err, out)
		}
	}
}

// TestPanicRecoveryGateRedOnInjectedDefect — направление (а): звено снято ->
// гейт краснеет И НАЗЫВАЕТ КООРДИНАТУ. Без координаты находка не действие.
func TestPanicRecoveryGateRedOnInjectedDefect(t *testing.T) {
	t.Parallel()
	root := synthTree(t, synthRootUnwired, nil)
	res := auditPanicRecoveryWiring(t, root)
	t.Log(res.summary)

	if len(res.findings) == 0 {
		t.Fatalf("звено снято, а гейт молчит — он не способен упасть.\n%s", res.summary)
	}
	joined := strings.Join(res.findings, "\n")
	if !strings.Contains(joined, "services/demo/cmd/demo/main.go") {
		t.Fatalf("находка не называет файл — по ней нечего чинить:\n%s", joined)
	}
	if !strings.Contains(joined, "unary") || !strings.Contains(joined, "stream") {
		t.Fatalf("находка не называет вид цепочки (unary/stream):\n%s", joined)
	}
	t.Logf("направление (а): гейт покраснел и назвал координату:\n%s", joined)
}

// TestPanicRecoveryGateSilentOnLawfulSameShape — направление (б): законная
// конструкция ТОЙ ЖЕ ФОРМЫ гейта не задевает.
//
// «Той же формы» здесь взято в самом неудобном для гейта виде: рядом с
// провязанным листенером лежит файл recovery.go с функцией startLRORecovery и
// пятью упоминаниями слова Recovery. Если бы гейт читал текст, он засчитал бы
// этот файл за защиту (ложно-зелёный на снятом звене) либо споткнулся бы о него;
// он читает исполняемую часть, поэтому не делает ни того, ни другого.
func TestPanicRecoveryGateSilentOnLawfulSameShape(t *testing.T) {
	t.Parallel()
	root := synthTree(t, synthRootWired, map[string]string{
		"services/demo/cmd/demo/recovery.go": synthLRORecoveryDecoy,
	})
	res := auditPanicRecoveryWiring(t, root)
	t.Log(res.summary)

	if len(res.findings) != 0 {
		t.Fatalf("законная конструкция объявлена находкой — гейт ловит форму, "+
			"а не существо:\n%s", strings.Join(res.findings, "\n"))
	}
	if res.covered == 0 {
		t.Fatalf("гейт не засчитал ни одного листенера — молчание означает "+
			"«не нашёл», а не «всё есть».\n%s", res.summary)
	}
	t.Logf("направление (б): гейт молчит, засчитано листенеров %d", res.covered)
}

// TestPanicRecoveryGateIgnoresLRORecoveryDecoyEvenWhenUnwired — прямая проверка
// самой ловушки: приманка лежит рядом, звено снято. Гейт обязан всё равно
// покраснеть. Эта проба и есть доказательство, что распознавание не по имени:
// без неё «гейт читает существо» осталось бы утверждением о намерении.
func TestPanicRecoveryGateIgnoresLRORecoveryDecoyEvenWhenUnwired(t *testing.T) {
	t.Parallel()
	root := synthTree(t, synthRootUnwired, map[string]string{
		"services/demo/cmd/demo/recovery.go": synthLRORecoveryDecoy,
	})
	res := auditPanicRecoveryWiring(t, root)
	if len(res.findings) == 0 {
		t.Fatalf("приманка «recovery» засчитана за защиту — гейт читает текст, "+
			"а не исполняемую часть.\n%s", res.summary)
	}
	t.Logf("ловушка не обманула гейт: %s", strings.Join(res.findings, "\n"))
}

// synthCarrierRoot — композиционный корень НА НОСИТЕЛЕ: своих серверов не
// собирает, цепочку не держит, зовёт `servicehost.Serve`. Ровно так выглядят
// пять переведённых сервисов.
const synthCarrierRoot = `package main

import (
	"context"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/corelib/servicehost"
)

func serve(ctx context.Context) error {
	return servicehost.Serve(ctx, descriptor(),
		func(reg grpc.ServiceRegistrar) {},
		func(reg grpc.ServiceRegistrar) {},
	)
}
`

// synthMuteRoot — корень, который НЕ поднимает своих листенеров и НЕ зовёт
// носитель. Он и есть неоднозначный случай: гейт «не нашёл листенеров» тут
// неотличим от «распознавание сломано», если предпосылку не проверять.
const synthMuteRoot = `package main

func serve() error { return nil }
`

// synthCarrierPkg — минимальный носитель, чтобы импорт в синтетическом дереве
// резолвился так же, как в настоящем.
const synthCarrierPkg = `package servicehost

import (
	"context"

	"google.golang.org/grpc"
)

type Registrar func(grpc.ServiceRegistrar)

func Serve(ctx context.Context, d any, public, internal Registrar) error { return nil }
`

// TestPanicRecoveryGateAcceptsACarrierBorneComponent — направление (б) для
// НОВОЙ половины предпосылки: компонент без своих листенеров законен ровно
// тогда, когда листенеры поднимает носитель.
//
// Без этой пробы предпосылка ловила бы форму («листенеров ноль») вместо существа
// («ноль, и никто не поднимает их вместо него») — и объявляла бы находкой сам
// перевод, краснея тем сильнее, чем дальше он продвинулся.
func TestPanicRecoveryGateAcceptsACarrierBorneComponent(t *testing.T) {
	t.Parallel()
	root := synthTree(t, synthCarrierRoot, map[string]string{
		"pkg/servicehost/serve.go": synthCarrierPkg,
	})
	res := auditPanicRecoveryWiring(t, root)
	t.Log(res.summary)
	if len(res.findings) != 0 {
		t.Fatalf("компонент на носителе объявлен находкой:\n%s", strings.Join(res.findings, "\n"))
	}
	if !strings.Contains(res.summary, "компонентов на носителе контура (своих листенеров нет) 1") {
		t.Fatalf("перепись не назвала компонент на носителе — «ноль находок» неотличимо "+
			"от «ноль прочитанного»:\n%s", res.summary)
	}
}

// TestPanicRecoveryGateRefusesAComponentThatServesNothing — направление (а) для
// той же половины: ни своих листенеров, ни носителя → предпосылка распознавания
// не выполнена, и молчание гейта ничего не доказывало бы.
//
// Гоняется ТА ЖЕ функция, что исполняет предпосылку в гейте
// (`componentsWithoutAListenerOrACarrier`), а не её копия: копия доказывала бы
// свойство копии. Вход — тот же, что аудит собирает по дереву.
func TestPanicRecoveryGateRefusesAComponentThatServesNothing(t *testing.T) {
	t.Parallel()
	components := []listenerComponent{{name: "demo", cmdRoot: "services/demo/cmd"}}

	// (а) ни листенеров, ни носителя — предпосылка обязана назвать компонент.
	carrierless, borne := componentsWithoutAListenerOrACarrier(components,
		map[string]int{}, map[string]bool{})
	if len(carrierless) == 0 {
		t.Fatal("компонент без листенеров и без носителя принят — предпосылка распознавания " +
			"не проверяется, и молчание гейта ничего не доказывает")
	}
	if !strings.Contains(strings.Join(carrierless, "\n"), "своих листенеров нет и носитель не позван") {
		t.Fatalf("отказ не называет предмет:\n%s", strings.Join(carrierless, "\n"))
	}
	if borne != 0 {
		t.Fatalf("компонент без носителя засчитан носителю: %d", borne)
	}

	// (б) законные близнецы той же формы: свои листенеры ЛИБО носитель — оба молчат.
	if got, _ := componentsWithoutAListenerOrACarrier(components,
		map[string]int{"demo": 2}, map[string]bool{}); len(got) != 0 {
		t.Fatalf("компонент со своими листенерами объявлен находкой: %v", got)
	}
	if got, borne := componentsWithoutAListenerOrACarrier(components,
		map[string]int{}, map[string]bool{"demo": true}); len(got) != 0 || borne != 1 {
		t.Fatalf("компонент на носителе объявлен находкой (%v) либо не сосчитан (%d)", got, borne)
	}
}

// synthServiceBuilder — СБОРКА СЛУЖБЫ рядом с провязанным слушателем.
//
// Форма та же — вызов `NewServer` пакетным именем, — но конструктор УМЕЕТ
// ОТКАЗАТЬ, а полученное значение регистрируется НА слушателе вторым аргументом,
// то есть само слушателем не является. Ровно так композиционные корни владельцев
// журналов поднимают общий сервер потока.
const synthServiceBuilder = `package main

import (
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/subscription"
)

func buildStream() {
	srv, err := subscription.NewServer(subscription.Config{})
	if err != nil {
		return
	}
	_ = srv
	_ = grpcsrv.NewServer(
		grpc.ChainUnaryInterceptor(grpcsrv.UnaryPanicRecovery()),
		grpc.ChainStreamInterceptor(grpcsrv.StreamPanicRecovery()),
	)
}
`

// synthPairAssignedListener — НАСТОЯЩИЙ слушатель, чей конструктор отдаёт ПАРУ.
//
// Это самый неудобный для гейта случай и предмет отдельной пробы: форма
// присваивания у него ровно такая же, как у сборки службы выше, а звена
// восстановления паники нет. Отличает их ОДНО — на нём регистрируют, то есть с
// ним обращаются как со слушателем.
const synthPairAssignedListener = `package main

import (
	"github.com/PRO-Robotech/corelib/grpcsrv"
)

func serve() {
	srv, err := grpcsrv.NewServer()
	if err != nil {
		return
	}
	RegisterDemoServiceServer(srv, nil)
}

func RegisterDemoServiceServer(srv interface{}, impl interface{}) {}
`

// TestPanicRecoveryGateSkipsAServiceBuilderNextToAWiredListener — направление (б)
// НОВОЙ ветви распознавания: сборка службы рядом с провязанным слушателем гейт не
// задевает, и она ВИДНА в переписи.
//
// Перепись здесь — не украшение, а половина утверждения: без неё «находок ноль»
// не отличить от «ветвь не исполнялась ни разу». Прежде так и было — во всех
// пробах этого файла перепись печатала «отсеяно 0», то есть новая ветвь не
// исполнялась ни в одну сторону.
func TestPanicRecoveryGateSkipsAServiceBuilderNextToAWiredListener(t *testing.T) {
	t.Parallel()
	root := synthTree(t, synthServiceBuilder, nil)
	res := auditPanicRecoveryWiring(t, root)
	t.Log(res.summary)

	if len(res.findings) != 0 {
		t.Fatalf("сборка службы объявлена слушателем без звена — гейт требует цепочку "+
			"от того, у кого её нет вовсе:\n%s", strings.Join(res.findings, "\n"))
	}
	if res.serviceBuilders == 0 {
		t.Fatal("перепись отсеяла НОЛЬ мест: новая ветвь распознавания не исполнилась, " +
			"и молчание гейта получено по другой причине")
	}
	if res.covered == 0 {
		t.Fatalf("настоящий слушатель рядом не засчитан — молчание означает «не нашёл»:\n%s", res.summary)
	}
	t.Logf("направление (б): отсеяно сборок службы %d, засчитано слушателей %d",
		res.serviceBuilders, res.covered)
}

// TestPanicRecoveryGateStillSeesAListenerWhoseConstructorReturnsAPair —
// направление (а) той же ветви, и оно СТРОЖЕ первого.
//
// Дискриминатор «конструктор умеет отказать» сам по себе был бы про ФОРМУ
// ПРИСВАИВАНИЯ, а не про предмет: научись общий конструктор слушателя отдавать
// пару — настоящие слушатели отсеялись бы МОЛЧА, и гейт остался бы зелёным ровно
// там, где обязан краснеть. Проба подаёт именно этот вход: пара со ошибкой,
// звена нет, но на значении РЕГИСТРИРУЮТ.
func TestPanicRecoveryGateStillSeesAListenerWhoseConstructorReturnsAPair(t *testing.T) {
	t.Parallel()
	root := synthTree(t, synthPairAssignedListener, nil)
	res := auditPanicRecoveryWiring(t, root)
	t.Log(res.summary)

	if len(res.findings) == 0 {
		t.Fatalf("слушатель с парным конструктором ОТСЕЯН как сборка службы: гейт судит "+
			"форму присваивания вместо предмета, и слушатель без звена прошёл бы "+
			"молча.\n%s", res.summary)
	}
	if res.serviceBuilders != 0 {
		t.Fatalf("тот же вход одновременно засчитан отсеянным (%d) — вердикт неоднозначен",
			res.serviceBuilders)
	}
	t.Logf("направление (а): %s", strings.Join(res.findings, "\n"))
}

// ── ОСЬ: РАСПОЛОЖЕНИЕ КОПИИ НЕ МЕНЯЕТ ВЕРДИКТА (#2429) ───────────────────────
//
// Гейт судит ТО дерево, которое ему назвали. Где при этом лежит рабочая копия —
// его не касается: вложена она в другой checkout продукта или нет, ответ обязан
// быть один и тот же.
//
// Ось заведена по цене. Прежняя редакция разрешала импорты подъёмом к САМОМУ
// ВНЕШНЕМУ `go.mod`, то есть во вложенной копии читала ЧУЖОЙ checkout, ничего
// там не находила и объявляла все листенеры беззвенными. Отказ был уверенным и
// адресным — три координаты, — и по нему завели P0 о продакшн-дефекте (#2427),
// которого нет. Подтвердили его две независимые полосы: обе работали во
// вложенных копиях, то есть тиражировали одну систематическую ошибку, а не
// проверили находку дважды.
//
// Пара ниже отличается РОВНО одним фактом — расположением копии. Обе половины
// обязательны: без «вложенная молчит на исправном» ось не ловила бы дефект, без
// «вложенная краснеет на снятом звене» она зеленела бы на гейте, разучившемся
// падать вообще.

// TestPanicRecoveryGateIsIndifferentToWhereTheWorktreeLies — несущее утверждение
// оси: вердикт вложенной копии СОВПАДАЕТ с вердиктом невложенной. Сравниваются
// числа, а не «обе зелены»: гейт, переставший видеть листенеры, тоже «зелен».
func TestPanicRecoveryGateIsIndifferentToWhereTheWorktreeLies(t *testing.T) {
	t.Parallel()
	plain := auditPanicRecoveryWiring(t, synthTree(t, synthRootWired, nil))
	nested := auditPanicRecoveryWiring(t, synthTreeNested(t, synthRootWired, nil))
	t.Log("невложенная: " + plain.summary)
	t.Log("вложенная:   " + nested.summary)

	if plain.listeners == 0 || plain.covered == 0 {
		t.Fatalf("контроль пуст: невложенная копия не дала ни листенеров, ни "+
			"засчитанных звеньев — сравнивать нечего.\n%s", plain.summary)
	}
	if nested.listeners != plain.listeners || nested.covered != plain.covered {
		t.Fatalf("расположение копии изменило вердикт: невложенная — листенеров %d, "+
			"со звеном %d; вложенная — %d и %d. Гейт заглянул за границу судимого "+
			"дерева.\nневложенная: %s\nвложенная:   %s",
			plain.listeners, plain.covered, nested.listeners, nested.covered,
			plain.summary, nested.summary)
	}
	if len(nested.findings) != len(plain.findings) {
		t.Fatalf("расположение копии изменило число находок: %d против %d.\n%s",
			len(nested.findings), len(plain.findings), strings.Join(nested.findings, "\n"))
	}
	t.Logf("вердикт не зависит от расположения: листенеров %d, со звеном %d, находок %d",
		nested.listeners, nested.covered, len(nested.findings))
}

// TestPanicRecoveryGateStillRedInsideANestedWorktree — вторая половина пары: во
// вложенной копии гейт обязан СОХРАНИТЬ способность падать. Без неё предыдущая
// проба зеленела бы на гейте, который молчит всегда.
func TestPanicRecoveryGateStillRedInsideANestedWorktree(t *testing.T) {
	t.Parallel()
	res := auditPanicRecoveryWiring(t, synthTreeNested(t, synthRootUnwired, nil))
	t.Log(res.summary)

	if len(res.findings) == 0 {
		t.Fatalf("звено снято, копия вложена — а гейт молчит: во вложенности он "+
			"перестал быть способен упасть.\n%s", res.summary)
	}
	joined := strings.Join(res.findings, "\n")
	if !strings.Contains(joined, "services/demo/cmd/demo/main.go") {
		t.Fatalf("находка не называет файл — по ней нечего чинить:\n%s", joined)
	}
	t.Logf("во вложенной копии гейт по-прежнему краснеет и называет координату:\n%s", joined)
}

// TestPanicRecoveryCensusNamesTheTreeItWalked — перепись обязана называть, КАКОЕ
// дерево осмотрено.
//
// Ложность была неотличима от настоящей находки именно потому, что из вывода не
// было видно, чьё дерево прочитано: семь чисел переписи совпадали дословно,
// расходилось одно. Названное дерево — то, по чему это видно сразу.
func TestPanicRecoveryCensusNamesTheTreeItWalked(t *testing.T) {
	t.Parallel()
	root := synthTree(t, synthRootWired, nil)
	res := auditPanicRecoveryWiring(t, root)

	if !strings.Contains(res.summary, root) {
		t.Fatalf("перепись не называет осмотренное дерево %q — по её выводу нельзя "+
			"сказать, чьё дерево прочитано:\n%s", root, res.summary)
	}
	if !strings.Contains(res.summary, "листенеров ") {
		t.Fatalf("перепись не называет числа листенеров:\n%s", res.summary)
	}
	t.Logf("перепись называет дерево и числа: %s", res.summary)
}
