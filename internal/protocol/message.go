package protocol

const (
	PING string = "PING"
	EXIT string = "EXIT"

	CREATE  string = "CREATE"
	DESTROY string = "DESTROY"
	RESET   string = "RESET"
	RAPORT  string = "RAPORT"

	UNKNOWN string = "UNKNOWN"
)

const (
	INFO    int = 100
	SUCCESS int = 200
	ERROR   int = 400
)

type Message struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Code    int    `json:"code,omitempty"`
	Content string `json:"content,omitempty"`
}
