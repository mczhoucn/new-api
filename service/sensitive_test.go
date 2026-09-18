package service

import (
	"reflect"
	"testing"
)

func TestSensitiveWordContainsWithWords(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		words     []string
		wantMatch bool
	}{
		{name: "empty words", text: "hello blocked", wantMatch: false},
		{name: "empty text", text: "", words: []string{"blocked"}, wantMatch: false},
		{name: "case insensitive and trimmed", text: "this contains BLOCKED content", words: []string{"  blocked  "}, wantMatch: true},
		{name: "blank entries ignored", text: "this contains blocked content", words: []string{"", "  ", "blocked"}, wantMatch: true},
		{name: "blank words only", text: "this contains blocked content", words: []string{"", "\t"}, wantMatch: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMatch, gotWords := SensitiveWordContainsWithWords(tt.text, tt.words)
			if gotMatch != tt.wantMatch {
				t.Fatalf("SensitiveWordContainsWithWords() match = %v, want %v, words = %v", gotMatch, tt.wantMatch, gotWords)
			}
			if tt.wantMatch && len(gotWords) == 0 {
				t.Fatal("SensitiveWordContainsWithWords() returned no matched words")
			}
		})
	}
}

func TestSensitiveWordContainsWithWordsDoesNotMutateInput(t *testing.T) {
	words := []string{" blocked ", "blocked", "other"}
	before := append([]string(nil), words...)

	SensitiveWordContainsWithWords("blocked", words)

	if !reflect.DeepEqual(words, before) {
		t.Fatalf("word list was mutated: got %#v, want %#v", words, before)
	}
}
