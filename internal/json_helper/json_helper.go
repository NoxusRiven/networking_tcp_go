package json_helper

import "encoding/json"

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
