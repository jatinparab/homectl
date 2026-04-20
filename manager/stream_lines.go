package manager

import (
	"bufio"
	"context"
	"io"
	"sync"
)

// pipeLineStream reads newline-delimited data from stream and invokes handle for each line.
// It owns the stream lifecycle and stops when the context is canceled or handle returns false.
func (m *Manager) pipeLineStream(ctx context.Context, stream io.ReadCloser, handle func(string) bool) error {
	done := make(chan struct{})
	var once sync.Once
	closeStream := func() {
		once.Do(func() {
			_ = stream.Close()
		})
	}

	go func() {
		select {
		case <-ctx.Done():
			closeStream()
		case <-done:
		}
	}()

	defer func() {
		close(done)
		closeStream()
	}()

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !handle(line) {
			return nil
		}
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return err
	}
	return ctx.Err()
}
