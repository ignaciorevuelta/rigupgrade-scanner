package main

import (
	"encoding/json"
	"testing"
)

func TestLinkGen(t *testing.T) {
	for code, want := range map[uint32]int{0: 0, 1: 1, 3: 3, 4: 4, 5: 5, 9: 0} {
		if got := linkGen(code); got != want {
			t.Errorf("linkGen(%d) = %d, want %d", code, got, want)
		}
	}
}

// Logs what this machine reports; never fails. Run with: go test -v -run Live
func TestLiveDisks(t *testing.T) {
	b, _ := json.Marshal(readDisks())
	t.Log(string(b))
}

// Logs this machine's SMBIOS summary; never fails. Run with: go test -v -run Live
func TestLiveSMBIOS(t *testing.T) {
	b, _ := json.Marshal(parseSMBIOS(readSMBIOS()))
	t.Log(string(b)[:min(400, len(b))])
}
