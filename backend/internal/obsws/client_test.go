package obsws

import "testing"

func TestComputeAuthentication(t *testing.T) {
	got := ComputeAuthentication("password", "salt", "challenge")
	if got != "zTM5ki6L2vVvBQiTG9ckH1Lh64AbnCf6XZ226UmnkIA=" {
		t.Fatalf("unexpected auth response: %s", got)
	}
}
