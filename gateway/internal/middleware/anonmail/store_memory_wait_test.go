// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestMemoryStore_CX2_93_OnlyTheBucketWaitIsABucketWaitTimeout — у memory
// ожидание ограничено сроком (CX2-93 (б)) и различимо по месту (CX2-93 (а)):
//
//   - полоса ключа удержана соседом → 503 в пределах anonMailStoreWait,
//     недоступности +1, ожиданий ведра +0;
//   - ведро удержано соседом → 503 в пределах anonMailStoreWait, +1 и +1;
//   - близнец: ничего не удержано → пропуск, оба счётчика 0.
//
// Ожидание без срока (sync.Mutex) повесило бы первые два варианта, и проба
// упала бы по сроку; счётчик по классу ошибки, а не по шагу, покрасил бы
// первый вариант. Строка журнала несёт префикс пакета один раз (GS-E2-10).
func TestMemoryStore_CX2_93_OnlyTheBucketWaitIsABucketWaitTimeout(t *testing.T) {
	const src = "198.51.100.93"
	keys, err := KeysFor(src)
	if err != nil {
		t.Fatal(err)
	}
	stripe := int(uint32(keyLockPair(keys.Source).obj) % memStripes) // #nosec G115 -- номер полосы
	cases := []struct {
		name               string
		hold               func(m *MemoryStore) chan struct{}
		code               int
		unavailable, waits uint64
		step               string
	}{
		{"полоса ключа удержана", func(m *MemoryStore) chan struct{} { return m.stripes[stripe] }, http.StatusServiceUnavailable, 1, 0, stepLock},
		{"ведро удержано", func(m *MemoryStore) chan struct{} { return m.bucketLock }, http.StatusServiceUnavailable, 1, 1, stepBucket},
		{"близнец: ничего не удержано", func(*MemoryStore) chan struct{} { return nil }, http.StatusOK, 0, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := testLimits()
			logs := &syncBuffer{}
			var mem *MemoryStore
			r := newRig(t, l, func(cl *testClock) Store {
				m, err := NewMemoryStore(l, cl.Now, slog.New(slog.NewTextHandler(logs, nil)))
				if err != nil {
					t.Fatal(err)
				}
				mem = m
				return m
			}, 0)
			if ch := c.hold(mem); ch != nil {
				ch <- struct{}{}
				defer func() { <-ch }()
			}
			done := make(chan int, 1)
			start := time.Now()
			go func() { done <- r.send(pathRecovery, src, "", "").Code }()
			var code int
			select {
			case code = <-done:
			case <-time.After(10 * anonMailStoreWait):
				t.Fatalf("ожидание не ограничено: ответа нет за %s", 10*anonMailStoreWait)
			}
			took := time.Since(start)
			if code != c.code {
				t.Fatalf("ответ %d, ожидался %d", code, c.code)
			}
			if c.code == http.StatusServiceUnavailable && took > anonMailStoreWait+200*time.Millisecond {
				t.Errorf("503 через %s — ожидание дольше anonMailStoreWait", took)
			}
			if s := r.gate.Stats(); s.StoreUnavailable != c.unavailable || s.BucketWaitTimeouts != c.waits {
				t.Errorf("счётчики %+v, ожидалось недоступности %d, ожиданий ведра %d", s, c.unavailable, c.waits)
			}
			for _, ln := range strings.Split(logs.String(), "\n") {
				if c.step != "" && strings.Contains(ln, "step="+c.step) && strings.Count(ln, "anonmail:") != 1 {
					t.Errorf("префикс «anonmail:» в строке журнала не один: %q", ln)
				}
			}
			t.Logf("%s: %d за %s · %+v", c.name, code, took, r.gate.Stats())
		})
	}
}
