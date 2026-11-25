package model

import (
	"slices"
	"testing"
)

func TestBuildSearchTokens(t *testing.T) {
	u := &User{
		UserID:     "00000042",
		Account:    "alice.wang",
		Nickname:   "王小明",
		EmployeeNo: "EMP-1024",
		Email:      "alice@example.com",
		JobTitle:   "音视频工程师",
	}
	tokens := BuildSearchTokens(u, "研发中心", "音视频组")
	for _, want := range []string{"00000042", "alice.wang", "alice", "wang", "王小明", "小明", "明", "emp-1024", "1024", "研发中心", "音视频组"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("expected token %q in %v", want, tokens)
		}
	}
}
