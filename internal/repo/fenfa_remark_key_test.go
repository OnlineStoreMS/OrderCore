package repo

import "testing"

func TestFenFaRemarkLookupKey(t *testing.T) {
	if got := FenFaRemarkLookupKey("OC1", "skuA"); got != "OC1"+FenFaRemarkSKUSep+"skuA" {
		t.Fatalf("got %q", got)
	}
	if got := FenFaRemarkLookupKey("OC1", ""); got != "OC1" {
		t.Fatalf("bare got %q", got)
	}
	if FenFaRemarkLookupKey("", "sku") != "" {
		t.Fatal("empty order")
	}
}
