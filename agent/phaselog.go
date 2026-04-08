package agent

import "sync"

const phaseLogRingSize = 100

// PhaseLogBuffer is a thread-safe ring buffer with live follower support.
// It stores the last phaseLogRingSize lines and fans out new lines to followers.
type PhaseLogBuffer struct {
	mu        sync.Mutex
	ring      []string
	pos       int // next write position
	count     int // total lines written (for Snapshot ordering)
	followers map[chan string]struct{}
}

// NewPhaseLogBuffer creates a new PhaseLogBuffer.
func NewPhaseLogBuffer() *PhaseLogBuffer {
	return &PhaseLogBuffer{
		ring:      make([]string, phaseLogRingSize),
		followers: make(map[chan string]struct{}),
	}
}

// Write appends a line to the ring buffer and fans out to all followers (non-blocking).
func (b *PhaseLogBuffer) Write(line string) {
	b.mu.Lock()
	b.ring[b.pos] = line
	b.pos = (b.pos + 1) % phaseLogRingSize
	if b.count < phaseLogRingSize {
		b.count++
	}
	// Copy followers to avoid holding lock during send
	followers := make([]chan string, 0, len(b.followers))
	for ch := range b.followers {
		followers = append(followers, ch)
	}
	b.mu.Unlock()

	for _, ch := range followers {
		select {
		case ch <- line:
		default:
			// drop if follower is slow
		}
	}
}

// Snapshot returns buffered lines in order.
func (b *PhaseLogBuffer) Snapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.count == 0 {
		return nil
	}

	n := b.count
	if n > phaseLogRingSize {
		n = phaseLogRingSize
	}

	lines := make([]string, 0, n)
	start := (b.pos - n + phaseLogRingSize) % phaseLogRingSize
	for i := 0; i < n; i++ {
		lines = append(lines, b.ring[(start+i)%phaseLogRingSize])
	}
	return lines
}

// Follow subscribes to new lines. Returns a channel that receives new lines.
func (b *PhaseLogBuffer) Follow() chan string {
	ch := make(chan string, 64)
	b.mu.Lock()
	b.followers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unfollow removes a subscriber and closes its channel.
func (b *PhaseLogBuffer) Unfollow(ch chan string) {
	b.mu.Lock()
	if _, ok := b.followers[ch]; ok {
		delete(b.followers, ch)
		close(ch)
	}
	b.mu.Unlock()
}

// Clear resets the buffer and closes all follower channels to signal phase completion.
func (b *PhaseLogBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.followers {
		close(ch)
	}
	b.followers = make(map[chan string]struct{})
	b.pos = 0
	b.count = 0
	for i := range b.ring {
		b.ring[i] = ""
	}
}
