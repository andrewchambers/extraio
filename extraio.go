package extraio

import (
	"bytes"
	"io"
	"io/ioutil"
	"net"
	"os/exec"
	"strconv"
	"sync/atomic"
	"time"
)

type MergedReadWriteCloser struct {
	RC io.ReadCloser
	WC io.WriteCloser
}

func (m *MergedReadWriteCloser) Read(buf []byte) (int, error) {
	return m.RC.Read(buf)
}

func (m *MergedReadWriteCloser) Write(buf []byte) (int, error) {
	return m.WC.Write(buf)
}

func (m *MergedReadWriteCloser) Close() error {
	_ = m.RC.Close()
	_ = m.WC.Close()
	return nil
}

func SocketPair() (io.ReadWriteCloser, io.ReadWriteCloser) {
	a, b := io.Pipe()
	x, y := io.Pipe()

	return &MergedReadWriteCloser{
			RC: a,
			WC: y,
		}, &MergedReadWriteCloser{
			RC: x,
			WC: b,
		}
}

// PrefixSuffixSaver from unexported stdlib code.

// PrefixSuffixSaver is an io.Writer which retains the first N bytes
// and the last N bytes written to it. The Bytes() methods reconstructs
// it with a pretty error message.
type PrefixSuffixSaver struct {
	N         int // max size of prefix or suffix
	prefix    []byte
	suffix    []byte // ring buffer once len(suffix) == N
	suffixOff int    // offset to write into suffix
	skipped   int64
}

func (w *PrefixSuffixSaver) Write(p []byte) (n int, err error) {
	lenp := len(p)
	p = w.fill(&w.prefix, p)

	// Only keep the last w.N bytes of suffix data.
	if overage := len(p) - w.N; overage > 0 {
		p = p[overage:]
		w.skipped += int64(overage)
	}
	p = w.fill(&w.suffix, p)

	// w.suffix is full now if p is non-empty. Overwrite it in a circle.
	for len(p) > 0 { // 0, 1, or 2 iterations.
		n := copy(w.suffix[w.suffixOff:], p)
		p = p[n:]
		w.skipped += int64(n)
		w.suffixOff += n
		if w.suffixOff == w.N {
			w.suffixOff = 0
		}
	}
	return lenp, nil
}

// fill appends up to len(p) bytes of p to *dst, such that *dst does not
// grow larger than w.N. It returns the un-appended suffix of p.
func (w *PrefixSuffixSaver) fill(dst *[]byte, p []byte) (pRemain []byte) {
	if remain := w.N - len(*dst); remain > 0 {
		add := minInt(len(p), remain)
		*dst = append(*dst, p[:add]...)
		p = p[add:]
	}
	return p
}

func (w *PrefixSuffixSaver) Bytes() []byte {
	if w.suffix == nil {
		return w.prefix
	}
	if w.skipped == 0 {
		return append(w.prefix, w.suffix...)
	}
	var buf bytes.Buffer
	buf.Grow(len(w.prefix) + len(w.suffix) + 50)
	buf.Write(w.prefix)
	buf.WriteString("\n... omitting ")
	buf.WriteString(strconv.FormatInt(w.skipped, 10))
	buf.WriteString(" bytes ...\n")
	buf.Write(w.suffix[w.suffixOff:])
	buf.Write(w.suffix[:w.suffixOff])
	return buf.Bytes()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Sets cmd.Stderr to io.Discard
// sets cmd.Stdout and cmd.Stdin to pipes connected
// to the returned read write closer.
func CmdReadWriteCloser(cmd *exec.Cmd) io.ReadWriteCloser {
	a, b := io.Pipe()
	x, y := io.Pipe()

	cmd.Stderr = ioutil.Discard
	cmd.Stdout = b
	cmd.Stdin = x

	rwc := &MergedReadWriteCloser{
		RC: a,
		WC: y,
	}

	return rwc
}

type MeteredConn struct {
	Conn net.Conn
	// if accessed concurrently, Read with sync/atomic
	ReadCount int64
	// if accessed concurrently, Read with sync/atomic
	WriteCount int64
}

func NewMeteredConn(c net.Conn) *MeteredConn {
	return &MeteredConn{
		Conn: c,
	}
}

func (mConn *MeteredConn) Read(buf []byte) (int, error) {
	n, err := mConn.Conn.Read(buf)
	atomic.AddInt64(&mConn.ReadCount, int64(n))
	return n, err
}

func (mConn *MeteredConn) Write(buf []byte) (int, error) {
	n, err := mConn.Conn.Write(buf)
	atomic.AddInt64(&mConn.WriteCount, int64(n))
	return n, err
}

func (mConn *MeteredConn) Close() error {
	return mConn.Conn.Close()
}

func (mConn *MeteredConn) LocalAddr() net.Addr {
	return mConn.Conn.LocalAddr()
}

func (mConn *MeteredConn) RemoteAddr() net.Addr {
	return mConn.Conn.RemoteAddr()
}

func (mConn *MeteredConn) SetDeadline(t time.Time) error {
	return mConn.Conn.SetDeadline(t)
}

func (mConn *MeteredConn) SetReadDeadline(t time.Time) error {
	return mConn.Conn.SetReadDeadline(t)
}

func (mConn *MeteredConn) SetWriteDeadline(t time.Time) error {
	return mConn.Conn.SetWriteDeadline(t)
}

type MeteredWriter struct {
	W          io.Writer
	WriteCount int64
}

func (mw *MeteredWriter) Write(buf []byte) (int, error) {
	n, err := mw.W.Write(buf)
	atomic.AddInt64(&mw.WriteCount, int64(n))
	return n, err
}

type MeteredReader struct {
	R         io.Reader
	ReadCount int64
}

func (mw *MeteredReader) Read(buf []byte) (int, error) {
	n, err := mw.R.Read(buf)
	atomic.AddInt64(&mw.ReadCount, int64(n))
	return n, err
}
