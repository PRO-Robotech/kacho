// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trust_anchor_lists_are_independent_test.go — ПРЕДПОСЫЛКА гейта посадки
// доверия: `SSL_CERT_FILE` и `SSL_CERT_DIR` управляют РАЗНЫМИ списками, и
// набор корней есть их ОБЪЕДИНЕНИЕ (#1753).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ ЭТО ПРОВЕРЯТЬ, А НЕ ЗНАТЬ
//
// Соседний гейт (trust_anchor_claim_matches_declaration_test.go) требует: блок,
// объявивший доверие ТОЛЬКО-ВНУТРЕННИМ, обязан пинить ОБЕ переменные. Требование
// это не вкус — оно опирается на ФАКТ О БИБЛИОТЕКЕ: одна переменная замещает
// только свой список, а другой продолжает читаться. Изменится факт — требование
// станет ложью, а гейт продолжит её энфорсить, оставаясь зелёным.
//
// Поэтому предпосылка не подразумевается, а ИЗМЕРЯЕТСЯ — тем же вызовом
// `x509.SystemCertPool`, которым её измеряет продукт.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЦЕНА ОШИБКИ ИЗМЕРЕНА, А НЕ ПРЕДПОЛОЖЕНА (#1753)
//
// Боевой профиль утверждал уверенно и без пометки: «SSL_CERT_FILE ЗАМЕЩАЕТ
// системный набор корней целиком, поэтому исходящий TLS этого пода доверяет
// ТОЛЬКО внутреннему удостоверяющему». Замер показал обратное, и ошибка была
// направлена в сторону БОЛЬШЕГО доверия, чем считал автор: набор шёл
// 123 → 124 (якорь ДОБАВЛЕН), а не 123 → 1.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПОДПРОЦЕССЫ
//
// `x509.SystemCertPool` считает набор ОДИН РАЗ на процесс (`sync.Once`), поэтому
// в одном процессе измеримо ровно одно окружение. Каждый случай исполняется
// отдельным запуском ЭТОГО ЖЕ тестового бинаря — приём `TestHelperProcess` из
// стандартной библиотеки (`os/exec`).
//
// `TestTrustAnchorPoolChild` вне подпроцесса не делает ничего и НЕ является
// пропуском: пропуск скрыл бы неисполнение, а тут исполнять нечего — вся его
// работа определена только при заданном окружении, и она в этом прогоне уже
// сделана родителем.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ВСЕ СЛУЧАИ ГЕРМЕТИЧНЫ
//
// В КАЖДОМ случае пиниты ОБЕ переменные, поэтому хранилище машины не участвует
// ни в одном утверждении, и вердикт одинаков на машине разработчика, в ранере и
// в пустом образе. Число корней хозяйской машины печатается ПЕРЕПИСЬЮ, но
// не утверждается: оно свойство машины, а не дерева.
//
//	случай            SSL_CERT_FILE   SSL_CERT_DIR    корней  что доказывает
//	только каталог    несуществующий  каталог(2)      2       список КАТАЛОГОВ читается сам по себе
//	объединение       файл(1)         каталог(2)      3       набор есть ОБЪЕДИНЕНИЕ, а не замещение
//	только файл       файл(1)         несуществующий  1       исключительность требует ОБЕИХ
//
// Первый и третий вместе показывают, что каждая переменная правит ТОЛЬКО свой
// список; второй — что вклады складываются.
package deploy_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// trustPoolChildEnv — маркер подпроцесса.
const trustPoolChildEnv = "KACHO_TRUST_POOL_CHILD"

// TestTrustAnchorPoolChild — тело подпроцесса. Вне подпроцесса не делает
// ничего (см. шапку: это не пропуск).
func TestTrustAnchorPoolChild(t *testing.T) {
	if os.Getenv(trustPoolChildEnv) != "1" {
		return
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		fmt.Printf("KACHO_POOL_ERR=%v\n", err)
		return
	}
	//nolint:staticcheck // Subjects() устарел для пулов из файлов, но для
	// системного пула остаётся единственным способом СОСЧИТАТЬ корни, а счёт
	// здесь и есть предмет измерения.
	fmt.Printf("KACHO_POOL_ROOTS=%d\n", len(pool.Subjects()))
}

// writeSelfSignedPEM кладёт по пути один самоподписанный сертификат.
func writeSelfSignedPEM(t *testing.T, path, cn string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ключ: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("сертификат: %v", err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("запись %s: %v", path, err)
	}
}

// countRootsUnder запускает подпроцесс с заданным окружением и возвращает число
// корней, которое увидел `x509.SystemCertPool`.
func countRootsUnder(t *testing.T, certFile, certDir string) int {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^TestTrustAnchorPoolChild$", "-test.v")
	cmd.Env = append(os.Environ(),
		trustPoolChildEnv+"=1",
		"SSL_CERT_FILE="+certFile,
		"SSL_CERT_DIR="+certDir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("подпроцесс не отработал (FILE=%q DIR=%q): %v\n%s", certFile, certDir, err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "KACHO_POOL_ROOTS="); ok {
			n, convErr := strconv.Atoi(v)
			if convErr != nil {
				t.Fatalf("нечитаемое число корней %q: %v", v, convErr)
			}
			return n
		}
		if v, ok := strings.CutPrefix(line, "KACHO_POOL_ERR="); ok {
			t.Fatalf("набор корней не построился: %s", v)
		}
	}
	t.Fatalf("подпроцесс не назвал число корней; вывод:\n%s", out)
	return -1
}

// TestTrustAnchorListsAreIndependent — предпосылка соседнего гейта.
func TestTrustAnchorListsAreIndependent(t *testing.T) {
	dir := t.TempDir()

	anchors := filepath.Join(dir, "anchors")
	if err := os.MkdirAll(anchors, 0o755); err != nil {
		t.Fatalf("каталог якорей: %v", err)
	}
	writeSelfSignedPEM(t, filepath.Join(anchors, "one.crt"), "kacho-probe-dir-1")
	writeSelfSignedPEM(t, filepath.Join(anchors, "two.crt"), "kacho-probe-dir-2")

	lone := filepath.Join(dir, "lone.crt")
	writeSelfSignedPEM(t, lone, "kacho-probe-file-1")

	absent := filepath.Join(dir, "no-such-file.crt")
	absentDir := filepath.Join(dir, "no-such-dir")

	// ── ПЕРЕПИСЬ: величина хозяйской машины печатается, но НЕ утверждается ──
	// «ноль находок» обязано быть отличимо от «ноль прочитанного», а магнитуда
	// объясняет, почему ошибка комментария была направлена в сторону БОЛЬШЕГО
	// доверия: замещаемый список — не тот, в котором лежит основная масса.
	hostOnlyFile := countRootsUnder(t, lone, "")
	t.Logf("перепись (свойство МАШИНЫ, не дерева): "+
		"корней при пинённом только файле = %d; каталоги по умолчанию продолжают читаться", hostOnlyFile)

	cases := []struct {
		name     string
		certFile string
		certDir  string
		want     int
		proves   string
	}{
		{
			name: "только каталог: список КАТАЛОГОВ читается сам по себе",
			// Файл пинится несуществующим НАМЕРЕННО: это отсекает список файлов
			// по умолчанию, не подмешивая ни одного корня. Останется ровно то,
			// что дал каталог.
			certFile: absent, certDir: anchors, want: 2,
			proves: "SSL_CERT_DIR наполняет набор независимо от SSL_CERT_FILE",
		},
		{
			name:     "объединение: набор есть СУММА двух списков",
			certFile: lone, certDir: anchors, want: 3,
			proves: "ни одна из переменных не отменяет вклад другой",
		},
		{
			name:     "только файл: исключительность требует ОБЕИХ",
			certFile: lone, certDir: absentDir, want: 1,
			proves: "набор сужается до якоря лишь когда пинены ОБА списка",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := countRootsUnder(t, tc.certFile, tc.certDir)
			if got != tc.want {
				t.Errorf("ПРЕДПОСЫЛКА ГЕЙТА НЕ ДЕРЖИТСЯ: корней %d, ожидалось %d.\n"+
					"случай доказывал: %s\n"+
					"это значит, что требование соседнего гейта («ТОЛЬКО-ВНУТРЕННЕЕ ⇒ пинить обе переменные») "+
					"опирается на факт, которого больше нет — сверьте гейт, а не подгоняйте число",
					got, tc.want, tc.proves)
			}
		})
	}

	// ── НЕСУЩЕЕ СЛЕДСТВИЕ, СКАЗАННОЕ ОТДЕЛЬНО ──
	// Три случая выше по отдельности — числа; вместе они означают ровно одно, и
	// именно это утверждает гейт.
	both := countRootsUnder(t, lone, absentDir)
	fileOnly := countRootsUnder(t, lone, anchors)
	if !(both < fileOnly) {
		t.Errorf("пиннинг ОДНОЙ переменной сузил набор не меньше, чем пиннинг ОБЕИХ (%d против %d) — "+
			"тогда требование гейта избыточно и его надо снимать, а не держать", fileOnly, both)
	}
}
