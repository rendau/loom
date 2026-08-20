package model

// Источники строк лога (согласованы с server_v1.TaskLogSource).
const (
	SourceLog    = "log"
	SourceStdout = "stdout"
	SourceStderr = "stderr"
	SourceServer = "server" // строки самого control plane (причина смерти пода)
)

// Entry — строка лога попытки таска.
type Entry struct {
	TsUnixMs int64
	Source   string
	Line     string
}

// AttemptKey — идентификация попытки, которой принадлежит лог.
type AttemptKey struct {
	RunId   string
	Task    string
	Attempt int32
}
