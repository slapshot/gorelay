package strictjson

import (
	"strings"
	"testing"
)

func TestObject(t *testing.T) {
	tests := []struct {
		name string
		input string
		ok bool
	}{
		{"simple", `{"a":1,"nested":{"x":[true,null,2]}}`, true},
		{"duplicate top-level", `{"a":1,"a":2}`, false},
		{"duplicate nested", `{"a":{"x":1,"x":2}}`, false},
		{"array nested duplicate", `{"a":[{"x":1,"x":2}]}`, false},
		{"trailing token", `{"a":1} {"b":2}`, false},
		{"root array", `[1,2,3]`, false},
		{"invalid UTF-8", string([]byte{0xff}), false},
		{"incomplete", `{"a":`, false},
		{"too deep", strings.Repeat(`{"a":`, 40) + "1" + strings.Repeat("}", 40), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Object([]byte(tt.input))
			if (err == nil) != tt.ok {
				t.Fatalf("Object(%q) error = %v; want success=%t", tt.input, err, tt.ok)
			}
		})
	}
}
