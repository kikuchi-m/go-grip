package cmd

import (
	"reflect"
	"testing"
)

// The test shares the global rootCmd, so it must not run in parallel.
func TestExcludePatternsKeepsEmptyValue(t *testing.T) {
	flag := rootCmd.Flags().Lookup("exclude")
	t.Cleanup(func() {
		if v, ok := flag.Value.(interface{ Replace([]string) error }); ok {
			_ = v.Replace(nil)
		}
	})

	if err := rootCmd.ParseFlags([]string{"-x", "", "-x", ".git"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	got := excludePatterns(rootCmd)
	want := []string{"", ".git"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("excludePatterns = %q, want %q", got, want)
	}
}

func TestExcludePatternsKeepsLoneEmptyValue(t *testing.T) {
	flag := rootCmd.Flags().Lookup("exclude")
	t.Cleanup(func() {
		if v, ok := flag.Value.(interface{ Replace([]string) error }); ok {
			_ = v.Replace(nil)
		}
	})

	if err := rootCmd.ParseFlags([]string{"-x", ""}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	got := excludePatterns(rootCmd)
	if !reflect.DeepEqual(got, []string{""}) {
		t.Fatalf("excludePatterns = %q, want a single empty pattern", got)
	}
}
