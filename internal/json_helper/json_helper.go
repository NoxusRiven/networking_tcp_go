package json_helper

import "encoding/json"

//!TODO: DecodeContent and both EncodeContent are depricated clean code and get rid of those functions
func DecodeContent(content any, out any) error {
	switch v := content.(type) {
	case string:
		return json.Unmarshal([]byte(v), out)

	case []byte:
		return json.Unmarshal(v, out)

	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}

		return json.Unmarshal(raw, out)
	}
}

func EncodeContentStr(content any) (string, error) {
	bytes, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	str := string(bytes)

	return str, nil
}

func EncodeContentByte(content any) (any, error) {
	return json.Marshal(content)
}

// JSON string -> Any object
func FromJSON[T any](jsonStr string) (T, error) {
	var obj T
	err := json.Unmarshal([]byte(jsonStr), &obj)
	return obj, err
}

// Any object -> JSON string
func ToJSON(obj any) (string, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
