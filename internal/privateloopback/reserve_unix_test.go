// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build unix

package privateloopback

import (
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
)

// rivalBinds — соперник: пытается занять адрес и сообщает, удалось ли.
func rivalBinds(t *testing.T, addr string) error {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	if err == nil {
		_ = l.Close()
	}
	return err
}

func wildcardOf(t *testing.T, addr string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	return net.JoinHostPort("0.0.0.0", port)
}

func dialRefused(t *testing.T, addr string) {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err == nil {
		_ = c.Close()
		t.Fatalf("к %s подключились — за адресом кто-то слушает", addr)
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("подключение к %s: ожидался отказ соединения, получено %v", addr, err)
	}
}

// TestHeldAddressCannotBeTakenByARival — удержание: соперник не занимает адрес
// ни на нём самом, ни на всех адресах; подключение отвергается.
//
// Близнец — тот же адрес после снятия удержания: соперник его занимает, ровно как
// занимал порт, закрытый пробой прежней формы («открыл :0, прочитал, закрыл»).
// Разница между ветками — ровно один факт: держит ли порт сокет пробы.
func TestHeldAddressCannotBeTakenByARival(t *testing.T) {
	h := Hold(t)
	t.Run("удержание", func(t *testing.T) {
		for _, a := range []string{h.Addr(), wildcardOf(t, h.Addr())} {
			if err := rivalBinds(t, a); !errors.Is(err, syscall.EADDRINUSE) {
				t.Errorf("соперник на %s: ожидался отказ «адрес занят», получено %v", a, err)
			}
		}
		dialRefused(t, h.Addr())
	})
	t.Run("близнец: удержание снято", func(t *testing.T) {
		h.release()
		if err := rivalBinds(t, h.Addr()); err != nil {
			t.Fatalf("предпосылка близнеца: соперник обязан занять освобождённый %s, получено %v", h.Addr(), err)
		}
	})
}

// TestHeldAddressComesAliveWithoutAGap — Listen оживляет тот же адрес тем же
// сокетом: до него отказ соединения, после — ответ слушателя пробы.
func TestHeldAddressComesAliveWithoutAGap(t *testing.T) {
	h := Hold(t)
	dialRefused(t, h.Addr())
	l := h.Listen(t)
	if l.Addr().String() != h.Addr() {
		t.Fatalf("слушатель на %s, удержание на %s", l.Addr(), h.Addr())
	}
	go func() {
		c, err := l.Accept()
		if err == nil {
			_, _ = c.Write([]byte("ok"))
			_ = c.Close()
		}
	}()
	c, err := net.DialTimeout("tcp", h.Addr(), 2*time.Second)
	if err != nil {
		t.Fatalf("оживлённый адрес %s не принимает: %v", h.Addr(), err)
	}
	defer func() { _ = c.Close() }()
	buf := make([]byte, 2)
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if n, _ := c.Read(buf); string(buf[:n]) != "ok" {
		t.Fatalf("ответил не слушатель пробы: %q", buf[:n])
	}
}

// TestLentAddressIsBoundByTheCodeAndReleased — одолженный адрес: явная привязка
// слушателя Go проходит, второй слушатель на занятом адресе — отказ (проверка
// «порт освобождён» сохраняет зубы), после закрытия привязка проходит снова; до
// привязки подключение отвергается.
func TestLentAddressIsBoundByTheCodeAndReleased(t *testing.T) {
	addr := Lend(t)
	dialRefused(t, addr)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("явная привязка к одолженному %s: %v", addr, err)
	}
	if err := rivalBinds(t, addr); !errors.Is(err, syscall.EADDRINUSE) {
		_ = l.Close()
		t.Fatalf("второй слушатель на занятом %s: ожидался отказ «адрес занят», получено %v", addr, err)
	}
	_ = l.Close()
	if err := rivalBinds(t, addr); err != nil {
		t.Fatalf("после закрытия слушателя одолженный %s не занимается снова: %v", addr, err)
	}
}

// bindWithoutReuse — соперник без SO_REUSEADDR: только привязка, без слушания.
// Слушатель Go ставит SO_REUSEADDR всегда и потому одолженного адреса этим
// способом не отличит; соперник без него отличает «порт удержан» от «порт
// свободен».
func bindWithoutReuse(t *testing.T, addr string) error {
	t.Helper()
	ta, err := net.ResolveTCPAddr("tcp4", addr)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Close(fd) }()
	sa := &syscall.SockaddrInet4{Port: ta.Port}
	copy(sa.Addr[:], ta.IP.To4())
	return syscall.Bind(fd, sa)
}

// TestLentAddressIsHeldWhileLent — публичный Lend держит адрес до конца ПРОБЫ,
// которая его одолжила: внутри подтеста соперник без SO_REUSEADDR получает
// «адрес занят»; после завершения подтеста (его Cleanup снял удержание) —
// привязывается. Прежняя форма («открыл, прочитал, закрыл») и снятие удержания
// раньше конца пробы отдали бы порт сопернику сразу — первая ветка покраснела бы.
func TestLentAddressIsHeldWhileLent(t *testing.T) {
	var addr string
	t.Run("одолжен", func(t *testing.T) {
		addr = Lend(t)
		if err := bindWithoutReuse(t, addr); !errors.Is(err, syscall.EADDRINUSE) {
			t.Fatalf("пока адрес %s одолжен, соперник без SO_REUSEADDR обязан получить «адрес занят», получено %v", addr, err)
		}
	})
	if addr == "" {
		t.Fatal("предпосылка близнеца: подтест не одолжил адреса")
	}
	t.Run("близнец: проба, одолжившая адрес, завершилась", func(t *testing.T) {
		if err := bindWithoutReuse(t, addr); err != nil {
			t.Fatalf("после конца одолжившей пробы соперник обязан занять %s, получено %v", addr, err)
		}
	})
}
