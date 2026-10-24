package main

import (
	"testing"
)

func TestParseQuickEditOutput(t *testing.T) {
	// 1. Tagged replacement
	out1 := "Here is the code:\n<replacement>\nconst x = 42;\n</replacement>\nDone."
	parsed, err := parseQuickEditOutput(out1)
	if err != nil || parsed != "const x = 42;" {
		t.Fatalf("unexpected parse result: %q, err: %v", parsed, err)
	}

	// 2. Fallback fenced block
	out2 := "Sure, try this:\n```ts\nlet y = 100;\n```"
	parsed, err = parseQuickEditOutput(out2)
	if err != nil || parsed != "let y = 100;" {
		t.Fatalf("unexpected fenced result: %q, err: %v", parsed, err)
	}

	// 3. Multiple tags (chooses last one)
	out3 := "<replacement>old example</replacement>\n<replacement>final code</replacement>"
	parsed, err = parseQuickEditOutput(out3)
	if err != nil || parsed != "final code" {
		t.Fatalf("expected last tag: %q, err: %v", parsed, err)
	}
	// 4. Indentation of the first line is code, not padding
	out5 := "<replacement>\n\n    if x {\n        y()\n    }\n\n</replacement>"
	parsed, err = parseQuickEditOutput(out5)
	if err != nil || parsed != "    if x {\n        y()\n    }" {
		t.Fatalf("indentation lost: %q, err: %v", parsed, err)
	}

	// 5. No tags or code blocks -> error
	out4 := "I don't know how to do that."
	_, err = parseQuickEditOutput(out4)
	if err == nil {
		t.Fatal("expected error on output with no code or tags")
	}
}
