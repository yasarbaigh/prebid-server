package logger

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

type ServiceLogger struct {
	writer *lumberjack.Logger
	level  string
}

func (l *ServiceLogger) Debugf(msg string, args ...any) {
	if l.level == "DEBUG" {
		l.write("DEBUG", msg, args...)
	}
}

func (l *ServiceLogger) Infof(msg string, args ...any) {
	if l.level == "DEBUG" || l.level == "INFO" {
		l.write("INFO", msg, args...)
	}
}

func (l *ServiceLogger) Warnf(msg string, args ...any) {
	if l.level == "DEBUG" || l.level == "INFO" || l.level == "WARN" || l.level == "WARNING" {
		l.write("WARN", msg, args...)
	}
}

func (l *ServiceLogger) Errorf(msg string, args ...any) {
	l.write("ERROR", msg, args...)
}

func (l *ServiceLogger) Fatalf(msg string, args ...any) {
	l.write("FATAL", msg, args...)
	os.Exit(1)
}

func (l *ServiceLogger) write(level, msg string, args ...any) {
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, fmt.Sprintf(msg, args...))
	l.writer.Write([]byte(line))
}

func NewServiceLogger(path string, maxSize, maxBackups, maxAge int, compress bool, level string) Logger {
	return &ServiceLogger{
		writer: &lumberjack.Logger{
			Filename:   path,
			MaxSize:    maxSize,
			MaxBackups: maxBackups,
			MaxAge:     maxAge,
			Compress:   compress,
			LocalTime:  true,
		},
		level: level,
	}
}
