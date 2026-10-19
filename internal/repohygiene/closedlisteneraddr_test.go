// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// TestNoProbeUsesTheAddressOfAClosedListener — гейт класса kacho#3331 по дереву:
// мест «выбрал порт — закрыл — отдал» ноль. Обход, не прочитавший ни одного
// файла или не увидевший ни одного слушателя, — «не выполнилось», а не зелёный.
func TestNoProbeUsesTheAddressOfAClosedListener(t *testing.T) {
	t.Parallel()
	c, err := ClosedListenerAddrsInTree(repoRoot(t))
	if err != nil {
		t.Fatalf("обход дерева не выполнился: %v", err)
	}
	t.Log(c.String())
	for _, r := range c.Refusals() {
		t.Error(r)
	}
}

// TestClosedListenerAddrGateRefusesWhatItCouldNotRead — перепись с неразобранным
// файлом — отказ, а не зелёный; пустой обход — отказ; чистая перепись — молчание.
func TestClosedListenerAddrGateRefusesWhatItCouldNotRead(t *testing.T) {
	t.Parallel()
	clean := ClosedListenerAddrCensus{Files: 3, Listeners: 2}
	if r := clean.Refusals(); len(r) != 0 {
		t.Fatalf("чистая перепись дала отказы: %v", r)
	}
	unparsed := ClosedListenerAddrCensus{Files: 3, Listeners: 2, Unparsed: []string{"a/b.go: 3:1: expected declaration"}}
	if r := unparsed.Refusals(); len(r) != 1 || !strings.Contains(r[0], "a/b.go") {
		t.Fatalf("неразобранный файл обязан дать отказ с координатой: %v", r)
	}
	empty := ClosedListenerAddrCensus{}
	if r := empty.Refusals(); len(r) != 1 || !strings.Contains(r[0], "предпосылка") {
		t.Fatalf("пустой обход обязан дать отказ предпосылки: %v", r)
	}
}

// closedListenerSynth — инъекции: дефект и законные близнецы той же формы.
var closedListenerSynth = map[string]struct {
	src  string
	want int // находок
}{
	// ДЕФЕКТ: адрес возвращён после закрытия (форма ka1FreeAddr, freeAddr).
	"returned after close": {want: 1, src: `package p
import "net"
func freeAddr() string {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}`},
	// ДЕФЕКТ: закрытие внутри аргумента вызова (require.NoError(t, l.Close())).
	"close inside an argument": {want: 1, src: `package p
import "net"
func freeAddr(check func(error)) string {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	check(l.Close())
	return addr
}`},
	// ДЕФЕКТ: адрес унесён литералом структуры (форма newIAMBackend).
	"carried by a struct literal": {want: 1, src: `package p
import "net"
type backend struct{ addr string }
func newBackend() *backend {
	lis, _ := net.Listen("tcp", "127.0.0.1:0")
	b := &backend{addr: lis.Addr().String()}
	_ = lis.Close()
	return b
}`},
	// ДЕФЕКТ: «мёртвый адрес» остановленной службы (форма stoppedAddr).
	"dead address used by a case table": {want: 1, src: `package p
import "net"
func cases() []string {
	stopped, _ := net.Listen("tcp", "127.0.0.1:0")
	stoppedAddr := stopped.Addr().String()
	_ = stopped.Close()
	return []string{stoppedAddr}
}`},
	// ДЕФЕКТ: tls.Listen — та же форма.
	"tls listener": {want: 1, src: `package p
import "crypto/tls"
func freeAddr(cfg *tls.Config) string {
	l, _ := tls.Listen("tcp", "127.0.0.1:0", cfg)
	a := l.Addr().String()
	l.Close()
	return a
}`},
	// ДЕФЕКТ: объявление var у слушателя и у носителя.
	"var declarations": {want: 1, src: `package p
import "net"
func freeAddr() string {
	var l, _ = net.Listen("tcp", "127.0.0.1:0")
	var addr = l.Addr().String()
	_ = l.Close()
	return addr
}`},
	// БЛИЗНЕЦ: те же объявления var, закрытие отложено.
	"var declarations deferred": {want: 0, src: `package p
import "net"
func serve(use func(string)) {
	var l, _ = net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	var addr = l.Addr().String()
	use(addr)
}`},
	// ДЕФЕКТ: адрес уходит в поле структуры, структура живёт после закрытия.
	"field assignment": {want: 1, src: `package p
import "net"
type backend struct{ addr string }
func newBackend() *backend {
	b := &backend{}
	lis, _ := net.Listen("tcp", "127.0.0.1:0")
	b.addr = lis.Addr().String()
	_ = lis.Close()
	return b
}`},
	// БЛИЗНЕЦ: поле заполнено, слушатель закрыт в t.Cleanup.
	"field assignment cleanup": {want: 0, src: `package p
import "net"
type tb interface{ Cleanup(func()) }
type backend struct{ addr string }
func newBackend(t tb) *backend {
	b := &backend{}
	lis, _ := net.Listen("tcp", "127.0.0.1:0")
	b.addr = lis.Addr().String()
	t.Cleanup(func() { _ = lis.Close() })
	return b
}`},
	// ДЕФЕКТ: адрес прочитан у слушателя ПОСЛЕ закрытия, без носителя.
	"addr read after close": {want: 1, src: `package p
import "net"
func freeAddr() string {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	_ = l.Close()
	return l.Addr().String()
}`},
	// БЛИЗНЕЦ: адрес прочитан до закрытия и после закрытия не нужен.
	"addr read before close": {want: 0, src: `package p
import "net"
func once(use func(string)) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	use(l.Addr().String())
	_ = l.Close()
}`},
	// ДЕФЕКТ: носитель через промежуточное имя.
	"carrier through an intermediate": {want: 1, src: `package p
import "net"
func freeAddr() string {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	a := l.Addr()
	s := a.String()
	_ = l.Close()
	return s
}`},
	// БЛИЗНЕЦ: промежуточное имя, использование до закрытия.
	"carrier through an intermediate before close": {want: 0, src: `package p
import "net"
func once(use func(string)) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	a := l.Addr()
	s := a.String()
	use(s)
	_ = l.Close()
}`},
	// ДЕФЕКТ: закрытие в ветке, которая НЕ завершается, — путь доходит до адреса.
	"close in a branch that falls through": {want: 1, src: `package p
import "net"
func pick(ok bool) string {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	if !ok {
		_ = l.Close()
	}
	return addr
}`},
	// БЛИЗНЕЦ: закрытие только в ветке ошибки, которая сразу уходит.
	"close only in the error branch": {want: 0, src: `package p
import "net"
func serve(start func() error) string {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	if err := start(); err != nil {
		_ = l.Close()
		return ""
	}
	return addr
}`},
	// ДЕФЕКТ: цикл подбора порта — break уводит из цикла, а не из функции.
	"pick loop with break": {want: 1, src: `package p
import "net"
func pick() string {
	var addr string
	for {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			continue
		}
		addr = l.Addr().String()
		_ = l.Close()
		break
	}
	return addr
}`},
	// БЛИЗНЕЦ: тот же цикл, закрытие только в ветке ошибки с return.
	"pick loop error branch returns": {want: 0, src: `package p
import "net"
func pick(start func() error) string {
	for {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			continue
		}
		addr := l.Addr().String()
		if err := start(); err != nil {
			_ = l.Close()
			return ""
		}
		return addr
	}
}`},
	// БЛИЗНЕЦ: закрытие в ветке ошибки, завершённой t.Fatal.
	"close in a fatal branch": {want: 0, src: `package p
import "net"
type tb interface{ Fatal(...any) }
func serve(t tb, start func() error) string {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	if err := start(); err != nil {
		_ = l.Close()
		t.Fatal(err)
	}
	return addr
}`},
	// БЛИЗНЕЦ: результат вызова с адресом в аргументе — не адрес.
	"call result is not a carrier": {want: 0, src: `package p
import "net"
func probe(use func(string) error) error {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	err := use(addr)
	_ = l.Close()
	return err
}`},
	// БЛИЗНЕЦ: закрытие отложено — слушатель служит пробе до конца.
	"deferred close": {want: 0, src: `package p
import "net"
func serve(use func(string)) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	addr := l.Addr().String()
	use(addr)
}`},
	// БЛИЗНЕЦ: закрытие в t.Cleanup — литерал функции, конец пробы.
	"close in a cleanup literal": {want: 0, src: `package p
import "net"
type tb interface{ Cleanup(func()) }
func serve(t tb) string {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() {
		_ = l.Close()
	})
	return l.Addr().String()
}`},
	// БЛИЗНЕЦ: адрес использован ДО закрытия, после — нет.
	"used before close only": {want: 0, src: `package p
import "net"
func once(use func(string)) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	use(addr)
	_ = l.Close()
}`},
	// БЛИЗНЕЦ: закрыт слушатель без чтения адреса (проверка «порт свободен»).
	"rebind check without addr": {want: 0, src: `package p
import "net"
func released(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	_ = l.Close()
	return nil
}`},
	// БЛИЗНЕЦ: слово в комментарии и строке — не узел.
	"text is not a node": {want: 0, src: `package p
// l, _ := net.Listen("tcp", "127.0.0.1:0"); addr := l.Addr().String(); l.Close(); return addr
func text() string { return "addr := l.Addr().String(); _ = l.Close(); return addr" }`},
}

func TestClosedListenerAddrGateInjections(t *testing.T) {
	t.Parallel()
	for name, c := range closedListenerSynth {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, found, err := ClosedListenerAddrsInSource(strings.ReplaceAll(name, " ", "_")+".go", c.src)
			if err != nil {
				t.Fatalf("синтетика не разобрана: %v", err)
			}
			if len(found) != c.want {
				t.Fatalf("находок %d, ожидалось %d: %v", len(found), c.want, found)
			}
			for _, f := range found {
				if !strings.Contains(f.String(), "privateloopback") || !strings.Contains(f.Pos, ".go:") {
					t.Errorf("находка не называет координату и законную форму: %s", f)
				}
			}
		})
	}
}
