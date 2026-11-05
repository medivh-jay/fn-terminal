package terminal

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type Quota struct {
	mu                     sync.Mutex
	perUser, total, active int
	users                  map[uint32]int
}

func NewQuota(perUser, total int) *Quota {
	return &Quota{perUser: perUser, total: total, users: make(map[uint32]int)}
}
func (q *Quota) Acquire(uid uint32) (func(), error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.active >= q.total || q.users[uid] >= q.perUser {
		return nil, errors.New("terminal session limit reached")
	}
	q.active++
	q.users[uid]++
	var once sync.Once
	return func() {
		once.Do(func() {
			q.mu.Lock()
			defer q.mu.Unlock()
			q.active--
			q.users[uid]--
			if q.users[uid] == 0 {
				delete(q.users, uid)
			}
		})
	}, nil
}

// Cancellation causes recorded when a session reaches an administrator limit.
// They let the broker tell the user why the shell ended instead of just dropping it.
var (
	ErrIdleExpired = errors.New("terminal idle limit reached")
	ErrMaxExpired  = errors.New("terminal duration limit reached")
)

type Activity struct {
	last  atomic.Int64
	start time.Time
}

func NewActivity() *Activity { a := &Activity{start: time.Now()}; a.Touch(); return a }
func (a *Activity) Touch()   { a.last.Store(time.Now().UnixNano()) }
func (a *Activity) Monitor(ctx context.Context, cancel context.CancelCauseFunc, idle, max time.Duration) {
	step := min(idle/4, max/4, time.Second)
	if step < time.Millisecond {
		step = time.Millisecond
	}
	t := time.NewTicker(step)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if now.Sub(a.start) >= max {
				cancel(ErrMaxExpired)
				return
			}
			if now.Sub(time.Unix(0, a.last.Load())) >= idle {
				cancel(ErrIdleExpired)
				return
			}
		}
	}
}
