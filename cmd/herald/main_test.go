package main

import "testing"

func TestDispatchUnknownCommand(t *testing.T) {
	if code := dispatch([]string{"bogus"}); code != 2 {
		t.Errorf("unknown command: want exit 2, got %d", code)
	}
}

func TestDispatchNoArgs(t *testing.T) {
	if code := dispatch(nil); code != 2 {
		t.Errorf("no args: want exit 2, got %d", code)
	}
}

func TestDispatchHelp(t *testing.T) {
	if code := dispatch([]string{"--help"}); code != 0 {
		t.Errorf("help: want exit 0, got %d", code)
	}
}
