package protocol

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
)

/*
Jeśli chcesz, mogę też pokazać jedną zmianę w Twoim Message struct, która sprawi że Twój własny protokół TCP będzie ~5-10× szybszy niż JSON i jednocześnie łatwiejszy do parsowania (to jest bardzo przydatne w systemach typu controller/agent).
*/

type MessageType string

const (
	// client
	PING     MessageType = "PING"
	EXIT     MessageType = "EXIT"
	UPLOAD   MessageType = "FILE_UPLOAD"
	DOWNLOAD MessageType = "FILE_DOWNLOAD"

	// setup
	REG_API   MessageType = "REGISTER_API"
	REG_AGENT MessageType = "REGISTER_AGENT"
	REG_LB    MessageType = "REGISTER_LOADBALANCER"

	// agent/loadbalancer
	CREATE    MessageType = "CREATE"
	DESTROY   MessageType = "DESTROY"
	RESET     MessageType = "RESET"
	RAPORT    MessageType = "RAPORT"
	HEARTBEAT MessageType = "HEARTBEAT"

	UNKNOWN MessageType = "UNKNOWN"
)

type CodeType int

const (
	INFO    CodeType = 100
	SUCCESS CodeType = 200
	ERROR   CodeType = 400
)

type Message struct {
	SessionID    string      `json:"sessionID,omitempty"`
	ConnectionID string      `json:"connectionID,omitempty"`
	Type         MessageType `json:"type"`
	Code         CodeType    `json:"code,omitempty"`
	Content      string      `json:"content,omitempty"`
}

func (m Message) String() string {

	parts := []string{
		fmt.Sprintf("ID=%s", m.SessionID),
		fmt.Sprintf("Type=%v", m.Type),
	}

	if m.Code != 0 {
		parts = append(parts, fmt.Sprintf("Code=%v", m.Code))
	}

	if m.Content != "" {
		parts = append(parts, fmt.Sprintf("Content=%s", m.Content))
	}

	return strings.Join(parts, " | ")
}

func Send(writer *bufio.Writer, msg Message) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	raw = append(raw, '\n')

	_, err = writer.Write(raw)
	if err != nil {
		return err
	}

	return writer.Flush()
}

func Receive(reader *bufio.Reader) (Message, error) {
	raw, err := reader.ReadBytes('\n')
	if err != nil {
		return Message{}, err
	}

	var msg Message
	err = json.Unmarshal(raw, &msg)

	return msg, err
}
