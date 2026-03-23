package crypto

import "testing"

func TestNewKeyPair_SignVerify(t *testing.T) {
	kp, err := NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("synthos")
	sig := Sign(kp.Private, msg)
	if !Verify(kp.Public, msg, sig) {
		t.Fatal("signature should verify")
	}
	if Verify(kp.Public, []byte("other"), sig) {
		t.Fatal("wrong message must not verify")
	}
}

func TestPublicKeyBytes_RoundTrip(t *testing.T) {
	kp, err := NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	h := PublicKeyHex(kp.Public)
	b, err := PublicKeyBytes(h)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(kp.Public) {
		t.Fatal("public key bytes mismatch")
	}
}
