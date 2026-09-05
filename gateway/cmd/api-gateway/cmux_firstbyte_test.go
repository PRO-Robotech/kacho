// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/soheilhy/cmux"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// serveEdgeStack поднимает ТУ ЖЕ композицию, что и шлюз: мультиплексор края с
// бюджетом на первый байт + http.Server с ReadHeaderTimeout на «всём остальном».
// Обе части нужны вместе, потому что они делят между собой одно окно: пока
// протокол не распознан, соединением владеет мультиплексор и таймауты сервера к
// нему не относятся; после распознавания — наоборот. Проба без второй части
// наблюдала бы передачу соединения, а не его закрытие.
func serveEdgeStack(t *testing.T, firstByteBudget, headerTimeout time.Duration) (addr string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	m := newEdgeCmux(l, firstByteBudget)
	grpcL := m.MatchWithWriters(
		cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"),
	)
	httpL := m.Match(cmux.Any())

	srv := &http.Server{
		ReadHeaderTimeout: headerTimeout,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}),
	}
	go func() { _ = srv.Serve(httpL) }()
	go func() {
		for {
			c, acceptErr := grpcL.Accept()
			if acceptErr != nil {
				return
			}
			_ = c.Close()
		}
	}()
	go func() { _ = m.Serve() }()

	t.Cleanup(func() {
		_ = srv.Close()
		m.Close()
		_ = l.Close()
	})
	return l.Addr().String()
}

// serveMatchedCmux регистрирует те же матчеры и в том же порядке, что и шлюз, и
// обслуживает мультиплексор, УДЕРЖИВАЯ принятые соединения. Нужен предпосылочной
// пробе: там предмет — поведение самого мультиплексора, поэтому закрывать
// соединение не должна ни одна другая сторона.
func serveMatchedCmux(t *testing.T, l net.Listener, m cmux.CMux) <-chan net.Conn {
	t.Helper()
	grpcL := m.MatchWithWriters(
		cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"),
	)
	httpL := m.Match(cmux.Any())

	accepted := make(chan net.Conn, 8)
	drain := func(sub net.Listener) {
		for {
			c, acceptErr := sub.Accept()
			if acceptErr != nil {
				return
			}
			accepted <- c
		}
	}
	go drain(grpcL)
	go drain(httpL)
	go func() { _ = m.Serve() }()

	t.Cleanup(func() {
		m.Close()
		_ = l.Close()
	})
	return accepted
}

// TestEdgeStack_SilentConnectionIsClosedWithinBudget — соединение, которое
// открыли и ничего не прислали, обязано быть закрыто в пределах бюджета.
//
// Без бюджета на распознавание оно не доходит до http.Server вовсе: первый
// матчер выполняет блокирующее чтение из сырого соединения, поэтому
// ReadHeaderTimeout к этому окну не относится, и горутина, дескриптор и буфер
// распознавания удерживаются без аутентификации неограниченно долго. Проба
// наблюдаемая: читаем свою сторону сокета и ждём фактического закрытия, а не
// «вызвана ли функция».
func TestEdgeStack_SilentConnectionIsClosedWithinBudget(t *testing.T) {
	const (
		budget        = 300 * time.Millisecond
		headerTimeout = 300 * time.Millisecond
	)
	addr := serveEdgeStack(t, budget, headerTimeout)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	limit := 8 * (budget + headerTimeout)
	if deadlineErr := conn.SetReadDeadline(time.Now().Add(limit)); deadlineErr != nil {
		t.Fatalf("set read deadline: %v", deadlineErr)
	}
	buf := make([]byte, 1)
	start := time.Now()
	_, readErr := conn.Read(buf)
	elapsed := time.Since(start)

	if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
		t.Fatalf("молчащее соединение прожило %v (бюджет распознавания %v + %v на заголовки) "+
			"и не было закрыто: горутина, дескриптор и буфер распознавания удерживаются без "+
			"аутентификации и без ограничения по времени", elapsed, budget, headerTimeout)
	}
	// EOF, reset или ответ 408 от http.Server — любое из них означает, что
	// соединение больше не припарковано.
	if readErr != nil && readErr != io.EOF && !strings.Contains(readErr.Error(), "reset") {
		t.Fatalf("неожиданная ошибка чтения %v (за %v)", readErr, elapsed)
	}
}

// TestEdgeStack_SlowButLiveClientIsServed — законная форма того же поведения:
// клиент, который присылает первый байт медленно, но В ПРЕДЕЛАХ бюджета, обязан
// быть распознан и обслужен. Без этой половины бюджет ловил бы форму
// («соединения закрываются»), а не существо, и первый же медленный клиент
// заставил бы его снять.
func TestEdgeStack_SlowButLiveClientIsServed(t *testing.T) {
	const (
		budget        = 2 * time.Second
		headerTimeout = 2 * time.Second
	)
	addr := serveEdgeStack(t, budget, headerTimeout)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	time.Sleep(budget / 4)
	if _, writeErr := conn.Write([]byte("GET /healthz HTTP/1.1\r\nHost: x\r\n\r\n")); writeErr != nil {
		t.Fatalf("write after a slow start must succeed: %v", writeErr)
	}
	assertHTTPOK(t, conn, 4*budget)
}

// TestEdgeStack_KeepAliveConnectionSurvivesIdleLongerThanBudget — бюджет
// относится ТОЛЬКО к окну распознавания. Если дедлайн остался висеть на
// соединении, любое долгоживущее соединение (keep-alive REST, gRPC-стрим)
// оборвётся ровно через бюджет — это был бы обмен «защита от парковки» на
// массовые разрывы. Проба: запрос → простой заметно дольше бюджета → второй
// запрос по тому же соединению обязан быть обслужен.
func TestEdgeStack_KeepAliveConnectionSurvivesIdleLongerThanBudget(t *testing.T) {
	const (
		budget        = 300 * time.Millisecond
		headerTimeout = 5 * time.Second
	)
	addr := serveEdgeStack(t, budget, headerTimeout)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if _, writeErr := conn.Write([]byte("GET /healthz HTTP/1.1\r\nHost: x\r\n\r\n")); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
	}
	assertHTTPOK(t, conn, 5*time.Second)

	time.Sleep(4 * budget)

	if _, writeErr := conn.Write([]byte("GET /healthz HTTP/1.1\r\nHost: x\r\n\r\n")); writeErr != nil {
		t.Fatalf("второй запрос по keep-alive соединению не отправился (%v) — остаточный "+
			"дедлайн режет долгоживущие соединения", writeErr)
	}
	assertHTTPOK(t, conn, 5*time.Second)
}

// assertHTTPOK читает один HTTP-ответ и требует 200.
func assertHTTPOK(t *testing.T, conn net.Conn, within time.Duration) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(within)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("ответ не получен: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("тело ответа: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("законный клиент получил %d вместо 200", resp.StatusCode)
	}
}

// TestNoRawCmuxNewOutsideFactory — гейт на класс: бюджет на первый байт
// получается ПО ПОСТРОЕНИЮ, а не по памяти автора.
//
// Мультиплексор создаётся ровно в одном месте (newEdgeCmux). Прямой cmux.New в
// любом другом прод-файле шлюза заводит слушатель без бюджета, и заметить это
// глазами в диффе нельзя — поэтому проверяется механически, по синтаксическому
// дереву (упоминание cmux.New в комментарии или строке под запрет не попадает).
func TestNoRawCmuxNewOutsideFactory(t *testing.T) {
	root := gatewayTreeRoot(t)
	const factory = "cmd/api-gateway/cmux_firstbyte.go"

	// Состав дерева берётся у индекса git, а не с диска. `gateway/build/`
	// объявлен игнорируемым (gateway/.gitignore) и на машине, где шлюз собирали,
	// содержит сгенерированный .go. Обход диска читал бы его, и вердикт гейта
	// стал бы свойством рабочего каталога, а не коммита — в обе стороны: красный
	// на файле, которого в репозитории нет и по построению быть не может, гейт
	// обесценивает, а молчание в свежем checkout прячет то, о чём он обязан
	// говорить.
	sources, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("состав gateway/: %v", err)
	}

	var hits []string
	scanned := 0
	for _, path := range sources {
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			t.Fatalf("относительный путь для %s: %v", path, relErr)
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, "_test.go") || hasPathSegment(rel, "docs", "testdata") {
			continue
		}
		scanned++
		if rel == factory {
			continue
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		for _, line := range cmuxNewCallLines(t, rel, body) {
			hits = append(hits, rel+":"+strconv.Itoa(line))
		}
	}

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
	if scanned == 0 {
		t.Fatalf("гейт не прочитал ни одного прод-файла в %s — предпосылка обхода сломана", root)
	}
	t.Logf("осмотрено прод-файлов шлюза: %d; единственная точка создания: %s", scanned, factory)

	if len(hits) > 0 {
		t.Errorf("cmux.New вне единственной фабрики: %s\n\n"+
			"Такой мультиплексор не имеет бюджета на первый байт: молчащее соединение "+
			"удерживает горутину и дескриптор без аутентификации и без ограничения по "+
			"времени. Создавай через newEdgeCmux(l, edgeFirstByteBudget).",
			strings.Join(hits, ", "))
	}
}

// hasPathSegment — путь содержит хотя бы один из названных СЕГМЕНТОВ.
//
// Именно сегмент, а не подстроку: прежний обход отбрасывал каталоги по имени
// (`filepath.SkipDir`), и подстрочная замена молча разошлась бы с ним — на
// верхнем уровне (`testdata/x.go`) и на пути, где имя каталога является частью
// другого имени.
func hasPathSegment(rel string, names ...string) bool {
	for _, seg := range strings.Split(rel, "/") {
		for _, n := range names {
			if seg == n {
				return true
			}
		}
	}
	return false
}

// TestEdgeCmuxFactoryPremiseHolds — запрет опирается на факт: у cmux есть ручка
// бюджета чтения, и по умолчанию она выключена. Если версия библиотеки сменит
// это (появится ненулевой дефолт или ручка исчезнет), требование надо
// пересмотреть, а не продолжать требовать своё.
func TestEdgeCmuxFactoryPremiseHolds(t *testing.T) {
	knobL, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = knobL.Close() }()

	// Ручка существует и вызываема на интерфейсе CMux — иначе тут не собралось бы.
	// Этот мультиплексор не обслуживается: предмет проверки — наличие ручки.
	var m cmux.CMux = cmux.New(knobL)
	m.SetReadTimeout(time.Second)

	// Дефолт по-прежнему «без бюджета»: голый cmux.New не закрывает молчащее
	// соединение. Проверяем это тем же наблюдаемым способом, что и основную
	// пробу, но на голом конструкторе и на ОТДЕЛЬНОМ слушателе — два
	// мультиплексора на одном слушателе конкурировали бы за Accept, и закрытие
	// пришло бы от чужого, а не от предмета проверки.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	serveMatchedCmux(t, l, cmux.New(l))

	conn, dialErr := net.Dial("tcp", l.Addr().String())
	if dialErr != nil {
		t.Fatalf("dial: %v", dialErr)
	}
	defer func() { _ = conn.Close() }()
	if deadlineErr := conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); deadlineErr != nil {
		t.Fatalf("set read deadline: %v", deadlineErr)
	}
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	netErr, ok := readErr.(net.Error)
	if !ok || !netErr.Timeout() {
		t.Fatalf("предпосылка изменилась: голый cmux.New больше не оставляет молчащее "+
			"соединение открытым (%v) — пересмотри запрет и бюджет", readErr)
	}
}

// cmuxNewCallLines — строки, где вызывается cmux.New.
func cmuxNewCallLines(t *testing.T, rel string, body []byte) []int {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, body, 0)
	if err != nil {
		t.Fatalf("разбор %s: %v", rel, err)
	}
	var lines []int
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "New" {
			return true
		}
		if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "cmux" {
			lines = append(lines, fset.Position(call.Pos()).Line)
		}
		return true
	})
	return lines
}

// gatewayTreeRoot — корень дерева шлюза (каталог gateway/).
func gatewayTreeRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "gateway")
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("каталог gateway/ не найден выше %s", dir)
		}
		dir = parent
	}
}
