package strictjson

import "testing"

func TestDecodeRejectsUnknownDuplicateAndMultipleValues(t *testing.T) {
	type payload struct {
		ID     string `json:"id"`
		Nested struct {
			Value int `json:"value"`
		} `json:"nested"`
	}
	valid := []byte(`{"id":"one","nested":{"value":1}}`)
	var decoded payload
	if err := Decode(valid, &decoded); err != nil || decoded.ID != "one" {
		t.Fatalf("valid object was rejected: %+v err=%v", decoded, err)
	}
	for _, data := range [][]byte{
		[]byte(`{"id":"one","unknown":true}`),
		[]byte(`{"id":"one","id":"two"}`),
		[]byte(`{"id":"one","nested":{"value":1,"value":2}}`),
		[]byte(`{"id":"one"}{"id":"two"}`),
	} {
		if err := Decode(data, &payload{}); err == nil {
			t.Fatalf("invalid JSON was accepted: %s", data)
		}
	}
}
