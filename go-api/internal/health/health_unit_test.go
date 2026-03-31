//go:build !integration

package health

import "testing"

func TestOK(t *testing.T) {
	got := OK()
	if got.Status != "ok" {
		t.Fatalf("expected status=ok, got %q", got.Status)
	}
}
