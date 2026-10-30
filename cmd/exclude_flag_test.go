package cmd

import (
	"io"
	"reflect"
	"strings"
	"testing"
)

// These tests share the global rootCmd, so they must not run in parallel.

// clearExclude drops the values a previous parse left in the flag, which
// would otherwise accumulate.
func clearExclude(t *testing.T) {
	t.Helper()

	flag := rootCmd.Flags().Lookup("exclude")
	if flag == nil {
		t.Fatal("expected an --exclude flag")
	}
	if v, ok := flag.Value.(interface{ Replace([]string) error }); ok {
		_ = v.Replace(nil)
	}
}

func resetExcludeFlag(t *testing.T) {
	t.Helper()

	clearExclude(t)
	t.Cleanup(func() {
		clearExclude(t)
		rootCmd.SetArgs(nil)
		_ = rootCmd.Flags().Set("browser", "true")
		_ = rootCmd.Flags().Set("port", "6419")
	})
}

func TestExcludePatternsKeepsEmptyValue(t *testing.T) {
	resetExcludeFlag(t)

	if err := rootCmd.ParseFlags([]string{"-x", "", "-x", ".git"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	got := excludePatterns(rootCmd)
	if want := []string{"", ".git"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("excludePatterns = %q, want %q", got, want)
	}
}

func TestExcludePatternsKeepsLoneEmptyValue(t *testing.T) {
	resetExcludeFlag(t)

	if err := rootCmd.ParseFlags([]string{"-x", ""}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	if got := excludePatterns(rootCmd); !reflect.DeepEqual(got, []string{""}) {
		t.Fatalf("excludePatterns = %q, want a single empty pattern", got)
	}
}

func TestRootRejectsInvalidExcludePattern(t *testing.T) {
	resetExcludeFlag(t)

	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"-x", "node_modules/"}, `did you mean "node_modules"?`},
		{[]string{"--exclude", "a/b"}, "'/' is not allowed"},
		{[]string{"-x", ""}, "can never match"},
	} {
		clearExclude(t)
		rootCmd.SetArgs(append([]string{"--browser=false", "--port=1"}, tt.args...))
		rootCmd.SetOut(io.Discard)
		rootCmd.SetErr(io.Discard)

		err := rootCmd.Execute()

		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("args %v: error = %v, want it to contain %q", tt.args, err, tt.want)
		}
	}
}
