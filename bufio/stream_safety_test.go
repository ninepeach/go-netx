package bufio

import (
 "bytes"
 "errors"
 "io"
 "net"
 "sync"
 "testing"
 "time"

 "github.com/ninepeach/go-netx/buf"
)

type invalidExtendedReader struct { n int }
func (r invalidExtendedReader) Read([]byte) (int,error) { return 0, io.EOF }
func (r invalidExtendedReader) ReadBuffer(b *buf.Buf) (int,error) { return r.n,nil }

func TestReadBufferRejectsInvalidExtendedCount(t *testing.T) {
 for _, n := range []int{-1, 5} {
  b,err:=ReadBuffer(invalidExtendedReader{n:n},4)
  if b!=nil || !errors.Is(err,ErrInvalidCount) { t.Fatalf("n=%d: %v %v",n,b,err) }
 }
}

type blockingWriter struct {
 entered chan struct{}
 release chan struct{}
 once sync.Once
 bytes.Buffer
}
func (w *blockingWriter) Write(p []byte)(int,error){
 w.once.Do(func(){close(w.entered); <-w.release})
 return w.Buffer.Write(p)
}

func TestCachedConnReadWaitsForWriteTo(t *testing.T){
 a,b:=net.Pipe()
 defer a.Close(); defer b.Close()
 c:=NewCachedConn(a,buf.Copy([]byte("prefix")))
 w:=&blockingWriter{entered:make(chan struct{}),release:make(chan struct{})}
 finished:=make(chan struct{})
 go func(){ defer close(finished); _,_=c.WriteTo(w) }()
 <-w.entered
 readDone:=make(chan struct{})
 go func(){defer close(readDone); p:=make([]byte,4); _,_=c.Read(p)}()
 select {case <-readDone: t.Fatal("Read bypassed active WriteTo");case <-time.After(20*time.Millisecond):}
 close(w.release)
 _=b.Close()
 <-finished
 <-readDone
 if !bytes.HasPrefix(w.Bytes(),[]byte("prefix")) {t.Fatalf("lost prefix: %q",w.Bytes())}
}
