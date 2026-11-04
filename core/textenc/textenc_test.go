package textenc

import (
	"testing"
	"unicode/utf8"
)

func TestRoundTrip(t *testing.T) {
	cases := []string{
		"",
		"hello world",
		"سلام، حال‌تان چطور است؟ ساعت ۱۰:۳۰ همدیگر را ببینیم.",
		"گژپچ ک ی ۀ «نقل‌قول» ٪۵۰",
		"mixed فارسی and English 123 😀 \n tab\t",
	}
	for _, s := range cases {
		ct, b := Encode(s)
		got, err := Decode(ct, b)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		if got != s {
			t.Fatalf("round trip: %q != %q", got, s)
		}
	}
}

func TestPersianIsOneBytePerChar(t *testing.T) {
	s := "سلام دوستان، امروز ساعت پنج در میدان شهر جمع می‌شویم"
	ct, b := Encode(s)
	if ct != TypeCompact {
		t.Fatal("expected compact encoding for Persian")
	}
	if len(b) != utf8.RuneCountInString(s) {
		t.Fatalf("compact=%d bytes, runes=%d", len(b), utf8.RuneCountInString(s))
	}
	if len(b)*2 > len(s)+10 {
		t.Fatalf("compact %d vs utf8 %d: expected ~half", len(b), len(s))
	}
}

func TestEnglishStaysUTF8(t *testing.T) {
	if ct, _ := Encode("plain ascii"); ct != TypeUTF8 {
		t.Fatal("ASCII should not pick compact (equal size)")
	}
}

func TestInvalid(t *testing.T) {
	if _, err := Expand([]byte{0xFF}); err == nil {
		t.Fatal("unassigned table slot must fail")
	}
	if _, err := Expand([]byte{0x00}); err == nil {
		t.Fatal("dangling escape must fail")
	}
}

func FuzzExpand(f *testing.F) {
	f.Add(Compact("سلام hello"))
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := Expand(b)
		if err != nil {
			return
		}
		if got, _ := Expand(Compact(s)); got != s {
			t.Fatalf("not stable: %q", s)
		}
	})
}
