package voicetype

import (
	"reflect"
	"sync"
	"testing"
)

func TestParseCommands(t *testing.T) {
	for _, test := range []struct {
		args []string
		want Command
	}{
		{nil, CommandShow}, {[]string{"--background"}, CommandBackground},
		{[]string{"--toggle"}, CommandToggle}, {[]string{"--quit"}, CommandQuit},
		{[]string{"--remove-shortcuts"}, CommandRemoveShortcuts},
		{[]string{"--help"}, CommandHelp}, {[]string{"--version"}, CommandVersion},
		{[]string{"doctor"}, CommandDoctor}, {[]string{"update"}, CommandUpdate},
	} {
		got, err := ParseCommand(test.args)
		if err != nil || got != test.want {
			t.Errorf("%v: %q %v", test.args, got, err)
		}
	}
	for _, args := range [][]string{{"start"}, {"version"}, {"--show-settings"}, {"--shortcut-action", "transcription"}, {"--toggle", "--background"}, {"--quit", "--toggle"}, {"--shortcut-action"}, {"--shortcut-action", "daemon"}, {"--toggle=true"}, {"--unknown"}, {"--remove-shortcuts", "--quit"}, {"--toggle", "key"}, {"--show-settings", "--show-settings"}, {""}, {"--"}} {
		if _, err := ParseCommand(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}
func TestCommandRouterStartupAndShutdown(t *testing.T) {
	var got []Command
	r := commandRouter{dispatch: func(command Command) { got = append(got, command) }}
	r.submit(CommandBackground)
	r.submit(CommandShow)
	r.submit(CommandToggle)
	r.submit(CommandQuit)
	if len(got) != 0 {
		t.Fatal("dispatched before startup")
	}
	r.start(true)
	r.submit(CommandBackground)
	r.submit(CommandToggle)
	if !reflect.DeepEqual(got, []Command{CommandShow, CommandToggle, CommandQuit, CommandToggle}) {
		t.Fatalf("dispatch %v", got)
	}
	r.close()
	r.submit(CommandShow)
	r.start(true)
	if len(got) != 4 {
		t.Fatal("dispatched after shutdown")
	}
	failed := commandRouter{dispatch: func(Command) { t.Fatal("startup failure dispatched") }}
	failed.submit(CommandToggle)
	failed.close()
	failed.start(true)
}
func TestCommandRouterBoundAndConcurrency(t *testing.T) {
	var commands int
	r := commandRouter{dispatch: func(Command) { commands++ }}
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() { defer wg.Done(); r.submit(CommandToggle) }()
	}
	wg.Wait()
	r.start(true)
	if commands != 32 {
		t.Fatalf("startup bound: %d", commands)
	}
}

func TestStartupRecoveryDoesNotAutomaticallyRecord(t *testing.T) {
	var got []Command
	r := commandRouter{dispatch: func(c Command) { got = append(got, c) }}
	r.submit(CommandToggle)
	r.start(false)
	if !reflect.DeepEqual(got, []Command{CommandShow}) {
		t.Fatal(got)
	}
	r.submit(CommandToggle)
	if got[1] != CommandToggle {
		t.Fatal("explicit command after recovery was ignored")
	}
}
