// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт профиля не-gRPC поверхности СПОСОБЕН упасть — и
// что падает он на существе, а не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву (`auditSurfaceProfile`),
// на синтетическом дереве: проба, повторяющая логику гейта своей копией,
// доказывала бы свойство копии.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// synthRootOwnListener — корень, поднимающий поверхность САМ. Это и есть дефект,
// причём в форме, снятой с дерева: литерал сервера, синхронная привязка,
// возвращаемое наружу гашение.
const synthRootOwnListener = `package main

import (
	"context"
	"net"
	"net/http"
	"time"
)

func startDiagnosticListener(addr string) (func() error, func(context.Context), error) {
	if addr == "" {
		return nil, func(context.Context) {}, nil
	}
	srv := &http.Server{Addr: addr, Handler: http.NewServeMux(), ReadHeaderTimeout: 5 * time.Second}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	return func() error { return srv.Serve(lis) },
		func(ctx context.Context) { _ = srv.Shutdown(ctx) }, nil
}
`

// synthRootAsyncListen — второй вид того же дефекта: привязка порта уезжает в
// горутину, поэтому занятый адрес становится строкой журнала.
const synthRootAsyncListen = `package main

import (
	"log/slog"
	"net/http"
)

func serveDataplane(addr string, h http.Handler, logger *slog.Logger) {
	go func() {
		if err := http.ListenAndServe(addr, h); err != nil {
			logger.Error("data-plane stopped", "err", err)
		}
	}()
}
`

// synthRootHandRolledTLS — третий вид: решение о транспорте принято строкой кода
// в корне, а не объявлением.
const synthRootHandRolledTLS = `package main

import (
	"crypto/tls"
	"net"
)

func wrap(lis net.Listener, cfg *tls.Config) net.Listener {
	if cfg != nil {
		return tls.NewListener(lis, cfg)
	}
	return lis
}
`

// synthRootOnProfile — тот же корень, переведённый на профиль: он приносит
// ДАННЫЕ и отдаёт их одной функции. Ни сервера, ни привязки, ни гашения.
const synthRootOnProfile = `package main

import (
	"context"
	"net/http"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"
)

func serveDiagnostics(ctx context.Context, addr string) error {
	d, err := servicecontract.NewSurface(servicecontract.Surface{
		Service: "kacho-demo",
		Name:    "диагностика",
		Mode:    servicecontract.ModeProduction,
		Addr:    servicecontract.Value(addr),
		Handler: http.NewServeMux(),
		Reach:   servicecontract.ReachClusterInternal,
		Auth: servicecontract.NotApplicable[servicecontract.SurfaceAuthMech](
			"внутренний скрейп"),
		ReadHeaderBudget: 5 * time.Second,
		RequestBudget:    servicecontract.Value(30 * time.Second),
		IdleBudget:       60 * time.Second,
		ShutdownBudget:   5 * time.Second,
	})
	if err != nil {
		return err
	}
	return servicehost.ServeSurface(ctx, d)
}
`

// synthSurfaceDecoy — ЛОВУШКА ТЕМЫ, неудобная для гейта нарочно.
//
// Имена `http.Server`, `http.ListenAndServe` и `tls.NewListener` стоят здесь в
// комментарии и в строковом литерале — то есть ровно там, где текстовый гейт
// нашёл бы их и покраснел на исправном коде. Исполняемая часть чиста: рядом
// стоит ЗАКОННЫЙ БЛИЗНЕЦ той же формы — литерал `http.Client` и вызов
// `http.NewServeMux`, — который гейт обязан пропустить.
const synthSurfaceDecoy = `package main

import (
	"net/http"
	"time"
)

// Историческая справка: до перевода на профиль этот корень собирал
// &http.Server{}, звал http.ListenAndServe и оборачивал слушатель через
// tls.NewListener. Теперь всё это в pkg/servicehost.
func doc() string {
	return "http.Server{} + http.ListenAndServe + tls.NewListener"
}

// Законный близнец: клиент — не слушатель, маршрутизатор — не сервер.
func peer() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

func routes() *http.ServeMux { return http.NewServeMux() }
`

// synthSurfaceTree строит минимальное дерево с ОДНИМ композиционным корнем.
//
// Файлы кладутся в индекс git: гейт берёт состав у `pkg/treecorpus`, то
// есть у git, а не у диска. Фикстура, которая этого не делает, оставляет гейт с
// пустым составом — и он молчит не потому, что нарушения нет, а потому, что
// смотреть было не на что.
func synthSurfaceTree(t *testing.T, svc string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
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
	for rel, body := range files {
		write("services/"+svc+"/cmd/"+svc+"/"+rel, body)
	}
	synthTrack(t, root)
	return root
}

// TestSurfaceGateRedOnOwnListener — направление (а), первый вид дефекта: корень
// собирает сервер сам → гейт находит это И НАЗЫВАЕТ КООРДИНАТУ.
func TestSurfaceGateRedOnOwnListener(t *testing.T) {
	root := synthSurfaceTree(t, "demo", map[string]string{"observability.go": synthRootOwnListener})
	res := auditSurfaceProfile(t, root)
	t.Log(res.summary)

	if len(res.findings) == 0 {
		t.Fatalf("корень поднимает слушатель сам, а гейт молчит — он не способен упасть.\n%s",
			res.summary)
	}
	all := joinSurfaceFindings(res.findings)
	if !strings.Contains(all, "services/demo/cmd/demo/observability.go") {
		t.Fatalf("находка не называет файл — по ней нечего чинить:\n%s", all)
	}
	if !strings.Contains(all, "литерал HTTP-сервера") {
		t.Fatalf("гейт не назвал литерал сервера:\n%s", all)
	}
	t.Logf("направление (а): гейт покраснел и назвал координату:\n%s", all)
}

// TestSurfaceGateRedOnAsyncListenAndOnHandRolledTLS — направление (а), два
// остальных вида. Они проверяются ОТДЕЛЬНО: у каждого свой производитель, и
// гейт, ловящий только литерал сервера, пропустил бы оба — а на дереве оба и
// были (плоскость данных поднималась в горутине, транспорт надевался вручную в
// четырёх местах).
func TestSurfaceGateRedOnAsyncListenAndOnHandRolledTLS(t *testing.T) {
	t.Run("привязка порта в горутине", func(t *testing.T) {
		res := auditSurfaceProfile(t, synthSurfaceTree(t, "demo",
			map[string]string{"dataplane.go": synthRootAsyncListen}))
		all := joinSurfaceFindings(res.findings)
		if !strings.Contains(all, "dataplane.go") {
			t.Fatalf("подъём в горутине не замечен — именно он даёт мёртвую поверхность при "+
				"живом процессе:\n%s\n%s", all, res.summary)
		}
	})
	t.Run("ручная обёртка транспортом", func(t *testing.T) {
		res := auditSurfaceProfile(t, synthSurfaceTree(t, "demo",
			map[string]string{"tls.go": synthRootHandRolledTLS}))
		all := joinSurfaceFindings(res.findings)
		if !strings.Contains(all, "обёртка слушателя транспортом") {
			t.Fatalf("решение о транспорте, принятое строкой кода, не замечено:\n%s\n%s",
				all, res.summary)
		}
	})
}

// TestSurfaceGateSilentOnConvertedRoot — направление (б): переведённый корень
// той же формы гейта не задевает.
func TestSurfaceGateSilentOnConvertedRoot(t *testing.T) {
	res := auditSurfaceProfile(t, synthSurfaceTree(t, "demo",
		map[string]string{"observability.go": synthRootOnProfile}))
	t.Log(res.summary)

	if len(res.findings) != 0 {
		t.Fatalf("переведённый корень объявлен находкой — гейт ловит форму, а не существо: %+v",
			res.findings)
	}
	if res.roots == 0 {
		t.Fatalf("гейт не засчитал ни одного корня — молчание означает «не нашёл», "+
			"а не «всё чисто».\n%s", res.summary)
	}
}

// TestSurfaceGateIgnoresProseDecoyEvenNextToADefect — прямая проверка ловушки:
// приманка и законный близнец лежат рядом со СНЯТЫМ переводом.
//
// Эта проба и есть доказательство, что распознавание идёт по исполняемой части:
// без неё «гейт читает AST, а не текст» осталось бы утверждением о намерении.
func TestSurfaceGateIgnoresProseDecoyEvenNextToADefect(t *testing.T) {
	quiet := auditSurfaceProfile(t, synthSurfaceTree(t, "demo",
		map[string]string{"doc.go": synthSurfaceDecoy}))
	if len(quiet.findings) != 0 {
		t.Fatalf("имена в комментарии, в строковом литерале и законный близнец засчитаны за "+
			"подъём — гейт читает текст, а не исполняемую часть: %+v", quiet.findings)
	}
	if quiet.files == 0 {
		t.Fatalf("приманка не прочитана вовсе — молчание ничего не значит.\n%s", quiet.summary)
	}

	loud := auditSurfaceProfile(t, synthSurfaceTree(t, "demo", map[string]string{
		"doc.go":           synthSurfaceDecoy,
		"observability.go": synthRootOwnListener,
	}))
	if len(loud.findings) == 0 {
		t.Fatalf("приманка заслонила настоящий дефект.\n%s", loud.summary)
	}
	for _, f := range loud.findings {
		if strings.Contains(f.where, "doc.go") {
			t.Fatalf("находка указывает на приманку вместо дефекта: %s — %s", f.where, f.what)
		}
	}
	t.Logf("ловушка не обманула гейт в обе стороны: молча на приманке и на законном близнеце, "+
		"красно на дефекте (%d находок)", len(loud.findings))
}

// TestSurfaceGateAliasedImportIsNotABlindSpot — файл вправе импортировать
// `net/http` под своим именем, и поиск по «http.» на этом был бы слеп.
//
// Проба заведена потому, что предпосылка гейта («локальное имя берётся из
// объявления импорта») иначе осталась бы утверждением о намерении.
func TestSurfaceGateAliasedImportIsNotABlindSpot(t *testing.T) {
	const aliased = `package main

import (
	stdhttp "net/http"
	"time"
)

func raise(addr string) *stdhttp.Server {
	return &stdhttp.Server{Addr: addr, ReadHeaderTimeout: 5 * time.Second}
}
`
	res := auditSurfaceProfile(t, synthSurfaceTree(t, "demo",
		map[string]string{"aliased.go": aliased}))
	if len(res.findings) == 0 {
		t.Fatalf("импорт под своим именем сделал подъём невидимым — предпосылка гейта неверна.\n%s",
			res.summary)
	}
}

// TestSurfaceExceptionsExpireWhenTheirSubjectIsGone — самоистечение перечня,
// доказанное УСТРОЙСТВОМ обхода, а не прочтением.
//
// Перечень сегодня ПУСТ (все места переведены), поэтому истекать нечему — и
// проба это фиксирует, а не пропускает молча: пустой перечень есть утверждение
// о дереве, и если завтра запись появится, она обязана падать, потеряв предмет.
func TestSurfaceExceptionsExpireWhenTheirSubjectIsGone(t *testing.T) {
	if len(surfaceProfileExceptions) == 0 {
		res := auditSurfaceProfile(t, repoRoot(t))
		if len(res.findings) != 0 {
			t.Fatalf("перечень пуст, а находки есть — значит записи были бы нужны: %+v", res.findings)
		}
		t.Logf("перечень исключений пуст и это верно: своего подъёма не-gRPC поверхности в "+
			"дереве нет.\n%s", res.summary)
		return
	}
	// Перечень непуст: берём ЛЮБОЕ имя из него (а не выписанное — выписанное
	// пережило бы правку перечня и проба стала бы вакуумной) и строим дерево, где
	// этот сервис уже переведён.
	var excused string
	for svc := range surfaceProfileExceptions {
		excused = svc
		break
	}
	res := auditSurfaceProfile(t, synthSurfaceTree(t, excused,
		map[string]string{"observability.go": synthRootOnProfile}))
	if len(res.findings) != 0 {
		t.Fatalf("переведённый корень дал находки: %+v", res.findings)
	}
	t.Logf("вход самоистечения построен: %q числится исключением, находок у него 0 — "+
		"гейт по дереву обязан на этом покраснеть", excused)
}

func joinSurfaceFindings(fs []surfaceFinding) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.service+" "+f.where+" "+f.what)
	}
	return strings.Join(out, "\n")
}
