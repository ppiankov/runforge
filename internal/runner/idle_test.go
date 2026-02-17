package runner

import (
	"bytes"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdleTimeoutReader_NoTimeout(t *testing.T) {
	buf := bytes.NewBufferString("hello")
	itr := newIdleTimeoutReader(buf, 0, nil)
	defer itr.Stop()

	p := make([]byte, 5)
	n, err := itr.Read(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5 bytes, got %d", n)
	}
	if itr.Idled() {
		t.Fatal("should not be idled with timeout=0")
	}
}

func TestIdleTimeoutReader_ResetsOnData(t *testing.T) {
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	var cancelled atomic.Bool
	cancel := func() { cancelled.Store(true) }
	itr := newIdleTimeoutReader(pr, 200*time.Millisecond, cancel)
	defer itr.Stop()

	// write data before timeout fires
	go func() {
		for i := 0; i < 5; i++ {
			time.Sleep(100 * time.Millisecond)
			_, _ = pw.Write([]byte("x"))
		}
	}()

	p := make([]byte, 1)
	for i := 0; i < 5; i++ {
		_, err := itr.Read(p)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}

	if itr.Idled() {
		t.Fatal("should not be idled — data was flowing")
	}
	if cancelled.Load() {
		t.Fatal("cancel should not have been called")
	}
}

func TestIdleTimeoutReader_FiresOnIdle(t *testing.T) {
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	var cancelled atomic.Bool
	cancel := func() {
		cancelled.Store(true)
		_ = pw.Close() // unblock the read
	}
	itr := newIdleTimeoutReader(pr, 100*time.Millisecond, cancel)
	defer itr.Stop()

	// don't write anything — let the idle timer fire
	p := make([]byte, 1)
	_, err := itr.Read(p)
	if err == nil {
		t.Fatal("expected error after idle timeout closed pipe")
	}

	if !itr.Idled() {
		t.Fatal("should be idled")
	}
	if !cancelled.Load() {
		t.Fatal("cancel should have been called")
	}
}

func TestIdleTimeoutReader_StopPreventsCancel(t *testing.T) {
	buf := bytes.NewBufferString("data")
	var cancelled atomic.Bool
	cancel := func() { cancelled.Store(true) }
	itr := newIdleTimeoutReader(buf, 50*time.Millisecond, cancel)

	// read all data, then stop the timer
	p := make([]byte, 4)
	_, _ = itr.Read(p)
	itr.Stop()

	// wait longer than timeout
	time.Sleep(100 * time.Millisecond)

	if cancelled.Load() {
		t.Fatal("cancel should not fire after Stop()")
	}
	if itr.Idled() {
		t.Fatal("should not be idled after Stop()")
	}
}
