package sharetoken

import (
	"encoding/hex"
	"strings"
	"testing"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestNewSignerRejectsWeakSecret(t *testing.T) {
	if _, err := NewSigner("short"); err == nil {
		t.Fatal("NewSigner() accepted a secret shorter than 32 bytes")
	}
}

func TestNewPublicIDReturnsLowercaseHex(t *testing.T) {
	id, err := NewPublicID()
	if err != nil {
		t.Fatalf("NewPublicID() error = %v", err)
	}
	if len(id) != 32 {
		t.Fatalf("len(NewPublicID()) = %d, want 32", len(id))
	}
	if id != strings.ToLower(id) {
		t.Fatalf("NewPublicID() = %q, want lowercase", id)
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Fatalf("NewPublicID() = %q, want valid hex: %v", id, err)
	}
}

func TestSignParseRoundTrip(t *testing.T) {
	signer, err := NewSigner(testSecret)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	id := "0123456789abcdef0123456789abcdef"

	value, legacy, err := signer.Parse(signer.Sign(id))
	if err != nil {
		t.Fatalf("Parse(Sign()) error = %v", err)
	}
	if legacy {
		t.Fatal("Parse(Sign()) legacy = true, want false")
	}
	if value != id {
		t.Fatalf("Parse(Sign()) value = %q, want %q", value, id)
	}
}

func TestSignParseRoundTripWithUnderscoreInSignature(t *testing.T) {
	signer, err := NewSigner(testSecret)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	const id = "00000000000000000000000000000001"
	const wantToken = "v1_00000000000000000000000000000001_kVCen8vHGjySlZF_mC5N22Bs13LHqfgxBbnHY7Botcg"
	token := signer.Sign(id)
	if token != wantToken {
		t.Fatalf("Sign() = %q, want %q", token, wantToken)
	}

	value, legacy, err := signer.Parse(token)
	if err != nil {
		t.Fatalf("Parse(Sign()) error = %v", err)
	}
	if legacy {
		t.Fatal("Parse(Sign()) legacy = true, want false")
	}
	if value != id {
		t.Fatalf("Parse(Sign()) value = %q, want %q", value, id)
	}
}

func TestParseRejectsTamperedToken(t *testing.T) {
	signer, err := NewSigner(testSecret)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	token := signer.Sign("0123456789abcdef0123456789abcdef")
	tampered := token[:len(token)-1] + "A"
	if tampered == token {
		tampered = token[:len(token)-1] + "B"
	}

	if _, _, err := signer.Parse(tampered); err == nil {
		t.Fatal("Parse() accepted a tampered token")
	}
}

func TestParseRejectsNonCanonicalEquivalentSignature(t *testing.T) {
	signer, err := NewSigner(testSecret)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}

	for _, suffix := range []string{"Z", "a", "b"} {
		t.Run(suffix, func(t *testing.T) {
			tampered := "v1_0123456789abcdef0123456789abcdef_eBVS-nqwfm3pAEH22cstXDcqq33wf6dgjV2ceEFuiA" + suffix
			if _, _, err := signer.Parse(tampered); err == nil {
				t.Fatal("Parse() accepted a non-canonical signature with altered pad bits")
			}
		})
	}
}

func TestParseRejectsUnknownVersion(t *testing.T) {
	signer, err := NewSigner(testSecret)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}

	if _, _, err := signer.Parse("v2_0123456789abcdef0123456789abcdef_signature"); err == nil {
		t.Fatal("Parse() accepted an unknown token version")
	}
}

func TestParseRecognizesLegacyShapeWithoutAuthenticating(t *testing.T) {
	signer, err := NewSigner(testSecret)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}

	value, legacy, err := signer.Parse("aB3dE6gH")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !legacy {
		t.Fatal("Parse() legacy = false, want true")
	}
	if value != "aB3dE6gH" {
		t.Fatalf("Parse() value = %q, want legacy token", value)
	}
}

func TestParseRejectsInvalidLegacyShape(t *testing.T) {
	signer, err := NewSigner(testSecret)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}

	for _, token := range []string{"aB3dE6g", "aB3dE6gH9", "aB3dE6-H"} {
		t.Run(token, func(t *testing.T) {
			if _, _, err := signer.Parse(token); err == nil {
				t.Fatalf("Parse(%q) accepted an invalid legacy shape", token)
			}
		})
	}
}

func TestLegacyDigest(t *testing.T) {
	const want = "34eab3c11084a02f97eec5f5c7f98b4b98903aa30932f30ca3bd73125390ae38"
	if got := LegacyDigest("aB3dE6gH"); got != want {
		t.Fatalf("LegacyDigest() = %q, want %q", got, want)
	}
}
