// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build unix

package privateloopback

// reserve_unix.go — адрес петли, удержанный сокетом БЕЗ слушания (kacho#3331).
//
// # Зачем
//
// Пробе нужен адрес, которого у неё нет как слушателя: адрес «остановленной
// службы» (к нему не подключится никто) либо адрес, который займёт САМ
// проверяемый код (сервер принимает строку, а не слушатель). Прежняя форма —
// открыть :0, прочитать порт, закрыть — отдаёт порт всем: ядро вправе выдать его
// следующему автовыбору (соседнему слушателю того же процесса, пробе соседнего
// пакета, исходящему соединению), и вердикт пробы становится свойством соседства.
// Гейт класса — internal/repohygiene/closedlisteneraddr.go.
//
// Здесь порт держит сокет, привязанный к адресу и НЕ слушающий: подключение к
// нему отвергается (слушателя нет), а сам порт занят на всё время пробы.
//
// # Две формы
//
// Hold — сокет без SO_REUSEADDR. Привязку к этому порту отвергает ядро любому
// другому сокету: на том же адресе, на общем и на всех адресах (0.0.0.0) — с
// SO_REUSEADDR и без. Адрес мёртв, и оживить его может только сама проба —
// Listen на том же сокете, без окна между «освободил» и «занял».
//
// Lend — сокет С SO_REUSEADDR. Явная привязка слушателя Go (он ставит
// SO_REUSEADDR всегда) к этому адресу проходит, пока одолженный сокет не слушает;
// повторная — тоже, после закрытия первого. Автовыбор порта ядром (привязка к
// :0 на том же адресе и на всех адресах, исходящее соединение) порт с такой
// привязкой пропускает: занятый порт никто, кроме пробы, от ядра не узнаёт.
// Замер механизма (Linux 7.0, своё сетевое пространство контейнера, диапазон
// 20000–29999 исчерпан целиком): автовыбор на том же адресе выдал 9999 портов,
// одолженный — 0 раз; на 0.0.0.0 — 9999 и 0; явный слушатель Go на одолженном
// адресе — успех дважды подряд. Автовыбор на ДРУГОМ конкретном адресе этот порт
// выдать вправе (на 127.0.0.1 — 1 раз), и это не столкновение: привязки к разным
// конкретным адресам друг другу не мешают.
//
// Чего Lend не держит: соперника, который знает номер порта, не спрашивая ядро
// (зашитый порт — класс kacho#2678, kacho#2680). На собственном адресе петли
// такого соперника нет: адреса он не знает тоже.

import (
	"errors"
	"net"
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"
)

// Held — адрес петли, который не займёт никто, кроме пробы.
type Held struct {
	addr string

	mu       sync.Mutex
	fd       int
	released bool
}

// Hold — адрес, к которому подключение отвергается и привязку к которому ядро не
// даёт никому до конца пробы (или до Listen). Удержание снимает tb.Cleanup.
func Hold(tb testing.TB) *Held {
	tb.Helper()
	fd, addr := bindPrivate(tb, false)
	h := &Held{addr: addr, fd: fd}
	tb.Cleanup(h.release)
	return h
}

// Addr — адрес удержания «хост:порт».
func (h *Held) Addr() string { return h.addr }

// Listen оживляет адрес: тот же сокет начинает слушать, поэтому между «адрес
// мёртв» и «адрес служит» нет окна, в которое его займёт посторонний. Слушатель
// закрывается tb.Cleanup (и может быть закрыт раньше вызывающим). Вызывается
// один раз.
func (h *Held) Listen(tb testing.TB) net.Listener {
	tb.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released {
		tb.Fatalf("privateloopback: удержание %s уже снято — слушать нечем", h.addr)
	}
	if err := syscall.Listen(h.fd, syscall.SOMAXCONN); err != nil {
		tb.Fatalf("privateloopback: удержание %s не слушает: %v", h.addr, err)
	}
	f := os.NewFile(uintptr(h.fd), "privateloopback-"+h.addr)
	h.released = true // сокет переходит слушателю; закрытие — через него
	l, err := net.FileListener(f)
	_ = f.Close()
	if err != nil {
		tb.Fatalf("privateloopback: слушатель удержания %s не собран: %v", h.addr, err)
	}
	tb.Cleanup(func() { _ = l.Close() })
	return l
}

func (h *Held) release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released {
		return
	}
	h.released = true
	_ = syscall.Close(h.fd)
}

// Lend — адрес для кода, который занимает адрес САМ: его явная привязка проходит,
// а автовыбор ядра порт никому не выдаёт до конца пробы. Удержание снимает
// tb.Cleanup.
func Lend(tb testing.TB) string {
	tb.Helper()
	addr, release := lend(tb)
	tb.Cleanup(release)
	return addr
}

// lend — одолженный адрес и снятие удержания (идемпотентное): пробе пакета нужно
// снять удержание раньше конца пробы, чтобы показать, что держал адрес именно
// одолженный сокет.
func lend(tb testing.TB) (string, func()) {
	tb.Helper()
	fd, addr := bindPrivate(tb, true)
	var once sync.Once
	return addr, func() { once.Do(func() { _ = syscall.Close(fd) }) }
}

// bindPrivate — сокет TCP, привязанный к своему адресу петли и порту, который
// выбрало ядро; слушания нет. На платформе без адресов петли, кроме общего, —
// 127.0.0.1 со строкой об этом, как у Listen.
func bindPrivate(tb testing.TB, reuse bool) (int, string) {
	tb.Helper()
	ip := privateIP(tb)
	fd, addr, err := bindAt(ip, reuse)
	if err != nil && (runtime.GOOS == "linux" || !errors.Is(err, syscall.EADDRNOTAVAIL)) {
		tb.Fatalf("privateloopback: удержание на собственном адресе петли %s не заведено: %v", ip, err)
	}
	if err != nil {
		tb.Logf("privateloopback: платформа не отдаёт адресов петли, кроме общего (%v); "+
			"удержание — на 127.0.0.1", err)
		if fd, addr, err = bindAt(net.IPv4(127, 0, 0, 1), reuse); err != nil {
			tb.Fatalf("privateloopback: удержание на общем адресе петли не заведено: %v", err)
		}
	}
	return fd, addr
}

func bindAt(ip net.IP, reuse bool) (int, string, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return 0, "", err
	}
	syscall.CloseOnExec(fd)
	if reuse {
		if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
			_ = syscall.Close(fd)
			return 0, "", err
		}
	}
	sa := &syscall.SockaddrInet4{}
	copy(sa.Addr[:], ip.To4())
	if err := syscall.Bind(fd, sa); err != nil {
		_ = syscall.Close(fd)
		return 0, "", err
	}
	got, err := syscall.Getsockname(fd)
	if err != nil {
		_ = syscall.Close(fd)
		return 0, "", err
	}
	in4, ok := got.(*syscall.SockaddrInet4)
	if !ok {
		_ = syscall.Close(fd)
		return 0, "", syscall.EAFNOSUPPORT
	}
	return fd, (&net.TCPAddr{IP: net.IP(in4.Addr[:]), Port: in4.Port}).String(), nil
}
