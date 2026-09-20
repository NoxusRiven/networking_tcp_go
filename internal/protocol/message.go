package protocol

import (
	"bufio"
	"encoding/json"
	//"fmt"
	//"strings"
)

/*
Jeśli chcesz, mogę też pokazać jedną zmianę w Twoim Message struct, która sprawi że Twój własny protokół TCP będzie ~5-10× szybszy niż JSON i jednocześnie łatwiejszy do parsowania (to jest bardzo przydatne w systemach typu controller/agent).
*/

type MessageType string

const (
	// client
	PING     MessageType = "PING"
	IDLE     MessageType = "IDLE"
	EXIT     MessageType = "EXIT"
	UPLOAD   MessageType = "FILE_UPLOAD"
	DOWNLOAD MessageType = "FILE_DOWNLOAD"

	// setup
	REG_API   MessageType = "REGISTER_API"
	REG_AGENT MessageType = "REGISTER_AGENT"
	REG_LB    MessageType = "REGISTER_LOADBALANCER"

	// system
	CREATE  MessageType = "CREATE"
	DESTROY MessageType = "DESTROY"
	RESET   MessageType = "RESET"
	//update can be used for many things based on context of node using it and content of message
	UPDATE    MessageType = "UPDATE"
	RAPORT    MessageType = "RAPORT"
	HEARTBEAT MessageType = "HEARTBEAT"

	//? might not be needed, check this out
	LB_SYNC MessageType = "SYNCRONIZE_LOADBALANCERS"

	UNKNOWN MessageType = "UNKNOWN"
)

type CodeType int

const (
	INFO    CodeType = 100
	SUCCESS CodeType = 200
	ERROR   CodeType = 400
)

// TODO: add connection type (stream, non-stream itp) and fix shutting of channels
type Message struct {
	ID           string      `json:"ID,omitempty"`
	SessionID    string      `json:"sessionID,omitempty"`
	ConnectionID string      `json:"connectionID,omitempty"`
	Type         MessageType `json:"type"`
	IsStream     bool        `json:"isStream,omitempty"`
	Code         CodeType    `json:"code,omitempty"`
	Content      any         `json:"content,omitempty"`
}

// func (m Message) String() string {

// 	parts := []string{
// 		fmt.Sprintf("ID=%s", m.ID),
// 		fmt.Sprintf("Type=%v", m.Type),
// 	}

// 	if m.Code != 0 {
// 		parts = append(parts, fmt.Sprintf("Code=%v", m.Code))
// 	}

// 	if m.Content != "" {
// 		parts = append(parts, fmt.Sprintf("Content=%v", m.Content))
// 	}

// 	return strings.Join(parts, " | ")
// }

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

func DecodeContent(content any, out any) error {
	raw, err := json.Marshal(content)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
