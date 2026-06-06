package sign

import "testing"

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("checksums file contents")
	sig, err := Sign(priv, msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !Verify(pub, msg, sig) {
		t.Fatal("valid signature rejected")
	}
}

func TestVerifyRejectsTamper(t *testing.T) {
	pub, priv, _ := GenerateKey()
	sig, _ := Sign(priv, []byte("original"))
	if Verify(pub, []byte("tampered"), sig) {
		t.Fatal("signature should not verify against tampered message")
	}

	// Wrong key.
	otherPub, _, _ := GenerateKey()
	if Verify(otherPub, []byte("original"), sig) {
		t.Fatal("signature should not verify under a different key")
	}
}

func TestVerifyMalformed(t *testing.T) {
	pub, _, _ := GenerateKey()
	if Verify(pub, []byte("x"), "nothex") {
		t.Fatal("malformed signature accepted")
	}
	if Verify("nothex", []byte("x"), "aabb") {
		t.Fatal("malformed key accepted")
	}
}
