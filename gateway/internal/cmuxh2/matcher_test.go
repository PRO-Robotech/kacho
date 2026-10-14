// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cmuxh2

import (
	"bytes"
	"io"
	"runtime"
	"testing"

	"github.com/soheilhy/cmux"
	"golang.org/x/net/http2/hpack"
)

func headerBlock(fields ...string) []byte {
	var buf bytes.Buffer
	enc := hpack.NewEncoder(&buf)
	for i := 0; i+1 < len(fields); i += 2 {
		_ = enc.WriteField(hpack.HeaderField{Name: fields[i], Value: fields[i+1]})
	}
	return buf.Bytes()
}

// streamFrame — кадр потока 1: HEADERS и CONTINUATION на потоке 0 разбор
// отвергает как ошибку соединения, и матчер решал бы «не совпало» по ней.
func streamFrame(typ, flags byte, payload ...byte) []byte {
	f := frame(typ, flags, payload...)
	f[8] = 1
	return f
}

const (
	flagEndHeaders = 0x4
	frameCont      = 0x9
)

// Корпус входов, на которых матчер обязан решать и писать ровно то же, что
// матчер cmux, — настоящий, а не его пересказ.
func matcherCorpus() map[string][]byte {
	pre := []byte(clientPreface)
	grpcHdr := headerBlock(":method", "POST", ":path", "/x.Svc/M", "content-type", "application/grpc")
	restHdr := headerBlock(":method", "GET", ":path", "/iam/v1/me", "content-type", "application/json")
	noCT := headerBlock(":method", "GET", ":path", "/healthz")
	split := len(grpcHdr) / 2
	return map[string][]byte{
		"grpc":                    cat(pre, settings, windowUp, streamFrame(frameHeaders, flagEndHeaders, grpcHdr...)),
		"rest json":               cat(pre, settings, streamFrame(frameHeaders, flagEndHeaders, restHdr...)),
		"no content-type":         cat(pre, settings, streamFrame(frameHeaders, flagEndHeaders, noCT...)),
		"two settings, ack":       cat(pre, settings, settingsAck, settings, streamFrame(frameHeaders, flagEndHeaders, grpcHdr...)),
		"grpc over continuation":  cat(pre, settings, streamFrame(frameHeaders, 0, grpcHdr[:split]...), streamFrame(frameCont, flagEndHeaders, grpcHdr[split:]...)),
		"http1":                   []byte("GET /iam/v1/me HTTP/1.1\r\nHost: x\r\n\r\n"),
		"preface then eof":        cat(pre, settings),
		"short http1-like prefix": []byte("PRI * HTTP/1.1\r\n"),
	}
}

// Законный близнец: на корпусе решение и записанные байты совпадают с cmux.
func TestMatcherAgreesWithCmux(t *testing.T) {
	ours := MatchHeaderFieldSendSettings("content-type", "application/grpc")
	theirs := cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc")
	var matched, unmatched int
	for name, in := range matcherCorpus() {
		var wOurs, wTheirs bytes.Buffer
		gotOurs := ours(&wOurs, bytes.NewReader(in))
		gotTheirs := theirs(&wTheirs, bytes.NewReader(in))
		if gotTheirs {
			matched++
		} else {
			unmatched++
		}
		if gotOurs != gotTheirs {
			t.Errorf("%s: решение %v, у cmux %v", name, gotOurs, gotTheirs)
		}
		if !bytes.Equal(wOurs.Bytes(), wTheirs.Bytes()) {
			t.Errorf("%s: записано %q, у cmux %q", name, wOurs.Bytes(), wTheirs.Bytes())
		}
	}
	t.Logf("корпус: совпало у cmux %d · не совпало %d", matched, unmatched)
	if matched < 2 || unmatched < 2 {
		t.Fatalf("корпус не различает исходы (совпало %d, не совпало %d) — сравнение с cmux ничего не утверждает", matched, unmatched)
	}
}

// allocatedBy — байты кучи, выделенные вызовом f.
func allocatedBy(f func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	f()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// Предмет: заголовок длинного кадра до HEADERS не выделяет буфер по своей
// длине. Входом служит только заголовок — тела нет, как у соединения, которое
// заявило кадр и замолчало. Сравнение с cmux показывает, что замер способен
// увидеть выделение (иначе «мало» значило бы «не меряли»).
func TestMatcherDoesNotAllocateByAnnouncedFrameLength(t *testing.T) {
	const announced = 1<<20 - 1
	in := cat([]byte(clientPreface), settings,
		frameHeaderOfLength(announced, 0xfa))
	ours := MatchHeaderFieldSendSettings("content-type", "application/grpc")
	theirs := cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc")

	var got bool
	oursAlloc := allocatedBy(func() { got = ours(io.Discard, bytes.NewReader(in)) })
	theirsAlloc := allocatedBy(func() { _ = theirs(io.Discard, bytes.NewReader(in)) })
	t.Logf("выделено на заявленный кадр %d Б: наш матчер %d Б · cmux %d Б", announced, oursAlloc, theirsAlloc)
	if got {
		t.Fatal("кадр неизвестного типа без заголовков признан gRPC")
	}
	if theirsAlloc < announced {
		t.Fatalf("замер не видит выделения: у cmux %d Б при заявленных %d — сравнивать не с чем", theirsAlloc, announced)
	}
	if oursAlloc > 2*matchReadFrameSize+MatchHeaderBudget {
		t.Fatalf("матчер выделил %d Б на заявленный кадр — буфер растёт по длине из заголовка кадра", oursAlloc)
	}
}

// countingReader считает байты, которые у него забрали.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// endlessContinuation — блок заголовков, который не кончается: HEADERS без
// END_HEADERS и сколько угодно CONTINUATION с полями, не несущими content-type.
type endlessContinuation struct{ buf bytes.Buffer }

func (e *endlessContinuation) Read(p []byte) (int, error) {
	if e.buf.Len() == 0 {
		e.buf.Write(streamFrame(frameCont, 0, headerBlock("x-pad", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")...))
	}
	return e.buf.Read(p)
}

// Предмет: матчер читает соединение не дальше бюджета, даже когда блок
// заголовков не кончается. Без бюджета он читал бы, пока соединение шлёт.
func TestMatcherStopsAtTheHeaderBudget(t *testing.T) {
	head := cat([]byte(clientPreface), settings, streamFrame(frameHeaders, 0, headerBlock(":method", "POST")...))
	src := &countingReader{r: io.MultiReader(bytes.NewReader(head), io.LimitReader(&endlessContinuation{}, 64*MatchHeaderBudget))}
	got := MatchHeaderFieldSendSettings("content-type", "application/grpc")(io.Discard, src)
	t.Logf("прочитано матчером %d Б при бюджете %d Б", src.n, MatchHeaderBudget)
	if got {
		t.Fatal("блок без content-type признан gRPC")
	}
	if src.n > MatchHeaderBudget {
		t.Fatalf("матчер прочитал %d Б — больше бюджета %d", src.n, MatchHeaderBudget)
	}
	if src.n < MatchHeaderBudget/2 {
		t.Fatalf("матчер прочитал %d Б и остановился раньше бюджета — проба не дошла до предмета", src.n)
	}
}
