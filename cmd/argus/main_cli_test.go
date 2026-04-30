package main

import (
	"testing"
)

func TestParseSubcommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantSub     string
		wantSubArgs []string
		wantErr     bool
	}{
		{
			name:        "no args defaults to serve",
			args:        []string{},
			wantSub:     "serve",
			wantSubArgs: nil,
		},
		{
			name:        "migrate subcommand (no direction)",
			args:        []string{"migrate"},
			wantSub:     "migrate",
			wantSubArgs: []string{},
		},
		{
			name:        "serve subcommand",
			args:        []string{"serve"},
			wantSub:     "serve",
			wantSubArgs: []string{},
		},
		{
			name:        "migrate up",
			args:        []string{"migrate", "up"},
			wantSub:     "migrate",
			wantSubArgs: []string{"up"},
		},
		{
			name:        "migrate down 3",
			args:        []string{"migrate", "down", "3"},
			wantSub:     "migrate",
			wantSubArgs: []string{"down", "3"},
		},
		{
			name:        "migrate down -all",
			args:        []string{"migrate", "down", "-all"},
			wantSub:     "migrate",
			wantSubArgs: []string{"down", "-all"},
		},
		{
			name:        "seed no args",
			args:        []string{"seed"},
			wantSub:     "seed",
			wantSubArgs: []string{},
		},
		{
			name:        "seed specific file",
			args:        []string{"seed", "003_comprehensive_seed.sql"},
			wantSub:     "seed",
			wantSubArgs: []string{"003_comprehensive_seed.sql"},
		},
		{
			name:        "version",
			args:        []string{"version"},
			wantSub:     "version",
			wantSubArgs: nil,
		},
		{
			name:        "--help flag",
			args:        []string{"--help"},
			wantSub:     "help",
			wantSubArgs: nil,
		},
		{
			name:        "-h flag",
			args:        []string{"-h"},
			wantSub:     "help",
			wantSubArgs: nil,
		},
		{
			name:    "unknown subcommand returns error",
			args:    []string{"garbage"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, subArgs, err := parseSubcommand(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none (sub=%q)", sub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sub != tt.wantSub {
				t.Errorf("sub = %q, want %q", sub, tt.wantSub)
			}
			if len(subArgs) != len(tt.wantSubArgs) {
				t.Errorf("subArgs = %v, want %v", subArgs, tt.wantSubArgs)
				return
			}
			for i, arg := range subArgs {
				if arg != tt.wantSubArgs[i] {
					t.Errorf("subArgs[%d] = %q, want %q", i, arg, tt.wantSubArgs[i])
				}
			}
		})
	}
}
