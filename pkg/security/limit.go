package security

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"time"
)

type byteLimiter struct {
	inner          io.Reader
	bytesPerSecond int64
	now            func() time.Time
	sleep          func(time.Duration)
	started        time.Time
	read           int64
}

func (r *byteLimiter) Read(p []byte) (int, error) {
	if r.bytesPerSecond > 0 && int64(len(p)) > r.bytesPerSecond {
		p = p[:r.bytesPerSecond]
	}
	n, err := r.inner.Read(p)
	r.read += int64(n)
	if r.bytesPerSecond > 0 {
		want := time.Duration(float64(r.read) / float64(r.bytesPerSecond) * float64(time.Second))
		if delay := want - r.now().Sub(r.started); delay > 0 {
			r.sleep(delay)
		}
	}
	return n, err
}

type limitedWriter struct {
	http.ResponseWriter
	bytesPerSecond int64
	started        time.Time
	written        int64
}

func (w *limitedWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *limitedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *limitedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.bytesPerSecond <= 0 {
		return w.ResponseWriter.Write(p)
	}
	total := 0
	for len(p) > 0 {
		next := len(p)
		if int64(next) > w.bytesPerSecond {
			next = int(w.bytesPerSecond)
		}
		n, err := w.ResponseWriter.Write(p[:next])
		total += n
		w.written += int64(n)
		p = p[n:]
		want := time.Duration(float64(w.written) / float64(w.bytesPerSecond) * float64(time.Second))
		if delay := want - time.Since(w.started); delay > 0 {
			time.Sleep(delay)
		}
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func RateLimit(next http.Handler, settings func() Settings) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := settings()
		if cfg.MaxUploadBytesSec > 0 && r.Body != nil {
			r.Body = struct {
				io.Reader
				io.Closer
			}{Reader: &byteLimiter{inner: r.Body, bytesPerSecond: cfg.MaxUploadBytesSec, now: time.Now, sleep: time.Sleep, started: time.Now()}, Closer: r.Body}
		}
		if cfg.MaxDownloadBytesSec > 0 {
			w = &limitedWriter{ResponseWriter: w, bytesPerSecond: cfg.MaxDownloadBytesSec, started: time.Now()}
		}
		next.ServeHTTP(w, r)
	})
}
