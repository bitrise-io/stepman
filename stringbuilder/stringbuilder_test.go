package stringbuilder

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppend(t *testing.T) {
	{
		b := Builder{}
		b.Add("test")
		require.Equal(t, "test", b.String())
	}

	{
		b := Builder{}
		b.AddBlack("test")
		require.Equal(t, "\x1b[30;1mtest\x1b[0m", b.String())
	}
}

func TestAppendNewLine(t *testing.T) {
	{
		b := Builder{}
		b.AddLn("test")
		require.Equal(t, "\ntest", b.String())
	}

	{
		b := Builder{}
		b.AddBlackLn("test")
		require.Equal(t, "\n\x1b[30;1mtest\x1b[0m", b.String())
	}
}

func TestStringBuilder(t *testing.T) {
	b := Builder{}
	b.Add("Hello")
	b.AddYellow(" World!")
	b.AddLn("New line")
	b.AddRed(" test.")
	b.AddLn("Another new line.")
	require.Equal(t, "Hello\x1b[33;1m World!\x1b[0m\nNew line\x1b[31;1m test.\x1b[0m\nAnother new line.", b.String())
}
