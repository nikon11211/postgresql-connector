package pgconnector

type Logger interface {
	Debug(msg string)
	Info(msg string)
	Warn(msg string)
	Error(msg string)
}

type NoopLogger struct{}

func (NoopLogger) Debug(msg string) { _ = msg }
func (NoopLogger) Info(msg string)  { _ = msg }
func (NoopLogger) Warn(msg string)  { _ = msg }
func (NoopLogger) Error(msg string) { _ = msg }
