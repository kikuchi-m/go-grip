package internal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParserSoftBreakIsASpaceByDefault(t *testing.T) {
	t.Parallel()

	out, err := NewParser(nil).MdToHTML([]byte("this is it.\nthat is that.\n"))

	require.NoError(t, err)
	require.Contains(t, string(out), "this is it.\nthat is that.")
	require.NotContains(t, string(out), "<br")
}

func TestParserHardWrapsTurnsLineBreaksIntoBr(t *testing.T) {
	t.Parallel()

	out, err := NewParser(nil, WithHardWraps()).MdToHTML([]byte("this is it.\nthat is that.\n"))

	require.NoError(t, err)
	require.Contains(t, string(out), "this is it.<br>\nthat is that.")
}

func TestParserHardWrapsLeavesParagraphsAndCodeAlone(t *testing.T) {
	t.Parallel()

	out, err := NewParser(nil, WithHardWraps()).MdToHTML([]byte("a\n\nb\n\n```\nx\ny\n```\n"))

	require.NoError(t, err)
	require.Equal(t, 0, strings.Count(string(out), "<br"))
}
