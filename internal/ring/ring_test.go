package ring

import (
	"bytes"
	"sync"
	"testing"
)

func TestSnapshot_Empty(t *testing.T) {
	b := New(64)
	if b.Snapshot() != nil {
		t.Fatal("expected nil snapshot for empty buffer")
	}
}

func TestWrite_NoWrap(t *testing.T) {
	b := New(64)
	data := []byte("hello")
	b.Write(data)
	got := b.Snapshot()
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestWrite_ExactFull(t *testing.T) {
	b := New(8)
	data := []byte("12345678")
	b.Write(data)
	got := b.Snapshot()
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestWrite_Wrap(t *testing.T) {
	b := New(8)
	b.Write([]byte("12345678"))
	b.Write([]byte("abcd"))
	got := b.Snapshot()
	want := []byte("5678abcd")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWrite_LargerThanBuffer(t *testing.T) {
	b := New(4)
	b.Write([]byte("123456789")) // 9 bytes into 4-byte ring
	got := b.Snapshot()
	want := []byte("6789")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWrite_MultipleSmall(t *testing.T) {
	b := New(8)
	b.Write([]byte("ABCDE"))
	b.Write([]byte("FGH"))
	got := b.Snapshot()
	want := []byte("ABCDEFGH")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestConcurrent(t *testing.T) {
	b := New(DefaultSize)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				b.Write([]byte("data"))
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = b.Snapshot()
			}
		}()
	}
	wg.Wait()
}
