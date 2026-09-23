// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package privateloopback — сервер пробы на СОБСТВЕННОМ адресе петли.
//
// # Зачем
//
// httptest.NewServer слушает 127.0.0.1 на порту, который выбрало ядро. Порт петли —
// ресурс машины, а не пробы: закрытый сервер освобождает его, ядро отдаёт его
// следующему слушателю, и клиент, переживший свой сервер (проба соседнего процесса
// с повтором, фоновое обновление, переподключение), приходит к новому. Сервер,
// считающий прибытия, записывает такое прибытие на счёт проверяемого, и проба
// «обращений ноль» или «ровно одно обращение» краснеет на продукте, который никуда
// лишнего не ходил. Под нагрузкой хука отправки это и происходило (#2851).
//
// Сузить СЧЁТ (по пути, по методу, по заголовку) нельзя: продукт, обратившийся не
// туда, выглядит для сервера ровно как посторонний, и сужение вычитало бы из счёта
// именно тот дефект, ради которого счётчик стоит. Поэтому сужается не счёт, а
// ДОСТУПНОСТЬ: сервер слушает случайный адрес из 127.0.0.0/8, отличный от общего
// 127.0.0.1. Посторонний знает порт, но не адрес, и до сервера не доходит; всякое
// прибытие, которое доходит, пришло по адресу, выданному пробой, и считается целиком.
//
// # Формы
//
// NewServer и NewUnstartedServer — для сервера HTTP (как у httptest); Listen — для
// всего прочего, что служит на слушателе TCP (сервер gRPC, свой http.Server).
//
// # Платформы
//
// На Linux петле принадлежит вся сеть 127.0.0.0/8, и отказ занять свой адрес там —
// поломка окружения: пакет роняет пробу, а не откатывается молча на общий адрес.
// Где петле принадлежит один адрес (macOS без псевдонимов), сервер слушает
// 127.0.0.1 и печатает об этом строку. Свойства пакета на такой платформе НЕТ:
// посторонний, знающий порт, до сервера доходит, и проба ошибается в обе стороны —
// лишним красным (счёт вырос на чужое прибытие) и ложным зелёным (чужое прибытие
// засчитано продукту: открыло ожидание первого обращения, перезаписало захваченный
// последний запрос, дало обработчику «хотя бы одно» обращение). Судит свойство
// конвейер: он идёт на Linux.
package privateloopback

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// NewServer — как httptest.NewServer, но сервер слушает свой адрес петли.
// Закрывает сервер вызывающий, как и у httptest.
func NewServer(tb testing.TB, h http.Handler) *httptest.Server {
	tb.Helper()
	s := NewUnstartedServer(tb, h)
	s.Start()
	return s
}

// NewUnstartedServer — как httptest.NewUnstartedServer: слушатель уже занят на
// своём адресе петли, настройка и Start/StartTLS — у вызывающего.
//
// Для StartTLS заранее положен сертификат на ЭТОТ адрес: встроенный сертификат
// httptest называет только 127.0.0.1 и example.com, и s.Client() такому серверу
// не поверил бы. Вызывающий, заменивший s.TLS своим (свой удостоверяющий центр,
// объявленное имя сервера), получает свой — как и у httptest.
//
// Сервер собирается тем же литералом, что и в httptest.NewUnstartedServer, но со
// своим слушателем: httptest.NewUnstartedServer сам занял бы порт на общем адресе,
// а закрытие этого слушателя отдало бы порт следующему.
func NewUnstartedServer(tb testing.TB, h http.Handler) *httptest.Server {
	tb.Helper()
	l := Listen(tb)
	cert, err := certificateFor(l.Addr())
	if err != nil {
		_ = l.Close()
		tb.Fatalf("privateloopback: сертификат адреса %s не выпущен: %v", l.Addr(), err)
	}
	return &httptest.Server{
		Listener: l,
		Config:   &http.Server{Handler: h},
		TLS:      &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
	}
}

// certificateFor — самоподписанный сертификат сервера на адрес слушателя. Он же
// корень доверия клиента s.Client(): httptest кладёт в пул клиента первый
// сертификат конфигурации TLS сервера.
func certificateFor(addr net.Addr) (tls.Certificate, error) {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return tls.Certificate{}, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(now.UnixNano()),
		Subject:               pkix.Name{CommonName: "privateloopback"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP(host)},
		DNSNames:              []string{"example.com", "*.example.com"},
	}
	der, err := x509.CreateCertificate(crand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// Listen — слушатель TCP на случайном адресе 127.x.y.z, где x ≠ 0: общий
// 127.0.0.1 (и вся 127.0.0.0/16) исключены по построению.
//
// Попытка одна. Порт выбирает ядро, поэтому «адрес занят» здесь не случается;
// отказ «адрес недоступен» значит, что платформа не отдаёт адресов петли, кроме
// общего, — и тогда не отдаст ни одного, сколько ни пробуй.
func Listen(tb testing.TB) net.Listener {
	tb.Helper()
	var b [3]byte
	if _, err := crand.Read(b[:]); err != nil {
		tb.Fatalf("privateloopback: случайный адрес петли не выбран: %v", err)
	}
	if b[0] == 0 {
		b[0] = 1 // 127.0.0.0/16 — общий адрес и его соседи — исключены
	}
	if b[2] == 0 || b[2] == 255 {
		b[2] = 1
	}
	ip := net.IPv4(127, b[0], b[1], b[2])
	l, err := net.Listen("tcp4", net.JoinHostPort(ip.String(), "0"))
	if err == nil {
		return l
	}
	if runtime.GOOS == "linux" || !errors.Is(err, syscall.EADDRNOTAVAIL) {
		tb.Fatalf("privateloopback: собственный адрес петли %s не занят: %v", ip, err)
	}
	tb.Logf("privateloopback: платформа %s не отдаёт адресов петли, кроме общего (%v); "+
		"сервер слушает 127.0.0.1 и доступен постороннему, знающему порт", runtime.GOOS, err)
	l, err = net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("privateloopback: общий адрес петли не занят: %v", err)
	}
	return l
}
