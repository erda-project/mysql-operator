package log

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

const (
	Ldate         = log.Ldate
	Ltime         = log.Ltime
	Lmicroseconds = log.Lmicroseconds
	Llongfile     = log.Llongfile
	Lshortfile    = log.Lshortfile
	LUTC          = log.LUTC
	Lmsgprefix    = log.Lmsgprefix
	LstdFlags     = log.LstdFlags
)

const (
	LevelDebug = (iota + 1) * 100
	LevelInfo
	LevelNotice
	LevelWarning
	LevelError
	LevelCritical
	LevelPanic
	LevelFatal
)

const levelDepth = 4

var levels = map[int]string{
	LevelDebug:    "DEBUG",
	LevelInfo:     "INFO",
	LevelNotice:   "NOTICE",
	LevelWarning:  "WARNING",
	LevelError:    "ERROR",
	LevelCritical: "CRITICAL",
	LevelPanic:    "PANIC",
	LevelFatal:    "FATAL",
}

func AddBracket() {
	for k, v := range levels {
		levels[k] = "[" + v + "]"
	}
}

func AddColon() {
	for k, v := range levels {
		levels[k] = v + ":"
	}
}

func SetLevelName(level int, name string) {
	levels[level] = name
}

func GetLevelName(level int) string {
	if name, ok := levels[level]; ok {
		return name
	}
	return strconv.Itoa(level)
}

func GetNameLevel(name string) (level int) {
	name = strings.ToUpper(name)
	for k, v := range levels {
		if v == name {
			return k
		}
	}
	level, _ = strconv.Atoi(name)
	return
}

type Logger struct {
	level  int
	logger *log.Logger
}

func New(out io.Writer, prefix string, flag, level int) *Logger {
	return &Logger{
		level:  level,
		logger: log.New(out, prefix, flag),
	}
}

func (l *Logger) Flags() int {
	return l.logger.Flags()
}

func (l *Logger) SetFlags(flag int) {
	l.logger.SetFlags(flag)
}

func (l *Logger) SetOutput(w io.Writer) {
	l.logger.SetOutput(w)
}

func (l *Logger) Prefix() string {
	return l.logger.Prefix()
}

func (l *Logger) SetPrefix(prefix string) {
	l.logger.SetPrefix(prefix)
}

func (l *Logger) Level() int {
	return l.level
}

func (l *Logger) SetLevel(level int) {
	l.level = level
}

func (l *Logger) output(level, calldepth int, s string) error {
	if l == std {
		calldepth++
	}
	s = GetLevelName(level) + " " + s
	if level == LevelCritical {
		b := debug.Stack()
		if s[len(s)-1] != '\n' {
			s += "\n"
		}
		s += string(b)
	}
	return l.logger.Output(calldepth, s)
}

func (l *Logger) Err(level, calldepth int, err error, a ...interface{}) error {
	if err != nil && level >= l.level {
		s := err.Error()
		if len(a) > 0 {
			a = append(a, s)
			s = fmt.Sprintln(a...)
		}
		return l.output(level, calldepth, s)
	}
	return nil
}

func (l *Logger) Output(level, calldepth int, a ...interface{}) error {
	if level >= l.level {
		return l.output(level, calldepth, fmt.Sprint(a...))
	}
	return nil
}

func (l *Logger) Outputf(level, calldepth int, format string, a ...interface{}) error {
	if level >= l.level {
		return l.output(level, calldepth, fmt.Sprintf(format, a...))
	}
	return nil
}

func (l *Logger) Outputln(level, calldepth int, a ...interface{}) error {
	if level >= l.level {
		return l.output(level, calldepth, fmt.Sprintln(a...))
	}
	return nil
}

func (l *Logger) ErrDebug(err error, a ...interface{}) {
	_ = l.Err(LevelDebug, levelDepth, err, a...)
}

func (l *Logger) ErrNotice(err error, a ...interface{}) {
	_ = l.Err(LevelNotice, levelDepth, err, a...)
}

func (l *Logger) ErrInfo(err error, a ...interface{}) {
	_ = l.Err(LevelInfo, levelDepth, err, a...)
}

func (l *Logger) ErrWarning(err error, a ...interface{}) {
	_ = l.Err(LevelWarning, levelDepth, err, a...)
}

func (l *Logger) ErrError(err error, a ...interface{}) {
	_ = l.Err(LevelError, levelDepth, err, a...)
}

func (l *Logger) ErrCritical(err error, a ...interface{}) {
	_ = l.Err(LevelCritical, levelDepth, err, a...)
}

func (l *Logger) ErrPanic(err error, a ...interface{}) {
	if err != nil {
		_ = l.Err(LevelPanic, levelDepth, err, a...)
		panic(err)
	}
}

func (l *Logger) ErrFatal(err error, a ...interface{}) {
	if err != nil {
		_ = l.Err(LevelFatal, levelDepth, err, a...)
		os.Exit(1)
	}
}

func (l *Logger) Debug(a ...interface{}) {
	_ = l.Output(LevelDebug, levelDepth, a...)
}

func (l *Logger) Notice(a ...interface{}) {
	_ = l.Output(LevelNotice, levelDepth, a...)
}

func (l *Logger) Info(a ...interface{}) {
	_ = l.Output(LevelInfo, levelDepth, a...)
}

func (l *Logger) Warning(a ...interface{}) {
	_ = l.Output(LevelWarning, levelDepth, a...)
}

func (l *Logger) Error(a ...interface{}) {
	_ = l.Output(LevelError, levelDepth, a...)
}

func (l *Logger) Critical(a ...interface{}) {
	_ = l.Output(LevelCritical, levelDepth, a...)
}

func (l *Logger) Panic(a ...interface{}) {
	s := fmt.Sprint(a...)
	if LevelPanic >= l.level {
		_ = l.output(LevelPanic, levelDepth-1, s)
	}
	panic(s)
}

func (l *Logger) Fatal(a ...interface{}) {
	_ = l.Output(LevelFatal, levelDepth, a...)
	os.Exit(1)
}

func (l *Logger) Debugf(format string, a ...interface{}) {
	_ = l.Outputf(LevelDebug, levelDepth, format, a...)
}

func (l *Logger) Noticef(format string, a ...interface{}) {
	_ = l.Outputf(LevelNotice, levelDepth, format, a...)
}

func (l *Logger) Infof(format string, a ...interface{}) {
	_ = l.Outputf(LevelInfo, levelDepth, format, a...)
}

func (l *Logger) Warningf(format string, a ...interface{}) {
	_ = l.Outputf(LevelWarning, levelDepth, format, a...)
}

func (l *Logger) Errorf(format string, a ...interface{}) {
	_ = l.Outputf(LevelError, levelDepth, format, a...)
}

func (l *Logger) Criticalf(format string, a ...interface{}) {
	_ = l.Outputf(LevelCritical, levelDepth, format, a...)
}

func (l *Logger) Panicf(format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	if LevelPanic >= l.level {
		_ = l.output(LevelPanic, levelDepth-1, s)
	}
	panic(s)
}

func (l *Logger) Fatalf(format string, a ...interface{}) {
	_ = l.Outputf(LevelFatal, levelDepth, format, a...)
	os.Exit(1)
}

func (l *Logger) Debugln(a ...interface{}) {
	_ = l.Outputln(LevelDebug, levelDepth, a...)
}

func (l *Logger) Infoln(a ...interface{}) {
	_ = l.Outputln(LevelInfo, levelDepth, a...)
}

func (l *Logger) Noticeln(a ...interface{}) {
	_ = l.Outputln(LevelNotice, levelDepth, a...)
}

func (l *Logger) Warningln(a ...interface{}) {
	_ = l.Outputln(LevelWarning, levelDepth, a...)
}

func (l *Logger) Errorln(a ...interface{}) {
	_ = l.Outputln(LevelError, levelDepth, a...)
}

func (l *Logger) Criticalln(a ...interface{}) {
	_ = l.Outputln(LevelCritical, levelDepth, a...)
}

func (l *Logger) Panicln(a ...interface{}) {
	s := fmt.Sprintln(a...)
	if LevelPanic >= l.level {
		_ = l.output(LevelPanic, levelDepth-1, s)
	}
	panic(s)
}

func (l *Logger) Fatalln(a ...interface{}) {
	_ = l.Outputln(LevelFatal, levelDepth, a...)
	os.Exit(1)
}

var std = New(os.Stderr, "", LstdFlags, LevelInfo)

func SetOutput(w io.Writer) {
	std.SetOutput(w)
}

func Flags() int {
	return std.Flags()
}

func SetFlags(flag int) {
	std.SetFlags(flag)
}

func Prefix() string {
	return std.Prefix()
}

func SetPrefix(prefix string) {
	std.SetPrefix(prefix)
}

func Level() int {
	return std.Level()
}

func SetLevel(level int) {
	std.SetLevel(level)
}

func Err(level, calldepth int, err error, a ...interface{}) error {
	return std.Err(level, calldepth, err, a...)
}

func Output(level, calldepth int, a ...interface{}) error {
	return std.Output(level, calldepth, a...)
}

func Outputf(level, calldepth int, format string, a ...interface{}) error {
	return std.Outputf(level, calldepth, format, a...)
}

func Outputln(level, calldepth int, a ...interface{}) error {
	return std.Outputln(level, calldepth, a...)
}

func ErrDebug(err error, a ...interface{}) {
	std.ErrDebug(err, a...)
}

func ErrInfo(err error, a ...interface{}) {
	std.ErrInfo(err, a...)
}

func ErrNotice(err error, a ...interface{}) {
	std.ErrNotice(err, a...)
}

func ErrWarning(err error, a ...interface{}) {
	std.ErrWarning(err, a...)
}

func ErrError(err error, a ...interface{}) {
	std.ErrError(err, a...)
}

func ErrCritical(err error, a ...interface{}) {
	std.ErrCritical(err, a...)
}

func ErrPanic(err error, a ...interface{}) {
	std.ErrPanic(err, a...)
}

func ErrFatal(err error, a ...interface{}) {
	std.ErrFatal(err, a...)
}

func Debug(a ...interface{}) {
	std.Debug(a...)
}

func Info(a ...interface{}) {
	std.Info(a...)
}

func Notice(a ...interface{}) {
	std.Notice(a...)
}

func Warning(a ...interface{}) {
	std.Warning(a...)
}

func Error(a ...interface{}) {
	std.Error(a...)
}

func Critical(a ...interface{}) {
	std.Critical(a...)
}

func Panic(a ...interface{}) {
	std.Panic(a...)
}

func Fatal(a ...interface{}) {
	std.Fatal(a...)
}

func Debugf(format string, a ...interface{}) {
	std.Debugf(format, a...)
}

func Infof(format string, a ...interface{}) {
	std.Infof(format, a...)
}

func Noticef(format string, a ...interface{}) {
	std.Noticef(format, a...)
}

func Warningf(format string, a ...interface{}) {
	std.Warningf(format, a...)
}

func Errorf(format string, a ...interface{}) {
	std.Errorf(format, a...)
}

func Criticalf(format string, a ...interface{}) {
	std.Criticalf(format, a...)
}

func Panicf(format string, a ...interface{}) {
	std.Panicf(format, a...)
}

func Fatalf(format string, a ...interface{}) {
	std.Fatalf(format, a...)
}

func Debugln(a ...interface{}) {
	std.Debugln(a...)
}

func Infoln(a ...interface{}) {
	std.Infoln(a...)
}

func Noticeln(a ...interface{}) {
	std.Noticeln(a...)
}

func Warningln(a ...interface{}) {
	std.Warningln(a...)
}

func Errorln(a ...interface{}) {
	std.Errorln(a...)
}

func Criticalln(a ...interface{}) {
	std.Criticalln(a...)
}

func Panicln(a ...interface{}) {
	std.Panicln(a...)
}

func Fatalln(a ...interface{}) {
	std.Fatalln(a...)
}
