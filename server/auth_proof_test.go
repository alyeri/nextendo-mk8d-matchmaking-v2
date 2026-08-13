package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"testing"
	"time"
)

func TestSignedPIDForUsernameFromBAASExtraData(t *testing.T) {
	oldSecret := nextendoSecret
	nextendoSecret = []byte("unit-test-nextendo-secret")
	t.Cleanup(func() { nextendoSecret = oldSecret })

	const pid = uint64(1800000123)
	proof := testNX2Token(pid, time.Now().Add(time.Hour))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"sub":"user","nnex":%q}`, proof)))
	jwt := "eyJhbGciOiJSUzI1NiJ9." + payload + ".signature"
	extra := append([]byte{0x00, 0x19, 0x7f}, []byte(jwt)...)
	extra = append(extra, 0x00)

	got, present, valid := signedPIDForUsername("1800000123", extra)
	if !present || !valid || got != pid {
		t.Fatalf("got pid=%d present=%v valid=%v", got, present, valid)
	}
	if _, present, valid := signedPIDForUsername("1800000999", extra); !present || valid {
		t.Fatalf("mismatched username accepted: present=%v valid=%v", present, valid)
	}
}

func TestSignedPIDForUsernameRejectsExpiredProof(t *testing.T) {
	oldSecret := nextendoSecret
	nextendoSecret = []byte("unit-test-nextendo-secret")
	t.Cleanup(func() { nextendoSecret = oldSecret })

	proof := testNX2Token(1800000123, time.Now().Add(-time.Minute))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"nnex":%q}`, proof)))
	extra := []byte("eyJ9." + payload + ".signature")
	_, present, valid := signedPIDForUsername("1800000123", extra)
	if !present || valid {
		t.Fatalf("expired proof accepted: present=%v valid=%v", present, valid)
	}
}

func testNX2Token(pid uint64, expiry time.Time) string {
	raw := fmt.Sprintf("%d.test-user.%d", pid, expiry.Unix())
	mac := hmac.New(sha256.New, nextendoSecret)
	_, _ = mac.Write([]byte("nex:" + raw))
	return "nx2." + base64.RawURLEncoding.EncodeToString([]byte(raw)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
