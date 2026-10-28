package auth

import "testing"

func TestPasswordAndJWT(t *testing.T) {
	h, err := HashPassword("beta-password-1")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "beta-password-1") || CheckPassword(h, "wrong") {
		t.Fatal("password check broken")
	}
	tok, err := SignAccess("test-secret", "usr_1", "a@example.ma", "cmp_1")
	if err != nil {
		t.Fatal(err)
	}
	c, err := VerifyAccess("test-secret", tok)
	if err != nil || c.CompanyID != "cmp_1" {
		t.Fatalf("jwt verify broken: %v %+v", err, c)
	}
	if _, err := VerifyAccess("other", tok); err == nil {
		t.Fatal("wrong secret must fail")
	}
	if HashToken("abc") == "abc" {
		t.Fatal("share token must be stored hashed, not raw")
	}
}
