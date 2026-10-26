package events

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// MaxLogSize is the size at which events.log is rotated (deployment.md
// §9.4).
const MaxLogSize = 10 << 20

// LogWriter appends frames to /work/.playground/events.log, one JSON object
// per line, rotating to events.log.1 at MaxLogSize. Writes happen on a
// background goroutine; when the queue is full frames are dropped rather
// than blocking the bus.
type LogWriter struct {
	path  string
	queue chan []byte
	done  chan struct{}
	once  sync.Once
	log   *slog.Logger
	// MaxSize overrides MaxLogSize (tests).
	MaxSize int64
}

// NewLogWriter starts a writer for path. Call Close to flush and stop.
func NewLogWriter(path string, log *slog.Logger) *LogWriter {
	if log == nil {
		log = slog.Default()
	}
	w := &LogWriter{path: path, queue: make(chan []byte, 1024), done: make(chan struct{}), log: log, MaxSize: MaxLogSize}
	go w.run()
	return w
}

// Sink is a Bus sink.
func (w *LogWriter) Sink(f Frame) {
	select {
	case w.queue <- f.Data:
	default:
	}
}

// Close flushes queued frames and stops the writer.
func (w *LogWriter) Close() {
	w.once.Do(func() { close(w.queue) })
	<-w.done
}

func (w *LogWriter) run() {
	defer close(w.done)
	var f *os.File
	var size int64
	open := func() {
		if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
			w.log.Warn("event log", "err", err)
			return
		}
		var err error
		f, err = os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			w.log.Warn("event log", "err", err)
			f = nil
			return
		}
		if st, err := f.Stat(); err == nil {
			size = st.Size()
		}
	}
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()
	for data := range w.queue {
		if f == nil {
			open()
			if f == nil {
				continue
			}
		}
		if size+int64(len(data))+1 > w.MaxSize && size > 0 {
			_ = f.Close()
			_ = os.Rename(w.path, w.path+".1")
			f = nil
			size = 0
			open()
			if f == nil {
				continue
			}
		}
		n, err := f.Write(append(data, '\n'))
		size += int64(n)
		if err != nil {
			w.log.Warn("event log write", "err", err)
		}
	}
}
