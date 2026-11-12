package cmd

import "testing"

func TestHardWrapFlagIsDeclaredAndOffByDefault(t *testing.T) {
	flag := rootCmd.Flags().Lookup("hard-wrap")
	if flag == nil {
		t.Fatal("expected a --hard-wrap flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("default = %q, want false", flag.DefValue)
	}
}
