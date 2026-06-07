package agent

import (
	"context"
	"sync"
	"time"

	"github.com/mcpeixoto/hopper/internal/client"
)

// logSink is an io.Writer that batches a running job's stdout/stderr and streams
// it to the control plane as live log appends. Writes are buffered and flushed
// either when the buffer grows past flushBytes or on a periodic tick.
type logSink struct {
	client *client.Client
	jobID  string

	mu  sync.Mutex
	buf []byte
}

const (
	flushBytes    = 4 << 10 // flush when buffered output exceeds 4 KiB
	flushInterval = 750 * time.Millisecond
)

func newLogSink(c *client.Client, jobID string) *logSink {
	return &logSink{client: c, jobID: jobID}
}

// Write buffers output, flushing eagerly once it gets large.
func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.buf = append(s.buf, p...)
	big := len(s.buf) >= flushBytes
	s.mu.Unlock()
	if big {
		s.flush(context.Background())
	}
	return len(p), nil
}

// run flushes on a ticker until ctx is cancelled.
func (s *logSink) run(ctx context.Context) {
	t := time.NewTicker(flushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.flush(ctx)
		}
	}
}

// flush sends any buffered output (best-effort; errors are ignored since logs are
// also captured and uploaded as an artifact at completion).
func (s *logSink) flush(ctx context.Context) {
	s.mu.Lock()
	if len(s.buf) == 0 {
		s.mu.Unlock()
		return
	}
	data := s.buf
	s.buf = nil
	s.mu.Unlock()
	fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = s.client.AppendLog(fctx, s.jobID, data)
}
