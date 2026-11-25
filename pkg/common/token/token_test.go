package token

import "testing"

func TestCreateTokenProducesUniqueTokensRapidly(t *testing.T) {
	tok := New(1, "test-secret")
	t1, err := tok.CreateToken("00000002")
	if err != nil {
		t.Fatalf("CreateToken #1 failed: %v", err)
	}
	t2, err := tok.CreateToken("00000002")
	if err != nil {
		t.Fatalf("CreateToken #2 failed: %v", err)
	}
	if t1 == t2 {
		t.Fatalf("expected distinct JWTs for rapid logins of same userID, got identical token")
	}
	u1, err := tok.GetToken(t1)
	if err != nil || u1 != "00000002" {
		t.Fatalf("GetToken(t1) = (%q, %v), want 00000002", u1, err)
	}
	u2, err := tok.GetToken(t2)
	if err != nil || u2 != "00000002" {
		t.Fatalf("GetToken(t2) = (%q, %v), want 00000002", u2, err)
	}
}
