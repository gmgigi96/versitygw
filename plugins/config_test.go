package plugins_test

import (
	"reflect"
	"testing"

	"github.com/versity/versitygw/plugins"
)

type TestConfig struct {
	Int       int
	Bool      bool
	String    string
	IntTag    int    `config:"int_tag"`
	BoolTag   bool   `config:"bool_tag"`
	StringTag string `config:"string_tag"`
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		input map[string]string
		out   TestConfig
		err   bool // if an error is expected
	}{
		{
			input: map[string]string{},
			out:   TestConfig{},
		},
		{
			input: map[string]string{
				"Int":    "1",
				"Bool":   "true",
				"String": "test",
			},
			out: TestConfig{
				Int:    1,
				Bool:   true,
				String: "test",
			},
		},
		{
			input: map[string]string{
				"int_tag":    "1",
				"bool_tag":   "true",
				"string_tag": "test",
			},
			out: TestConfig{
				IntTag:    1,
				BoolTag:   true,
				StringTag: "test",
			},
		},
		{
			input: map[string]string{
				"int":    "1",
				"bool":   "true",
				"string": "test",
			},
			out: TestConfig{},
		},
	}

	for _, test := range tests {
		var got TestConfig
		err := plugins.ParseConfig(test.input, &got)
		if err != nil && !test.err {
			t.Fatalf("got unexpected err %v", err)
		}
		if err == nil && test.err {
			t.Fatal("an error was expected but got nil err")
		}

		if !reflect.DeepEqual(got, test.out) {
			t.Fatalf("parsed config does not match with expected. got=%+v expected=%+v", got, test.out)
		}
	}
}
