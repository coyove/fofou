// This code is in Public Domain. Take all the code you want, I'll just write more.
package server

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// TimestampedMsg is a messsage with a timestamp
type TimestampedMsg struct {
	Time time.Time
	Msg  string
}

// CircularMessagesBuf is a circular buffer for messages
type CircularMessagesBuf struct {
	Msgs []TimestampedMsg
	pos  int
	full bool
}

// TimeStr formats a log timestamp
func (m *TimestampedMsg) TimeString() string {
	return m.Time.Format("2006-01-02 15:04:05")
}

// NewCircularMessagesBuf creates a new circular buffer
func NewCircularMessagesBuf(cap int) *CircularMessagesBuf {
	return &CircularMessagesBuf{
		Msgs: make([]TimestampedMsg, cap, cap),
		pos:  0,
		full: false,
	}
}

// Add adds a message
func (b *CircularMessagesBuf) Add(s string) {
	var msg = TimestampedMsg{time.Now(), s}
	if b.pos == cap(b.Msgs) {
		b.pos = 0
		b.full = true
	}
	b.Msgs[b.pos] = msg
	b.pos++
}

// GetOrdered returns ordered messages
func (b *CircularMessagesBuf) GetOrdered() []*TimestampedMsg {
	size := b.pos
	if b.full {
		size = cap(b.Msgs)
	}
	res := make([]*TimestampedMsg, size, size)
	for i := 0; i < size; i++ {
		p := b.pos - 1 - i
		if p < 0 {
			p = cap(b.Msgs) + p
		}
		res[i] = &b.Msgs[p]
	}
	return res
}

type Logger struct {
	Errors    *CircularMessagesBuf
	Notices   *CircularMessagesBuf
	UseStdout bool
	logFile   *os.File
}

func NewLogger(errorsMax, noticesMax int, useStdout bool, logFile string) *Logger {
	l := &Logger{
		Errors:    NewCircularMessagesBuf(errorsMax),
		Notices:   NewCircularMessagesBuf(noticesMax),
		UseStdout: useStdout,
	}
	l.logFile, _ = os.Create(logFile + "_" + time.Now().Format("2006_01_02_15_04_05") + ".log")
	return l
}

func printer(lv, msg string) string {
	src := ""
	if lv == "E" {
		_, fn, line, _ := runtime.Caller(2)
		fn = filepath.Base(fn)
		src = fmt.Sprintf(" %s:%d", fn, line)
	}
	return fmt.Sprintf("[%s %s%s] %s", lv, time.Now().Format(time.StampMilli), src, msg)
}

func (l *Logger) Error(format string, v ...interface{}) {
	s := fmt.Sprintf(format, v...)
	x := printer("E", s)
	l.Errors.Add(s)
	if l.UseStdout {
		fmt.Println(x)
	}
	if l.logFile != nil {
		l.logFile.WriteString(x + "\n")
	}
}

func (l *Logger) Notice(format string, v ...interface{}) {
	s := fmt.Sprintf(format, v...)
	x := printer("I", s)
	l.Notices.Add(s)
	if l.UseStdout {
		fmt.Println(x)
	}
	if l.logFile != nil {
		l.logFile.WriteString(x + "\n")
	}
}

// GetErrors returns error messages
func (l *Logger) GetErrors() []*TimestampedMsg {
	return l.Errors.GetOrdered()
}

// GetNotices returns notice messages
func (l *Logger) GetNotices() []*TimestampedMsg {
	return l.Notices.GetOrdered()
}
