package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger struct {
	*zap.Logger
}

type Config struct {
	LogLevel    string
	DevMode     bool
	ServiceName string
}

func NewLogger(cfg Config) (*Logger, error) {
	var zapCfg zap.Config
	if cfg.DevMode {
		zapCfg = zap.NewDevelopmentConfig()
	} else {
		zapCfg = zap.NewProductionConfig()
		// Use console encoder for human-readable output instead of JSON
		zapCfg.Encoding = "console"
		zapCfg.EncoderConfig.TimeKey = "" // Remove timestamp
		zapCfg.EncoderConfig.LevelKey = "level"
		zapCfg.EncoderConfig.NameKey = ""       // Remove logger name
		zapCfg.EncoderConfig.CallerKey = ""     // Remove caller info
		zapCfg.EncoderConfig.StacktraceKey = "" // Remove stacktrace
		zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	level, err := zapcore.ParseLevel(cfg.LogLevel)
	if err != nil {
		return nil, err
	}
	zapCfg.Level = zap.NewAtomicLevelAt(level)

	logger, err := zapCfg.Build()
	if err != nil {
		return nil, err
	}

	return &Logger{Logger: logger}, nil
}

func NewTestLogger() *Logger {
	zapLogger := zap.NewNop()
	return &Logger{
		Logger: zapLogger,
	}
}

func (l *Logger) Info(msg string, fields ...zap.Field) {
	l.Logger.Info(msg, fields...)
}

func (l *Logger) Error(msg string, fields ...zap.Field) {
	l.Logger.Error(msg, fields...)
}

func (l *Logger) Fatal(msg string, fields ...zap.Field) {
	l.Logger.Fatal(msg, fields...)
}

func (l *Logger) Warn(msg string, fields ...zap.Field) {
	l.Logger.Warn(msg, fields...)
}

func (l *Logger) Debug(msg string, fields ...zap.Field) {
	l.Logger.Debug(msg, fields...)
}
