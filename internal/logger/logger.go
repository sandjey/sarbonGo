package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ANSI colors for console
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorBold    = "\033[1m"
	colorDim     = "\033[2m"
)

// colorLevelEncoder encodes level with colors: Error=red, Warn=yellow, Info=green, Debug=cyan.
func colorLevelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	var s string
	switch l {
	case zapcore.DebugLevel:
		s = colorCyan + "DEBUG" + colorReset
	case zapcore.InfoLevel:
		s = colorGreen + "INFO " + colorReset
	case zapcore.WarnLevel:
		s = colorYellow + "WARN " + colorReset
	case zapcore.ErrorLevel, zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		s = colorRed + "ERROR" + colorReset
	default:
		s = l.CapitalString()
	}
	enc.AppendString(s)
}

// NewDevelopment returns a zap.Logger with colored console output for local dev.
func NewDevelopment() *zap.Logger {
	cfg := zap.NewDevelopmentEncoderConfig()
	cfg.EncodeLevel = colorLevelEncoder
	enc := zapcore.NewConsoleEncoder(cfg)
	core := zapcore.NewCore(enc, zapcore.AddSync(os.Stderr), zapcore.DebugLevel)
	return zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
}

// NewDevelopmentWithHub is like NewDevelopment but ALSO tees every log entry
// into the given LogHub, so the /terminal live view gets a verbatim copy of
// what's on stderr (with sensitive data masked by the hub before broadcast).
func NewDevelopmentWithHub(hub *LogHub) *zap.Logger {
	if hub == nil {
		return NewDevelopment()
	}
	cfg := zap.NewDevelopmentEncoderConfig()
	cfg.EncodeLevel = colorLevelEncoder
	enc := zapcore.NewConsoleEncoder(cfg)
	consoleCore := zapcore.NewCore(enc, zapcore.AddSync(os.Stderr), zapcore.DebugLevel)
	// Hub gets a plain (color-less) copy — raw ANSI escape codes render as
	// garbage in the browser.
	plainCfg := zap.NewDevelopmentEncoderConfig()
	plainEnc := zapcore.NewConsoleEncoder(plainCfg)
	hubCore := zapcore.NewCore(plainEnc, zapcore.AddSync(hub), zapcore.DebugLevel)
	return zap.New(zapcore.NewTee(consoleCore, hubCore), zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
}

// NewProductionWithHub wraps zap's production JSON logger so every entry ALSO
// lands in the LogHub. Stdout still gets the real JSON — the hub gets a
// human-friendly console copy.
func NewProductionWithHub(hub *LogHub) (*zap.Logger, error) {
	if hub == nil {
		l, err := zap.NewProduction()
		return l, err
	}
	// stdout: JSON (same as zap.NewProduction).
	jsonCfg := zap.NewProductionEncoderConfig()
	jsonEnc := zapcore.NewJSONEncoder(jsonCfg)
	stdoutCore := zapcore.NewCore(jsonEnc, zapcore.AddSync(os.Stdout), zapcore.InfoLevel)
	// hub: plain text (easier to eyeball in /terminal).
	plainCfg := zap.NewDevelopmentEncoderConfig()
	plainEnc := zapcore.NewConsoleEncoder(plainCfg)
	hubCore := zapcore.NewCore(plainEnc, zapcore.AddSync(hub), zapcore.InfoLevel)
	return zap.New(zapcore.NewTee(stdoutCore, hubCore), zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)), nil
}
