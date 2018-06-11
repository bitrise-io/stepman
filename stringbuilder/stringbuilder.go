package stringbuilder

import (
	"fmt"

	"github.com/bitrise-io/go-utils/colorstring"
)

// Builder ...
type Builder struct {
	s string
}

// New ...
func New() *Builder {
	return &Builder{}
}

func (b *Builder) add(colorFunc colorstring.ColorfFunc, format string, v ...interface{}) *Builder {
	if colorFunc == nil {
		b.s += fmt.Sprintf(format, v...)
	} else {
		b.s += colorFunc(format, v...)
	}
	return b
}

func (b *Builder) addLn(colorFunc colorstring.ColorfFunc, format string, v ...interface{}) *Builder {
	b.add(nil, "\n")
	b.add(colorFunc, format, v...)
	return b
}

// AddNewLine ...
func (b *Builder) AddNewLine() *Builder {
	return b.add(nil, "\n")
}

// Add ...
func (b *Builder) Add(format string, v ...interface{}) *Builder {
	return b.add(nil, format, v...)
}

// AddBlack ...
func (b *Builder) AddBlack(format string, v ...interface{}) *Builder {
	return b.add(colorstring.Blackf, format, v...)
}

// AddRed ...
func (b *Builder) AddRed(format string, v ...interface{}) *Builder {
	return b.add(colorstring.Redf, format, v...)
}

// AddGreen ...
func (b *Builder) AddGreen(format string, v ...interface{}) *Builder {
	return b.add(colorstring.Greenf, format, v...)
}

// AddYellow ...
func (b *Builder) AddYellow(format string, v ...interface{}) *Builder {
	return b.add(colorstring.Yellowf, format, v...)
}

// AddBlue ...
func (b *Builder) AddBlue(format string, v ...interface{}) *Builder {
	return b.add(colorstring.Bluef, format, v...)
}

// AddMagenta ...
func (b *Builder) AddMagenta(format string, v ...interface{}) *Builder {
	return b.add(colorstring.Magentaf, format, v...)
}

// AddCyan ...
func (b *Builder) AddCyan(format string, v ...interface{}) *Builder {
	return b.add(colorstring.Cyanf, format, v...)
}

// AddLn ...
func (b *Builder) AddLn(format string, v ...interface{}) *Builder {
	return b.addLn(nil, format, v...)
}

// AddBlackLn ...
func (b *Builder) AddBlackLn(format string, v ...interface{}) *Builder {
	return b.addLn(colorstring.Blackf, format, v...)
}

// AddRedLn ...
func (b *Builder) AddRedLn(format string, v ...interface{}) *Builder {
	return b.addLn(colorstring.Redf, format, v...)
}

// AddGreenLn ...
func (b *Builder) AddGreenLn(format string, v ...interface{}) *Builder {
	return b.addLn(colorstring.Greenf, format, v...)
}

// AddYellowLn ...
func (b *Builder) AddYellowLn(format string, v ...interface{}) *Builder {
	return b.addLn(colorstring.Yellowf, format, v...)
}

// AddBlueLn ...
func (b *Builder) AddBlueLn(format string, v ...interface{}) *Builder {
	return b.addLn(colorstring.Bluef, format, v...)
}

// AddMagentaLn ...
func (b *Builder) AddMagentaLn(format string, v ...interface{}) *Builder {
	return b.addLn(colorstring.Magentaf, format, v...)
}

// AddCyanLn ...
func (b *Builder) AddCyanLn(format string, v ...interface{}) *Builder {
	return b.addLn(colorstring.Cyanf, format, v...)
}

// String ....
func (b Builder) String() string {
	return b.s
}
