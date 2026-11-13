package service

import (
	"strconv"
	"sync"
	"time"
)

// 槽位释放唤醒。
//
// 等待槽位的请求原本只靠退避轮询（100ms 起、最长 2s）发现空位：槽位空出后平均要
// 晚约 1 秒才被领取，期间新到的请求还可能先抢走，排在前面的请求反而越等越久。
// 本进程内释放槽位时，按入队顺序唤醒最早的一个等待者立即去抢；轮询保留为兜底
// （跨进程释放、槽位过期清理等不经过这里的情况）。

// SlotKind 区分账号槽位与用户槽位。
type SlotKind string

const (
	SlotKindAccount SlotKind = "account"
	SlotKindUser    SlotKind = "user"
)

func slotWaitKey(kind SlotKind, id int64) string {
	return string(kind) + ":" + strconv.FormatInt(id, 10)
}

type slotWaitHub struct {
	mu     sync.Mutex
	seq    uint64
	queues map[string][]*SlotReleaseWatch
}

func newSlotWaitHub() *slotWaitHub {
	return &slotWaitHub{queues: make(map[string][]*SlotReleaseWatch)}
}

// SlotReleaseWatch 是一个等待者的登记。等待者在 C() 上收到信号后应立即尝试抢槽；
// 抢槽失败继续等待即可，排队位置不变。用完必须 Close。
type SlotReleaseWatch struct {
	hub    *slotWaitHub
	seq    uint64
	ch     chan struct{}
	keys   map[string]struct{}
	closed bool
}

func (h *slotWaitHub) watch() *SlotReleaseWatch {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	return &SlotReleaseWatch{hub: h, seq: h.seq, ch: make(chan struct{}, 1), keys: make(map[string]struct{})}
}

// C 在被唤醒时可读。
func (w *SlotReleaseWatch) C() <-chan struct{} {
	if w == nil {
		return nil
	}
	return w.ch
}

// Add 让等待者开始关注更多槽位；排队先后仍按最初登记的顺序。
func (w *SlotReleaseWatch) Add(kind SlotKind, ids ...int64) {
	if w == nil {
		return
	}
	h := w.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if w.closed {
		return
	}
	for _, id := range ids {
		key := slotWaitKey(kind, id)
		if _, ok := w.keys[key]; ok {
			continue
		}
		w.keys[key] = struct{}{}
		queue := h.queues[key]
		pos := len(queue)
		for pos > 0 && queue[pos-1].seq > w.seq {
			pos--
		}
		queue = append(queue, nil)
		copy(queue[pos+1:], queue[pos:])
		queue[pos] = w
		h.queues[key] = queue
	}
}

// Close 注销等待者。
func (w *SlotReleaseWatch) Close() {
	if w == nil {
		return
	}
	h := w.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	w.closed = true
	for key := range w.keys {
		queue := h.queues[key]
		for i, other := range queue {
			if other == w {
				queue = append(queue[:i], queue[i+1:]...)
				break
			}
		}
		if len(queue) == 0 {
			delete(h.queues, key)
		} else {
			h.queues[key] = queue
		}
	}
	w.keys = map[string]struct{}{}
}

// notify 唤醒关注该槽位、且当前没有未处理信号的最早等待者。
func (h *slotWaitHub) notify(kind SlotKind, id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, w := range h.queues[slotWaitKey(kind, id)] {
		select {
		case w.ch <- struct{}{}:
			return
		default:
			// 已有待处理信号（正在抢槽），交给下一个等待者。
		}
	}
}

// WatchSlotRelease 登记一个等待者，在本进程释放对应槽位时被唤醒。
func (s *ConcurrencyService) WatchSlotRelease(kind SlotKind, ids ...int64) *SlotReleaseWatch {
	if s == nil || s.waitHub == nil {
		return nil
	}
	w := s.waitHub.watch()
	w.Add(kind, ids...)
	return w
}

func (s *ConcurrencyService) notifySlotReleased(kind SlotKind, id int64) {
	if s != nil && s.waitHub != nil {
		s.waitHub.notify(kind, id)
	}
}

// Poke 主动唤醒等待者一次（例如等待计划的宽限期结束、允许改用其他账号时）。
func (w *SlotReleaseWatch) Poke() {
	if w == nil {
		return
	}
	select {
	case w.ch <- struct{}{}:
	default:
	}
}

// 账号槽位占用时长统计。
//
// 粘性账号忙时要估算「等它空出一个槽位要多久」：并发 c、前面已有 n 人排队、单次占用
// 平均 D 时，期望等待约 (n+1)·D/c。D 按账号在本进程内做指数滑动平均，释放槽位时更新。

const accountHoldEWMAAlpha = 0.2

type accountHoldStats struct {
	mu   sync.Mutex
	ewma map[int64]float64 // 秒
}

func (h *accountHoldStats) observe(accountID int64, d time.Duration) {
	if h == nil || d <= 0 {
		return
	}
	sec := d.Seconds()
	h.mu.Lock()
	defer h.mu.Unlock()
	if prev, ok := h.ewma[accountID]; ok {
		h.ewma[accountID] = prev + accountHoldEWMAAlpha*(sec-prev)
	} else {
		h.ewma[accountID] = sec
	}
}

// AccountMeanHold 返回本进程观测到的该账号单次槽位平均占用时长。
func (s *ConcurrencyService) AccountMeanHold(accountID int64) (time.Duration, bool) {
	if s == nil || s.holdStats == nil {
		return 0, false
	}
	s.holdStats.mu.Lock()
	defer s.holdStats.mu.Unlock()
	sec, ok := s.holdStats.ewma[accountID]
	if !ok {
		return 0, false
	}
	return time.Duration(sec * float64(time.Second)), true
}

// TrackClaudeOAuthHold records only successfully acquired Claude OAuth slots.
// The wrapper also makes repeated release paths idempotent.
func (s *ConcurrencyService) TrackClaudeOAuthHold(accountID int64, release func()) func() {
	if release == nil || s == nil {
		return release
	}
	acquiredAt := time.Now()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.holdStats.observe(accountID, time.Since(acquiredAt))
			release()
		})
	}
}
