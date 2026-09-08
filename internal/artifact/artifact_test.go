package artifact

import (
	"bytes"
	"testing"
)

func TestPutGet(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.Put([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.Put([]byte("hello"))
	if err != nil || id != id2 {
		t.Fatalf("dedup %s %s %v", id, id2, err)
	}
	b, err := s.Get(id)
	if err != nil || !bytes.Equal(b, []byte("hello")) {
		t.Fatalf("%q %v", b, err)
	}
}
