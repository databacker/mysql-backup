package log

type LogEntry struct {
	Run       string                 `json:"run"`
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Fields    map[string]interface{} `json:"fields"`
	Message   string                 `json:"message"`
}
