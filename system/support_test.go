package system

import "testing"

func TestSupportChunk(t *testing.T) {
	if got := string(supportChunk("aGVsbG8=")); got != "hello" {
		t.Fatalf("base64 chunk decoded to %q", got)
	}
	if got := string(supportChunk("not base64!")); got != "not base64!" {
		t.Fatalf("raw chunk became %q", got)
	}
}
