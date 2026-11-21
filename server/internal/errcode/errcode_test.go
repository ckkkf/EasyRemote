package errcode

import "testing"

func TestNamesUniqueAndSegmentsValid(t *testing.T) {
	seenCode := map[Code]bool{}
	seenName := map[string]bool{}
	for _, e := range All() {
		if seenCode[e.Code] {
			t.Fatalf("duplicate code %d", e.Code)
		}
		if seenName[e.Name] {
			t.Fatalf("duplicate name %s", e.Name)
		}
		seenCode[e.Code] = true
		seenName[e.Name] = true

		seg := Segment(e.Code)
		if seg < 1 || seg > 9 {
			t.Fatalf("code %d has invalid segment %d", e.Code, seg)
		}
		if e.Code%100000 == 0 {
			t.Fatalf("code %d ends with 00000, reserved", e.Code)
		}
	}
}

func TestSegmentMapping(t *testing.T) {
	cases := map[Code]int{
		AuthInvalidCredential: 1,
		ConnDeviceNotFound:    2,
		SessionNotFound:       3,
		SrvInternal:           7,
	}
	for code, want := range cases {
		if got := Segment(code); got != want {
			t.Fatalf("Segment(%d)=%d want %d", code, got, want)
		}
	}
}

func TestNameUnknown(t *testing.T) {
	if Name(Code(999999)) != "UNKNOWN" {
		t.Fatal("want UNKNOWN")
	}
}

func TestIsMatching(t *testing.T) {
	err := New(ConnCaptchaLocked, "locked")
	if !Is(err, ConnCaptchaLocked) {
		t.Fatal("want match")
	}
	if Is(err, ConnCaptchaInvalid) {
		t.Fatal("want no match")
	}
	if Is(nil, ConnCaptchaLocked) {
		t.Fatal("nil should not match")
	}
}
