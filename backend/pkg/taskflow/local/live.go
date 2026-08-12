package local

import (
	"sync"
	"time"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

// liveChunkLimit 内存环形缓冲上限（chunks）。超过后丢弃最旧的，
// 保证 flush=true 的重放不会无限增长。
const liveChunkLimit = 20000

// LiveStream 是任务的实时输出流：环形缓冲（供重放）+ 订阅者广播。
// 语义与远端 taskflow 的 task-live 一致。
type LiveStream struct {
	mu     sync.Mutex
	chunks []*taskflow.TaskChunk
	subs   map[chan *taskflow.TaskChunk]struct{}
	seq    uint64
}

func NewLiveStream() *LiveStream {
	return &LiveStream{subs: make(map[chan *taskflow.TaskChunk]struct{})}
}

// Publish 追加一个 chunk 并广播给所有订阅者（非阻塞；订阅者队列满则丢弃该 chunk）。
func (l *LiveStream) Publish(chunk *taskflow.TaskChunk) {
	l.mu.Lock()
	if chunk.Seq == 0 {
		l.seq++
		chunk.Seq = l.seq
	}
	if chunk.Timestamp == 0 {
		chunk.Timestamp = time.Now().UnixNano()
	}
	l.chunks = append(l.chunks, chunk)
	if len(l.chunks) > liveChunkLimit {
		l.chunks = append([]*taskflow.TaskChunk(nil), l.chunks[len(l.chunks)-liveChunkLimit:]...)
	}
	for ch := range l.subs {
		select {
		case ch <- chunk:
		default:
		}
	}
	l.mu.Unlock()
}

// Replay 重放历史 chunk。
func (l *LiveStream) Replay(fn func(*taskflow.TaskChunk) error) error {
	l.mu.Lock()
	chunks := make([]*taskflow.TaskChunk, len(l.chunks))
	copy(chunks, l.chunks)
	l.mu.Unlock()
	for _, c := range chunks {
		if err := fn(c); err != nil {
			return err
		}
	}
	return nil
}

func (l *LiveStream) Subscribe(ch chan *taskflow.TaskChunk) {
	l.mu.Lock()
	l.subs[ch] = struct{}{}
	l.mu.Unlock()
}

func (l *LiveStream) Unsubscribe(ch chan *taskflow.TaskChunk) {
	l.mu.Lock()
	delete(l.subs, ch)
	l.mu.Unlock()
}
