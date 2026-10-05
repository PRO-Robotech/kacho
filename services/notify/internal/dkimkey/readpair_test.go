// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dkimkey_test

// readpair_test.go — чтение пары DKIM из одного поколения тома (полоса N14;
// приёмка NTF-1 NTF1-P14, P17; замысел §12а «Ключ и селектор DKIM», CX1-134,
// CX1-140; постоянная §8 `dkimkey.generationRetries = 2`).
//
// Контракт испытуемого, который утверждают эти пробы (имена — из §12а):
//
//	type PairFS interface {
//	    Readlink(name string) (string, error)
//	    ReadFile(name string) ([]byte, error)
//	}
//	var OS PairFS                                   // порт корня — пакет os
//	type Pair struct {
//	    Selector   string
//	    Key        *rsa.PrivateKey
//	    Generation string                           // разрешённое поколение `..data`
//	    Digest     [32]byte                         // SHA-256(len‖ключ‖len‖селектор)
//	}
//	func ReadPair(keyFile, selectorFile string) (Pair, error)        // = ReadPairFS(OS, …)
//	func ReadPairFS(fsys PairFS, keyFile, selectorFile string) (Pair, error)
//
// Каталог тома — форма kubelet: `..<поколение>/<имя>`, `..data` → поколение,
// `<имя>` → `..data/<имя>`. Переключение поколения (шаг 9 kubelet) и удаление
// прежнего (шаг 12) делает крючок пробы между чтениями — у порта чтения
// удаления нет (CX1-140 (в)).

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/PRO-Robotech/kacho/services/notify/internal/dkimkey"
)

const (
	keyName = "dkim.key"
	selName = "dkim.selector"
	genA    = "..2026_10_05_00_00_00.000000001"
	genB    = "..2026_10_05_00_01_00.000000002"
	genC    = "..2026_10_05_00_02_00.000000003"
)

var (
	keysOnce sync.Once
	keyA     *rsa.PrivateKey
	keyB     *rsa.PrivateKey
	keysErr  error
)

func keys(t *testing.T) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	keysOnce.Do(func() {
		if keyA, keysErr = rsa.GenerateKey(rand.Reader, 2048); keysErr != nil {
			return
		}
		keyB, keysErr = rsa.GenerateKey(rand.Reader, 2048)
	})
	if keysErr != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ключи пробы не выпущены: %v", keysErr)
	}
	return keyA, keyB
}

func keyPEM(k *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
}

// volume — каталог тома формы kubelet под управлением пробы.
type volume struct {
	t   *testing.T
	dir string
}

func newVolume(t *testing.T) *volume {
	t.Helper()
	return &volume{t: t, dir: t.TempDir()}
}

// generation кладёт поколение с файлами; ссылки `<имя>` → `..data/<имя>`
// заводятся для каждого имени один раз.
func (v *volume) generation(gen string, files map[string][]byte) {
	v.t.Helper()
	if err := os.Mkdir(filepath.Join(v.dir, gen), 0o755); err != nil {
		v.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: поколение %s: %v", gen, err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(v.dir, gen, name), body, 0o600); err != nil {
			v.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: файл %s/%s: %v", gen, name, err)
		}
		link := filepath.Join(v.dir, name)
		if _, err := os.Lstat(link); os.IsNotExist(err) {
			if err := os.Symlink(filepath.Join("..data", name), link); err != nil {
				v.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ссылка %s: %v", name, err)
			}
		}
	}
}

// swap — шаг 9 kubelet: атомарное переключение `..data` на поколение.
func (v *volume) swap(gen string) {
	v.t.Helper()
	tmp := filepath.Join(v.dir, "..data_tmp")
	if err := os.Symlink(gen, tmp); err != nil {
		v.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ..data_tmp: %v", err)
	}
	if err := os.Rename(tmp, filepath.Join(v.dir, "..data")); err != nil {
		v.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: переключение ..data на %s: %v", gen, err)
	}
}

// removeGeneration — шаг 12 kubelet: прежний каталог поколения удалён.
func (v *volume) removeGeneration(gen string) {
	v.t.Helper()
	if err := os.RemoveAll(filepath.Join(v.dir, gen)); err != nil {
		v.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: удаление %s: %v", gen, err)
	}
}

func (v *volume) paths() (string, string) {
	return filepath.Join(v.dir, keyName), filepath.Join(v.dir, selName)
}

// hookFS — порт чтения поверх настоящего каталога: считает `Readlink` и зовёт
// крючок после каждого `ReadFile` (номер чтения — с единицы).
type hookFS struct {
	mu        sync.Mutex
	readlinks int
	reads     int
	after     func(n int)
}

func (h *hookFS) Readlink(name string) (string, error) {
	h.mu.Lock()
	h.readlinks++
	h.mu.Unlock()
	return os.Readlink(name)
}

func (h *hookFS) ReadFile(name string) ([]byte, error) {
	b, err := os.ReadFile(name)
	h.mu.Lock()
	h.reads++
	n := h.reads
	h.mu.Unlock()
	if h.after != nil {
		h.after(n)
	}
	return b, err
}

func samePair(t *testing.T, p dkimkey.Pair, wantSel string, wantKey *rsa.PrivateKey, wantGen string) {
	t.Helper()
	if p.Selector != wantSel {
		t.Fatalf("селектор пары %q, ожидался %q", p.Selector, wantSel)
	}
	if p.Key == nil || p.Key.N.Cmp(wantKey.N) != 0 || p.Key.E != wantKey.E {
		t.Fatalf("ключ пары не из поколения %s: пара смешанного поколения", wantGen)
	}
	if p.Generation != wantGen {
		t.Fatalf("поколение пары %q, ожидалось %q", p.Generation, wantGen)
	}
}

// Близнец всех проб файла: поколение одно, переключения нет — пара A целиком.
func TestReadPairReadsOneGeneration(t *testing.T) {
	a, _ := keys(t)
	v := newVolume(t)
	v.generation(genA, map[string][]byte{keyName: keyPEM(a), selName: []byte("a")})
	v.swap(genA)
	k, s := v.paths()
	p, err := dkimkey.ReadPairFS(&hookFS{}, k, s)
	if err != nil {
		t.Fatalf("пара одного поколения не прочитана: %v", err)
	}
	samePair(t, p, "a", a, genA)
	p2, err := dkimkey.ReadPair(k, s)
	if err != nil {
		t.Fatalf("ReadPair на порту os не прочитал ту же пару: %v", err)
	}
	if p2.Digest != p.Digest {
		t.Fatal("дайджест одной и той же пары различается между двумя чтениями")
	}
}

// CX1-134: `..data` переключён на B между чтением ключа и селектора, A не
// удалён — пара читается целиком из разрешённого A.
func TestReadPairSwitchBetweenReadsYieldsOneGeneration(t *testing.T) {
	a, b := keys(t)
	v := newVolume(t)
	v.generation(genA, map[string][]byte{keyName: keyPEM(a), selName: []byte("a")})
	v.generation(genB, map[string][]byte{keyName: keyPEM(b), selName: []byte("b")})
	v.swap(genA)
	k, s := v.paths()
	fs := &hookFS{after: func(n int) {
		if n == 1 {
			v.swap(genB)
		}
	}}
	p, err := dkimkey.ReadPairFS(fs, k, s)
	if err != nil {
		t.Fatalf("переключение без удаления дало отказ: %v", err)
	}
	samePair(t, p, "a", a, genA)
}

// CX1-140: переключение на B и удаление A между чтениями (шаги 9 и 12 kubelet)
// — повтор разрешения `..data`, пара B целиком.
func TestReadPairRemovedGenerationIsReResolved(t *testing.T) {
	a, b := keys(t)
	v := newVolume(t)
	v.generation(genA, map[string][]byte{keyName: keyPEM(a), selName: []byte("a")})
	v.generation(genB, map[string][]byte{keyName: keyPEM(b), selName: []byte("b")})
	v.swap(genA)
	k, s := v.paths()
	fs := &hookFS{after: func(n int) {
		if n == 1 {
			v.swap(genB)
			v.removeGeneration(genA)
		}
	}}
	p, err := dkimkey.ReadPairFS(fs, k, s)
	if err != nil {
		t.Fatalf("удалённое поколение не разрешено заново: %v", err)
	}
	samePair(t, p, "b", b, genB)
}

// CX1-140: крючок переключает и удаляет поколение на каждом чтении —
// повторов не больше generationRetries = 2, исход «не читается».
func TestReadPairExhaustedRetriesIsUnreadable(t *testing.T) {
	a, b := keys(t)
	v := newVolume(t)
	gens := []string{genA, genB, genC, "..2026_10_05_00_03_00.000000004", "..2026_10_05_00_04_00.000000005"}
	for i, g := range gens {
		k := a
		if i%2 == 1 {
			k = b
		}
		v.generation(g, map[string][]byte{keyName: keyPEM(k), selName: []byte("s" + string(rune('a'+i)))})
	}
	v.swap(gens[0])
	cur := 0
	k, s := v.paths()
	fs := &hookFS{after: func(int) {
		if cur+1 < len(gens) {
			v.swap(gens[cur+1])
			v.removeGeneration(gens[cur])
			cur++
		}
	}}
	_, err := dkimkey.ReadPairFS(fs, k, s)
	if err == nil {
		t.Fatal("том, переключаемый на каждом чтении, дал пару — повтор не ограничен")
	}
	if !strings.Contains(err.Error(), "не читается") {
		t.Fatalf("исчерпание повторов названо не «не читается»: %v", err)
	}
	// Первое разрешение, generationRetries = 2 повтора и, быть может, одно
	// разрешение, установившее исчерпание; больше — повтор не ограничен.
	if fs.readlinks < 3 || fs.readlinks > 4 {
		t.Fatalf("разрешений ..data %d, ожидалось 3–4 (первое и generationRetries = 2 повтора)", fs.readlinks)
	}
}

// CX1-140: селектора нет при неизменном поколении — «нет в объекте», повтора
// нет: разрешений `..data` ровно 2 (первое и проверка «поколение то же»).
func TestReadPairMissingSelectorInAStableGenerationIsAbsent(t *testing.T) {
	a, _ := keys(t)
	v := newVolume(t)
	v.generation(genA, map[string][]byte{keyName: keyPEM(a)})
	v.swap(genA)
	k, s := v.paths()
	fs := &hookFS{}
	_, err := dkimkey.ReadPairFS(fs, k, s)
	if err == nil {
		t.Fatal("пара без селектора прочитана")
	}
	if !strings.Contains(err.Error(), "нет в объекте") || !strings.Contains(err.Error(), selName) {
		t.Fatalf("отсутствие селектора не названо «селектор DKIM %s: нет в объекте»: %v", selName, err)
	}
	if fs.readlinks != 2 {
		t.Fatalf("разрешений ..data %d, ожидалось ровно 2", fs.readlinks)
	}
}

// CX1-134 (а): пути ключа и селектора в разных каталогах — отказ с именем
// ручки селектора.
func TestReadPairPathsInDifferentDirectoriesAreRefused(t *testing.T) {
	a, _ := keys(t)
	v1, v2 := newVolume(t), newVolume(t)
	v1.generation(genA, map[string][]byte{keyName: keyPEM(a)})
	v1.swap(genA)
	v2.generation(genA, map[string][]byte{selName: []byte("a")})
	v2.swap(genA)
	k, _ := v1.paths()
	_, s := v2.paths()
	_, err := dkimkey.ReadPairFS(&hookFS{}, k, s)
	if err == nil {
		t.Fatal("пара из двух каталогов прочитана")
	}
	if !strings.Contains(err.Error(), "KACHO_NOTIFY_DKIM_SELECTOR_FILE") {
		t.Fatalf("отказ не называет ручку KACHO_NOTIFY_DKIM_SELECTOR_FILE: %v", err)
	}
}

// TestVolumeFixtureIsAKubeletVolume — фикстура доказывается раньше, чем ею
// судят: `..data` указывает на поколение, ссылка `<имя>` читается через него,
// переключение меняет прочитанное, удалённое поколение не читается.
// Испытуемого проба не зовёт: её красное — сломанная фикстура, а не предмет.
func TestVolumeFixtureIsAKubeletVolume(t *testing.T) {
	v := newVolume(t)
	v.generation(genA, map[string][]byte{selName: []byte("a")})
	v.generation(genB, map[string][]byte{selName: []byte("b")})
	v.swap(genA)
	_, s := v.paths()
	if gen, err := os.Readlink(filepath.Join(v.dir, "..data")); err != nil || gen != genA {
		t.Fatalf("..data → %q, %v; ожидалось %q", gen, err, genA)
	}
	if b, err := os.ReadFile(s); err != nil || string(b) != "a" {
		t.Fatalf("ссылка селектора прочитала %q, %v", b, err)
	}
	v.swap(genB)
	v.removeGeneration(genA)
	if b, err := os.ReadFile(s); err != nil || string(b) != "b" {
		t.Fatalf("после переключения ссылка прочитала %q, %v", b, err)
	}
	if _, err := os.ReadFile(filepath.Join(v.dir, genA, selName)); !os.IsNotExist(err) {
		t.Fatalf("удалённое поколение читается: %v", err)
	}
}
