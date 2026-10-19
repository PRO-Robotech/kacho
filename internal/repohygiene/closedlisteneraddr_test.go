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
	c, err := ClosedListenerAddrsInTree(repoRoot(t))
	if err != nil {
		t.Fatalf("обход дерева не выполнился: %v", err)
	}
	t.Log(c.String())
	if c.Files == 0 || c.Listeners == 0 {
		t.Fatalf("предпосылка обхода не выполнена — проверять нечего: %s", c.String())
	}
	for _, f := range c.Findings {
		t.Error(f.String())
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
	for name, c := range closedListenerSynth {
		t.Run(name, func(t *testing.T) {
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
